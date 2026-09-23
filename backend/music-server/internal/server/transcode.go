package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	transcodeTimeout = 10 * time.Minute
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

type compatibleCopy struct {
	relPath string
	mime    string
	size    int64
}

func (s *Server) makeCompatibleCopy(trackID, originalRel string) *compatibleCopy {
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

	ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-loglevel", "error",
		"-i", filepath.Join(s.dataDir, originalRel),
		"-vn",
		"-c:a", "aac", "-b:a", "256k",
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
