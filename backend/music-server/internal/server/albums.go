package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

const albumArtistUpdate = `album_artist = CASE WHEN ? <> '' THEN ? WHEN album = ? THEN album_artist ELSE '' END`

const effectiveAlbumArtist = `COALESCE(NULLIF(t.album_artist, ''), NULLIF(t.artist, ''), 'Unknown Artist')`

const maxAlbumGroups = 200

func albumContextKey(name, artist string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		Name   string `json:"name"`
		Artist string `json:"artist"`
	}{name, artist})
	return "album:" + strings.TrimRight(buf.String(), "\n")
}

func (s *Server) handleSetAlbumArtist(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		Album       string   `json:"album"`
		Artists     []string `json:"artists"`
		AlbumArtist string   `json:"albumArtist"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	album := strings.TrimSpace(body.Album)
	albumArtist := strings.TrimSpace(body.AlbumArtist)
	if album == "" || len(body.Artists) == 0 || len(body.Artists) > maxAlbumGroups {
		writeErr(w, http.StatusBadRequest, "pick an album and at least one artist")
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.internalErr(w, "begin album artist", err)
		return
	}
	defer tx.Rollback()

	updated, skipped := 0, 0
	now := nowRFC3339()
	for _, from := range body.Artists {
		from = strings.TrimSpace(from)
		if from == "" {
			continue
		}
		match := `t.album = ? AND ` + effectiveAlbumArtist + ` = ? AND ` + visibleTo("t.user_id")
		var total int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM tracks t WHERE `+match, album, from, u.ID).Scan(&total); err != nil {
			s.internalErr(w, "count album tracks", err)
			return
		}
		ownerCheck := `t.user_id = ?`
		args := []any{albumArtist, now, album, from, u.ID, u.ID}
		if u.IsAdmin() {
			ownerCheck = `1 = 1`
			args = args[:len(args)-1]
		}
		res, err := tx.Exec(`UPDATE tracks SET album_artist = CASE WHEN ? = artist THEN '' ELSE ? END, updated_at = ?
			WHERE id IN (SELECT t.id FROM tracks t WHERE `+match+` AND `+ownerCheck+`)`,
			append([]any{albumArtist}, args...)...)
		if err != nil {
			s.internalErr(w, "set album artist", err)
			return
		}
		n, _ := res.RowsAffected()
		updated += int(n)
		skipped += total - int(n)

		if albumArtist == "" || albumArtist == from || int(n) < total {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO library_pins (user_id, kind, name, artist, created_at)
			SELECT user_id, kind, name, ?, created_at FROM library_pins WHERE user_id = ? AND kind = 'album' AND name = ? AND artist = ?`,
			albumArtist, u.ID, album, from); err != nil {
			s.internalErr(w, "move album pin", err)
			return
		}
		if _, err := tx.Exec(`DELETE FROM library_pins WHERE user_id = ? AND kind = 'album' AND name = ? AND artist = ?`,
			u.ID, album, from); err != nil {
			s.internalErr(w, "move album pin", err)
			return
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO playback_prefs (user_id, context, shuffle, repeat, updated_at)
			SELECT user_id, ?, shuffle, repeat, updated_at FROM playback_prefs WHERE user_id = ? AND context = ?`,
			albumContextKey(album, albumArtist), u.ID, albumContextKey(album, from)); err != nil {
			s.internalErr(w, "move album prefs", err)
			return
		}
		if _, err := tx.Exec(`DELETE FROM playback_prefs WHERE user_id = ? AND context = ?`, u.ID, albumContextKey(album, from)); err != nil {
			s.internalErr(w, "move album prefs", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.internalErr(w, "commit album artist", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"updated": updated, "skipped": skipped})
}
