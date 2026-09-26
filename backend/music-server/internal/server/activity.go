package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"music-server/internal/config"
)

type ActivityEvent struct {
	At      string `json:"at"`
	Context string `json:"context"`
	Message string `json:"message"`
	Count   int    `json:"count"`
}

type TrimEvent struct {
	At        string  `json:"at"`
	TrackID   string  `json:"trackId"`
	Title     string  `json:"title"`
	FromStart float64 `json:"fromStart"`
	FromEnd   float64 `json:"fromEnd"`
}

type recent[T any] struct {
	mu    sync.Mutex
	max   int
	items []T
}

func (r *recent[T]) add(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, v)
	if len(r.items) > r.max {
		r.items = append([]T(nil), r.items[len(r.items)-r.max:]...)
	}
}

func (r *recent[T]) newestFirst() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]T, len(r.items))
	for i, v := range r.items {
		out[len(r.items)-1-i] = v
	}
	return out
}

var recentCuts = &recent[TrimEvent]{max: 20}

var recentErrors = struct {
	sync.Mutex
	items []ActivityEvent
}{}

func noteError(ctx, msg string) {
	recentErrors.Lock()
	defer recentErrors.Unlock()
	e := ActivityEvent{At: nowRFC3339(), Context: ctx, Message: msg, Count: 1}
	for i, old := range recentErrors.items {
		if old.Context == ctx {
			e.Count += old.Count
			recentErrors.items = append(recentErrors.items[:i], recentErrors.items[i+1:]...)
			break
		}
	}
	recentErrors.items = append(recentErrors.items, e)
	if len(recentErrors.items) > 50 {
		recentErrors.items = append([]ActivityEvent(nil), recentErrors.items[len(recentErrors.items)-50:]...)
	}
}

func errorsNewestFirst() []ActivityEvent {
	recentErrors.Lock()
	defer recentErrors.Unlock()
	out := make([]ActivityEvent, len(recentErrors.items))
	for i, e := range recentErrors.items {
		out[len(recentErrors.items)-1-i] = e
	}
	return out
}

type SourceHealth struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	LastOK       string `json:"lastOk,omitempty"`
	LastError    string `json:"lastError,omitempty"`
	LastErrorAt  string `json:"lastErrorAt,omitempty"`
	FailingSince string `json:"failingSince,omitempty"`
}

var sourceHealth = struct {
	sync.Mutex
	byKey map[string]*SourceHealth
}{byKey: map[string]*SourceHealth{}}

func noteSource(key string, err error) {
	var status *statusError
	if err != nil && errors.As(err, &status) && status.code < http.StatusInternalServerError {
		err = nil
	}
	sourceHealth.Lock()
	defer sourceHealth.Unlock()
	h := sourceHealth.byKey[key]
	if h == nil {
		h = &SourceHealth{Key: key}
		sourceHealth.byKey[key] = h
	}
	now := nowRFC3339()
	if err == nil {
		h.LastOK, h.FailingSince = now, ""
		return
	}
	h.LastError, h.LastErrorAt = err.Error(), now
	if h.FailingSince == "" {
		h.FailingSince = now
	}
}

var analysing = struct {
	sync.Mutex
	trackID string
}{}

func setAnalysing(id string) {
	analysing.Lock()
	analysing.trackID = id
	analysing.Unlock()
}

type YTDLPInfo struct {
	Version   string `json:"version"`
	Path      string `json:"path"`
	JSRuntime string `json:"jsRuntime"`
}

var ytdlpInfoCache = struct {
	sync.Mutex
	at   time.Time
	info *YTDLPInfo
}{}

func (y *ytdlpSource) info() *YTDLPInfo {
	ytdlpInfoCache.Lock()
	defer ytdlpInfoCache.Unlock()
	if ytdlpInfoCache.info == nil || time.Since(ytdlpInfoCache.at) > 10*time.Minute {
		ytdlpInfoCache.info = &YTDLPInfo{Version: y.version(), Path: y.bin, JSRuntime: y.probeJSRuntime()}
		ytdlpInfoCache.at = time.Now()
	}
	return ytdlpInfoCache.info
}

