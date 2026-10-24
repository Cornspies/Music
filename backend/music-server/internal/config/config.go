package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

const (
	DefaultPort    = "3001"
	DefaultDataDir = "data"
)

type Config struct {
	Port                   string
	DataDir                string
	LogDir                 string
	WebDir                 string
	BindAddress            string
	JWTSecret              string
	YouTubeAPIKey          string
	MetadataLookupDisabled bool
	MetricsAddr            string
	MetricsToken           string
	LoadedFiles            []string
	// HomeDir is set when the .env next to the executable was used: the
	// server then runs from that folder, so a release bundle works no matter
	// where it is started from.
	HomeDir string
}

func Load() (Config, error) {
	loaded, home := loadEnvFiles()

	cfg := Config{
		Port:                   firstNonEmpty(os.Getenv("MUSIC_PORT"), DefaultPort),
		DataDir:                firstNonEmpty(os.Getenv("MUSIC_DATA_DIR"), DefaultDataDir),
		LogDir:                 Value("MUSIC_LOG_DIR"),
		WebDir:                 Value("MUSIC_WEB_DIR"),
		BindAddress:            Value("MUSIC_BIND"),
		JWTSecret:              strings.TrimSpace(os.Getenv("MUSIC_JWT_SECRET")),
		YouTubeAPIKey:          YouTubeAPIKey(),
		MetadataLookupDisabled: MetadataLookupDisabled(),
		MetricsAddr:            Value("MUSIC_METRICS_ADDR"),
		MetricsToken:           Value("MUSIC_METRICS_TOKEN"),
		LoadedFiles:            loaded,
		HomeDir:                home,
	}

	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("MUSIC_JWT_SECRET must be at least 32 characters (got %d)", len(cfg.JWTSecret))
	}
	for _, name := range toggles {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" && v != "0" && v != "1" {
			return Config{}, fmt.Errorf("%s must be 0 or 1 (got %q)", name, v)
		}
	}
	return cfg, nil
}

// toggles are the on/off settings: 1 is on; 0, or not set, is off. Anything
// else stops the server at start-up, so a typo can't silently do the
// opposite of what was meant.
var toggles = []string{
	"MUSIC_ALLOW_REGISTRATION",
	"MUSIC_DISABLE_ANALYSIS",
	"MUSIC_DISABLE_AUTO_TRIM",
	"MUSIC_DISABLE_DEEZER",
	"MUSIC_DISABLE_DISCOVER",
	"MUSIC_DISABLE_LYRICS_LOOKUP",
	"MUSIC_DISABLE_METADATA_LOOKUP",
	"MUSIC_DISABLE_SHARING",
	"MUSIC_DISABLE_SOUNDCLOUD",
	"MUSIC_DISABLE_TRANSCODE",
}

func loadEnvFiles() ([]string, string) {
	var loaded []string

	tryLoad := func(path string) {
		if path == "" {
			return
		}
		if err := godotenv.Load(path); err == nil {
			loaded = append(loaded, path)
		}
	}

	loadDir := func(dir string) {
		for _, name := range []string{".env.local", ".env"} {
			candidate := filepath.Join(dir, name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				tryLoad(candidate)
			}
		}
	}

	if explicit := strings.TrimSpace(os.Getenv("MUSIC_ENV_FILE")); explicit != "" {
		tryLoad(explicit)
		return loaded, ""
	}

	// A .env beside the executable, as in a release bundle, wins over the
	// working directory.
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			home := filepath.Dir(exe)
			if loadDir(home); len(loaded) > 0 {
				return loaded, home
			}
		}
	}

	dir, err := os.Getwd()
	if err != nil {
		return loaded, ""
	}
	for i := 0; i < 6; i++ {
		loadDir(dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return loaded, ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func YouTubeAPIKey() string {
	return Value("MUSIC_YOUTUBE_API_KEY")
}

func MetadataLookupDisabled() bool {
	return Flag("MUSIC_DISABLE_METADATA_LOOKUP")
}

func Value(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// Flag reports whether an on/off setting is 1. Load has already refused any
// value other than 0 or 1 for the names in toggles.
func Flag(name string) bool {
	return strings.TrimSpace(os.Getenv(name)) == "1"
}
