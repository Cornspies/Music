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
	"time"

	"music-server/internal/config"
)

const (
	trimTimeout         = 2 * time.Minute
	silenceThresholdDB  = "-45dB"
	silenceMinSeconds   = "0.3"
	autoTrimThresholdDB = "-55dB"
	autoTrimMinSeconds  = 0.5
	autoTrimMargin      = 0.05
)

func autoTrimEnabled() bool {
	return !config.Flag("MUSIC_DISABLE_AUTO_TRIM") && ffmpegBinary() != ""
}

type SilenceInfo struct {
	Duration      float64 `json:"duration"`
	LeadingEnd    float64 `json:"leadingEnd"`
	TrailingStart float64 `json:"trailingStart"`
}

var (
	silenceStartRe = regexp.MustCompile(`silence_start: (-?[0-9.]+)`)
	silenceEndRe   = regexp.MustCompile(`silence_end: (-?[0-9.]+)`)
	decodedTimeRe  = regexp.MustCompile(`time=([0-9]+):([0-9]{2}):([0-9]{2}(?:\.[0-9]+)?)`)
)

func decodedLength(ffmpegOutput string) float64 {
	m := decodedTimeRe.FindAllStringSubmatch(ffmpegOutput, -1)
	if len(m) == 0 {
		return 0
	}
	last := m[len(m)-1]
	h, _ := strconv.Atoi(last[1])
	min, _ := strconv.Atoi(last[2])
	sec, _ := strconv.ParseFloat(last[3], 64)
	return float64(h*3600+min*60) + sec
}

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

func detectSilence(ctx context.Context, bin, abs, noise, minSeconds string, duration float64) (SilenceInfo, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-hide_banner", "-nostats",
		"-i", abs,
		"-af", "silencedetect=noise="+noise+":d="+minSeconds,
		"-f", "null", "-",
	)
	out, err := cmd.CombinedOutput()
	if d := decodedLength(string(out)); d > 0 {
		duration = d
	}
	info := SilenceInfo{Duration: duration, TrailingStart: duration}
	starts := silenceStartRe.FindAllStringSubmatch(string(out), -1)
	ends := silenceEndRe.FindAllStringSubmatch(string(out), -1)
	for i, m := range starts {
		start, _ := strconv.ParseFloat(m[1], 64)
		end := duration
		if i < len(ends) {
			end, _ = strconv.ParseFloat(ends[i][1], 64)
		}
		switch {
		case start <= 0.05 && i == 0:
			info.LeadingEnd = end
		case end >= duration-0.15 && start > info.LeadingEnd:
			info.TrailingStart = start
		}
	}
	return info, err
}

func cutAudio(ctx context.Context, bin, abs string, start, end, duration float64) error {
	ext := strings.ToLower(filepath.Ext(abs))
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
		Start       float64 `json:"start"`
		End         float64 `json:"end"`
		ShiftLyrics *bool   `json:"shiftLyrics"`
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
	ctx, cancel := context.WithTimeout(r.Context(), trimTimeout)
	defer cancel()
	if err := cutAudio(ctx, bin, abs, body.Start, body.End, t.Duration); err != nil {
		logErr("trim", err)
		writeErr(w, http.StatusBadGateway, "ffmpeg couldn't cut this file")
		return
	}
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

func (s *Server) afterAudioChanged(id, filePath string, lyricsShift float64) {
	s.recordNewAudio(id, filePath)
	_, _ = s.db.Exec(`UPDATE tracks SET loudness = NULL, waveform_json = '', analyzed_at = '', edges_checked = 2, codec = '' WHERE id = ?`, id)
	s.queueAnalysis(id)
	if lyricsShift > 0 {
		s.shiftSyncedLyrics(id, -lyricsShift, false)
	}
}

func (s *Server) recordNewAudio(id, filePath string) {
	abs := filepath.Join(s.dataDir, filePath)
	var size int64
	if fi, err := os.Stat(abs); err == nil {
		size = fi.Size()
	}
	_, _ = s.db.Exec(`UPDATE tracks SET duration = ?, file_size = ?, updated_at = ? WHERE id = ?`,
		probeDuration(abs), size, nowRFC3339(), id)
	stopCompatibleCopy(id)
	if alt := s.altPathFor(id); alt != "" {
		os.Remove(filepath.Join(s.dataDir, alt))
		_, _ = s.db.Exec(`UPDATE tracks SET alt_path = '', alt_mime = '', alt_size = 0 WHERE id = ?`, id)
	}
	s.startCompatibleCopy(id, filePath)
}

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
	duration = info.Duration
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
		return false, nil
	}
	if err := cutAudio(ctx, bin, abs, start, end, duration); err != nil {
		return false, err
	}
	s.recordNewAudio(id, rel)
	if start > 0 {
		s.shiftSyncedLyrics(id, -start, true)
	}
	log.Printf("trim: track %s — cut %.2fs of silence from the start, %.2fs from the end", id, start, duration-end)
	var title string
	_ = s.db.QueryRow(`SELECT title FROM tracks WHERE id = ?`, id).Scan(&title)
	recentCuts.add(TrimEvent{At: nowRFC3339(), TrackID: id, Title: title, FromStart: start, FromEnd: duration - end})
	return true, nil
}

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

func (s *Server) removeTrimBackups() {
	dir := filepath.Join(s.dataDir, "uploads")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var removed int
	var freed int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".orig") {
			continue
		}
		var size int64
		if info, err := e.Info(); err == nil {
			size = info.Size()
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			logErr("remove trim backup", err)
			continue
		}
		removed++
		freed += size
	}
	if removed > 0 {
		log.Printf("trim: removed %d untrimmed backups (%.1f MB)", removed, float64(freed)/(1<<20))
	}
}
