package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"music-server/internal/config"
)

const shareTokenBytes = 16

const maxShareDays = 7

type TrackShare struct {
	Token     string `json:"token"`
	TrackID   string `json:"trackId"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Plays     int    `json:"plays"`
	Mine      bool   `json:"mine"`
}

type SharedTrack struct {
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album"`
	Duration   float64 `json:"duration"`
	HasCover   bool    `json:"hasCover"`
	SharedBy   string  `json:"sharedBy"`
	CoverColor string  `json:"coverColor,omitempty"`
}

func sharingDisabled() bool { return config.Flag("MUSIC_DISABLE_SHARING") }

func newShareToken() (string, error) {
	buf := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

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
	days := body.ExpiresInDays
	if days <= 0 || days > maxShareDays {
		days = maxShareDays
	}
	expires := time.Now().UTC().AddDate(0, 0, days).Format(time.RFC3339)
	now := nowRFC3339()
	if _, err := s.db.Exec(`INSERT INTO track_shares (token, track_id, created_by, created_at, expires_at, plays)
		VALUES (?,?,?,?,?,0)`, token, id, u.ID, now, expires); err != nil {
		s.internalErr(w, "create share", err)
		return
	}
	writeJSON(w, http.StatusCreated, TrackShare{Token: token, TrackID: id, CreatedAt: now, ExpiresAt: expires, Mine: true})
}

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
	_, _ = s.db.Exec(`UPDATE track_shares SET plays = plays + 1 WHERE token = ?`, r.PathValue("token"))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSharedTrackForMember(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trackID, _, ok := s.shareFor(token)
	if !ok || sharingDisabled() {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	t, _, _, ok := s.getTrack(w, r, userFrom(r).ID, trackID)
	if !ok {
		return
	}
	_, _ = s.db.Exec(`UPDATE track_shares SET plays = plays + 1 WHERE token = ?`, token)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, t)
}

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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, fileName, timeZero, f)
}

func (s *Server) handleSharedCover(w http.ResponseWriter, r *http.Request) {
	trackID, _, ok := s.shareFor(r.PathValue("token"))
	if !ok || sharingDisabled() {
		writeErr(w, http.StatusNotFound, "this link has expired or been turned off")
		return
	}
	b, err := os.ReadFile(filepath.Join(s.covDir, trackID+".img"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.forgetMissingCover(trackID)
		}
		writeErr(w, http.StatusNotFound, "no cover art")
		return
	}
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

func (s *Server) startShareCleanup() {
	_, _ = s.db.Exec(`UPDATE track_shares SET expires_at = strftime('%Y-%m-%dT%H:%M:%SZ', created_at, ?)
		WHERE COALESCE(expires_at, '') = ''`, fmt.Sprintf("+%d days", maxShareDays))
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

type AdminShare struct {
	Token     string `json:"token"`
	TrackID   string `json:"trackId"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	SharedBy  string `json:"sharedBy"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	Plays     int    `json:"plays"`
}

func (s *Server) handleAdminShares(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`
		SELECT sh.token, sh.track_id, t.title, t.artist, COALESCE(u.username, ''), sh.created_at,
		       COALESCE(sh.expires_at, ''), sh.plays
		FROM track_shares sh
		JOIN tracks t ON t.id = sh.track_id
		LEFT JOIN users u ON u.id = sh.created_by
		WHERE COALESCE(sh.expires_at, '') = '' OR sh.expires_at > ?
		ORDER BY sh.created_at DESC`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		s.internalErr(w, "list shares", err)
		return
	}
	defer rows.Close()
	out := []AdminShare{}
	for rows.Next() {
		var sh AdminShare
		if err := rows.Scan(&sh.Token, &sh.TrackID, &sh.Title, &sh.Artist, &sh.SharedBy, &sh.CreatedAt, &sh.ExpiresAt, &sh.Plays); err != nil {
			s.internalErr(w, "scan share", err)
			return
		}
		out = append(out, sh)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAdminRevokeShare(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`DELETE FROM track_shares WHERE token = ?`, r.PathValue("token")); err != nil {
		s.internalErr(w, "revoke share", err)
		return
	}
	writeNoContent(w)
}
