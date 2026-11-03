package command

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/handler/middleware"
	"github.com/disgoorg/snowflake/v2"

	"github.com/blueshapes/gomusic/internal/audio"
	"github.com/blueshapes/gomusic/internal/bot"
	"github.com/blueshapes/gomusic/internal/player"
	"github.com/blueshapes/gomusic/internal/util"
)

const queuePageSize = 10

// cmd is one slash command plus its aliases (the Kotlin bot registered aliases
// as extra commands with the same handler).
type cmd struct {
	name        string
	description string
	aliases     []string
	options     []discord.ApplicationCommandOption
	handler     func(b *bot.Bot) handler.SlashCommandHandler
}

// registry returns the command table. It is a function rather than a package
// variable so a handler can list the table (help) without an init cycle.
func registry() []cmd {
	return []cmd{
		{
			name:        "play",
			description: "曲を再生します",
			aliases:     []string{"p"},
			options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{Name: "query", Description: "URL または検索語", Required: true},
			},
			handler: handlePlay,
		},
		{
			name:        "pause",
			description: "再生を一時停止(または再開)します",
			handler:     handlePause,
		},
		{
			name:        "skip",
			description: "現在の曲をスキップします",
			handler:     handleSkip,
		},
		{
			name:        "nowplaying",
			description: "現在再生中の曲の情報を表示します",
			aliases:     []string{"np"},
			handler:     handleNowPlaying,
		},
		{
			name:        "queue",
			description: "キューを表示します",
			aliases:     []string{"q"},
			options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{Name: "page", Description: "ページ番号", MinValue: ptr(1)},
			},
			handler: handleQueue,
		},
		{
			name:        "shuffle",
			description: "キューをシャッフルします",
			handler:     handleShuffle,
		},
		{
			name:        "clear",
			description: "キューを空にします",
			handler:     handleClear,
		},
		{
			name:        "loop",
			description: "ループの設定をします",
			options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "type",
					Description: "ループの種類",
					Required:    true,
					Choices: []discord.ApplicationCommandOptionChoiceString{
						{Name: "off", Value: "off"},
						{Name: "queue", Value: "queue"},
					},
				},
			},
			handler: handleLoop,
		},
		{
			name:        "seek",
			description: "再生位置を移動します",
			options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{Name: "position", Description: "移動先 (例: 1:30)", Required: true},
			},
			handler: handleSeek,
		},
		{
			name:        "volume",
			description: "音量を変更します",
			options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{Name: "percent", Description: "音量 (%) 0-200", Required: true, MinValue: ptr(0), MaxValue: ptr(200)},
			},
			handler: handleVolume,
		},
		{
			name:        "join",
			description: "VCに接続します",
			handler:     handleJoin,
		},
		{
			name:        "leave",
			description: "VCから切断します",
			aliases:     []string{"disconnect", "dc", "kick", "stop", "quit"},
			handler:     handleLeave,
		},
		{
			name:        "help",
			description: "ヘルプを表示します",
			handler:     handleHelp,
		},
		{
			name:        "ping",
			description: "疎通確認をします",
			handler: func(*bot.Bot) handler.SlashCommandHandler {
				return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
					return e.CreateMessage(discord.MessageCreate{Content: "pong"})
				}
			},
		},
	}
}

// Definitions expands the registry (including aliases) into slash commands.
func Definitions() []discord.ApplicationCommandCreate {
	var defs []discord.ApplicationCommandCreate
	for _, c := range registry() {
		for _, name := range c.allNames() {
			defs = append(defs, discord.SlashCommandCreate{
				Name:        name,
				Description: c.description,
				Options:     c.options,
			})
		}
	}
	return defs
}

func (c cmd) allNames() []string { return append([]string{c.name}, c.aliases...) }

// Register wires the routes and returns the listener to hand to the bot.
func Register(b *bot.Bot) *handler.Mux {
	r := handler.New()
	// disgo dispatches gateway events synchronously: without this, a handler
	// that waits on the voice connection (conn.Open waits for VOICE_STATE_UPDATE
	// / VOICE_SERVER_UPDATE) would block the very events it is waiting for.
	r.Use(middleware.Go)

	for _, c := range registry() {
		h := c.handler(b)
		for _, name := range c.allNames() {
			r.SlashCommand("/"+name, h)
		}
	}
	r.Component("/play/{token}", handlePlaySelect(b))
	return r
}

