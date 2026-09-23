package server

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"
)

// Trimming.
//
// Rips often carry a second or two of silence at either end, or a chunk of
// applause on a live recording. Two ways to cut:
//
//   - Automatically: the first time a track is analysed (every upload, and
//     once for everything already in the library), silence at either end is
//     cut off for good — no copy of the original is kept. The threshold is
//     strict (-60 dB, near digital silence, for at least half a second), so
//     a quiet intro or the tail of a fade-out stays; only dead air goes.
//     MUSIC_DISABLE_AUTO_TRIM switches it off.
//   - By hand: an uploader or admin picks the cut points in the trim dialog.
//     That keeps the untouched original beside the file, so it can be undone.
//
// Either way the audio isn't re-encoded where that would cost quality (see
// cutAudio), embedded cover art and tags are kept, and everything derived
// from the audio — the compatible copy, loudness and waveform, synced lyrics
// timing — is redone. The stored file hash stays the one of the upload, so
// uploading the same file again is still recognised as a duplicate.

const (
	trimTimeout = 2 * time.Minute
	// For the trim dialog's "Detect silence": anything this quiet for at
	// least this long, suggested to the person, who decides.
	silenceThresholdDB = "-45dB"
	silenceMinSeconds  = "0.3"
	// For the automatic trim, which decides alone: near digital silence,
	// for at least half a second, and a little of it left in place.
	autoTrimThresholdDB = "-60dB"
	autoTrimMinSeconds  = 0.5
	autoTrimMargin      = 0.05
)

func autoTrimEnabled() bool {
	return !config.Flag("MUSIC_DISABLE_AUTO_TRIM") && ffmpegBinary() != ""
}

// SilenceInfo is GET /api/tracks/{id}/silence: how much silence the file
// starts and ends with, as suggested cut points.
type SilenceInfo struct {
	Duration float64 `json:"duration"`
	// LeadingEnd is where sound first starts (0 when it starts at once).
	LeadingEnd float64 `json:"leadingEnd"`
	// TrailingStart is where the sound last stops (== duration when it runs to the end).
	TrailingStart float64 `json:"trailingStart"`
}

var (
	silenceStartRe = regexp.MustCompile(`silence_start: (-?[0-9.]+)`)
	silenceEndRe   = regexp.MustCompile(`silence_end: (-?[0-9.]+)`)
)

