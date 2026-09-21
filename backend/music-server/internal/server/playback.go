package server

import (
        "database/sql"
        "encoding/json"
        "errors"
        "net/http"
        "strings"
        "time"
)

// PlaybackState is what a client needs to pick up where the listener left
// off — in a new tab after this one was closed, or on another device. The
// track and queue are returned as full objects so a client can restore them
// without a second round of requests.
//
// Devices keep each other up to date through UpdatedAt: a save answers with
// the stamp it produced, the GET carries it as an ETag, and a client coming
// back to the foreground asks with If-None-Match — a 304 means nothing
// happened elsewhere, a 200 is the other device's state to adopt.
type PlaybackState struct {
        Track    Track   `json:"track"`
        Position float64 `json:"position"`
        // Queue is the "up next" list, current track included, with any track
        // deleted since it was saved already dropped.
        Queue      []Track `json:"queue"`
        QueueIndex int     `json:"queueIndex"`
        UpdatedAt  string  `json:"updatedAt"`
}

type playbackRequest struct {
        TrackID  string  `json:"trackId"`
        Position float64 `json:"position"`
        // Queue is the full "up next" list as track IDs. Nil means "unchanged":
        // clients send it only when it actually changes, so the frequent
        // position-only saves stay a few dozen bytes.
        Queue []string `json:"queue"`
}

// maxSavedQueue caps how much of a queue is remembered. "Play all" on a big
// library would otherwise store the whole library per save; a window around
// the current track is what "continue listening" needs.
const maxSavedQueue = 500

// playbackStamp is updated_at for playback_state: RFC 3339 with
// milliseconds, so two saves in the same second still get distinct stamps
// (they double as the ETag).
func playbackStamp() string {
        return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func playbackETag(updatedAt string) string { return `"` + updatedAt + `"` }

// handleGetPlayback returns the caller's saved position, or 204 when there is
// nothing to resume.
func (s *Server) handleGetPlayback(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)

        var (
                trackID   string
                position  float64
                queueJSON string
                updatedAt string
        )
        err := s.db.QueryRow(`SELECT track_id, position, queue_json, updated_at FROM playback_state WHERE user_id = ?`, u.ID).
                Scan(&trackID, &position, &queueJSON, &updatedAt)
        if errors.Is(err, sql.ErrNoRows) {
                writeNoContent(w)
                return
        } else if err != nil {
                s.internalErr(w, "fetch playback", err)
                return
        }
        // The browser must not answer from its own cache: the conditional
        // request is the client's, made against the stamp it last saw.
        w.Header().Set("Cache-Control", "no-store")
        w.Header().Set("ETag", playbackETag(updatedAt))
        if match := r.Header.Get("If-None-Match"); match != "" && match == playbackETag(updatedAt) {
                w.WriteHeader(http.StatusNotModified)
                return
        }

        row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(u.ID), trackID)...)
        track, _, _, err := scanTrack(row.Scan)
        if errors.Is(err, sql.ErrNoRows) {
                // The cascade normally clears this, but never resurrect a
                // deleted track either way.
                _, _ = s.db.Exec(`DELETE FROM playback_state WHERE user_id = ?`, u.ID)
                writeNoContent(w)
                return
        } else if err != nil {
                s.internalErr(w, "fetch playback track", err)
                return
        }

        var ids []string
        _ = json.Unmarshal([]byte(queueJSON), &ids) // a bad blob just means an empty queue
        queue, err := s.tracksByIDs(u.ID, ids)
        if err != nil {
                s.internalErr(w, "fetch playback queue", err)
                return
        }
        index := -1
        for i := range queue {
                if queue[i].ID == track.ID {
                        index = i
                        break
                }
        }
        if index == -1 {
                // The queue never lacked the current track when saved, but a
                // trimmed or partially deleted one might: keep it playable.
                queue = append([]Track{*track}, queue...)
                index = 0
        }

        // A track that was played to (within a second of) its end starts over
        // rather than resuming on its final moment.
        if track.Duration > 0 && position >= track.Duration-1 {
                position = 0
        }

        writeJSON(w, http.StatusOK, PlaybackState{
                Track: *track, Position: position, Queue: queue, QueueIndex: index, UpdatedAt: updatedAt,
        })
}

