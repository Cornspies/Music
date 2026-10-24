package server

import (
	"net/http"
	"strconv"
	"strings"
)

type PlayEvent struct {
	Track    Track  `json:"track"`
	PlayedAt string `json:"playedAt"`
}

type HistoryResponse struct {
	Events          []PlayEvent `json:"events"`
	ListenedSeconds float64     `json:"listenedSeconds"`
	PlayCount       int         `json:"playCount"`
	TrackCount      int         `json:"trackCount"`
	Next            string      `json:"next,omitempty"`
}

func (s *Server) recordPlay(userID, trackID string) {
	if _, err := s.db.Exec(`INSERT INTO play_events (user_id, track_id, played_at, listened_seconds) VALUES (?, ?, ?, 0)`,
		userID, trackID, nowRFC3339()); err != nil {
		logErr("record play", err)
		return
	}
	playsTotal.Inc()
}

const listenedExpr = `pe.listened_seconds`

const maxListenReport = 15 * 60

func (s *Server) handleTrackListened(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	var body struct {
		Seconds float64 `json:"seconds"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if body.Seconds <= 0 || body.Seconds > maxListenReport {
		writeNoContent(w)
		return
	}
	if _, err := s.db.Exec(`
		UPDATE play_events SET listened_seconds = listened_seconds + ?
		WHERE id = (SELECT id FROM play_events WHERE user_id = ? AND track_id = ? ORDER BY played_at DESC, id DESC LIMIT 1)`,
		body.Seconds, u.ID, id); err != nil {
		s.internalErr(w, "record listening", err)
		return
	}
	writeNoContent(w)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()

	limit := 50
	if n, err := strconv.Atoi(strings.TrimSpace(q.Get("limit"))); err == nil && n > 0 {
		limit = min(n, 500)
	}
	since := strings.TrimSpace(q.Get("since"))
	distinct := q.Get("distinct") == "1" || strings.EqualFold(q.Get("distinct"), "true")

	var totals struct {
		count   int
		tracks  int
		seconds float64
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(1), COUNT(DISTINCT pe.track_id), COALESCE(SUM(`+listenedExpr+`), 0)
		FROM play_events pe JOIN tracks t ON t.id = pe.track_id
		WHERE pe.user_id = ? AND pe.played_at > ? AND `+visibleTo("t.user_id"), u.ID, since, u.ID).
		Scan(&totals.count, &totals.tracks, &totals.seconds); err != nil {
		s.internalErr(w, "history totals", err)
		return
	}

	beforeAt, beforeKey, _ := strings.Cut(strings.TrimSpace(q.Get("before")), "|")
	var query string
	var args []any
	if distinct {
		query = trackSelect + `
			JOIN (SELECT track_id, MAX(played_at) AS played_at FROM play_events
			      WHERE user_id = ? AND played_at > ? GROUP BY track_id) pe ON pe.track_id = t.id
			WHERE ? = '' OR pe.played_at < ? OR (pe.played_at = ? AND t.id < ?)
			ORDER BY pe.played_at DESC, t.id DESC LIMIT ?`
		query = strings.Replace(query, "\nFROM tracks t", ",\n       pe.played_at, t.id\nFROM tracks t", 1)
		args = append(trackSelectArgs(u.ID), u.ID, since, beforeAt, beforeAt, beforeAt, beforeKey, limit)
	} else {
		eventID, _ := strconv.ParseInt(beforeKey, 10, 64)
		query = trackSelect + `
			JOIN play_events pe ON pe.track_id = t.id AND pe.user_id = ? AND pe.played_at > ?
			WHERE ? = '' OR pe.played_at < ? OR (pe.played_at = ? AND pe.id < ?)
			ORDER BY pe.played_at DESC, pe.id DESC LIMIT ?`
		query = strings.Replace(query, "\nFROM tracks t", ",\n       pe.played_at, CAST(pe.id AS TEXT)\nFROM tracks t", 1)
		args = append(trackSelectArgs(u.ID), u.ID, since, beforeAt, beforeAt, beforeAt, eventID, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		s.internalErr(w, "list history", err)
		return
	}
	defer rows.Close()

	events := []PlayEvent{}
	next := ""
	for rows.Next() {
		var playedAt, key string
		t, _, _, err := scanTrackWith(rows.Scan, &playedAt, &key)
		if err != nil {
			s.internalErr(w, "scan history", err)
			return
		}
		events = append(events, PlayEvent{Track: *t, PlayedAt: playedAt})
		next = playedAt + "|" + key
	}
	if err := rows.Err(); err != nil {
		s.internalErr(w, "iterate history", err)
		return
	}
	if len(events) < limit {
		next = ""
	}
	writeJSON(w, http.StatusOK, HistoryResponse{
		Events: events, ListenedSeconds: totals.seconds, PlayCount: totals.count, TrackCount: totals.tracks, Next: next,
	})
}

func scanTrackWith(scan func(dest ...any) error, extra ...any) (*Track, string, string, error) {
	return scanTrack(func(dest ...any) error {
		return scan(append(dest, extra...)...)
	})
}
