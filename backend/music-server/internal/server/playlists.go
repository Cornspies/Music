package server

import (
        "database/sql"
        "errors"
        "net/http"
        "strings"

        "github.com/google/uuid"
)

type Playlist struct {
        ID            string  `json:"id"`
        Name          string  `json:"name"`
        TrackCount    int     `json:"trackCount"`
        TotalDuration float64 `json:"totalDuration"`
        CreatedAt     string  `json:"createdAt"`
        // Playlists are private by default. Making one public lets every other
        // signed-in user find and play it — but never edit it.
        IsPublic bool `json:"isPublic"`
        // Who made it. Only meaningful for playlists shared by someone else,
        // but always populated so the client can tell "mine" from "theirs".
        Owner    Uploader `json:"owner"`
        IsMine   bool     `json:"isMine"`
        // Pinned playlists belong to someone else but the viewer has chosen to
        // keep them in their sidebar.
        IsPinned bool `json:"isPinned"`
}

// playlistDetail is a playlist plus its tracks. Embedding Playlist keeps the
// detail payload in lockstep with the list payload — add a field to Playlist
// and this endpoint returns it automatically.
type playlistDetail struct {
        *Playlist
        Tracks []*Track `json:"tracks"`
}

// playlistSelect returns every column Playlist needs; callers add the WHERE.
// It takes one leading bind value — the viewing user's id, for the is-pinned
// check — so build argument lists with playlistSelectArgs.
const playlistSelect = `
SELECT EXISTS(SELECT 1 FROM playlist_pins pp WHERE pp.user_id = ? AND pp.playlist_id = p.id),
       p.id, p.name, p.created_at, p.is_public, p.user_id,
       COALESCE(u.username, ''), COALESCE(u.has_avatar, 0),
       (SELECT COUNT(1) FROM playlist_tracks pt JOIN tracks t ON t.id = pt.track_id WHERE pt.playlist_id = p.id),
       (SELECT COALESCE(SUM(t.duration), 0) FROM playlist_tracks pt JOIN tracks t ON t.id = pt.track_id WHERE pt.playlist_id = p.id)
FROM playlists p
LEFT JOIN users u ON u.id = p.user_id`

// playlistSelectArgs returns the leading bind values playlistSelect expects.
// Append any further WHERE arguments after these.
func playlistSelectArgs(userID string) []any { return []any{userID} }

func scanPlaylist(scan func(dest ...any) error, viewerID string) (*Playlist, error) {
        var (
                p         Playlist
                isPublic  int
                hasAvatar int
                isPinned  int
        )
        if err := scan(&isPinned, &p.ID, &p.Name, &p.CreatedAt, &isPublic, &p.Owner.ID,
                &p.Owner.Username, &hasAvatar, &p.TrackCount, &p.TotalDuration); err != nil {
                return nil, err
        }
        p.IsPublic = isPublic == 1
        p.IsPinned = isPinned == 1
        p.Owner.HasAvatar = hasAvatar == 1
        p.IsMine = p.Owner.ID == viewerID
        return &p, nil
}

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        // Own playlists plus the ones pinned from other people, so the sidebar
        // is a single source of truth. A pinned playlist that has since been
        // unshared drops out on its own (is_public = 1 is required).
        s.writePlaylists(w, u, `
                WHERE p.user_id = ?
                   OR (p.is_public = 1 AND EXISTS(
                           SELECT 1 FROM playlist_pins pp WHERE pp.user_id = ? AND pp.playlist_id = p.id))
                ORDER BY (p.user_id <> ?) ASC, p.created_at DESC`,
                u.ID, u.ID, u.ID)
}

// handleListPublicPlaylists returns playlists other people have chosen to
// share. Your own are excluded — they're already in /api/playlists.
func (s *Server) handleListPublicPlaylists(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        s.writePlaylists(w,
                u,
                ` WHERE p.is_public = 1 AND p.user_id <> ? ORDER BY p.created_at DESC`,
                u.ID)
}

