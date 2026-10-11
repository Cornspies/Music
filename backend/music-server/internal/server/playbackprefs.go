package server

import (
	"net/http"
	"strings"
)

const (
	maxPlaybackPrefs   = 2000
	maxPrefContextSize = 700
)

type PlaybackPref struct {
	Context string `json:"context"`
	Shuffle string `json:"shuffle"`
	Repeat  string `json:"repeat"`
}

func validPref(p PlaybackPref) bool {
	switch {
	case p.Context == "" || len(p.Context) > maxPrefContextSize:
		return false
	case p.Shuffle != "off" && p.Shuffle != "random" && p.Shuffle != "norepeat":
		return false
	case p.Repeat != "off" && p.Repeat != "all" && p.Repeat != "one":
		return false
	}
	return true
}

func (s *Server) handleListPlaybackPrefs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT context, shuffle, repeat FROM playback_prefs WHERE user_id = ?`, userFrom(r).ID)
	if err != nil {
		s.internalErr(w, "list playback prefs", err)
		return
	}
	defer rows.Close()
	out := []PlaybackPref{}
	for rows.Next() {
		var p PlaybackPref
		if err := rows.Scan(&p.Context, &p.Shuffle, &p.Repeat); err != nil {
			s.internalErr(w, "scan playback pref", err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutPlaybackPref(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var p PlaybackPref
	if err := jsonDecodeStrict(r, w, &p); err != nil {
		return
	}
	p.Context = strings.TrimSpace(p.Context)
	if !validPref(p) {
		writeErr(w, http.StatusBadRequest, "that isn't a valid shuffle or repeat setting")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO playback_prefs (user_id, context, shuffle, repeat, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(user_id, context) DO UPDATE SET shuffle = excluded.shuffle, repeat = excluded.repeat, updated_at = excluded.updated_at`,
		u.ID, p.Context, p.Shuffle, p.Repeat, nowRFC3339()); err != nil {
		s.internalErr(w, "save playback pref", err)
		return
	}
	_, _ = s.db.Exec(`DELETE FROM playback_prefs WHERE user_id = ? AND context NOT IN (
		SELECT context FROM playback_prefs WHERE user_id = ? ORDER BY updated_at DESC LIMIT ?)`, u.ID, u.ID, maxPlaybackPrefs)
	writeNoContent(w)
}
