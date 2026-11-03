package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// ErrCreated is returned by Load when the config file did not exist and a
// default one was written instead. The caller should tell the user to edit it.
var ErrCreated = errors.New("config file created with defaults")

type Config struct {
	Discord Discord `toml:"discord"`
	YtDlp   YtDlp   `toml:"yt_dlp"`
	Audio   Audio   `toml:"audio"`
}

type Discord struct {
	Token    string   `toml:"token"`
	GuildIDs []uint64 `toml:"guild_ids"`
}

type YtDlp struct {
	// Bin is the yt-dlp executable (path or name on PATH).
	Bin string `toml:"bin"`
	// Cookies is an optional path to a cookies file for age/bot restricted videos.
	Cookies string `toml:"cookies"`
	// MaxPlaylist caps how many entries a playlist URL expands to. It is passed
	// as --playlist-end, so entries beyond it are never fetched (resolve costs
	// ~4ms per entry). Absent/0 means the default; a negative value means no cap.
	MaxPlaylist int `toml:"max_playlist"`
	// ExtraArgs are appended to every yt-dlp call.
	// NOTE: node is not enabled by default in yt-dlp, so --no-js-runtimes must
	// come before --js-runtimes node or deno silently wins.
	ExtraArgs []string `toml:"extra_args"`
}

type Audio struct {
	// Ffmpeg is the ffmpeg executable (path or name on PATH).
	Ffmpeg string `toml:"ffmpeg"`
	// CacheDir holds the remuxed ogg/opus files, keyed by video id.
	CacheDir string `toml:"cache_dir"`
	// MaxCacheMB is the disk budget for the cache; oldest files are evicted.
	MaxCacheMB int `toml:"max_cache_mb"`
	// Concurrency limits parallel yt-dlp/ffmpeg jobs.
	Concurrency int `toml:"concurrency"`
}

func Default() Config {
	return Config{
		Discord: Discord{
			Token:    "token is here",
			GuildIDs: []uint64{},
		},
		YtDlp: YtDlp{
			Bin:         "yt-dlp",
			MaxPlaylist: 100,
			ExtraArgs:   []string{"--no-js-runtimes", "--js-runtimes", "node"},
		},
		Audio: Audio{
			Ffmpeg:      "ffmpeg",
			CacheDir:    "cache",
			MaxCacheMB:  4096,
			Concurrency: 2,
		},
	}
}

// applyDefaults fills in every zero value so configs written by older versions
// (or hand-edited ones) keep working.
func (c *Config) applyDefaults() {
	d := Default()
	if c.YtDlp.Bin == "" {
		c.YtDlp.Bin = d.YtDlp.Bin
	}
	if c.YtDlp.MaxPlaylist == 0 {
		c.YtDlp.MaxPlaylist = d.YtDlp.MaxPlaylist
	}
	if c.YtDlp.ExtraArgs == nil {
		c.YtDlp.ExtraArgs = d.YtDlp.ExtraArgs
	}
	if c.Audio.Ffmpeg == "" {
		c.Audio.Ffmpeg = d.Audio.Ffmpeg
	}
	if c.Audio.CacheDir == "" {
		c.Audio.CacheDir = d.Audio.CacheDir
	}
	if c.Audio.MaxCacheMB == 0 {
		c.Audio.MaxCacheMB = d.Audio.MaxCacheMB
	}
	if c.Audio.Concurrency == 0 {
		c.Audio.Concurrency = d.Audio.Concurrency
	}
}

// Load reads the TOML config at path. If it does not exist, a default config is
// written there and ErrCreated is returned.
func Load(path string) (Config, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		// docker bind-mounts a directory when the host file is missing
		return Config{}, fmt.Errorf("%s is a directory: create the file before starting (or drop the mount)", path)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		f, err := os.Create(path)
		if err != nil {
			return Config{}, fmt.Errorf("create %s: %w", path, err)
		}
		if err := toml.NewEncoder(f).Encode(cfg); err != nil {
			f.Close()
			return Config{}, fmt.Errorf("write %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return Config{}, fmt.Errorf("close %s: %w", path, err)
		}
		return Config{}, ErrCreated
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	// Fields added after the file was written are absent, so fill them in, then
	// let env override the file (containers pass CACHE_DIR/TOKEN instead of
	// editing the mounted config).
	cfg.applyDefaults()
	if err := cfg.applyEnv(); err != nil {
		return Config{}, err
	}
	if cfg.Discord.Token == "" || cfg.Discord.Token == "token is here" {
		return Config{}, fmt.Errorf("%s: discord.token is not set (or set DISCORD_TOKEN)", path)
	}
	if len(cfg.Discord.GuildIDs) == 0 {
		return Config{}, fmt.Errorf("%s: discord.guild_ids is empty (or set DISCORD_GUILD_IDS)", path)
	}
	return cfg, nil
}

// applyEnv overrides the file with environment variables. Anything left unset
// keeps its value from config.toml.
func (c *Config) applyEnv() error {
	if v := os.Getenv("DISCORD_TOKEN"); v != "" {
		c.Discord.Token = v
	}
	if v := os.Getenv("DISCORD_GUILD_IDS"); v != "" {
		ids := make([]uint64, 0, 1)
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseUint(part, 10, 64)
			if err != nil {
				return fmt.Errorf("DISCORD_GUILD_IDS: %q is not a guild id", part)
			}
			ids = append(ids, id)
		}
		c.Discord.GuildIDs = ids
	}
	if v := os.Getenv("CACHE_DIR"); v != "" {
		c.Audio.CacheDir = v
	}
	if v := os.Getenv("MAX_CACHE_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("MAX_CACHE_MB: %q is not a number", v)
		}
		c.Audio.MaxCacheMB = n
	}
	if v := os.Getenv("CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("CONCURRENCY: %q is not a number", v)
		}
		c.Audio.Concurrency = n
	}
	if v := os.Getenv("FFMPEG"); v != "" {
		c.Audio.Ffmpeg = v
	}
	if v := os.Getenv("YTDLP_BIN"); v != "" {
		c.YtDlp.Bin = v
	}
	return nil
}
