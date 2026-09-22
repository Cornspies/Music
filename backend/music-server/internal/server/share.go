package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"music-server/internal/config"
)

// Share links: a link to one track that plays for whoever opens it, with no
// account on this server.
//
// That is the point — a link you send to a friend — but it is also the one
// place where the library leaves the circle of people who have signed in, so
// it is deliberately narrow: sharing is per track and opt-in (nothing is
// public until someone shares it), the link carries a 128-bit random token
// that is the only way in, it can be revoked at any time, and it exposes
// exactly one track's title, artist, album, cover and audio — no library, no
// usernames, no other tracks. MUSIC_DISABLE_SHARING turns the whole feature
// off for a server that shouldn't serve anyone but its members.
//
// The public endpoints take the token in the path and never look at the
// session, so they work in a friend's browser, in a chat app's link preview
// and in a media player, Range requests included.

const shareTokenBytes = 16

// TrackShare is a share link as the clients see it.
type TrackShare struct {
	Token     string `json:"token"`
	TrackID   string `json:"trackId"`
	CreatedAt string `json:"createdAt"`
	// ExpiresAt is "" for a link that doesn't expire.
	ExpiresAt string `json:"expiresAt,omitempty"`
	// Plays counts how often the link has been opened.
	Plays int `json:"plays"`
	// Mine is true when the caller created this link.
	Mine bool `json:"mine"`
}

// SharedTrack is what the public endpoint tells an anonymous visitor about
// the track behind a link: enough to show and play it, nothing else.
type SharedTrack struct {
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Duration float64 `json:"duration"`
	HasCover bool    `json:"hasCover"`
	// SharedBy is the display name of whoever made the link.
	SharedBy string `json:"sharedBy"`
	// CoverColor tints the page, as in the app.
	CoverColor string `json:"coverColor,omitempty"`
}

func sharingDisabled() bool { return config.Flag("MUSIC_DISABLE_SHARING") }

