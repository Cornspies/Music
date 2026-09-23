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
	BindAddress            string
	JWTSecret              string
	YouTubeAPIKey          string
	MetadataLookupDisabled bool
	LoadedFiles            []string
}

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

	if explicit := strings.TrimSpace(os.Getenv("MUSIC_ENV_FILE")); explicit != "" {
		tryLoad(explicit)
		return loaded
	}

	dir, err := os.Getwd()
	if err != nil {
		return loaded
	}
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

func Flag(names ...string) bool {
	for _, name := range names {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "1", "true", "yes", "on":
			return true
		case "":
			continue
		default:
			return false
		}
	}
	return false
}
