package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"music-server/internal/config"
	"music-server/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Flags default to the resolved config, so precedence ends up as
	// flag > environment variable > .env file > built-in default.
	port := flag.String("port", cfg.Port, "HTTP listen port (env: MUSIC_PORT)")
	bind := flag.String("bind", cfg.BindAddress,
		"address to listen on; empty means all interfaces (env: MUSIC_BIND)")
	dataDir := flag.String("data", cfg.DataDir, "data directory for db, uploads, covers (env: MUSIC_DATA_DIR)")
	flag.Parse()

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}

	srv, err := server.New(abs, server.Options{JWTSecret: cfg.JWTSecret})
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	if len(cfg.LoadedFiles) > 0 {
		log.Printf("music-server config: loaded %s", strings.Join(cfg.LoadedFiles, ", "))
	} else {
		log.Printf("music-server config: no .env file found, using environment and defaults")
	}
	// Say plainly which metadata sources are live: a missing API key is the
	// most common reason for "why didn't it find my track?".
	switch {
	case cfg.MetadataLookupDisabled:
		log.Printf("music-server metadata lookup: disabled")
	case cfg.YouTubeAPIKey != "":
		log.Printf("music-server metadata lookup: MusicBrainz, Deezer, iTunes, YouTube")
	default:
		log.Printf("music-server metadata lookup: MusicBrainz, Deezer, iTunes " +
			"(set MUSIC_YOUTUBE_API_KEY to add YouTube)")
	}
	// An empty bind address keeps the previous behaviour of listening on
	// every interface. Set MUSIC_BIND=127.0.0.1 when a reverse proxy on the
	// same host is the only thing that should reach the API — that restricts
	// what can connect *in* without touching outgoing traffic, which the
	// metadata lookups need.
	addr := *bind + ":" + *port
	log.Printf("music-server listening on %s (data: %s, pid: %d)", addr, abs, os.Getpid())
	log.Fatal(http.ListenAndServe(addr, srv.Routes()))
}
