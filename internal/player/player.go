package player

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"

	"github.com/blueshapes/gomusic/internal/audio"
)

// LoopType mirrors the Kotlin bot: repeat nothing, or repeat the whole queue.
type LoopType int

const (
	LoopOff LoopType = iota
	LoopQueue
)

// Voice is the part of the bot the player needs: get me a live conn.
type Voice interface {
	Join(ctx context.Context, guildID, channelID snowflake.ID) (voice.Conn, error)
}

// Player owns one guild's queue and voice playback.
type Player struct {
	guildID snowflake.ID
	voice   Voice
	cache   *audio.Cache
	log     *slog.Logger

	mu        sync.Mutex
	queue     []audio.Track
	current   *audio.Track
	channelID snowflake.ID
	loop      LoopType
	playing   bool
	destroyed bool
	provider  *provider
	volume    int
}

func New(guildID snowflake.ID, v Voice, cache *audio.Cache, log *slog.Logger) *Player {
	return &Player{guildID: guildID, voice: v, cache: cache, log: log, volume: 100}
}

// Enqueue adds tracks and starts playback if nothing is playing. Preparing the
// first track happens in the background so commands answer immediately.
func (p *Player) Enqueue(channelID snowflake.ID, tracks ...audio.Track) {
	if len(tracks) == 0 {
		return
	}

	p.mu.Lock()
	p.channelID = channelID
	p.queue = append(p.queue, tracks...)
	start := !p.playing
	if start {
		p.playing = true
	}
	p.mu.Unlock()

	// warm the next few so the queue does not stall on downloads
	for i, t := range tracks {
		if i >= 3 {
			break
		}
		go p.prefetch(t)
	}
	if start {
		go p.advance(context.Background())
	}
}

func (p *Player) prefetch(t audio.Track) {
	if p.cache.Prepared(t.ID) {
		return
	}
	if _, err := p.cache.Prepare(context.Background(), t); err != nil {
		p.log.Debug("prefetch failed", "id", t.ID, "err", err)
	}
}

// advance prepares and starts the next track. It runs in its own goroutine: it
// downloads, joins voice and swaps the OpusFrameProvider.
func (p *Player) advance(ctx context.Context) {
	p.mu.Lock()
	if p.destroyed {
		p.mu.Unlock()
		return
	}
	if len(p.queue) == 0 {
		p.current = nil
		p.playing = false
		p.provider = nil
		p.mu.Unlock()
		p.log.Info("queue drained")
		return
	}
	track := p.queue[0]
	p.queue = p.queue[1:]
	p.current = &track
	channelID := p.channelID
	p.mu.Unlock()

	stop := func(err error) {
		p.log.Error("playback failed", "id", track.ID, "err", err)
		p.mu.Lock()
		p.current = nil
		p.playing = false
		p.provider = nil
		p.mu.Unlock()
	}

	path, err := p.cache.Prepare(ctx, track)
	if err != nil {
		stop(err)
		return
	}
	conn, err := p.voice.Join(ctx, p.guildID, channelID)
	if err != nil {
		stop(err)
		return
	}
	reader, err := audio.Open(path)
	if err != nil {
		stop(err)
		return
	}

	prov := &provider{reader: reader, log: p.log}
	prov.onEnd = func() { p.onTrackEnd(track) }

	p.mu.Lock()
	prov.SetVolume(p.volume)
	if p.destroyed {
		p.mu.Unlock()
		prov.Close()
		return
	}
	p.provider = prov
	p.mu.Unlock()

	conn.SetOpusFrameProvider(prov)
	p.log.Info("now playing", "id", track.ID, "title", track.Title, "duration", track.Duration)
}

func (p *Player) onTrackEnd(finished audio.Track) {
	p.mu.Lock()
	if p.destroyed {
		p.mu.Unlock()
		return
	}
	if p.loop == LoopQueue {
		p.queue = append(p.queue, finished)
	}
	p.mu.Unlock()

	p.advance(context.Background())
}

// Skip starts the next track immediately.
func (p *Player) Skip() {
	p.advance(context.Background())
}

// Stop tears the player down (used by /leave).
func (p *Player) Stop() {
	p.mu.Lock()
	p.destroyed = true
	p.queue = nil
	p.current = nil
	p.playing = false
	prov := p.provider
	p.provider = nil
	p.mu.Unlock()

	if prov != nil {
		prov.Close()
	}
}

func (p *Player) Destroyed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.destroyed
}

func (p *Player) ChannelID() snowflake.ID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.channelID
}

