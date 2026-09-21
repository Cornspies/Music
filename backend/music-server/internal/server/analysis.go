package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"music-server/internal/config"
)

// Audio analysis runs in the background after upload (and, on start-up, for
// anything uploaded before it existed). Two numbers come out of it:
//
//   - integrated loudness in LUFS (EBU R128, via ffmpeg's ebur128 filter),
//     so clients can normalise volume between quiet and loud masters;
//   - a coarse waveform: waveformBuckets peak values in 0-100, enough to draw
//     a seek bar, small enough to keep in the database.
//
// Both are best-effort: no ffmpeg, or a file ffmpeg can't decode, simply
// leaves the track without them, exactly as before the feature existed.

const (
	waveformBuckets = 200
	// Decoded mono at this rate; three hours of audio is ~86 MB of samples,
	// and the read is capped below that anyway.
	waveformSampleRate = 4000
	maxWaveformBytes   = 96 << 20
	analysisTimeout    = 5 * time.Minute
	// Queue depth. Uploads past this are picked up by the start-up backfill.
	analysisQueueDepth = 4096
)

var analysisQueue = make(chan string, analysisQueueDepth)

// queueAnalysis schedules a track for loudness + waveform analysis. It never
// blocks the caller: a full queue means the next restart's backfill will get
// to it instead.
func (s *Server) queueAnalysis(trackID string) {
	if config.Flag("MUSIC_DISABLE_ANALYSIS") {
		return
	}
	select {
	case analysisQueue <- trackID:
	default:
		log.Printf("analysis: queue full, deferring track %s to next start", trackID)
	}
}

// StartBackgroundWork launches the analysis worker and queues every track
// that has never been analysed. Called once from main after the server is
// constructed.
func (s *Server) StartBackgroundWork() {
	s.purgeStaleLyricsMisses()
	go s.backfillCoverColors()
	s.startMetadataWorker()
	s.loadTrimmedTracks()
	s.startPendingUploadSweeper()
	go s.backfillFileHashes()
	if config.Flag("MUSIC_DISABLE_ANALYSIS") {
		log.Printf("music-server analysis: disabled")
		return
	}
	if !TranscodingAvailable() {
		log.Printf("music-server analysis: ffmpeg not found, loudness/waveform analysis is off")
		return
	}
	go s.analysisWorker()
	go s.backfillAnalysis()
}

func (s *Server) analysisWorker() {
	for id := range analysisQueue {
		// Share the CPU cap with transcoding: a bulk upload should not run
		// two ffmpegs per file on top of each other.
		transcodeSlots <- struct{}{}
		s.analyzeTrack(id)
		<-transcodeSlots
	}
}

