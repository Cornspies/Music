package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"
)

var needsCompatibleCopy = map[string]bool{
	".ogg":  true,
	".oga":  true,
	".ogx":  true,
	".opus": true,
	".spx":  true,
	".weba": true,
	".webm": true,
	".wma":  true,
}

const (
	compatibleExt    = ".m4a"
	compatibleMime   = "audio/mp4"
	compatibleKbps   = 256
	transcodeTimeout = time.Hour
)

var (
	ffmpegOnce sync.Once
	ffmpegPath string
)

func ffmpegBinary() string {
	ffmpegOnce.Do(func() {
		if custom := config.Value("MUSIC_FFMPEG_PATH"); custom != "" {
			if _, err := os.Stat(custom); err == nil {
				ffmpegPath = custom
				return
			}
			logErr("ffmpeg", fmt.Errorf("MUSIC_FFMPEG_PATH points at %q, which does not exist", custom))
		}
		if found, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpegPath = found
		}
	})
	return ffmpegPath
}

func TranscodingAvailable() bool { return ffmpegBinary() != "" }

func compatibleCopyBytes(ext string, duration float64) int64 {
	if !needsCompatibleCopy[strings.ToLower(ext)] || duration <= 0 ||
		config.Flag("MUSIC_DISABLE_TRANSCODE") || !TranscodingAvailable() {
		return 0
	}
	return int64(duration * compatibleKbps * 1000 / 8)
}

type compatibleCopy struct {
	relPath string
	mime    string
	size    int64
}

func (s *Server) makeCompatibleCopy(parent context.Context, trackID, originalRel string) *compatibleCopy {
	ext := strings.ToLower(filepath.Ext(originalRel))
	if !needsCompatibleCopy[ext] {
		return nil
	}
	if config.Flag("MUSIC_DISABLE_TRANSCODE") {
		return nil
	}
	bin := ffmpegBinary()
	if bin == "" {
		logErr("transcode", fmt.Errorf(
			"%s needs a compatible copy for iOS but ffmpeg was not found; "+
				"install ffmpeg or set MUSIC_FFMPEG_PATH", ext))
		return nil
	}

	relPath := filepath.Join("uploads", trackID+compatibleExt)
	outPath := filepath.Join(s.dataDir, relPath)
	tmpPath := outPath + ".part"

	ctx, cancel := context.WithTimeout(parent, transcodeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-loglevel", "error",
		"-i", filepath.Join(s.dataDir, originalRel),
		"-vn",
		"-c:a", "aac", "-b:a", strconv.Itoa(compatibleKbps)+"k",
		"-map_metadata", "0",
		"-map_metadata", "0:s:0",
		"-f", "mp4", "-movflags", "+faststart",
		"-y", tmpPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmpPath)
		logErr("transcode", fmt.Errorf("ffmpeg failed for %s: %v: %s",
			originalRel, err, strings.TrimSpace(string(out))))
		return nil
	}

	info, err := os.Stat(tmpPath)
	if err != nil || info.Size() == 0 {
		os.Remove(tmpPath)
		logErr("transcode", fmt.Errorf("ffmpeg produced no output for %s", originalRel))
		return nil
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		os.Remove(tmpPath)
		logErr("transcode", fmt.Errorf("could not store compatible copy: %w", err))
		return nil
	}

	return &compatibleCopy{relPath: relPath, mime: compatibleMime, size: info.Size()}
}

func (s *Server) backfillCompatibleCopies() {
	if !TranscodingAvailable() || config.Flag("MUSIC_DISABLE_TRANSCODE") {
		return
	}
	rows, err := s.db.Query(`SELECT id, file_path, COALESCE(alt_path, '') FROM tracks`)
	if err != nil {
		logErr("find missing compatible copies", err)
		return
	}
	type missing struct{ id, path string }
	var todo []missing
	for rows.Next() {
		var id, path, alt string
		if err := rows.Scan(&id, &path, &alt); err != nil {
			continue
		}
		if !needsCompatibleCopy[strings.ToLower(filepath.Ext(path))] {
			continue
		}
		if alt != "" {
			if _, err := os.Stat(filepath.Join(s.dataDir, alt)); err == nil {
				continue
			}
		}
		todo = append(todo, missing{id, path})
	}
	rows.Close()
	if len(todo) == 0 {
		return
	}
	log.Printf("transcode: making %d missing compatible copies", len(todo))
	for _, t := range todo {
		s.startCompatibleCopy(t.id, t.path)
	}
}
