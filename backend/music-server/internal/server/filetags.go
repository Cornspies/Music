package server

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

type TagChange struct {
	Field   string `json:"field"`
	Current string `json:"current"`
	File    string `json:"file"`
}

type FileTagDiff struct {
	Track   *Track      `json:"track"`
	Changes []TagChange `json:"changes"`
}

var tagFields = map[string]bool{"title": true, "artist": true, "album": true, "albumArtist": true, "genre": true, "year": true}

func fileTagChanges(t *Track, tags fileMetadata) []TagChange {
	changes := []TagChange{}
	text := func(field, current, file string) {
		if file != "" && strings.TrimSpace(current) != file {
			changes = append(changes, TagChange{Field: field, Current: current, File: file})
		}
	}
	text("title", t.Title, tags.title)
	text("artist", t.Artist, tags.artist)
	text("album", t.Album, tags.album)
	text("albumArtist", t.AlbumArtist, albumArtistFor(tags.albumArtist, trimDefault(tags.artist, t.Artist)))
	text("genre", t.Genre, tags.genre)
	if tags.year > 0 && tags.year != t.Year {
		current := ""
		if t.Year > 0 {
			current = strconv.Itoa(t.Year)
		}
		changes = append(changes, TagChange{Field: "year", Current: current, File: strconv.Itoa(tags.year)})
	}
	return changes
}

func (s *Server) restoreFileTags(t *Track, filePath string, only map[string]bool) ([]string, error) {
	tags := readMetadata(filepath.Join(s.dataDir, filePath))
	title, artist, album, albumArtist, genre, year := t.Title, t.Artist, t.Album, t.AlbumArtist, t.Genre, t.Year
	restored := []string{}
	for _, c := range fileTagChanges(t, tags) {
		if only != nil && !only[c.Field] {
			continue
		}
		switch c.Field {
		case "title":
			title = c.File
		case "artist":
			artist = c.File
		case "album":
			album = c.File
		case "albumArtist":
			albumArtist = c.File
		case "genre":
			genre = c.File
		case "year":
			year = tags.year
		}
		restored = append(restored, c.Field)
	}
	if len(restored) == 0 {
		return restored, nil
	}
	if _, err := s.db.Exec(`UPDATE tracks SET title = ?, artist = ?, album = ?, album_artist = ?, genre = ?, year = ?, updated_at = ? WHERE id = ?`,
		title, artist, album, albumArtist, genre, year, nowRFC3339(), t.ID); err != nil {
		return nil, err
	}
	if title != t.Title || artist != t.Artist {
		s.forgetLookedUpLyrics(t.ID)
	}
	return restored, nil
}

func canEditTrack(u *User, t *Track) bool {
	return t.UploadedBy.ID == u.ID || u.IsAdmin()
}

func (s *Server) handleTrackFileTags(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, filePath, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	if !canEditTrack(u, t) {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"changes": fileTagChanges(t, readMetadata(filepath.Join(s.dataDir, filePath))),
	})
}

func (s *Server) handleRestoreTrackTags(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	var body struct {
		Fields []string `json:"fields"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	t, filePath, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if !canEditTrack(u, t) {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
		return
	}
	var only map[string]bool
	if len(body.Fields) > 0 {
		only = map[string]bool{}
		for _, f := range body.Fields {
			if !tagFields[f] {
				writeErr(w, http.StatusBadRequest, "unknown field")
				return
			}
			only[f] = true
		}
	}
	restored, err := s.restoreFileTags(t, filePath, only)
	if err != nil {
		s.internalErr(w, "restore tags", err)
		return
	}
	updated, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"track": updated, "restored": restored})
}

func (s *Server) handleLibraryFileTags(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	query := trackSelect + ` WHERE 1 = 1`
	args := trackSelectArgs(u.ID)
	if !u.IsAdmin() {
		query += ` AND t.user_id = ?`
		args = append(args, u.ID)
	}
	rows, err := s.db.Query(query+` ORDER BY t.created_at DESC`, args...)
	if err != nil {
		s.internalErr(w, "list tracks", err)
		return
	}
	type candidate struct {
		track    *Track
		filePath string
	}
	var all []candidate
	for rows.Next() {
		t, fp, _, err := scanTrack(rows.Scan)
		if err != nil {
			rows.Close()
			s.internalErr(w, "scan track", err)
			return
		}
		all = append(all, candidate{t, fp})
	}
	rows.Close()

	diffs := []FileTagDiff{}
	for _, c := range all {
		if changes := fileTagChanges(c.track, readMetadata(filepath.Join(s.dataDir, c.filePath))); len(changes) > 0 {
			diffs = append(diffs, FileTagDiff{Track: c.track, Changes: changes})
		}
	}
	writeJSON(w, http.StatusOK, diffs)
}

func (s *Server) handleLibraryRestoreTags(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		TrackIDs []string `json:"trackIds"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if len(body.TrackIDs) > 10000 {
		writeErr(w, http.StatusBadRequest, "too many tracks at once")
		return
	}
	restored := 0
	for _, id := range body.TrackIDs {
		row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(u.ID), id)...)
		t, fp, _, err := scanTrack(row.Scan)
		if err != nil || !canEditTrack(u, t) {
			continue
		}
		fields, err := s.restoreFileTags(t, fp, nil)
		if err != nil {
			logErr("restore tags", err)
			continue
		}
		if len(fields) > 0 {
			restored++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"restored": restored})
}