func (s *Server) writePlaylists(w http.ResponseWriter, u *User, where string, args ...any) {
        rows, err := s.db.Query(playlistSelect+where, append(playlistSelectArgs(u.ID), args...)...)
        if err != nil {
                s.internalErr(w, "list playlists", err)
                return
        }
        defer rows.Close()

        list := []*Playlist{}
        for rows.Next() {
                p, err := scanPlaylist(rows.Scan, u.ID)
                if err != nil {
                        s.internalErr(w, "scan playlist", err)
                        return
                }
                list = append(list, p)
        }
        writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        var body struct {
                Name string `json:"name"`
        }
        if err := jsonDecodeStrict(r, w, &body); err != nil {
                return
        }
        name := trimDefault(body.Name, "")
        if name == "" || len(name) > 100 {
                writeErr(w, http.StatusBadRequest, "playlist name must be 1-100 characters")
                return
        }

        id := uuid.NewString()
        createdAt := nowRFC3339()
        if _, err := s.db.Exec(`INSERT INTO playlists (id, user_id, name, created_at) VALUES (?,?,?,?)`,
                id, u.ID, name, createdAt); err != nil {
                s.internalErr(w, "create playlist", err)
                return
        }
        // Read it back rather than hand-building the response: that way the
        // new playlist carries owner/isMine/isPublic like every other one.
        p, ok := s.getPlaylist(w, u, id)
        if !ok {
                return
        }
        writeJSON(w, http.StatusCreated, p)
}

// getPlaylist fetches a playlist the user is allowed to *see*: their own, or
// someone else's that's been made public. A private playlist belonging to
// another user reads as "not found" — no reason to confirm it exists.
func (s *Server) getPlaylist(w http.ResponseWriter, u *User, id string) (*Playlist, bool) {
        row := s.db.QueryRow(playlistSelect+` WHERE p.id = ? AND (p.user_id = ? OR p.is_public = 1)`,
                append(playlistSelectArgs(u.ID), id, u.ID)...)
        p, err := scanPlaylist(row.Scan, u.ID)
        if errors.Is(err, sql.ErrNoRows) {
                writeErr(w, http.StatusNotFound, "playlist not found")
                return nil, false
        } else if err != nil {
                s.internalErr(w, "fetch playlist", err)
                return nil, false
        }
        return p, true
}

// requireOwnPlaylist is getPlaylist plus "and you may change it".
func (s *Server) requireOwnPlaylist(w http.ResponseWriter, u *User, id string) (*Playlist, bool) {
        p, ok := s.getPlaylist(w, u, id)
        if !ok {
                return nil, false
        }
        if !p.IsMine {
                writeErr(w, http.StatusForbidden, "this playlist belongs to someone else")
                return nil, false
        }
        return p, true
}

func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        p, ok := s.getPlaylist(w, u, id)
        if !ok {
                return
        }

        rows, err := s.db.Query(trackSelect+`
                JOIN playlist_tracks pt ON pt.track_id = t.id
                WHERE pt.playlist_id = ?
                ORDER BY pt.position ASC`, append(trackSelectArgs(u.ID), id)...)
        if err != nil {
                s.internalErr(w, "list playlist tracks", err)
                return
        }
        defer rows.Close()

        tracks := []*Track{}
        for rows.Next() {
                t, _, _, err := scanTrack(rows.Scan)
                if err != nil {
                        s.internalErr(w, "scan playlist track", err)
                        return
                }
                tracks = append(tracks, t)
        }

        writeJSON(w, http.StatusOK, playlistDetail{Playlist: p, Tracks: tracks})
}

// handlePinPlaylist keeps someone else's public playlist in your sidebar.
// Your own are always listed, so pinning one is rejected rather than silently
// doing nothing.
func (s *Server) handlePinPlaylist(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        p, ok := s.getPlaylist(w, u, id)
        if !ok {
                return
        }
        if p.IsMine {
                writeErr(w, http.StatusBadRequest, "your own playlists are always in the sidebar")
                return
        }
        if _, err := s.db.Exec(
                `INSERT OR IGNORE INTO playlist_pins (user_id, playlist_id, created_at) VALUES (?,?,?)`,
                u.ID, id, nowRFC3339()); err != nil {
                s.internalErr(w, "pin playlist", err)
                return
        }
        updated, ok := s.getPlaylist(w, u, id)
        if !ok {
                return
        }
        writeJSON(w, http.StatusOK, updated)
}