func (p *Player) Current() (audio.Track, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil {
		return audio.Track{}, false
	}
	return *p.current, true
}

// Next is the track that will play after the current one.
func (p *Player) Next() (audio.Track, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return audio.Track{}, false
	}
	return p.queue[0], true
}

// Queue returns a copy of the upcoming tracks.
func (p *Player) Queue() []audio.Track {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]audio.Track(nil), p.queue...)
}

// Shuffle randomises the upcoming queue.
func (p *Player) Shuffle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	rand.Shuffle(len(p.queue), func(i, j int) {
		p.queue[i], p.queue[j] = p.queue[j], p.queue[i]
	})
}

// Clear drops the upcoming queue; the current track keeps playing.
func (p *Player) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = nil
}

// Position is how far into the current track playback is.
func (p *Player) Position() time.Duration {
	p.mu.Lock()
	prov := p.provider
	p.mu.Unlock()
	if prov == nil {
		return 0
	}
	return prov.Position()
}

// Seek jumps to d inside the current track.
func (p *Player) Seek(d time.Duration) error {
	p.mu.Lock()
	prov := p.provider
	p.mu.Unlock()
	if prov == nil {
		return errors.New("nothing is playing")
	}
	return prov.Seek(d)
}

// SetVolume sets the volume in percent (0-200). Playback at 100% avoids all
// audio processing; anything else costs a decode+encode per frame.
func (p *Player) SetVolume(percent int) {
	percent = max(0, min(percent, 200))

	p.mu.Lock()
	p.volume = percent
	prov := p.provider
	p.mu.Unlock()

	if prov != nil {
		prov.SetVolume(percent)
	}
}

func (p *Player) Volume() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// TogglePause flips pause and reports the new paused state.
func (p *Player) TogglePause() bool {
	p.mu.Lock()
	prov := p.provider
	p.mu.Unlock()
	if prov == nil {
		return false
	}
	return prov.TogglePause()
}

func (p *Player) Paused() bool {
	p.mu.Lock()
	prov := p.provider
	p.mu.Unlock()
	if prov == nil {
		return false
	}
	return prov.Paused()
}

func (p *Player) SetLoop(l LoopType) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loop = l
}

func (p *Player) Loop() LoopType {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loop
}

// provider feeds 20ms opus frames to disgo. Pausing is just returning no
// frames: disgo then sends silence and clears the speaking flag by itself.
type provider struct {
	reader *audio.Reader
	log    *slog.Logger
	onEnd  func()

	volume atomic.Int64 // percent; 100 = passthrough
	gainer *audio.Gainer

	paused atomic.Bool
	once   sync.Once
	mu     sync.Mutex
	closed bool
}

func (pr *provider) ProvideOpusFrame() ([]byte, error) {
	if pr.paused.Load() {
		return nil, nil
	}

	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return nil, nil
	}

	frame, err := pr.reader.ReadPacket()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			pr.log.Warn("read opus packet", "err", err)
		}
		// advance on a different goroutine: SetOpusFrameProvider closes this
		// provider, which takes pr.mu again.
		pr.once.Do(func() { go pr.onEnd() })
		return nil, nil
	}

	if v := pr.volume.Load(); v != 100 {
		out, gerr := pr.gainLocked(frame, float64(v)/100)
		if gerr != nil {
			pr.log.Warn("volume processing failed, playing dry", "err", gerr)
			return frame, nil
		}
		return out, nil
	}
	return frame, nil
}

// gainLocked changes the volume of one frame, creating the codec pair on first
// use so a player at 100% never pays for it. Caller holds pr.mu.
func (pr *provider) gainLocked(frame []byte, gain float64) ([]byte, error) {
	if pr.gainer == nil {
		g, err := audio.NewGainer()
		if err != nil {
			return nil, err
		}
		pr.gainer = g
	}
	return pr.gainer.Apply(frame, gain)
}

func (pr *provider) SetVolume(percent int) { pr.volume.Store(int64(percent)) }

func (pr *provider) Seek(d time.Duration) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return errors.New("provider closed")
	}
	return pr.reader.Seek(d)
}

func (pr *provider) Close() {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return
	}
	pr.closed = true
	pr.reader.Close()
}

func (pr *provider) Position() time.Duration {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.closed {
		return 0
	}
	return pr.reader.Position()
}

func (pr *provider) Paused() bool { return pr.paused.Load() }

func (pr *provider) TogglePause() bool {
	paused := !pr.paused.Load()
	pr.paused.Store(paused)
	return paused
}
