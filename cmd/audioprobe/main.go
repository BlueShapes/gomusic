// Command audioprobe exercises the audio layer without Discord: resolve
// metadata, build the ogg cache and walk packets.
//
//	go run ./cmd/audioprobe -search "lofi hip hop"
//	go run ./cmd/audioprobe -prepare "https://youtu.be/dQw4w9WgXcQ"
//	go run ./cmd/audioprobe -play cache/dQw4w9WgXcQ.ogg -seek 30s -packets 50
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/blueshapes/gomusic/internal/audio"
	"github.com/blueshapes/gomusic/internal/config"
)

func main() {
	var (
		search  = flag.String("search", "", "ytsearch query to resolve")
		resolve = flag.String("resolve", "", "URL (video or playlist) to resolve without downloading")
		prepare = flag.String("prepare", "", "URL or id to download into the cache")
		play    = flag.String("play", "", "cached ogg file to walk")
		seek    = flag.Duration("seek", 0, "seek position for -play")
		packets = flag.Int("packets", 20, "packets to read for -play")
		limit   = flag.Int("limit", 5, "search result limit")
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(*search, *resolve, *prepare, *play, *seek, *packets, *limit); err != nil {
		slog.Error("failed", "err", err)
		os.Exit(1)
	}
}

func run(search, resolve, prepare, play string, seek time.Duration, packets, limit int) error {
	ctx := context.Background()
	cfg := config.Default()
	yt := &audio.YtDlp{
		Bin:         cfg.YtDlp.Bin,
		ExtraArgs:   cfg.YtDlp.ExtraArgs,
		MaxPlaylist: cfg.YtDlp.MaxPlaylist,
		Log:         slog.Default(),
	}
	dir := os.Getenv("CACHE_DIR")
	if dir == "" {
		dir = "cache"
	}
	cache, err := audio.NewCache(dir, cfg.Audio.Ffmpeg, cfg.Audio.MaxCacheMB, cfg.Audio.Concurrency, yt, slog.Default())
	if err != nil {
		return err
	}

	switch {
	case search != "":
		start := time.Now()
		tracks, err := yt.Search(ctx, search, limit)
		if err != nil {
			return err
		}
		fmt.Printf("search %q: %d hits in %v\n", search, len(tracks), time.Since(start).Round(time.Millisecond))
		for i, t := range tracks {
			fmt.Printf("  %d. %-60.60s %v  %s  (%s)\n", i+1, t.Title, t.Duration.Round(time.Second), t.Uploader, t.ID)
		}

	case resolve != "":
		start := time.Now()
		tracks, err := yt.Resolve(ctx, resolve)
		if err != nil {
			return err
		}
		fmt.Printf("resolved %d track(s) in %v\n", len(tracks), time.Since(start).Round(time.Millisecond))
		for i, t := range tracks {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(tracks)-10)
				break
			}
			fmt.Printf("  %d. %-60.60s %v  (%s)\n", i+1, t.Title, t.Duration.Round(time.Second), t.ID)
		}

	case prepare != "":
		tracks, err := yt.Resolve(ctx, prepare)
		if err != nil {
			return err
		}
		fmt.Printf("resolved %d track(s)\n", len(tracks))
		for _, t := range tracks {
			start := time.Now()
			path, err := cache.Prepare(ctx, t)
			if err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			fmt.Printf("  %-50.50s -> %s (%.1f MB, %v)\n", t.Title, path, float64(info.Size())/(1<<20), time.Since(start).Round(time.Millisecond))
		}

	case play != "":
		r, err := audio.Open(play)
		if err != nil {
			return err
		}
		defer r.Close()

		if seek > 0 {
			start := time.Now()
			if err := r.Seek(seek); err != nil {
				return err
			}
			fmt.Printf("seek to %v took %v (position %v)\n", seek, time.Since(start).Round(time.Millisecond), r.Position())
		}
		for i := range packets {
			p, err := r.ReadPacket()
			if errors.Is(err, io.EOF) {
				fmt.Printf("EOF after %d packets at %v\n", i, r.Position())
				return nil
			}
			if err != nil {
				return err
			}
			if i < 3 || i == packets-1 {
				fmt.Printf("  packet %d: %d bytes, %v, pos %v\n", i, len(p), audio.PacketDuration(p), r.Position())
			}
		}
		var total int
		for {
			p, err := r.ReadPacket()
			if err != nil {
				break
			}
			total += len(p)
		}
		fmt.Printf("walked rest of file: %d bytes\n", total)

	default:
		flag.Usage()
	}
	return nil
}
