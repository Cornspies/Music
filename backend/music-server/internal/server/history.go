package server

import (
	"net/http"
	"strconv"
	"strings"
)

// Listening history: one play_events row per listen, per user. The per-track
// counter in play_stats stays as the cheap "plays" number; this is the
// timeline behind "recently played" and the listening stats, and it works
// across devices because it lives on the server.

// PlayEvent is a single listen.
type PlayEvent struct {
	Track    Track  `json:"track"`
	PlayedAt string `json:"playedAt"`
}

// HistoryResponse is what GET /api/history returns.
type HistoryResponse struct {
	Events []PlayEvent `json:"events"`
	// ListenedSeconds is the time actually spent listening in the window, as
	// reported by the clients (plays older than that reporting count in full).
	ListenedSeconds float64 `json:"listenedSeconds"`
	PlayCount       int     `json:"playCount"`
	// Distinct tracks played in the window.
	TrackCount int `json:"trackCount"`
}

// recordPlay appends to the history. Errors are logged, not returned: a
// missed history row must never fail a play.
func (s *Server) recordPlay(userID, trackID string) {
	if _, err := s.db.Exec(`INSERT INTO play_events (user_id, track_id, played_at) VALUES (?, ?, ?)`,
		userID, trackID, nowRFC3339()); err != nil {
		logErr("record play", err)
	}
}

// listenedExpr is the seconds a play row counts for. Rows written before
// clients reported progress (reported = 0) fall back to the track's length,
// so old history doesn't collapse to zero; new rows count what was heard.
const listenedExpr = `CASE WHEN pe.reported = 1 THEN pe.listened_seconds ELSE t.duration END`

// handleTrackListened: POST /api/tracks/{id}/listened {"seconds": n}. Adds n
// seconds of actual listening to the caller's latest play of the track.
// Clients send this every so often and when playback stops, so a track
// skipped after ten seconds counts ten seconds, not its full length.
func (s *Server) handleTrackListened(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	var body struct {
		Seconds float64 `json:"seconds"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	// One report covers at most a couple of minutes of real time; anything
	// larger is a bug or a fabrication, not listening.
	if body.Seconds <= 0 || body.Seconds > 180 {
		writeNoContent(w)
		return
	}
	if _, err := s.db.Exec(`
		UPDATE play_events SET listened_seconds = listened_seconds + ?, reported = 1
		WHERE id = (SELECT id FROM play_events WHERE user_id = ? AND track_id = ? ORDER BY played_at DESC, id DESC LIMIT 1)`,
		body.Seconds, u.ID, id); err != nil {
		s.internalErr(w, "record listening", err)
		return
	}
	writeNoContent(w)
}

// handleHistory returns the caller's recent listens, newest first.
//
//	?limit=  how many events (default 50, max 500)
//	?since=  RFC 3339 cut-off; only plays after it (default: no cut-off)
//	?distinct=1  collapse repeats so each track appears once, at its latest play
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	q := r.URL.Query()

	limit := 50
	if n, err := strconv.Atoi(strings.TrimSpace(q.Get("limit"))); err == nil && n > 0 {
		limit = min(n, 500)
	}
	since := strings.TrimSpace(q.Get("since"))
	distinct := q.Get("distinct") == "1" || strings.EqualFold(q.Get("distinct"), "true")

	// Totals over the whole window (not just the page of events).
	var totals struct {
		count   int
		tracks  int
		seconds float64
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(1), COUNT(DISTINCT pe.track_id), COALESCE(SUM(`+listenedExpr+`), 0)
		FROM play_events pe JOIN tracks t ON t.id = pe.track_id
		WHERE pe.user_id = ? AND pe.played_at > ?`, u.ID, since).
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
	// Both shapes need the played_at column alongside the track columns;
	// trackSelect ends at "FROM ... LEFT JOIN ...", so splice it in.
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

// scanTrackWith is scanTrack for queries that append extra columns after
// trackSelect's own.
func scanTrackWith(scan func(dest ...any) error, extra ...any) (*Track, string, string, error) {
	return scanTrack(func(dest ...any) error {
		return scan(append(dest, extra...)...)
	})
}
