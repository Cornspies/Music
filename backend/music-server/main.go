package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"music-server/internal/config"
	"music-server/internal/logfile"
	"music-server/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	port := flag.String("port", cfg.Port, "HTTP listen port (env: MUSIC_PORT)")
	bind := flag.String("bind", cfg.BindAddress,
		"address to listen on; empty means all interfaces (env: MUSIC_BIND)")
	dataDir := flag.String("data", cfg.DataDir, "data directory for db, uploads, covers (env: MUSIC_DATA_DIR)")
	logDir := flag.String("logs", cfg.LogDir,
		"directory for daily log files, deleted after 14 days; empty means <data>/logs (env: MUSIC_LOG_DIR)")
	webDir := flag.String("web", cfg.WebDir,
		"directory with the static web app (bun run build:static); empty means the web app isn't served (env: MUSIC_WEB_DIR)")
	flag.Parse()

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}

	logs := *logDir
	if logs == "" {
		logs = filepath.Join(abs, "logs")
	}
	if logs, err = filepath.Abs(logs); err != nil {
		log.Fatalf("resolve log dir: %v", err)
	}
	if w, err := logfile.Open(logs); err != nil {
		log.Printf("music-server logs: %v; logging to stderr only", err)
	} else {
		log.Printf("music-server logs: writing to %s, kept for %d days", logs, logfile.KeepDays)
		log.SetOutput(w)
	}

	web := *webDir
	if web != "" {
		if web, err = filepath.Abs(web); err != nil {
			log.Fatalf("resolve web dir: %v", err)
		}
		if _, err := os.Stat(filepath.Join(web, "index.html")); err != nil {
			log.Printf("music-server web app: %s has no index.html; run `bun run build:static` and point MUSIC_WEB_DIR at its out/ directory", web)
		} else {
			log.Printf("music-server web app: serving %s", web)
		}
	}

	srv, err := server.New(abs, server.Options{JWTSecret: cfg.JWTSecret, WebDir: web})
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	if len(cfg.LoadedFiles) > 0 {
		log.Printf("music-server config: loaded %s", strings.Join(cfg.LoadedFiles, ", "))
	} else {
		log.Printf("music-server config: no .env file found, using environment and defaults")
	}
	switch {
	case cfg.MetadataLookupDisabled:
		log.Printf("music-server metadata lookup: disabled")
	case cfg.YouTubeAPIKey != "":
		log.Printf("music-server metadata lookup: MusicBrainz, Deezer, iTunes, YouTube")
	default:
		log.Printf("music-server metadata lookup: MusicBrainz, Deezer, iTunes " +
			"(set MUSIC_YOUTUBE_API_KEY to add YouTube)")
	}
	addr := *bind + ":" + *port
	log.Printf("music-server listening on %s (data: %s, logs: %s, pid: %d)", addr, abs, logs, os.Getpid())
	srv.StartBackgroundWork()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Fatal(httpServer.ListenAndServe())
}