type ActivityTrack struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type ActivityStatus struct {
	Silence struct {
		Enabled    bool        `json:"enabled"`
		Rule       string      `json:"rule"`
		Total      int         `json:"total"`
		Pending    int         `json:"pending"`
		RecentCuts []TrimEvent `json:"recentCuts"`
	} `json:"silence"`
	Analysis struct {
		Enabled bool           `json:"enabled"`
		Waiting int            `json:"waiting"`
		Current *ActivityTrack `json:"current"`
	} `json:"analysis"`
	Imports []PlaylistImport `json:"imports"`
	Sources []SourceHealth   `json:"sources"`
	YTDLP   *YTDLPInfo       `json:"ytdlp"`
	Disk    struct {
		LibraryBytes  int64  `json:"libraryBytes"`
		DatabaseBytes int64  `json:"databaseBytes"`
		FreeBytes     uint64 `json:"freeBytes"`
		TotalBytes    uint64 `json:"totalBytes"`
	} `json:"disk"`
	Errors []ActivityEvent `json:"errors"`
}

func (s *Server) handleAdminActivity(w http.ResponseWriter, _ *http.Request) {
	var st ActivityStatus
	analysisOn := !config.Flag("MUSIC_DISABLE_ANALYSIS") && TranscodingAvailable()

	st.Silence.Enabled = analysisOn && autoTrimEnabled()
	st.Silence.Rule = fmt.Sprintf("%s for %gs", autoTrimThresholdDB, autoTrimMinSeconds)
	_ = s.db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(edges_checked = 0), 0) FROM tracks`).Scan(&st.Silence.Total, &st.Silence.Pending)
	st.Silence.RecentCuts = recentCuts.newestFirst()

	st.Analysis.Enabled = analysisOn
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM tracks WHERE COALESCE(analyzed_at, '') = ''`).Scan(&st.Analysis.Waiting)
	analysing.Lock()
	current := analysing.trackID
	analysing.Unlock()
	if current != "" {
		var title string
		_ = s.db.QueryRow(`SELECT title FROM tracks WHERE id = ?`, current).Scan(&title)
		st.Analysis.Current = &ActivityTrack{ID: current, Title: title}
	}

	st.Imports = allImports()

	var youtube *ytdlpSource
	st.Sources = []SourceHealth{}
	sourceHealth.Lock()
	for _, src := range discoverSources {
		h := SourceHealth{Key: src.key(), Name: src.name()}
		if known := sourceHealth.byKey[src.key()]; known != nil {
			h = *known
			h.Name = src.name()
		}
		st.Sources = append(st.Sources, h)
		if y, ok := src.(*ytdlpSource); ok {
			youtube = y
		}
	}
	sourceHealth.Unlock()
	if youtube != nil {
		st.YTDLP = youtube.info()
	}

	_ = s.db.QueryRow(`SELECT COALESCE(SUM(file_size + alt_size), 0) FROM tracks`).Scan(&st.Disk.LibraryBytes)
	for _, name := range []string{"music.db", "music.db-wal"} {
		if fi, err := os.Stat(filepath.Join(s.dataDir, name)); err == nil {
			st.Disk.DatabaseBytes += fi.Size()
		}
	}
	st.Disk.FreeBytes, st.Disk.TotalBytes, _ = diskSpace(s.dataDir)

	st.Errors = errorsNewestFirst()

	writeJSON(w, http.StatusOK, st)
}

func allImports() []PlaylistImport {
	imports.Lock()
	defer imports.Unlock()
	out := []PlaylistImport{}
	for id, j := range imports.byID {
		if !j.active() && time.Since(j.finishedAt) > importRetention {
			delete(imports.byID, id)
			continue
		}
		out = append(out, j.snapshot())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StartedAt > out[b].StartedAt })
	return out
}