// handleUnpinPlaylist removes the pin. It succeeds even when the playlist is
// no longer visible (the owner may have unshared it since), so the client can
// always drop it from the sidebar.
func (s *Server) handleUnpinPlaylist(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")
        if _, err := s.db.Exec(`DELETE FROM playlist_pins WHERE user_id = ? AND playlist_id = ?`,
                u.ID, id); err != nil {
                s.internalErr(w, "unpin playlist", err)
                return
        }
        row := s.db.QueryRow(playlistSelect+` WHERE p.id = ? AND (p.user_id = ? OR p.is_public = 1)`,
                append(playlistSelectArgs(u.ID), id, u.ID)...)
        p, err := scanPlaylist(row.Scan, u.ID)
        if err != nil {
                writeNoContent(w)
                return
        }
        writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        // Both fields are optional: send a name to rename, isPublic to share or
        // unshare, or both. Pointers so "absent" is distinguishable from "false".
        var body struct {
                Name     *string `json:"name"`
                IsPublic *bool   `json:"isPublic"`
        }
        if err := jsonDecodeStrict(r, w, &body); err != nil {
                return
        }
        if body.Name == nil && body.IsPublic == nil {
                writeErr(w, http.StatusBadRequest, "nothing to update")
                return
        }
        if _, ok := s.requireOwnPlaylist(w, u, id); !ok {
                return
        }

        sets := []string{}
        args := []any{}
        if body.Name != nil {
                name := trimDefault(*body.Name, "")
                if name == "" || len(name) > 100 {
                        writeErr(w, http.StatusBadRequest, "playlist name must be 1-100 characters")
                        return
                }
                sets = append(sets, "name = ?")
                args = append(args, name)
        }
        if body.IsPublic != nil {
                sets = append(sets, "is_public = ?")
                args = append(args, boolToInt(*body.IsPublic))
        }
        args = append(args, id, u.ID)

        if _, err := s.db.Exec(`UPDATE playlists SET `+strings.Join(sets, ", ")+` WHERE id = ? AND user_id = ?`, args...); err != nil {
                s.internalErr(w, "update playlist", err)
                return
        }
        p, ok := s.getPlaylist(w, u, id)
        if !ok {
                return
        }
        writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")
        if _, ok := s.requireOwnPlaylist(w, u, id); !ok {
                return
        }
        res, err := s.db.Exec(`DELETE FROM playlists WHERE id = ? AND user_id = ?`, id, u.ID)
        if err != nil {
                s.internalErr(w, "delete playlist", err)
                return
        }
        if n, _ := res.RowsAffected(); n == 0 {
                writeErr(w, http.StatusNotFound, "playlist not found")
                return
        }
        writeNoContent(w)
}

func (s *Server) handleAddPlaylistTrack(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        var body struct {
                TrackID string `json:"trackId"`
        }
        if err := jsonDecodeStrict(r, w, &body); err != nil {
                return
        }
        if body.TrackID == "" {
                writeErr(w, http.StatusBadRequest, "missing trackId")
                return
        }

        if _, ok := s.requireOwnPlaylist(w, u, id); !ok {
                return
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
        var already int
        if err := s.db.QueryRow(`SELECT COUNT(1) FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`, id, body.TrackID).Scan(&already); err != nil {
                s.internalErr(w, "check duplicate", err)
                return
        }
        if already > 0 {
                writeErr(w, http.StatusConflict, "track already in playlist")
                return
        }

        var next int
        if err := s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM playlist_tracks WHERE playlist_id = ?`, id).Scan(&next); err != nil {
                s.internalErr(w, "next position", err)
                return
        }
        if _, err := s.db.Exec(`INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES (?,?,?)`, id, body.TrackID, next); err != nil {
                s.internalErr(w, "add playlist track", err)
                return
        }
        writeJSON(w, http.StatusCreated, map[string]int{"position": next})
}

func (s *Server) handleRemovePlaylistTrack(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")
        trackID := r.PathValue("trackId")

        if _, ok := s.requireOwnPlaylist(w, u, id); !ok {
                return
        }
        res, err := s.db.Exec(`DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`, id, trackID)
        if err != nil {
                s.internalErr(w, "remove playlist track", err)
                return
        }
        if n, _ := res.RowsAffected(); n == 0 {
                writeErr(w, http.StatusNotFound, "track not in playlist")
                return
        }
        writeNoContent(w)
}
