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
}

func (s *Server) recordPlay(userID, trackID string) {
	if _, err := s.db.Exec(`INSERT INTO play_events (user_id, track_id, played_at, listened_seconds, reported) VALUES (?, ?, ?, 0, 1)`,
		userID, trackID, nowRFC3339()); err != nil {
		logErr("record play", err)
	}
}

const listenedExpr = `CASE WHEN pe.reported = 1 THEN pe.listened_seconds ELSE t.duration END`

func (s *Server) handleTrackListened(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	var body struct {
		Seconds float64 `json:"seconds"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if body.Seconds <= 0 || body.Seconds > 180 {
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

	var query string
	if distinct {
		query = trackSelect + `
			JOIN (SELECT track_id, MAX(played_at) AS played_at FROM play_events
			      WHERE user_id = ? AND played_at > ? GROUP BY track_id) pe ON pe.track_id = t.id
			ORDER BY pe.played_at DESC LIMIT ?`
	} else {
		query = trackSelect + `
			JOIN (SELECT track_id, played_at FROM play_events
			      WHERE user_id = ? AND played_at > ?
			      ORDER BY played_at DESC LIMIT ?) pe ON pe.track_id = t.id
			ORDER BY pe.played_at DESC`
	}
	query = strings.Replace(query, "\nFROM tracks t", ",\n       pe.played_at\nFROM tracks t", 1)

	rows, err := s.db.Query(query, append(trackSelectArgs(u.ID), u.ID, since, limit)...)
	if err != nil {
		s.internalErr(w, "list history", err)
		return
	}
	defer rows.Close()

	events := []PlayEvent{}
	for rows.Next() {
		var playedAt string
		t, _, _, err := scanTrackWith(rows.Scan, &playedAt)
		if err != nil {
			s.internalErr(w, "scan history", err)
			return
		}
		events = append(events, PlayEvent{Track: *t, PlayedAt: playedAt})
	}
	if err := rows.Err(); err != nil {
		s.internalErr(w, "iterate history", err)
		return
	}
	writeJSON(w, http.StatusOK, HistoryResponse{
		Events: events, ListenedSeconds: totals.seconds, PlayCount: totals.count, TrackCount: totals.tracks,
	})
}

func scanTrackWith(scan func(dest ...any) error, extra ...any) (*Track, string, string, error) {
	return scanTrack(func(dest ...any) error {
		return scan(append(dest, extra...)...)
	})
}
