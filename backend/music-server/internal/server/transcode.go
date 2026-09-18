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

// Some formats this server happily stores cannot be decoded everywhere. Ogg
// (Vorbis/Opus) is the big one: fine in browsers and on Android, silent on iOS,
// because AVFoundation has never supported the Ogg container. WMA and WebM are
// in the same boat.
//
// For those, a second copy is made at upload time in AAC/M4A, which every
// platform plays. The original is never touched and stays the default source —
// the copy exists only for clients that would otherwise get silence.
//
// Note this is a lossy-to-lossy conversion for Ogg sources, so it costs one
// generation of quality. That is the price of playing at all on iOS. Lossless
// sources (FLAC, ALAC, WAV, AIFF) are deliberately left alone: FLAC already
// plays on iOS 11+, and re-encoding lossless audio to AAC would throw away
// quality for no reason.
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
	compatibleExt  = ".m4a"
	compatibleMime = "audio/mp4"
	// Transcoding is CPU-bound and runs inside the upload request, so it is
	// bounded: a very long file is better left unconverted than holding the
	// connection open indefinitely.
	transcodeTimeout = 10 * time.Minute
)

// ffmpegOnce caches the lookup — it's the same answer for the process lifetime
// and upload is not the place to be probing the filesystem repeatedly.
var (
	ffmpegOnce sync.Once
	ffmpegPath string
)

// ffmpegBinary returns the ffmpeg to use, or "" when none is available.
// MUSIC_FFMPEG_PATH overrides the PATH lookup for unusual installs.
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

// TranscodingAvailable reports whether compatible copies can be produced.
func TranscodingAvailable() bool { return ffmpegBinary() != "" }

// compatibleCopy is the result of a successful conversion.
type compatibleCopy struct {
	relPath string // relative to dataDir, as stored in tracks.alt_path
	mime    string
	size    int64
}

// makeCompatibleCopy converts an uploaded file to AAC/M4A when the format needs
// it and ffmpeg is available. It is entirely best-effort: any failure means the
// track is stored with the original only, which is exactly how things behaved
// before this existed.
//
// Returns nil when no copy was made, for any reason.
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
	// Write to a temporary name and rename, so a killed ffmpeg can't leave a
	// truncated file that looks like a valid alternate.
	tmpPath := outPath + ".part"

	ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-loglevel", "error",
		"-i", filepath.Join(s.dataDir, originalRel),
		// Audio only: an .ogx may carry a video stream, which has no place in
		// an audio file and would break the m4a.
		"-vn",
		"-c:a", "aac", "-b:a", "256k",
		// Carry the tags across. Both forms are needed: Ogg keeps Vorbis
		// comments on the audio *stream*, while WebM/WMA put them at the
		// container level, and each mapping alone silently drops the other.
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