func (s *Server) handleTrackSilence(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, filePath, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	bin := ffmpegBinary()
	if bin == "" {
		writeErr(w, http.StatusConflict, "ffmpeg is not available on this server")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), trimTimeout)
	defer cancel()
	// It decodes the whole file, so it waits its turn with the other ffmpeg
	// work: requests can't pile up processes.
	select {
	case transcodeSlots <- struct{}{}:
		defer func() { <-transcodeSlots }()
	case <-ctx.Done():
		writeErr(w, http.StatusServiceUnavailable, "the server is busy; try again in a moment")
		return
	}
	info, err := detectSilence(ctx, bin, filepath.Join(s.dataDir, filePath), silenceThresholdDB, silenceMinSeconds, t.Duration)
	if err != nil && ctx.Err() != nil {
		writeErr(w, http.StatusGatewayTimeout, "silence detection took too long")
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// detectSilence finds the silence at the start and the end of a file:
// anything quieter than noise (e.g. "-45dB") for at least minSeconds.
// duration is the file's length, used to tell a silence that runs to the
// end of the file.
func detectSilence(ctx context.Context, bin, abs, noise, minSeconds string, duration float64) (SilenceInfo, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-hide_banner", "-nostats",
		"-i", abs,
		"-af", "silencedetect=noise="+noise+":d="+minSeconds,
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	info := SilenceInfo{Duration: duration, TrailingStart: duration}
	starts := silenceStartRe.FindAllStringSubmatch(string(out), -1)
	ends := silenceEndRe.FindAllStringSubmatch(string(out), -1)
	for i, m := range starts {
		start, _ := strconv.ParseFloat(m[1], 64)
		// ffmpeg reports an end for every silence, including one that runs
		// to the end of the file (its end is then the file's end).
		end := duration
		if i < len(ends) {
			end, _ = strconv.ParseFloat(ends[i][1], 64)
		}
		switch {
		case start <= 0.05 && i == 0:
			// Silence from the very start: the music begins where it ends.
			info.LeadingEnd = end
		case end >= duration-0.15 && start > info.LeadingEnd:
			// Silence running to the very end: the music stops where it starts.
			info.TrailingStart = start
		}
	}
	return info, err
}

// cutAudio keeps [start, end] of the file at abs, replacing it. How depends
// on the format, because each has its own way of going wrong:
//
//   - most (MP3, AAC/M4A, WAV, …): a stream copy, so lossy audio isn't
//     encoded a second time; cuts land on frame boundaries, tens of
//     milliseconds, plenty for silence;
//   - FLAC: re-encoded to FLAC, which is lossless, because a stream copy
//     keeps the old length in the header and players then show and seek by
//     the wrong duration;
//   - Opus and the other Ogg/WebM audio: a stream copy that seeks on the
//     output side, because seeking on the input side lands on page
//     boundaries and leaves the file longer than its header says. Those
//     containers can't carry a cover picture stream anyway.
//
// Cover art embedded in the file and the tags are kept.
func cutAudio(ctx context.Context, bin, abs string, start, end, duration float64) error {
	ext := strings.ToLower(filepath.Ext(abs))
	// Keep the extension: ffmpeg picks the output container from it.
	tmp := strings.TrimSuffix(abs, filepath.Ext(abs)) + ".trim" + filepath.Ext(abs)
	var seek []string
	if start > 0 {
		seek = append(seek, "-ss", formatSeconds(start))
	}
	if end < duration-0.05 {
		seek = append(seek, "-to", formatSeconds(end))
	}
	args := []string{"-nostdin", "-loglevel", "error", "-y"}
	switch ext {
	case ".ogg", ".oga", ".opus", ".ogx", ".spx", ".webm", ".weba":
		args = append(args, "-i", abs)
		args = append(args, seek...)
		args = append(args, "-map", "0:a:0", "-c", "copy")
	case ".flac":
		args = append(args, seek...)
		args = append(args, "-i", abs, "-map", "0:a:0", "-map", "0:v?", "-c:a", "flac", "-c:v", "copy")
	default:
		args = append(args, seek...)
		args = append(args, "-i", abs, "-map", "0:a:0", "-map", "0:v?", "-c", "copy")
	}
	args = append(args, "-map_metadata", "0", tmp)
	if out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// handleTrackTrim: POST /api/tracks/{id}/trim {"start": s, "end": e} keeps
// the audio between start and end (seconds; end 0 = to the end).
func (s *Server) handleTrackTrim(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	t, filePath, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit it")
		return
	}
	var body struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		// ShiftLyrics moves synced lyrics along with a cut at the start;
		// omitted means yes. Off is for files whose lyrics were timed to the
		// audio proper rather than to the silence in front of it.
		ShiftLyrics *bool `json:"shiftLyrics"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if body.End <= 0 || body.End > t.Duration {
		body.End = t.Duration
	}
	if body.Start < 0 || body.Start >= body.End-1 {
		writeErr(w, http.StatusBadRequest, "the cut would leave less than a second of audio")
		return
	}
	if body.Start < 0.05 && body.End >= t.Duration-0.05 {
		writeErr(w, http.StatusBadRequest, "nothing to cut")
		return
	}
	bin := ffmpegBinary()
	if bin == "" {
		writeErr(w, http.StatusConflict, "ffmpeg is not available on this server")
		return
	}

	abs := filepath.Join(s.dataDir, filePath)
	// The first cut keeps the original; later cuts work on the cut file but
	// "restore" always goes back to what was uploaded.
	backup := abs + ".orig"
	if _, err := os.Stat(backup); err != nil {
		if err := copyFile(abs, backup); err != nil {
			s.internalErr(w, "back up original", err)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), trimTimeout)
	defer cancel()
	if err := cutAudio(ctx, bin, abs, body.Start, body.End, t.Duration); err != nil {
		logErr("trim", err)
		writeErr(w, http.StatusBadGateway, "ffmpeg couldn't cut this file")
		return
	}
	setTrimmed(id, true)
	lyricsShift := body.Start
	if body.ShiftLyrics != nil && !*body.ShiftLyrics {
		lyricsShift = 0
	}
	s.afterAudioChanged(id, filePath, lyricsShift)
	out, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleTrackRestore: POST /api/tracks/{id}/trim/restore puts the uploaded
// original back.
func (s *Server) handleTrackRestore(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	t, filePath, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit it")
		return
	}
	abs := filepath.Join(s.dataDir, filePath)
	backup := abs + ".orig"
	if _, err := os.Stat(backup); err != nil {
		writeErr(w, http.StatusNotFound, "this track hasn't been cut")
		return
	}
	if err := os.Rename(backup, abs); err != nil {
		s.internalErr(w, "restore audio", err)
		return
	}
	setTrimmed(id, false)
	s.afterAudioChanged(id, filePath, 0) // also marks it checked: the original is wanted as it is
	// Lyrics timing can't be shifted back without knowing the old cut, so
	// they're looked up afresh.
	_, _ = s.db.Exec(`DELETE FROM lyrics WHERE track_id = ? AND source <> ?`, id, lyricsSourceManual)
	out, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// afterAudioChanged re-derives everything that depends on the audio bytes:
// duration and size, the hash, the compatible copy, loudness + waveform,
// and synced lyrics, which move earlier by lyricsShift seconds (the cut at
// the start, or 0 to leave their timing alone).
func (s *Server) afterAudioChanged(id, filePath string, lyricsShift float64) {
	s.recordNewAudio(id, filePath)
	// A cut by hand settles the question the automatic trim would ask.
	_, _ = s.db.Exec(`UPDATE tracks SET loudness = NULL, waveform_json = '', analyzed_at = '', edges_checked = 1 WHERE id = ?`, id)
	s.queueAnalysis(id)
	if lyricsShift > 0 {
		s.shiftSyncedLyrics(id, -lyricsShift, false)
	}
}

// recordNewAudio updates what the database knows about a track's file after
// its audio changed — length and size — and redoes the compatible copy,
// which was made from the old audio. The file hash is left alone: it names
// the uploaded file, so uploading that file again is still recognised.
func (s *Server) recordNewAudio(id, filePath string) {
	abs := filepath.Join(s.dataDir, filePath)
	var size int64
	if fi, err := os.Stat(abs); err == nil {
		size = fi.Size()
	}
	_, _ = s.db.Exec(`UPDATE tracks SET duration = ?, file_size = ?, updated_at = ? WHERE id = ?`,
		probeDuration(abs), size, nowRFC3339(), id)
	if alt := s.altPathFor(id); alt != "" {
		os.Remove(filepath.Join(s.dataDir, alt))
		_, _ = s.db.Exec(`UPDATE tracks SET alt_path = '', alt_mime = '', alt_size = 0 WHERE id = ?`, id)
	}
	s.startCompatibleCopy(id, filePath)
}

// trimSilentEdges cuts the silence off both ends of a track for good — no
// copy of the original is kept — and reports whether it cut anything. The
// analysis worker runs it the first time it sees a track, before measuring,
// so loudness and waveform describe the trimmed audio.
func (s *Server) trimSilentEdges(ctx context.Context, id, rel string) (bool, error) {
	bin := ffmpegBinary()
	abs := filepath.Join(s.dataDir, rel)
	duration := probeDuration(abs)
	if duration <= 0 {
		return false, fmt.Errorf("couldn't read the length of %s", rel)
	}
	info, err := detectSilence(ctx, bin, abs, autoTrimThresholdDB, strconv.FormatFloat(autoTrimMinSeconds, 'f', -1, 64), duration)
	if err != nil {
		return false, err
	}
	start, end := 0.0, duration
	if info.LeadingEnd >= autoTrimMinSeconds {
		start = info.LeadingEnd - autoTrimMargin
	}
	if duration-info.TrailingStart >= autoTrimMinSeconds {
		end = info.TrailingStart + autoTrimMargin
	}
	if start <= 0 && end >= duration {
		return false, nil
	}
	if end-start < 1 {
		return false, nil // (nearly) nothing but silence: not ours to judge
	}
	if err := cutAudio(ctx, bin, abs, start, end, duration); err != nil {
		return false, err
	}
	s.recordNewAudio(id, rel)
	// Lyrics from the file's tag or from LRCLIB are timed to the release,
	// which doesn't carry a rip's dead air — they fit better now as they
	// are. Only lyrics typed in by hand, against this very file, move.
	if start > 0 {
		s.shiftSyncedLyrics(id, -start, true)
	}
	log.Printf("trim: track %s — cut %.2fs of silence from the start, %.2fs from the end", id, start, duration-end)
	return true, nil
}

// shiftSyncedLyrics moves every LRC time tag by delta seconds; lines that
// would land before the start are clamped to 0. onlyManual limits it to
// lyrics someone typed in by hand.
func (s *Server) shiftSyncedLyrics(trackID string, delta float64, onlyManual bool) {
	var synced, source string
	if err := s.db.QueryRow(`SELECT synced, source FROM lyrics WHERE track_id = ?`, trackID).Scan(&synced, &source); err != nil || synced == "" {
		return
	}
	if onlyManual && source != lyricsSourceManual {
		return
	}
	shifted := lrcStamp.ReplaceAllStringFunc(synced, func(tag string) string {
		m := lrcStamp.FindStringSubmatch(tag)
		min, _ := strconv.Atoi(m[1])
		sec, _ := strconv.ParseFloat(m[2], 64)
		t := math.Max(0, float64(min)*60+sec+delta)
		return fmt.Sprintf("[%02d:%05.2f]", int(t)/60, t-float64(int(t)/60*60))
	})
	_, _ = s.db.Exec(`UPDATE lyrics SET synced = ? WHERE track_id = ?`, shifted, trackID)
}

// probeDuration asks ffprobe (next to ffmpeg) for the length of the file.
func probeDuration(path string) float64 {
	bin := ffmpegBinary()
	if bin == "" {
		return 0
	}
	probe := filepath.Join(filepath.Dir(bin), "ffprobe")
	if _, err := os.Stat(probe); err != nil {
		probe = "ffprobe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

func formatSeconds(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}

// trimmedSet remembers which tracks have a kept original, so the track JSON
// can say so without a stat() per row. Filled at start-up from the uploads
// directory and kept current by trim/restore.
type trimmedSet struct {
	sync.RWMutex
	ids map[string]bool
}

func (t *trimmedSet) has(id string) bool {
	t.RLock()
	defer t.RUnlock()
	return t.ids[id]
}

var trimmedTracks = &trimmedSet{ids: map[string]bool{}}

func (s *Server) loadTrimmedTracks() {
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "uploads"))
	if err != nil {
		return
	}
	trimmedTracks.Lock()
	defer trimmedTracks.Unlock()
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".orig") {
			base := strings.TrimSuffix(name, ".orig")
			trimmedTracks.ids[strings.TrimSuffix(base, filepath.Ext(base))] = true
		}
	}
}

func setTrimmed(id string, v bool) {
	trimmedTracks.Lock()
	defer trimmedTracks.Unlock()
	if v {
		trimmedTracks.ids[id] = true
	} else {
		delete(trimmedTracks.ids, id)
	}
}