func handlePlay(b *bot.Bot) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		guildID := e.GuildID()
		if guildID == nil {
			return reply(e, "DMでは使えません。")
		}
		// resolving + the first download take seconds, so ack immediately
		if err := e.DeferCreateMessage(true); err != nil {
			return err
		}

		channelID, ok := userVoiceChannel(b, e, *guildID)
		if !ok {
			return followup(e, "先にVCに接続してください。")
		}
		p := b.Players.GetOrCreate(*guildID)
		if cur := currentChannel(b, *guildID); cur != 0 && cur != channelID {
			return followup(e, "ボットの接続しているVCに接続していないようです。")
		}

		query := data.String("query")
		ctx, cancel := context.WithTimeout(e.Ctx, 60*time.Second)
		defer cancel()

		var (
			tracks []audio.Track
			err    error
		)
		if audio.IsURL(query) {
			tracks, err = b.YtDlp.Resolve(ctx, query)
		} else {
			// text queries show the hits instead of guessing the first one
			tracks, err = b.YtDlp.Search(ctx, query, searchResults)
			if err == nil {
				return sendSearchMenu(e, channelID, tracks)
			}
		}
		if err != nil {
			b.Log.Error("resolve failed", "query", query, "err", err)
			return followup(e, "曲の取得に失敗しました。理由: "+err.Error())
		}

		p.Enqueue(channelID, tracks...)
		if len(tracks) == 1 {
			return followup(e, fmt.Sprintf("`%s` をキューに追加しました。", tracks[0].Title))
		}
		return followup(e, fmt.Sprintf("%d曲をキューに追加しました。", len(tracks)))
	}
}

func handlePause(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		if _, ok := p.Current(); !ok {
			return reply(e, "何も再生していません。")
		}
		if p.TogglePause() {
			return e.CreateMessage(discord.MessageCreate{Content: "一時停止しました。"})
		}
		return e.CreateMessage(discord.MessageCreate{Content: "再生を再開します。"})
	}
}

func handleSkip(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		if _, ok := p.Current(); !ok {
			return reply(e, "何も再生していません。")
		}
		p.Skip()
		return e.CreateMessage(discord.MessageCreate{Content: "スキップしました。"})
	}
}

func handleNowPlaying(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		track, ok := p.Current()
		if !ok {
			return reply(e, "何も再生していません。")
		}
		if p.Paused() {
			track.Title += " (一時停止中)"
		}

		pos := p.Position()
		embed := discord.Embed{
			Title:       "再生中の曲",
			Description: "```" + track.Title + "```",
			Fields: []discord.EmbedField{
				{
					Name: "再生時間",
					Value: fmt.Sprintf("%s\n%s / %s",
						util.ProgressBar(pos, track.Duration, 20),
						util.HMS(pos), util.HMS(track.Duration)),
				},
				{Name: "アップロード者", Value: track.Uploader},
			},
		}
		if next, ok := p.Next(); ok {
			embed.Fields = append(embed.Fields, discord.EmbedField{Name: "次の曲", Value: next.Title})
		}
		return e.CreateMessage(discord.MessageCreate{Embeds: []discord.Embed{embed}})
	}
}

func handleQueue(b *bot.Bot) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		items := p.Queue()
		if len(items) == 0 {
			return reply(e, "キューは空です。")
		}

		page, _ := data.OptInt("page")
		if page < 1 {
			page = 1
		}
		pages := (len(items) + queuePageSize - 1) / queuePageSize
		if page > pages {
			return reply(e, "ページ数が範囲外です。")
		}

		start := (page - 1) * queuePageSize
		end := min(start+queuePageSize, len(items))
		lines := make([]string, 0, end-start)
		for i, t := range items[start:end] {
			lines = append(lines, fmt.Sprintf("%d: %.80s", start+i+1, t.Title))
		}

		return e.CreateMessage(discord.MessageCreate{Embeds: []discord.Embed{{
			Title:       fmt.Sprintf("キューリスト (%d ページ目)", page),
			Description: strings.Join(lines, "\n"),
		}}})
	}
}

func handleShuffle(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		p.Shuffle()
		return e.CreateMessage(discord.MessageCreate{Content: "キューをシャッフルしました。"})
	}
}

func handleClear(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		p.Clear()
		return e.CreateMessage(discord.MessageCreate{Content: "キューを空にしました。"})
	}
}

func handleLoop(b *bot.Bot) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		switch data.String("type") {
		case "off":
			p.SetLoop(player.LoopOff)
			return e.CreateMessage(discord.MessageCreate{Content: "ループを無効にしました。"})
		case "queue":
			p.SetLoop(player.LoopQueue)
			return e.CreateMessage(discord.MessageCreate{Content: "キューのループを有効にしました。"})
		default:
			return reply(e, "ループの種類が不正です。")
		}
	}
}

func handleSeek(b *bot.Bot) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		track, ok := p.Current()
		if !ok {
			return reply(e, "何も再生していません。")
		}

		pos, err := util.ParsePosition(data.String("position"))
		if err != nil {
			return reply(e, "位置の指定が不正です（例: 1:30）")
		}
		if track.Duration > 0 && pos >= track.Duration {
			return reply(e, fmt.Sprintf("曲の長さ (%s) を超えています。", util.HMS(track.Duration)))
		}
		if err := p.Seek(pos); err != nil {
			b.Log.Error("seek failed", "err", err)
			return reply(e, "シークに失敗しました。")
		}
		return e.CreateMessage(discord.MessageCreate{Content: fmt.Sprintf("%s に移動しました。", util.HMS(pos))})
	}
}

