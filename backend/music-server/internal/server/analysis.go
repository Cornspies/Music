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

const (
	waveformBuckets    = 200
	waveformSampleRate = 4000
	maxWaveformBytes   = 96 << 20
	analysisTimeout    = 5 * time.Minute
	analysisQueueDepth = 4096
)

var analysisQueue = make(chan string, analysisQueueDepth)

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

func (s *Server) StartBackgroundWork() {
	s.initDiscoverSources()
	s.startImportWorker()
	s.startShareCleanup()
	s.startListenSweeper()
	s.purgeStaleLyricsMisses()
	go s.backfillCoverColors()
	s.startMetadataWorker()
	s.removeTrimBackups()
	s.startPendingUploadSweeper()
	go s.backfillFileHashes()
	go s.repairMissingCovers()
	go s.shrinkStoredCovers()
	go s.backfillCompatibleCopies()
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
	if autoTrimEnabled() {
		s.recheckEdgesOnNewRule()
		go s.backfillEdgeChecks()
	}
}

const settingAutoTrimRule = "auto_trim_rule"

func (s *Server) recheckEdgesOnNewRule() {
	rule := autoTrimThresholdDB + " for " + strconv.FormatFloat(autoTrimMinSeconds, 'f', -1, 64) + "s"
	if prev, _ := s.getSetting(settingAutoTrimRule); prev == rule {
		return
	}
	res, err := s.db.Exec(`UPDATE tracks SET edges_checked = 0 WHERE edges_checked = 1`)
	if err != nil {
		logErr("silence trim rule", err)
		return
	}
	if err := s.putSetting(settingAutoTrimRule, rule); err != nil {
		logErr("silence trim rule", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("trim: the silence rule is now %s — checking %d track(s) again", rule, n)
	}
}

func (s *Server) backfillEdgeChecks() {
	defer logPanic("silence trim backfill")
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE edges_checked = 0 AND COALESCE(analyzed_at, '') <> '' ORDER BY created_at ASC`)
	if err != nil {
		logErr("silence trim backfill", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if len(ids) == 0 {
		return
	}
	log.Printf("trim: checking %d track(s) for silence at the start and end, in the background", len(ids))
	for _, id := range ids {
		analysisQueue <- id
	}
}

func (s *Server) analysisWorker() {
	for id := range analysisQueue {
		func() {
			transcodeSlots <- struct{}{}
			defer func() { <-transcodeSlots }()
			defer logPanic("analysis of track " + id)
			setAnalysing(id)
			defer setAnalysing("")
			s.analyzeTrack(id)
		}()
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
		analysisQueue <- id
	}
}

func (s *Server) analyzeTrack(trackID string) {
	var rel, analyzedAt, hash string
	var edgesChecked int
	if err := s.db.QueryRow(`SELECT file_path, COALESCE(analyzed_at, ''), edges_checked, file_hash FROM tracks WHERE id = ?`, trackID).
		Scan(&rel, &analyzedAt, &edgesChecked, &hash); err != nil {
		return
	}
	abs := filepath.Join(s.dataDir, rel)

	ctx, cancel := context.WithTimeout(context.Background(), analysisTimeout)
	defer cancel()

	if edgesChecked == 0 && autoTrimEnabled() {
		trimmed, err := s.trimSilentEdges(ctx, trackID, rel)
		if err != nil {
			logErr("trim silence", fmt.Errorf("track %s (checked again at the next start): %w", trackID, err))
		} else {
			_, _ = s.db.Exec(`UPDATE tracks SET edges_checked = 1 WHERE id = ?`, trackID)
		}
		if !trimmed && analyzedAt != "" {
			return
		}
	}

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
	res, err := s.db.Exec(`UPDATE tracks SET loudness = ?, waveform_json = ?, analyzed_at = ?, updated_at = ? WHERE id = ? AND file_path = ? AND file_hash = ?`,
		loud, peaksJSON, now, now, trackID, rel, hash)
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
	if v <= -69.9 {
		return 0, fmt.Errorf("track is silent")
	}
	return v, nil
}

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

type Waveform struct {
	Peaks []int `json:"peaks"`
}

func (s *Server) handleTrackWaveform(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var raw, updatedAt string
	err := s.db.QueryRow(`SELECT COALESCE(t.waveform_json, ''), COALESCE(t.updated_at, '') FROM tracks t
		WHERE t.id = ? AND `+visibleTo("t.user_id"), id, userFrom(r).ID).
		Scan(&raw, &updatedAt)
	if err != nil || raw == "" {
		writeErr(w, http.StatusNotFound, "no waveform for this track")
		return
	}
	etag := `"wave-` + id + `-` + updatedAt + `"`
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"peaks":` + raw + `}`))
}
