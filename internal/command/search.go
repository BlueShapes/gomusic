package command

import (
	"crypto/rand"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"

	"github.com/blueshapes/gomusic/internal/audio"
	"github.com/blueshapes/gomusic/internal/bot"
	"github.com/blueshapes/gomusic/internal/util"
)

const (
	searchResults = 5
	pendingTTL    = 2 * time.Minute
)

// pendingPlay is a search whose select menu is still on screen.
type pendingPlay struct {
	guildID   snowflake.ID
	channelID snowflake.ID
	tracks    []audio.Track
	created   time.Time
}

var (
	pendingMu sync.Mutex
	pending   = map[string]*pendingPlay{}
)

func storePending(p *pendingPlay) string {
	p.created = time.Now()
	token := rand.Text()

	pendingMu.Lock()
	defer pendingMu.Unlock()
	for t, e := range pending {
		if time.Since(e.created) > pendingTTL {
			delete(pending, t)
		}
	}
	pending[token] = p
	return token
}

func takePending(token string) (*pendingPlay, bool) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	p, ok := pending[token]
	delete(pending, token)
	if !ok || time.Since(p.created) > pendingTTL {
		return nil, false
	}
	return p, true
}

// sendSearchMenu answers a deferred /play with a select menu of the hits.
func sendSearchMenu(e *handler.CommandEvent, channelID snowflake.ID, tracks []audio.Track) error {
	guildID := e.GuildID()
	if guildID == nil {
		return nil
	}
	token := storePending(&pendingPlay{
		guildID:   *guildID,
		channelID: channelID,
		tracks:    tracks,
	})

	options := make([]discord.StringSelectMenuOption, 0, len(tracks))
	for i, t := range tracks {
		options = append(options, discord.
			NewStringSelectMenuOption(clip(t.Title, 100), strconv.Itoa(i)).
			WithDescription(clip(fmt.Sprintf("%s / %s", util.HMS(t.Duration), t.Uploader), 100)))
	}

	_, err := e.CreateFollowupMessage(discord.MessageCreate{
		Content: "検索結果から選んでください。",
		Components: []discord.LayoutComponent{
			discord.NewActionRow(discord.NewStringSelectMenu("/play/"+token, "曲を選択", options...)),
		},
	})
	return err
}

// handlePlaySelect queues the chosen search hit and closes the menu.
func handlePlaySelect(b *bot.Bot) handler.ComponentHandler {
	return func(e *handler.ComponentEvent) error {
		sel, ok := e.Data.(discord.StringSelectMenuInteractionData)
		if !ok || len(sel.Values) == 0 {
			return nil
		}
		p, ok := takePending(e.Vars["token"])
		if !ok {
			return closeMenu(e, "期限切れです。もう一度 /play してください。")
		}
		guildID := e.GuildID()
		if guildID == nil || *guildID != p.guildID {
			return closeMenu(e, "このメニューは使えません。")
		}
		idx, err := strconv.Atoi(sel.Values[0])
		if err != nil || idx < 0 || idx >= len(p.tracks) {
			return closeMenu(e, "選択が不正です。")
		}

		b.Players.GetOrCreate(p.guildID).Enqueue(p.channelID, p.tracks[idx])
		return closeMenu(e, fmt.Sprintf("`%s` をキューに追加しました。", p.tracks[idx].Title))
	}
}

func closeMenu(e *handler.ComponentEvent, content string) error {
	empty := []discord.LayoutComponent{}
	return e.UpdateMessage(discord.MessageUpdate{Content: &content, Components: &empty})
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
