package bot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	discocache "github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
	"github.com/thomas-vilte/dave-go/session"

	"github.com/blueshapes/gomusic/internal/audio"
	"github.com/blueshapes/gomusic/internal/config"
	"github.com/blueshapes/gomusic/internal/player"
)

// Bot owns the disgo client and every guild's voice connection.
type Bot struct {
	cfg     config.Config
	Client  *bot.Client
	Log     *slog.Logger
	YtDlp   *audio.YtDlp
	Cache   *audio.Cache
	Players *player.Manager
}

func New(cfg config.Config, log *slog.Logger) (*Bot, error) {
	yt := &audio.YtDlp{
		Bin:         cfg.YtDlp.Bin,
		Cookies:     cfg.YtDlp.Cookies,
		ExtraArgs:   cfg.YtDlp.ExtraArgs,
		MaxPlaylist: cfg.YtDlp.MaxPlaylist,
		Log:         log,
	}
	audioCache, err := audio.NewCache(cfg.Audio.CacheDir, cfg.Audio.Ffmpeg, cfg.Audio.MaxCacheMB, cfg.Audio.Concurrency, yt, log)
	if err != nil {
		return nil, err
	}

	client, err := disgo.New(cfg.Discord.Token,
		bot.WithLogger(log),
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentGuilds|gateway.IntentGuildVoiceStates),
		),
		// disgo caches nothing by default (CacheFlags is zero), so voice states
		// and members have to be enabled explicitly.
		bot.WithCacheConfigOpts(
			discocache.WithCaches(discocache.FlagVoiceStates, discocache.FlagMembers),
		),
		bot.WithVoiceManagerConfigOpts(
			voice.WithLogger(log),
			// DAVE (E2EE) is required to join voice channels since 2026.
			voice.WithDaveSessionCreateFunc(session.CreateFunc()),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	b := &Bot{cfg: cfg, Client: client, Log: log, YtDlp: yt, Cache: audioCache}
	b.Players = player.NewManager(b, audioCache, log)
	return b, nil
}

func (b *Bot) GuildIDs() []snowflake.ID {
	ids := make([]snowflake.ID, 0, len(b.cfg.Discord.GuildIDs))
	for _, id := range b.cfg.Discord.GuildIDs {
		ids = append(ids, snowflake.ID(id))
	}
	return ids
}

// SelfID is the bot's own user id.
func (b *Bot) SelfID() snowflake.ID {
	if self, ok := b.Client.Caches.SelfUser(); ok {
		return self.ID
	}
	return b.Client.ApplicationID
}

func (b *Bot) Open(ctx context.Context) error {
	return b.Client.OpenGateway(ctx)
}

func (b *Bot) Close(ctx context.Context) {
	if b.Client != nil {
		b.Client.Close(ctx)
	}
}

// Join connects to channelID and keeps the connection open with silence until a
// player takes over.
func (b *Bot) Join(ctx context.Context, guildID, channelID snowflake.ID) (voice.Conn, error) {
	conn := b.Client.VoiceManager.CreateConn(guildID)
	if cur := conn.ChannelID(); cur != nil && *cur == channelID {
		return conn, nil
	}

	start := time.Now()
	openCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	err := conn.Open(openCtx, channelID, false, false)
	if cur := conn.ChannelID(); cur != nil {
		// connected anyway (a slow handshake can outlive the context)
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("open voice conn: %w", err)
	}
	b.Log.Info("voice connection established", "guild_id", guildID, "channel_id", channelID, "took", time.Since(start))
	return conn, nil
}

// Leave disconnects from the guild's voice channel, if connected.
func (b *Bot) Leave(ctx context.Context, guildID snowflake.ID) {
	conn := b.Client.VoiceManager.GetConn(guildID)
	if conn == nil {
		return
	}
	conn.Close(ctx)
	b.Client.VoiceManager.RemoveConn(guildID)
}

// OnEvent keeps the voice manager honest: disconnect when Discord drops us, and
// leave when the last human walks out of the channel.
func (b *Bot) OnEvent(event bot.Event) {
	switch e := event.(type) {
	case *events.GuildVoiceStateUpdate:
		if e.VoiceState.UserID == b.SelfID() && e.VoiceState.ChannelID == nil {
			b.cleanupVoice(e.VoiceState.GuildID)
		}
	case *events.GuildVoiceLeave:
		b.leaveIfEmpty(e.VoiceState.GuildID, e.OldVoiceState.ChannelID)
	}
}

func (b *Bot) cleanupVoice(guildID snowflake.ID) {
	if conn := b.Client.VoiceManager.GetConn(guildID); conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		conn.Close(ctx)
		cancel()
		b.Client.VoiceManager.RemoveConn(guildID)
	}
	b.Players.Remove(guildID)
}

// leaveIfEmpty disconnects when no human is left in the bot's channel (the
// Kotlin bot did the same).
func (b *Bot) leaveIfEmpty(guildID snowflake.ID, oldChannelID *snowflake.ID) {
	conn := b.Client.VoiceManager.GetConn(guildID)
	if conn == nil || conn.ChannelID() == nil {
		return
	}
	channelID := *conn.ChannelID()
	if oldChannelID == nil || *oldChannelID != channelID {
		return // they left a channel we are not in
	}
	if b.humansIn(guildID, channelID) > 0 {
		return
	}

	b.Log.Info("last human left the voice channel, disconnecting", "guild_id", guildID, "channel_id", channelID)
	b.Players.Remove(guildID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b.Leave(ctx, guildID)
}

// humansIn counts non-bot users in a voice channel.
func (b *Bot) humansIn(guildID, channelID snowflake.ID) int {
	self := b.SelfID()
	humans := 0
	for vs := range b.Client.Caches.VoiceStates(guildID) {
		if vs.ChannelID == nil || *vs.ChannelID != channelID || vs.UserID == self {
			continue
		}
		if m, ok := b.Client.Caches.Member(guildID, vs.UserID); ok && m.User.Bot {
			continue
		}
		humans++
	}
	return humans
}
