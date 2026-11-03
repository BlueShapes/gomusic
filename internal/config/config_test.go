package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaultThenLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	if _, err := Load(path); !errors.Is(err, ErrCreated) {
		t.Fatalf("first load: want ErrCreated, got %v", err)
	}

	// generated file must be loadable, but still rejected while the token is
	// the placeholder (so the bot never starts with a dummy token)
	if _, err := Load(path); err == nil {
		t.Fatal("placeholder token should be rejected")
	}
}

// A config written by an older version (no [audio].ffmpeg) must still work:
// missing keys fall back to the defaults instead of becoming empty strings.
func TestLoadFillsMissingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	old := `[discord]
token = "real-token"
guild_ids = [1234]

[yt_dlp]
bin = "yt-dlp"

[audio]
cache_dir = "cache"
max_cache_mb = 512
concurrency = 1
`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audio.Ffmpeg != "ffmpeg" {
		t.Fatalf("ffmpeg = %q, want default", cfg.Audio.Ffmpeg)
	}
	if len(cfg.YtDlp.ExtraArgs) == 0 {
		t.Fatal("extra_args should fall back to the default js-runtime flags")
	}
	if cfg.Audio.MaxCacheMB != 512 || cfg.Audio.Concurrency != 1 {
		t.Fatalf("explicit values were overwritten: %+v", cfg.Audio)
	}
}

// Env wins over the file, so a container can point at /cache without editing
// the mounted config.toml.
func TestEnvOverridesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `[discord]
token = "file-token"
guild_ids = [1234]

[audio]
cache_dir = "cache"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CACHE_DIR", "/cache")
	t.Setenv("MAX_CACHE_MB", "512")
	t.Setenv("CONCURRENCY", "3")
	t.Setenv("DISCORD_TOKEN", "env-token")
	t.Setenv("DISCORD_GUILD_IDS", "111, 222")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audio.CacheDir != "/cache" || cfg.Audio.MaxCacheMB != 512 || cfg.Audio.Concurrency != 3 {
		t.Fatalf("audio env overrides not applied: %+v", cfg.Audio)
	}
	if cfg.Discord.Token != "env-token" {
		t.Fatalf("token = %q, want the env value", cfg.Discord.Token)
	}
	if len(cfg.Discord.GuildIDs) != 2 || cfg.Discord.GuildIDs[0] != 111 || cfg.Discord.GuildIDs[1] != 222 {
		t.Fatalf("guild ids = %v, want [111 222]", cfg.Discord.GuildIDs)
	}
}

func TestEnvRejectsBadValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[discord]\ntoken = \"t\"\nguild_ids = [1]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONCURRENCY", "two")
	if _, err := Load(path); err == nil {
		t.Fatal("want an error for a non-numeric CONCURRENCY")
	}
}

// Docker creates a directory when bind-mounting a missing file; say so clearly.
func TestLoadRejectsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("want an error when the config path is a directory")
	}
}
