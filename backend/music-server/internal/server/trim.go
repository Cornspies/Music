package server

import (
	"context"
	"fmt"
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
)

// Trimming.
//
// Rips often carry a second or two of silence at either end, or a chunk of
// applause on a live recording. An uploader or admin can cut the start and
// end of a track; the audio is cut with a stream copy (no re-encode, so a
// lossy file loses nothing more) and everything derived from it — the
// compatible copy, loudness and waveform, synced lyrics timing — is redone.
// The untouched original is kept beside the file so a cut can be undone.

const (
	trimTimeout = 2 * time.Minute
	// Silence is anything this quiet for at least this long.
	silenceThresholdDB = "-45dB"
	silenceMinSeconds  = "0.3"
)

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
	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-hide_banner", "-nostats",
		"-i", filepath.Join(s.dataDir, filePath),
		"-af", "silencedetect=noise="+silenceThresholdDB+":d="+silenceMinSeconds,
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() != nil {
		writeErr(w, http.StatusGatewayTimeout, "silence detection took too long")
		return
	}
	info := SilenceInfo{Duration: t.Duration, TrailingStart: t.Duration}
	starts := silenceStartRe.FindAllStringSubmatch(string(out), -1)
	ends := silenceEndRe.FindAllStringSubmatch(string(out), -1)
	for i, m := range starts {
		start, _ := strconv.ParseFloat(m[1], 64)
		// ffmpeg reports an end for every silence, including one that runs
		// to the end of the file (its end is then the file's end).
		end := t.Duration
		if i < len(ends) {
			end, _ = strconv.ParseFloat(ends[i][1], 64)
		}
		switch {
		case start <= 0.05 && i == 0:
			// Silence from the very start: the music begins where it ends.
			info.LeadingEnd = end
		case end >= t.Duration-0.15 && start > info.LeadingEnd:
			// Silence running to the very end: the music stops where it starts.
			info.TrailingStart = start
		}
	}
	writeJSON(w, http.StatusOK, info)
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

	// Keep the extension: ffmpeg picks the output container from it.
	ext := filepath.Ext(abs)
	tmp := strings.TrimSuffix(abs, ext) + ".trim" + ext
	ctx, cancel := context.WithTimeout(r.Context(), trimTimeout)
	defer cancel()
	args := []string{"-nostdin", "-loglevel", "error", "-y"}
	if body.Start > 0 {
		args = append(args, "-ss", formatSeconds(body.Start))
	}
	if body.End < t.Duration-0.05 {
		args = append(args, "-to", formatSeconds(body.End))
	}
	// Stream copy: cuts land on frame boundaries (tens of milliseconds),
	// which is plenty for silence, and the audio isn't re-encoded.
	args = append(args, "-i", abs, "-map", "0:a:0", "-c", "copy", "-map_metadata", "0", tmp)
	cmd := exec.CommandContext(ctx, bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		logErr("trim", fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out))))
		writeErr(w, http.StatusBadGateway, "ffmpeg couldn't cut this file")
		return
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		s.internalErr(w, "replace audio", err)
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
	s.afterAudioChanged(id, filePath, 0)
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
	abs := filepath.Join(s.dataDir, filePath)
	var size int64
	if fi, err := os.Stat(abs); err == nil {
		size = fi.Size()
	}
	duration := probeDuration(abs)
	hash, _ := fileSHA256(abs)
	_, _ = s.db.Exec(`UPDATE tracks SET duration = ?, file_size = ?, file_hash = ?, loudness = NULL, waveform_json = '', analyzed_at = '', updated_at = ? WHERE id = ?`,
		duration, size, hash, nowRFC3339(), id)

	// The compatible copy was made from the old audio.
	if alt := s.altPathFor(id); alt != "" {
		os.Remove(filepath.Join(s.dataDir, alt))
		_, _ = s.db.Exec(`UPDATE tracks SET alt_path = '', alt_mime = '', alt_size = 0 WHERE id = ?`, id)
	}
	s.startCompatibleCopy(id, filePath)
	s.queueAnalysis(id)

	if lyricsShift > 0 {
		s.shiftSyncedLyrics(id, -lyricsShift)
	}
}

// shiftSyncedLyrics moves every LRC time tag by delta seconds; lines that
// would land before the start are clamped to 0.
func (s *Server) shiftSyncedLyrics(trackID string, delta float64) {
	var synced string
	if err := s.db.QueryRow(`SELECT synced FROM lyrics WHERE track_id = ?`, trackID).Scan(&synced); err != nil || synced == "" {
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
