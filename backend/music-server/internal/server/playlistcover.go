package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const playlistCoverSize = 600

func (s *Server) playlistCoverPath(id string) string {
	return filepath.Join(s.dataDir, "playlist-covers", id+".jpg")
}

func (s *Server) handlePlaylistCover(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getPlaylist(w, userFrom(r), r.PathValue("id"))
	if !ok {
		return
	}
	if p.CoverVersion == "" {
		writeErr(w, http.StatusNotFound, "no cover")
		return
	}
	b, err := os.ReadFile(s.playlistCoverPath(p.ID))
	if err != nil {
		writeErr(w, http.StatusNotFound, "no cover")
		return
	}
	serveJPEG(w, r, b)
}

func (s *Server) handleUploadPlaylistCover(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	if _, ok := s.requireOwnPlaylist(w, u, id); !ok {
		return
	}
	raw, ok := s.readUploadedImage(w, r)
	if !ok {
		return
	}
	square, err := decodeSquareJPEG(raw, parseCropRect(r), playlistCoverSize)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that file isn't a readable image (JPEG, PNG or GIF)")
		return
	}
	target := s.playlistCoverPath(id)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		s.internalErr(w, "create playlist cover dir", err)
		return
	}
	if err := writeFileAtomic(target, square); err != nil {
		s.internalErr(w, "save playlist cover", err)
		return
	}
	s.setPlaylistCoverVersion(w, u, id, strconv.FormatInt(time.Now().UnixMilli(), 36))
}

func (s *Server) handleDeletePlaylistCover(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	if _, ok := s.requireOwnPlaylist(w, u, id); !ok {
		return
	}
	os.Remove(s.playlistCoverPath(id))
	s.setPlaylistCoverVersion(w, u, id, "")
}

func (s *Server) setPlaylistCoverVersion(w http.ResponseWriter, u *User, id, version string) {
	if _, err := s.db.Exec(`UPDATE playlists SET cover_version = ? WHERE id = ? AND user_id = ?`, version, id, u.ID); err != nil {
		s.internalErr(w, "update playlist cover", err)
		return
	}
	p, ok := s.getPlaylist(w, u, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) removePlaylistCoversOf(userID string) {
	rows, err := s.db.Query(`SELECT id FROM playlists WHERE user_id = ? AND cover_version <> ''`, userID)
	if err != nil {
		logErr("list playlist covers", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		os.Remove(s.playlistCoverPath(id))
	}
}