// handlePutPlayback records where the caller is. Position-only saves leave
// the stored queue as it is; a save that carries a queue replaces it.
// Answers with the new updatedAt, which the client keeps to tell its own
// state from another device's.
func (s *Server) handlePutPlayback(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        var body playbackRequest
        if err := jsonDecodeStrict(r, w, &body); err != nil {
                return
        }
        body.TrackID = strings.TrimSpace(body.TrackID)
        if body.TrackID == "" {
                writeErr(w, http.StatusBadRequest, "trackId is required")
                return
        }
        if body.Position < 0 {
                body.Position = 0
        }

        var exists int
        if err := s.db.QueryRow(`SELECT COUNT(1) FROM tracks WHERE id = ?`, body.TrackID).Scan(&exists); err != nil {
                s.internalErr(w, "check track", err)
                return
        }
        if exists == 0 {
                writeErr(w, http.StatusNotFound, "track not found")
                return
        }

        now := playbackStamp()
        if body.Queue == nil {
                _, err := s.db.Exec(`INSERT INTO playback_state (user_id, track_id, position, updated_at)
                        VALUES (?,?,?,?)
                        ON CONFLICT(user_id) DO UPDATE SET
                                track_id = excluded.track_id, position = excluded.position, updated_at = excluded.updated_at`,
                        u.ID, body.TrackID, body.Position, now)
                if err != nil {
                        s.internalErr(w, "save playback", err)
                        return
                }
                writeJSON(w, http.StatusOK, map[string]string{"updatedAt": now})
                return
        }

        queueJSON, err := json.Marshal(trimQueue(body.Queue, body.TrackID))
        if err != nil {
                s.internalErr(w, "encode queue", err)
                return
        }
        _, err = s.db.Exec(`INSERT INTO playback_state (user_id, track_id, position, queue_json, updated_at)
                VALUES (?,?,?,?,?)
                ON CONFLICT(user_id) DO UPDATE SET
                        track_id = excluded.track_id, position = excluded.position,
                        queue_json = excluded.queue_json, updated_at = excluded.updated_at`,
                u.ID, body.TrackID, body.Position, string(queueJSON), now)
        if err != nil {
                s.internalErr(w, "save playback", err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]string{"updatedAt": now})
}

// trimQueue keeps at most maxSavedQueue IDs, as a window around the current
// track biased towards what comes next — that's the part that gets played.
func trimQueue(ids []string, current string) []string {
        out := make([]string, 0, min(len(ids), maxSavedQueue))
        for _, id := range ids {
                if id = strings.TrimSpace(id); id != "" {
                        out = append(out, id)
                }
        }
        if len(out) <= maxSavedQueue {
                return out
        }
        idx := -1
        for i, id := range out {
                if id == current {
                        idx = i
                        break
                }
        }
        if idx == -1 {
                return out[:maxSavedQueue]
        }
        start := max(0, idx-50)
        end := min(len(out), start+maxSavedQueue)
        start = max(0, end-maxSavedQueue)
        return out[start:end]
}

// tracksByIDs resolves IDs to tracks in the order given, silently dropping
// any that no longer exist.
func (s *Server) tracksByIDs(userID string, ids []string) ([]Track, error) {
        if len(ids) == 0 {
                return []Track{}, nil
        }
        args := trackSelectArgs(userID)
        marks := make([]string, len(ids))
        for i, id := range ids {
                marks[i] = "?"
                args = append(args, id)
        }
        rows, err := s.db.Query(trackSelect+` WHERE t.id IN (`+strings.Join(marks, ",")+`)`, args...)
        if err != nil {
                return nil, err
        }
        defer rows.Close()
        byID := make(map[string]Track, len(ids))
        for rows.Next() {
                t, _, _, err := scanTrack(rows.Scan)
                if err != nil {
                        return nil, err
                }
                byID[t.ID] = *t
        }
        if err := rows.Err(); err != nil {
                return nil, err
        }
        out := make([]Track, 0, len(ids))
        for _, id := range ids {
                if t, ok := byID[id]; ok {
                        out = append(out, t)
                }
        }
        return out, nil
}