func (s *Server) backfillAnalysis() {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE COALESCE(analyzed_at, '') = '' ORDER BY created_at ASC`)
	if err != nil {
		logErr("analysis backfill", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	log.Printf("analysis: %d track(s) to analyse in the background", len(ids))
	for _, id := range ids {
		analysisQueue <- id // blocking is fine here, this is its own goroutine
	}
}

// analyzeTrack runs both passes and records the result. Failures are logged
// and the track is marked as attempted so it isn't retried on every start.
func (s *Server) analyzeTrack(trackID string) {
	var rel string
	if err := s.db.QueryRow(`SELECT file_path FROM tracks WHERE id = ?`, trackID).Scan(&rel); err != nil {
		return // deleted while queued
	}
	abs := filepath.Join(s.dataDir, rel)

	ctx, cancel := context.WithTimeout(context.Background(), analysisTimeout)
	defer cancel()

	var loudness *float64
	if v, err := measureLoudness(ctx, abs); err == nil {
		loudness = &v
	} else {
		logErr("analysis loudness", fmt.Errorf("track %s: %w", trackID, err))
	}
	peaksJSON := ""
	if peaks, err := computeWaveform(ctx, abs); err == nil {
		if b, err := json.Marshal(peaks); err == nil {
			peaksJSON = string(b)
		}
	} else {
		logErr("analysis waveform", fmt.Errorf("track %s: %w", trackID, err))
	}

	now := nowRFC3339()
	var loud any
	if loudness != nil {
		loud = *loudness
	}
	res, err := s.db.Exec(`UPDATE tracks SET loudness = ?, waveform_json = ?, analyzed_at = ?, updated_at = ? WHERE id = ?`,
		loud, peaksJSON, now, now, trackID)
	if err != nil {
		logErr("analysis record", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 1 && (loudness != nil || peaksJSON != "") {
		l := "n/a"
		if loudness != nil {
			l = fmt.Sprintf("%.1f LUFS", *loudness)
		}
		log.Printf("analysis: track %s done (loudness %s, waveform %v)", trackID, l, peaksJSON != "")
	}
}

var integratedLoudnessRE = regexp.MustCompile(`I:\s+(-?[0-9]+(?:\.[0-9]+)?)\s+LUFS`)

// measureLoudness returns the integrated loudness of a file in LUFS.
func measureLoudness(ctx context.Context, path string) (float64, error) {
	bin := ffmpegBinary()
	if bin == "" {
		return 0, fmt.Errorf("ffmpeg not available")
	}
	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-hide_banner", "-loglevel", "info",
		"-i", path, "-vn", "-af", "ebur128=framelog=quiet", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffmpeg: %v: %s", err, lastLine(stderr.String()))
	}
	matches := integratedLoudnessRE.FindAllStringSubmatch(stderr.String(), -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("no loudness in ffmpeg output")
	}
	v, err := strconv.ParseFloat(matches[len(matches)-1][1], 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("unparseable loudness")
	}
	// -70 LUFS is ebur128's floor and means silence; not useful for gain.
	if v <= -69.9 {
		return 0, fmt.Errorf("track is silent")
	}
	return v, nil
}

// computeWaveform decodes the file to low-rate mono PCM and reduces it to
// waveformBuckets peak values scaled 0-100.
func computeWaveform(ctx context.Context, path string) ([]int, error) {
	bin := ffmpegBinary()
	if bin == "" {
		return nil, fmt.Errorf("ffmpeg not available")
	}
	cmd := exec.CommandContext(ctx, bin,
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", path, "-vn", "-ac", "1", "-ar", strconv.Itoa(waveformSampleRate),
		"-f", "s16le", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, maxWaveformBytes))
	// Drain anything past the cap so ffmpeg can finish instead of blocking
	// on a full pipe.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil && len(raw) == 0 {
		return nil, fmt.Errorf("ffmpeg: %v: %s", waitErr, lastLine(stderr.String()))
	}
	samples := len(raw) / 2
	if samples < waveformBuckets {
		return nil, fmt.Errorf("too short to draw")
	}

	peaks := make([]float64, waveformBuckets)
	per := samples / waveformBuckets
	for b := 0; b < waveformBuckets; b++ {
		start := b * per
		end := start + per
		if b == waveformBuckets-1 {
			end = samples
		}
		var maxAbs float64
		for i := start; i < end; i++ {
			v := float64(int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8))
			if v < 0 {
				v = -v
			}
			if v > maxAbs {
				maxAbs = v
			}
		}
		peaks[b] = maxAbs / 32768
	}
	// Normalise so the loudest bucket is full height; a quiet master would
	// otherwise draw as a flat line.
	var top float64
	for _, p := range peaks {
		if p > top {
			top = p
		}
	}
	out := make([]int, waveformBuckets)
	for i, p := range peaks {
		if top > 0 {
			p /= top
		}
		// Slight curve so quiet passages stay visible.
		out[i] = int(math.Round(math.Pow(p, 0.7) * 100))
	}
	return out, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Waveform is what GET /api/tracks/{id}/waveform returns.
type Waveform struct {
	// Peaks are waveformBuckets values in 0-100, left to right.
	Peaks []int `json:"peaks"`
}

func (s *Server) handleTrackWaveform(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var raw string
	err := s.db.QueryRow(`SELECT COALESCE(waveform_json, '') FROM tracks WHERE id = ?`, id).Scan(&raw)
	if err != nil || raw == "" {
		writeErr(w, http.StatusNotFound, "no waveform for this track")
		return
	}
	// Stable per track once analysed, so browsers may keep it.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"peaks":` + raw + `}`))
}
