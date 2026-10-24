package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"music-server/internal/config"
)

// trimRule decides what counts as silence at a track's edges. Admins set it
// on the admin page; it applies to new uploads and to a library re-check.
type trimRule struct {
	ThresholdDB   float64 `json:"thresholdDb"`
	MinSeconds    float64 `json:"minSeconds"`
	MarginSeconds float64 `json:"marginSeconds"`
}

var defaultTrimRule = trimRule{ThresholdDB: -55, MinSeconds: 0.5, MarginSeconds: 0.05}

const settingTrimRule = "trim_rule"

func (r trimRule) validate() error {
	switch {
	case r.ThresholdDB < -90 || r.ThresholdDB > -20:
		return fmt.Errorf("the silence level must be between -90 and -20 dB")
	case r.MinSeconds < 0.1 || r.MinSeconds > 10:
		return fmt.Errorf("the shortest silence must be between 0.1 and 10 seconds")
	case r.MarginSeconds < 0 || r.MarginSeconds >= r.MinSeconds:
		return fmt.Errorf("the margin must be at least 0 and shorter than the shortest silence")
	}
	return nil
}

func (r trimRule) String() string {
	return fmt.Sprintf("%gdB for %gs", r.ThresholdDB, r.MinSeconds)
}

func (r trimRule) ffmpegThreshold() string {
	return strconv.FormatFloat(r.ThresholdDB, 'f', -1, 64) + "dB"
}

func (s *Server) trimRule() trimRule {
	raw, ok := s.getSetting(settingTrimRule)
	if !ok {
		return defaultTrimRule
	}
	var r trimRule
	if json.Unmarshal([]byte(raw), &r) != nil || r.validate() != nil {
		return defaultTrimRule
	}
	return r
}

// The feeder keeps this much room free in the analysis queue, so uploads
// made during a library-wide run still get a place in it.
const analysisFeedDepth = 64

func feedAnalysis(ids []string) {
	for _, id := range ids {
		for len(analysisQueue) >= analysisFeedDepth {
			time.Sleep(time.Second)
		}
		analysisQueue <- id
	}
}

type AnalysisSettings struct {
	Rule            trimRule `json:"rule"`
	Default         trimRule `json:"default"`
	AnalysisEnabled bool     `json:"analysisEnabled"`
	TrimEnabled     bool     `json:"trimEnabled"`
	Tracks          int      `json:"tracks"`
	ManuallyTrimmed int      `json:"manuallyTrimmed"`
}

func (s *Server) analysisSettings() AnalysisSettings {
	analysisOn := !config.Flag("MUSIC_DISABLE_ANALYSIS") && TranscodingAvailable()
	st := AnalysisSettings{
		Rule:            s.trimRule(),
		Default:         defaultTrimRule,
		AnalysisEnabled: analysisOn,
		TrimEnabled:     analysisOn && autoTrimEnabled(),
	}
	_ = s.db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(edges_checked = 2), 0) FROM tracks`).Scan(&st.Tracks, &st.ManuallyTrimmed)
	return st
}

func (s *Server) handleAdminAnalysis(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.analysisSettings())
}

// handleAdminReanalyze saves the trim rule and queues the whole library
// again: the silence trim, the loudness and waveform analysis, or both.
// Tracks trimmed or restored by hand keep their edges.
func (s *Server) handleAdminReanalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule     trimRule `json:"rule"`
		Trim     bool     `json:"trim"`
		Loudness bool     `json:"loudness"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := req.Rule.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st := s.analysisSettings()
	switch {
	case !st.AnalysisEnabled:
		writeErr(w, http.StatusConflict, "analysis is off: it needs ffmpeg, and MUSIC_DISABLE_ANALYSIS must not be 1")
		return
	case req.Trim && !st.TrimEnabled:
		writeErr(w, http.StatusConflict, "silence trimming is turned off by MUSIC_DISABLE_AUTO_TRIM")
		return
	}

	raw, _ := json.Marshal(req.Rule)
	if err := s.putSetting(settingTrimRule, string(raw)); err != nil {
		logErr("save trim rule", err)
		writeErr(w, http.StatusInternalServerError, "couldn't save the settings")
		return
	}
	if req.Trim {
		if _, err := s.db.Exec(`UPDATE tracks SET edges_checked = 0 WHERE edges_checked = 1`); err != nil {
			logErr("re-check silence", err)
			writeErr(w, http.StatusInternalServerError, "couldn't start the re-check")
			return
		}
	}
	if req.Loudness {
		if _, err := s.db.Exec(`UPDATE tracks SET analyzed_at = ''`); err != nil {
			logErr("re-check loudness", err)
			writeErr(w, http.StatusInternalServerError, "couldn't start the re-check")
			return
		}
	}

	if req.Trim || req.Loudness {
		rows, err := s.db.Query(`SELECT id FROM tracks WHERE analyzed_at = '' OR edges_checked = 0 ORDER BY created_at ASC`)
		if err != nil {
			logErr("re-check library", err)
			writeErr(w, http.StatusInternalServerError, "couldn't start the re-check")
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
		log.Printf("analysis: %s re-checking %d track(s) (silence %v, loudness %v, rule %s)",
			userFrom(r).Username, len(ids), req.Trim, req.Loudness, req.Rule)
		go feedAnalysis(ids)
	}
	writeJSON(w, http.StatusOK, s.analysisSettings())
}
