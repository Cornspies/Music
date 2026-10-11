package server

import (
	"net/http"
	"strings"
)

const (
	maxPins       = 200
	maxPinNameLen = 300
)

type LibraryPin struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Artist   string `json:"artist"`
	PinnedAt string `json:"pinnedAt"`
}

func cleanPin(kind, name, artist string) (LibraryPin, string) {
	p := LibraryPin{Kind: kind, Name: strings.TrimSpace(name), Artist: strings.TrimSpace(artist)}
	switch {
	case p.Kind != "artist" && p.Kind != "album":
		return p, "only artists and albums can be pinned"
	case p.Name == "":
		return p, "a name is needed"
	case len(p.Name) > maxPinNameLen || len(p.Artist) > maxPinNameLen:
		return p, "that name is too long"
	}
	if p.Kind == "artist" {
		p.Artist = ""
	}
	return p, ""
}

func (s *Server) handleListPins(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT kind, name, artist, created_at FROM library_pins WHERE user_id = ? ORDER BY created_at DESC`, userFrom(r).ID)
	if err != nil {
		s.internalErr(w, "list pins", err)
		return
	}
	defer rows.Close()
	out := []LibraryPin{}
	for rows.Next() {
		var p LibraryPin
		if err := rows.Scan(&p.Kind, &p.Name, &p.Artist, &p.PinnedAt); err != nil {
			s.internalErr(w, "scan pin", err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddPin(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Artist string `json:"artist"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	p, problem := cleanPin(body.Kind, body.Name, body.Artist)
	if problem != "" {
		writeErr(w, http.StatusBadRequest, problem)
		return
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM library_pins WHERE user_id = ?`, u.ID).Scan(&n); err != nil {
		s.internalErr(w, "count pins", err)
		return
	}
	if n >= maxPins {
		writeErr(w, http.StatusConflict, "you've pinned as much as fits; unpin something first")
		return
	}
	p.PinnedAt = nowRFC3339()
	if _, err := s.db.Exec(`INSERT INTO library_pins (user_id, kind, name, artist, created_at) VALUES (?,?,?,?,?)
		ON CONFLICT(user_id, kind, name, artist) DO NOTHING`, u.ID, p.Kind, p.Name, p.Artist, p.PinnedAt); err != nil {
		s.internalErr(w, "add pin", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleRemovePin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, problem := cleanPin(q.Get("kind"), q.Get("name"), q.Get("artist"))
	if problem != "" {
		writeErr(w, http.StatusBadRequest, problem)
		return
	}
	if _, err := s.db.Exec(`DELETE FROM library_pins WHERE user_id = ? AND kind = ? AND name = ? AND artist = ?`,
		userFrom(r).ID, p.Kind, p.Name, p.Artist); err != nil {
		s.internalErr(w, "remove pin", err)
		return
	}
	writeNoContent(w)
}