func newShareToken() (string, error) {
	buf := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// shareFor loads a live (not expired) share by token.
func (s *Server) shareFor(token string) (trackID, createdBy string, ok bool) {
	var expires string
	err := s.db.QueryRow(`SELECT track_id, created_by, COALESCE(expires_at, '') FROM track_shares WHERE token = ?`, token).
		Scan(&trackID, &createdBy, &expires)
	if err != nil {
		return "", "", false
	}
	if expires != "" {
		at, err := time.Parse(time.RFC3339, expires)
		if err != nil || at.Before(time.Now()) {
			return "", "", false
		}
	}
	return trackID, createdBy, true
}

// ---- owner side (signed in) --------------------------------------------------

// handleGetTrackShare: GET /api/tracks/{id}/share — the caller's link for
// this track, 404 when there is none. Admins also see someone else's.
func (s *Server) handleGetTrackShare(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	sh, err := s.loadShare(id, u)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "this track isn't shared")
		return
	} else if err != nil {
		s.internalErr(w, "load share", err)
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

func (s *Server) loadShare(trackID string, u *User) (*TrackShare, error) {
	query := `SELECT token, track_id, created_by, created_at, COALESCE(expires_at, ''), plays
		FROM track_shares WHERE track_id = ?`
	args := []any{trackID}
	if !u.IsAdmin() {
		query += ` AND created_by = ?`
		args = append(args, u.ID)
	}
	// An admin may see several; the caller's own comes first.
	query += ` ORDER BY (created_by = ?) DESC, created_at ASC LIMIT 1`
	args = append(args, u.ID)

	var sh TrackShare
	var createdBy string
	if err := s.db.QueryRow(query, args...).Scan(&sh.Token, &sh.TrackID, &createdBy, &sh.CreatedAt, &sh.ExpiresAt, &sh.Plays); err != nil {
		return nil, err
	}
	if sh.ExpiresAt != "" {
		if at, err := time.Parse(time.RFC3339, sh.ExpiresAt); err == nil && at.Before(time.Now()) {
			return nil, sql.ErrNoRows
		}
	}
	sh.Mine = createdBy == u.ID
	return &sh, nil
}

// handleCreateTrackShare: POST /api/tracks/{id}/share {"expiresInDays": n}.
// Returns the caller's existing link when there is one, so pressing "share"
// twice doesn't scatter links for the same track.
func (s *Server) handleCreateTrackShare(w http.ResponseWriter, r *http.Request) {
	if sharingDisabled() {
		writeErr(w, http.StatusNotFound, "share links are disabled on this server")
		return
	}
	u := userFrom(r)
	id := r.PathValue("id")
	if _, _, _, ok := s.getTrack(w, r, u.ID, id); !ok {
		return
	}
	var body struct {
		ExpiresInDays int `json:"expiresInDays"`
	}
	if r.ContentLength > 0 {
		if err := jsonDecodeStrict(r, w, &body); err != nil {
			return
		}
	}

	if sh, err := s.loadShare(id, u); err == nil && sh.Mine {
		writeJSON(w, http.StatusOK, sh)
		return
	}

	token, err := newShareToken()
	if err != nil {
		s.internalErr(w, "make share token", err)
		return
	}
	expires := ""
	if body.ExpiresInDays > 0 {
		expires = time.Now().UTC().AddDate(0, 0, body.ExpiresInDays).Format(time.RFC3339)
	}
	now := nowRFC3339()
	if _, err := s.db.Exec(`INSERT INTO track_shares (token, track_id, created_by, created_at, expires_at, plays)
		VALUES (?,?,?,?,?,0)`, token, id, u.ID, now, expires); err != nil {
		s.internalErr(w, "create share", err)
		return
	}
	writeJSON(w, http.StatusCreated, TrackShare{Token: token, TrackID: id, CreatedAt: now, ExpiresAt: expires, Mine: true})
}

// handleDeleteTrackShare: DELETE /api/tracks/{id}/share — the link stops
// working at once. Admins can revoke anyone's.
func (s *Server) handleDeleteTrackShare(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	query := `DELETE FROM track_shares WHERE track_id = ?`
	args := []any{id}
	if !u.IsAdmin() {
		query += ` AND created_by = ?`
		args = append(args, u.ID)
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		s.internalErr(w, "revoke share", err)
		return
	}
	writeNoContent(w)
}

// ---- public side (no account) ------------------------------------------------

// handleSharedTrack: GET /api/share/{token} — what the link's page shows.
func (s *Server) handleSharedTrack(w http.ResponseWriter, r *http.Request) {
	trackID, createdBy, ok := s.shareFor(r.PathValue("token"))
	if !ok || sharingDisabled() {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	var out SharedTrack
	var coverColor sql.NullString
	err := s.db.QueryRow(`SELECT t.title, t.artist, t.album, t.duration, t.has_cover, COALESCE(t.cover_color, ''),
		COALESCE(u.username, '')
		FROM tracks t LEFT JOIN users u ON u.id = ? WHERE t.id = ?`, createdBy, trackID).
		Scan(&out.Title, &out.Artist, &out.Album, &out.Duration, &out.HasCover, &coverColor, &out.SharedBy)
	if err != nil {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	out.CoverColor = coverColor.String
	// Counted here rather than on the stream, which a browser may request
	// several times for one listen (seeking, range requests).
	_, _ = s.db.Exec(`UPDATE track_shares SET plays = plays + 1 WHERE token = ?`, r.PathValue("token"))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

// handleSharedStream: GET /api/share/{token}/stream — the audio, with Range
// support, for the page's player and for anything else that follows a link.
func (s *Server) handleSharedStream(w http.ResponseWriter, r *http.Request) {
	trackID, _, ok := s.shareFor(r.PathValue("token"))
	if !ok || sharingDisabled() {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	var filePath, fileName, mimeType, altPath, altMime string
	err := s.db.QueryRow(`SELECT file_path, file_name, mime_type, COALESCE(alt_path, ''), COALESCE(alt_mime, '')
		FROM tracks WHERE id = ?`, trackID).Scan(&filePath, &fileName, &mimeType, &altPath, &altMime)
	if err != nil {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	// Same rule as the signed-in stream: a client that can't decode the
	// original asks for the compatible copy.
	if wantsCompatible(r) && altPath != "" {
		filePath, mimeType = altPath, altMime
		fileName = swapExtension(fileName, compatibleExt)
	}
	f, err := os.Open(filepath.Join(s.dataDir, filePath))
	if err != nil {
		s.internalErr(w, "open shared track", err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// A share link is not an invitation to embed this server's audio in
	// someone else's page.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, fileName, timeZero, f)
}

// handleSharedCover: GET /api/share/{token}/cover — the artwork.
func (s *Server) handleSharedCover(w http.ResponseWriter, r *http.Request) {
	trackID, _, ok := s.shareFor(r.PathValue("token"))
	if !ok || sharingDisabled() {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	b, err := os.ReadFile(filepath.Join(s.covDir, trackID+".img"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "no cover art")
		return
	}
	// Uploaded bytes: only serve them when they really are an image, or a
	// crafted "cover" would be stored XSS on this origin.
	ct := http.DetectContentType(b)
	if !allowedImageTypes[ct] {
		writeErr(w, http.StatusNotFound, "no cover art")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// purgeExpiredShares drops links that ran out, at start-up and daily.
func (s *Server) startShareCleanup() {
	purge := func() {
		_, _ = s.db.Exec(`DELETE FROM track_shares WHERE COALESCE(expires_at, '') <> '' AND expires_at < ?`,
			time.Now().UTC().Format(time.RFC3339))
	}
	purge()
	go func() {
		for {
			time.Sleep(24 * time.Hour)
			purge()
		}
	}()
}
