package server

import (
	"net/http"
	"strings"
)

func (s *Server) handleTrackCopies(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, _, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	out := []Track{}
	seen := map[string]bool{t.ID: true}
	add := func(c *Track) {
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, *c)
		}
	}

	var hash string
	if err := s.db.QueryRow(`SELECT file_hash FROM tracks WHERE id = ?`, t.ID).Scan(&hash); err == nil && hash != "" {
		rows, err := s.db.Query(libraryTrackSelect+` WHERE t.file_hash = ?`, append(trackSelectArgs(u.ID), hash)...)
		if err == nil {
			for rows.Next() {
				if c, _, _, err := scanTrack(rows.Scan); err == nil {
					add(c)
				}
			}
			rows.Close()
		}
	}
	artist := dropPlaceholder(t.Artist, "Unknown Artist")
	for _, m := range s.findSameSong(u.ID, t.Title, artist, t.Duration, true, artist != "") {
		c := m.Track
		add(&c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) checkMergeTarget(w http.ResponseWriter, viewerID string, drop *Track, keepID string) bool {
	if keepID == drop.ID {
		writeErr(w, http.StatusBadRequest, "a track can't be merged into itself")
		return false
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM tracks t WHERE t.id = ? AND `+visibleTo("t.user_id")+` AND `+sameLibrary("t.user_id"),
		keepID, viewerID, drop.UploadedBy.ID).Scan(&n)
	if err != nil {
		s.internalErr(w, "check merge target", err)
		return false
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "the track to keep wasn't found")
		return false
	}
	return true
}

func (s *Server) mergeTrackInto(dropID, keepID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowRFC3339()
	stmts := []struct {
		query string
		args  []any
	}{
		{`UPDATE OR IGNORE playlist_tracks SET track_id = ? WHERE track_id = ?`, []any{keepID, dropID}},
		{`INSERT INTO favorite_changes (user_id, track_id, changed_at)
			SELECT user_id, ?, ? FROM favorites WHERE track_id = ?
			ON CONFLICT(user_id, track_id) DO UPDATE SET changed_at = excluded.changed_at`, []any{keepID, now, dropID}},
		{`UPDATE OR IGNORE favorites SET track_id = ? WHERE track_id = ?`, []any{keepID, dropID}},
		{`INSERT INTO play_stats (user_id, track_id, plays, last_played_at)
			SELECT user_id, ?, plays, last_played_at FROM play_stats WHERE track_id = ?
			ON CONFLICT(user_id, track_id) DO UPDATE SET
				plays = play_stats.plays + excluded.plays,
				last_played_at = CASE WHEN excluded.last_played_at > COALESCE(play_stats.last_played_at, '')
					THEN excluded.last_played_at ELSE play_stats.last_played_at END`, []any{keepID, dropID}},
		{`UPDATE play_events SET track_id = ? WHERE track_id = ?`, []any{keepID, dropID}},
		{`UPDATE track_shares SET track_id = ? WHERE track_id = ?`, []any{keepID, dropID}},
		{`UPDATE playback_state SET
				track_id = CASE WHEN track_id = ? THEN ? ELSE track_id END,
				queue_json = REPLACE(queue_json, ?, ?)
			WHERE track_id = ? OR instr(queue_json, ?) > 0`,
			[]any{dropID, keepID, `"` + dropID + `"`, `"` + keepID + `"`, dropID, `"` + dropID + `"`}},
		{`UPDATE tracks SET
				plays = plays + (SELECT plays FROM tracks WHERE id = ?),
				last_played_at = CASE WHEN (SELECT last_played_at FROM tracks WHERE id = ?) > COALESCE(last_played_at, '')
					THEN (SELECT last_played_at FROM tracks WHERE id = ?) ELSE last_played_at END,
				updated_at = ?
			WHERE id = ?`, []any{dropID, dropID, dropID, now, keepID}},
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st.query, st.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) deleteTrackInto(w http.ResponseWriter, viewer *User, drop *Track, keepID string) bool {
	keepID = strings.TrimSpace(keepID)
	if !s.checkMergeTarget(w, viewer.ID, drop, keepID) {
		return false
	}
	if err := s.mergeTrackInto(drop.ID, keepID); err != nil {
		s.internalErr(w, "merge track", err)
		return false
	}
	return true
}
