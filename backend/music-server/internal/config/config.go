// Package config resolves the music server's settings from, in order of
// precedence: command-line flags (handled in main), real environment
// variables, a .env file, then built-in defaults.
//
// The .env file is looked up by walking upwards from the working directory, so
// the same file at the repository root serves both this server and the Next.js
// app — you don't need one copy per service.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Defaults used when nothing else supplies a value.
const (
	DefaultPort    = "3001"
	DefaultDataDir = "data"
)

// Config holds everything the server needs to start.
type Config struct {
	// Port is the HTTP listen port.
	Port string
	// DataDir holds the SQLite database, uploads, covers and avatars.
	DataDir string
	// BindAddress is the interface to listen on. Empty (the default) means
	// every interface. Set it to 127.0.0.1 when a local reverse proxy is the
	// only client — unlike a systemd IPAddressDeny rule, this restricts
	// inbound connections only and leaves outgoing requests alone.
	BindAddress string
	// JWTSecret, when set, replaces the secret that would otherwise be
	// generated into <DataDir>/secret.txt. Useful when several instances must
	// accept each other's tokens, or when you'd rather not persist it to disk.
	// Must be at least 32 characters.
	JWTSecret string
	// YouTubeAPIKey enables YouTube as an extra metadata source. Empty means
	// the source is skipped; the other three providers need no key.
	YouTubeAPIKey string
	// MetadataLookupDisabled turns off all online tag lookups on upload.
	MetadataLookupDisabled bool
	// LoadedFiles lists the .env files that were actually read, for logging.
	LoadedFiles []string
}

// Load reads .env files and resolves the configuration.
//
// Real environment variables always win over .env entries, which is what you
// want when running under systemd, Docker or a process manager.
func Load() (Config, error) {
	loaded := loadEnvFiles()

	cfg := Config{
		Port:                   firstNonEmpty(os.Getenv("MUSIC_PORT"), os.Getenv("PORT"), DefaultPort),
		DataDir:                firstNonEmpty(os.Getenv("MUSIC_DATA_DIR"), DefaultDataDir),
		BindAddress:            Value("MUSIC_BIND"),
		JWTSecret:              strings.TrimSpace(os.Getenv("MUSIC_JWT_SECRET")),
		YouTubeAPIKey:          YouTubeAPIKey(),
		MetadataLookupDisabled: MetadataLookupDisabled(),
		LoadedFiles:            loaded,
	}

	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("MUSIC_JWT_SECRET must be at least 32 characters (got %d)", len(cfg.JWTSecret))
	}
	return cfg, nil
}

// loadEnvFiles loads, in precedence order, MUSIC_ENV_FILE (if set) and then
// .env.local / .env found by walking up from the working directory. godotenv
// never overwrites a variable that is already set, so the first file to define
// a key wins and real environment variables beat all of them.
func loadEnvFiles() []string {
	var loaded []string

	tryLoad := func(path string) {
		if path == "" {
			return
		}
		if err := godotenv.Load(path); err == nil {
			loaded = append(loaded, path)
		}
	}

	// An explicit path wins and is not searched for.
	if explicit := strings.TrimSpace(os.Getenv("MUSIC_ENV_FILE")); explicit != "" {
		tryLoad(explicit)
		return loaded
	}

	dir, err := os.Getwd()
	if err != nil {
		return loaded
	}
	// Walk up far enough to reach a repo root from backend/music-server,
	// but stop at the filesystem root.
	for i := 0; i < 6; i++ {
		for _, name := range []string{".env.local", ".env"} {
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				tryLoad(candidate)
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return loaded
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// YouTubeAPIKey returns the configured YouTube Data API key, preferring the
// MUSIC_-prefixed name and falling back to the original unprefixed one so
// existing setups keep working.
//
// It is read on demand rather than passed around, because the metadata
// providers are package-level functions; the value comes from the same
// process environment that Load populated from .env.
func YouTubeAPIKey() string {
	return Value("MUSIC_YOUTUBE_API_KEY")
}

// MetadataLookupDisabled reports whether online tag lookups are switched off.
func MetadataLookupDisabled() bool {
	// MUSIC_ is the documented prefix; the earlier mixed-case spelling is
	// still honoured so existing setups don't silently start phoning out.
	return Flag("MUSIC_DISABLE_METADATA_LOOKUP")
}

// Value returns the first of the named environment variables that is set to a
// non-empty value, trimmed.
func Value(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// Flag reports whether an environment variable is set to a truthy value.
// Accepts 1/true/yes/on in any case; anything else (including unset) is false.
func Flag(names ...string) bool {
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "1", "true", "yes", "on":
			return true
		case "":
			continue
		default:
			// An explicitly set but non-truthy value means "off".
			return false
		}
	}
	return false
}
