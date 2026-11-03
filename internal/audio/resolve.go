package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Track is one playable entry, as far as yt-dlp tells us before downloading.
type Track struct {
	ID       string
	Title    string
	Duration time.Duration
	Uploader string
	URL      string
}

var urlRe = regexp.MustCompile(`^https?://.+`)

// IsURL reports whether the query is a URL (anything else is a search).
func IsURL(query string) bool { return urlRe.MatchString(query) }

// YtDlp runs the yt-dlp binary. Metadata is read with --flat-playlist, which is
// ~5x faster than full extraction (1.8s vs 8.6s for 5 search hits) at the cost
// of is_live; liveness is instead rejected at download time.
type YtDlp struct {
	Bin       string
	Cookies   string
	ExtraArgs []string
	// MaxPlaylist is passed as --playlist-end so large playlists are not even
	// fetched (resolve costs ~4ms per entry). <= 0 means no cap.
	MaxPlaylist int
	Log         *slog.Logger
}

func (y *YtDlp) args(extra ...string) []string {
	args := make([]string, 0, len(y.ExtraArgs)+len(extra)+4)
	args = append(args, y.ExtraArgs...)
	if y.Cookies != "" {
		args = append(args, "--cookies", y.Cookies)
	}
	if y.MaxPlaylist > 0 {
		args = append(args, "--playlist-end", strconv.Itoa(y.MaxPlaylist))
	}
	return append(args, extra...)
}

// Search runs a ytsearch query and returns up to limit hits.
func (y *YtDlp) Search(ctx context.Context, query string, limit int) ([]Track, error) {
	return y.resolve(ctx, fmt.Sprintf("ytsearch%d:%s", limit, query))
}

// Resolve expands a URL (video or playlist) into tracks.
func (y *YtDlp) Resolve(ctx context.Context, url string) ([]Track, error) {
	return y.resolve(ctx, url)
}

type info struct {
	Type       string  `json:"_type"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Duration   float64 `json:"duration"`
	Uploader   string  `json:"uploader"`
	Channel    string  `json:"channel"`
	URL        string  `json:"url"`
	WebpageURL string  `json:"webpage_url"`
	Entries    []info  `json:"entries"`
}

func (i info) track() Track {
	uploader := i.Uploader
	if uploader == "" {
		uploader = i.Channel
	}
	url := i.WebpageURL
	if url == "" {
		url = i.URL
	}
	return Track{
		ID:       i.ID,
		Title:    i.Title,
		Duration: time.Duration(i.Duration * float64(time.Second)),
		Uploader: uploader,
		URL:      url,
	}
}

func (y *YtDlp) resolve(ctx context.Context, target string) ([]Track, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, y.Bin, y.args("-J", "--no-playlist", "--flat-playlist", "--no-warnings", target)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp %q: %w: %s", target, err, lastLine(stderr.String()))
	}

	var i info
	if err := json.Unmarshal(out, &i); err != nil {
		return nil, fmt.Errorf("parse yt-dlp json: %w", err)
	}
	if i.Type == "playlist" {
		tracks := make([]Track, 0, len(i.Entries))
		skipped := 0
		for _, e := range i.Entries {
			t := e.track()
			// unavailable/private entries arrive as null: they have no id and
			// would poison the queue (Prepare rejects id-less tracks)
			if t.ID == "" {
				skipped++
				continue
			}
			tracks = append(tracks, t)
		}
		if skipped > 0 {
			y.Log.Debug("skipped unavailable playlist entries", "target", target, "count", skipped)
		}
		if len(tracks) == 0 {
			return nil, fmt.Errorf("no tracks in %q", target)
		}
		return tracks, nil
	}
	return []Track{i.track()}, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
