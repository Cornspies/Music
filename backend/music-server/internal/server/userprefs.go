package server

import (
	"net/http"
	"slices"
)

var userPrefValues = map[string][]string{
	"librarySort": {"created", "title", "artist", "plays", "recent"},
}

func (s *Server) handleListUserPrefs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT key, value FROM user_prefs WHERE user_id = ?`, userFrom(r).ID)
	if err != nil {
		s.internalErr(w, "list user prefs", err)
		return
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			s.internalErr(w, "scan user pref", err)
			return
		}
		if slices.Contains(userPrefValues[key], value) {
			out[key] = value
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutUserPref(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	allowed, ok := userPrefValues[key]
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown setting")
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if !slices.Contains(allowed, body.Value) {
		writeErr(w, http.StatusBadRequest, "that isn't a valid value for this setting")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO user_prefs (user_id, key, value, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		userFrom(r).ID, key, body.Value, nowRFC3339()); err != nil {
		s.internalErr(w, "save user pref", err)
		return
	}
	writeNoContent(w)
}
