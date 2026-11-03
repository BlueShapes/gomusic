package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Cache turns tracks into playable ogg files, one per video id.
//
// The normal path copies the source bitstream into ogg ("-c:a copy"): YouTube's
// bestaudio is already 48kHz stereo opus in 20ms frames, so no audio processing
// happens at all. Sources that are not Discord-ready are re-encoded instead.
type Cache struct {
	dir      string
	ffmpeg   string
	maxBytes int64
	yt       *YtDlp
	log      *slog.Logger

	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]*prepareJob
}

type prepareJob struct {
	done chan struct{}
	path string
	err  error
}

func NewCache(dir, ffmpeg string, maxMB, concurrency int, yt *YtDlp, log *slog.Logger) (*Cache, error) {
	if ffmpeg == "" {
		return nil, errors.New("audio.ffmpeg is empty")
	}
	if yt == nil || yt.Bin == "" {
		return nil, errors.New("yt_dlp.bin is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	// fail at startup instead of at the first download (e.g. a root-owned volume
	// with a non-root container user)
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return nil, fmt.Errorf("cache dir %s is not writable: %w", dir, err)
	}
	probe := f.Name()
	f.Close()
	os.Remove(probe)
	if concurrency < 1 {
		concurrency = 1
	}
	return &Cache{
		dir:      dir,
		ffmpeg:   ffmpeg,
		maxBytes: int64(maxMB) << 20,
		yt:       yt,
		log:      log,
		sem:      make(chan struct{}, concurrency),
		inflight: make(map[string]*prepareJob),
	}, nil
}

// Path is where the playable ogg for a track id lives.
func (c *Cache) Path(id string) string { return filepath.Join(c.dir, id+".ogg") }

// Prepared reports whether the track is already cached.
func (c *Cache) Prepared(id string) bool {
	_, err := os.Stat(c.Path(id))
	return err == nil
}

// Prepare returns the path of a Discord-ready ogg for t, downloading and
// remuxing it if needed. Concurrent callers for the same track share one job.
func (c *Cache) Prepare(ctx context.Context, t Track) (string, error) {
	if t.ID == "" {
		return "", errors.New("track has no id")
	}
	path := c.Path(t.ID)
	if info, err := os.Stat(path); err == nil {
		// touch so eviction stays LRU-ish
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		c.log.Info("cache hit", "id", t.ID, "file", filepath.Base(path), "size_mb", fmt.Sprintf("%.1f", float64(info.Size())/(1<<20)))
		return path, nil
	}

	c.mu.Lock()
	if job, ok := c.inflight[t.ID]; ok {
		c.mu.Unlock()
		c.log.Debug("waiting for in-flight download", "id", t.ID)
		select {
		case <-job.done:
			return job.path, job.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	job := &prepareJob{done: make(chan struct{})}
	c.inflight[t.ID] = job
	c.mu.Unlock()

	job.path, job.err = c.prepare(ctx, t)

	c.mu.Lock()
	delete(c.inflight, t.ID)
	c.mu.Unlock()
	close(job.done)
	return job.path, job.err
}

func (c *Cache) prepare(ctx context.Context, t Track) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	c.log.Info("downloading track", "id", t.ID, "title", t.Title, "duration", t.Duration.Round(time.Second))

	path := c.Path(t.ID)
	part := path + ".part"
	defer os.Remove(part)

	start := time.Now()
	err := c.transcode(ctx, t.URL, part, false)
	switch {
	case err == nil:
		if err := verifyDiscordReady(part); err == nil {
			return c.commit(part, path, "passthrough", start)
		} else {
			c.log.Warn("source is not Discord-ready, re-encoding", "id", t.ID, "reason", err)
		}
	default:
		c.log.Warn("passthrough failed, re-encoding", "id", t.ID, "err", err)
	}

	if err := c.transcode(ctx, t.URL, part, true); err != nil {
		return "", err
	}
	if err := verifyDiscordReady(part); err != nil {
		return "", fmt.Errorf("re-encoded file is not playable: %w", err)
	}
	return c.commit(part, path, "re-encoded", start)
}

func (c *Cache) commit(part, path, mode string, start time.Time) (string, error) {
	if err := os.Rename(part, path); err != nil {
		return "", fmt.Errorf("commit cache file: %w", err)
	}
	c.log.Info("cached track", "file", filepath.Base(path), "mode", mode, "took", time.Since(start))
	c.evict()
	return path, nil
}

// transcode runs "yt-dlp | ffmpeg" into out. reencode=false only remuxes the
// source bitstream into ogg.
func (c *Cache) transcode(ctx context.Context, url, out string, reencode bool) error {
	yt := exec.CommandContext(ctx, c.yt.Bin, c.yt.args(
		"-f", "bestaudio[ext=webm]/bestaudio/best",
		// live streams never end, so the cache file would never be complete
		"--match-filter", "!is_live",
		"--no-playlist",
		// exactly one item: a playlist URL slipped into a Track must not be
		// concatenated into one bogus file
		"--playlist-items", "1",
		"-o", "-",
		url,
	)...)
	var ytErr strings.Builder
	yt.Stderr = &ytErr

	ffArgs := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", "pipe:0", "-vn", "-map", "0:a"}
	if reencode {
		ffArgs = append(ffArgs, "-c:a", "libopus", "-b:a", "96k", "-ar", "48000", "-ac", "2", "-frame_duration", "20", "-application", "audio")
	} else {
		ffArgs = append(ffArgs, "-c:a", "copy")
	}
	ff := exec.CommandContext(ctx, c.ffmpeg, append(ffArgs, "-f", "ogg", out)...)
	var ffErr strings.Builder
	ff.Stderr = &ffErr

	stdout, err := yt.StdoutPipe()
	if err != nil {
		return err
	}
	ff.Stdin = stdout

	if err := yt.Start(); err != nil {
		return fmt.Errorf("start yt-dlp: %w", err)
	}
	if err := ff.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	ffWait := ff.Wait()
	ytWait := yt.Wait()
	if ytWait != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("yt-dlp timed out")
		}
		return fmt.Errorf("yt-dlp: %w: %s", ytWait, tail(ytErr.String()))
	}
	if ffWait != nil {
		return fmt.Errorf("ffmpeg: %w: %s", ffWait, tail(ffErr.String()))
	}
	return nil
}

// evict deletes the least recently used files until the cache fits its budget.
func (c *Cache) evict() {
	if c.maxBytes <= 0 {
		return
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		c.log.Warn("cache eviction skipped", "err", err)
		return
	}

	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".ogg" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(c.dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total <= c.maxBytes {
		return
	}

	slices.SortFunc(files, func(a, b file) int { return a.mod.Compare(b.mod) })
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			c.log.Warn("cache eviction failed", "file", f.path, "err", err)
			continue
		}
		total -= f.size
		c.log.Info("evicted cache file", "file", filepath.Base(f.path))
	}
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// Discard drains r to keep a pipe alive; used by tooling.
func Discard(r io.Reader) { _, _ = io.Copy(io.Discard, r) }