func handleVolume(b *bot.Bot) handler.SlashCommandHandler {
	return func(data discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		p, err := activePlayer(b, e)
		if err != nil {
			return err
		}
		p.SetVolume(data.Int("percent"))
		return e.CreateMessage(discord.MessageCreate{Content: fmt.Sprintf("音量を %d%% にしました。", p.Volume())})
	}
}

func handleJoin(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		guildID := e.GuildID()
		if guildID == nil {
			return reply(e, "DMでは使えません。")
		}
		// Connecting runs the DAVE handshake and can take seconds, which is
		// longer than the 3s window to acknowledge an interaction. Ack first.
		if err := e.DeferCreateMessage(true); err != nil {
			return err
		}

		channelID, ok := userVoiceChannel(b, e, *guildID)
		if !ok {
			return followup(e, "先にVCに接続してください。")
		}

		ctx, cancel := context.WithTimeout(e.Ctx, 40*time.Second)
		defer cancel()
		if _, err := b.Join(ctx, *guildID, channelID); err != nil {
			b.Log.Error("join failed", "guild_id", guildID, "err", err)
			return followup(e, "接続に失敗しました: "+err.Error())
		}
		return followup(e, fmt.Sprintf("`%s` に接続しました。", channelID))
	}
}

func handleLeave(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		guildID := e.GuildID()
		if guildID == nil {
			return reply(e, "DMでは使えません。")
		}
		if b.Client.VoiceManager.GetConn(*guildID) == nil {
			return reply(e, "接続していません。")
		}
		ctx, cancel := context.WithTimeout(e.Ctx, 10*time.Second)
		defer cancel()
		b.Players.Remove(*guildID)
		b.Leave(ctx, *guildID)
		return e.CreateMessage(discord.MessageCreate{Content: "切断しました。"})
	}
}

func handleHelp(b *bot.Bot) handler.SlashCommandHandler {
	return func(_ discord.SlashCommandInteractionData, e *handler.CommandEvent) error {
		reg := registry()
		lines := make([]string, 0, len(reg))
		for _, c := range reg {
			lines = append(lines, fmt.Sprintf("`/%s`: %s", c.name, c.description))
		}
		return e.CreateMessage(discord.MessageCreate{Embeds: []discord.Embed{{
			Title: "コマンド一覧",
			Fields: []discord.EmbedField{
				{Name: "一般", Value: strings.Join(lines, "\n")},
			},
		}}})
	}
}

// activePlayer is the shared "you are in the bot's VC and it has a queue"
// check used by the playback commands.
func activePlayer(b *bot.Bot, e *handler.CommandEvent) (*player.Player, error) {
	guildID := e.GuildID()
	if guildID == nil {
		return nil, reply(e, "DMでは使えません。")
	}
	channelID, ok := userVoiceChannel(b, e, *guildID)
	if !ok {
		return nil, reply(e, "先にVCに接続してください。")
	}
	p, ok := b.Players.Get(*guildID)
	if !ok || currentChannel(b, *guildID) == 0 {
		return nil, reply(e, "現在、ボットはどのVCにも参加していません。")
	}
	if currentChannel(b, *guildID) != channelID {
		return nil, reply(e, "ボットの接続しているVCに接続していないようです。")
	}
	return p, nil
}

// userVoiceChannel returns the caller's current voice channel.
func userVoiceChannel(b *bot.Bot, e *handler.CommandEvent, guildID snowflake.ID) (snowflake.ID, bool) {
	vs, ok := b.Client.Caches.VoiceState(guildID, e.User().ID)
	if !ok || vs.ChannelID == nil {
		b.Log.Debug("voice state missing from cache", "guild_id", guildID, "user_id", e.User().ID)
		return 0, false
	}
	return *vs.ChannelID, true
}

// currentChannel is the channel the bot's voice connection is in, if any.
func currentChannel(b *bot.Bot, guildID snowflake.ID) snowflake.ID {
	conn := b.Client.VoiceManager.GetConn(guildID)
	if conn == nil {
		return 0
	}
	if ch := conn.ChannelID(); ch != nil {
		return *ch
	}
	return 0
}

func reply(e *handler.CommandEvent, content string) error {
	return e.CreateMessage(discord.MessageCreate{
		Content: content,
		Flags:   discord.MessageFlagEphemeral,
	})
}

// followup answers a command that already deferred its response.
func followup(e *handler.CommandEvent, content string) error {
	_, err := e.CreateFollowupMessage(discord.MessageCreate{Content: content})
	return err
}

func ptr[T any](v T) *T { return &v }
