package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Duplicate detection at upload.
//
// Two things count as "already in the library": the identical file (same
// SHA-256 — a re-upload of the same rip), and a different file of the same
// song (same title and artist after normalising, or the same title and a
// length within a few seconds when the artist tag is missing). Either way
// the upload is parked instead of refused: the file is kept for a while
// under a pending id, the client shows the existing tracks, and the person
// either confirms ("add anyway") — which finishes the upload without sending
// the file again — or lets it lapse.

const pendingUploadTTL = 30 * time.Minute

// DuplicateMatch is one existing track that looks like the upload.
type DuplicateMatch struct {
	Track Track `json:"track"`
	// Reason is "same file" or "same song".
	Reason string `json:"reason"`
}

// DuplicateResponse is the 409 body an upload gets when matches exist.
type DuplicateResponse struct {
	Error string `json:"error"`
	// PendingID confirms or discards the held file.
	PendingID string           `json:"pendingId"`
	Matches   []DuplicateMatch `json:"matches"`
	// What the upload itself looks like, for the "this vs that" comparison.
	Upload struct {
		Title    string  `json:"title"`
		Artist   string  `json:"artist"`
		Album    string  `json:"album"`
		Duration float64 `json:"duration"`
		FileName string  `json:"fileName"`
	} `json:"upload"`
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// findDuplicates looks for existing tracks matching an upload. The library
// is shared, so someone else's copy counts too.
func (s *Server) findDuplicates(userID string, up *pendingUpload) DuplicateResponse {
	out := DuplicateResponse{Error: "already in the library"}
	title := trimDefault(up.meta.title, strings.TrimSuffix(up.fileName, filepath.Ext(up.fileName)))
	duration := up.meta.duration
	if duration <= 0 {
		duration = up.clientDur
	}
	out.Upload.Title, out.Upload.Artist, out.Upload.Album = title, up.meta.artist, up.meta.album
	out.Upload.Duration, out.Upload.FileName = duration, up.fileName

	seen := map[string]bool{}
	add := func(t *Track, reason string) {
		if t == nil || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		out.Matches = append(out.Matches, DuplicateMatch{Track: *t, Reason: reason})
	}

	if up.hash != "" {
		rows, err := s.db.Query(trackSelect+` WHERE t.file_hash = ?`, append(trackSelectArgs(userID), up.hash)...)
		if err == nil {
			for rows.Next() {
				if t, _, _, err := scanTrack(rows.Scan); err == nil {
					add(t, "same file")
				}
			}
			rows.Close()
		}
	}

	wantTitle := normalizeForMatch(title)
	if wantTitle == "" {
		return out
	}
	wantArtist := normalizeForMatch(up.meta.artist)
	// Candidates by cheap SQL prefilter (case-insensitive title), then the
	// real comparison in Go with the same normaliser the metadata scoring
	// uses, so "Song (Remastered 2011)" and "song - remastered 2011" agree.
	rows, err := s.db.Query(trackSelect+` WHERE lower(t.title) LIKE ?`, append(trackSelectArgs(userID), "%"+firstWord(wantTitle)+"%")...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		t, _, _, err := scanTrack(rows.Scan)
		if err != nil || normalizeForMatch(t.Title) != wantTitle {
			continue
		}
		haveArtist := normalizeForMatch(dropPlaceholder(t.Artist, "Unknown Artist"))
		switch {
		case wantArtist != "" && haveArtist != "" && wantArtist == haveArtist:
			add(t, "same song")
		case duration > 0 && t.Duration > 0 && math.Abs(t.Duration-duration) <= 3:
			// Same title and the same length: near-certainly the same song,
			// whatever the artist tag says (often missing on one of them).
			add(t, "same song")
		}
	}
	return out
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

// ---- held uploads ------------------------------------------------------------

var pendingUploads = struct {
	sync.Mutex
	byID map[string]*pendingUpload
}{byID: map[string]*pendingUpload{}}

func (s *Server) holdUpload(up *pendingUpload) string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	id := hex.EncodeToString(buf)
	up.heldAt = time.Now()
	pendingUploads.Lock()
	pendingUploads.byID[id] = up
	pendingUploads.Unlock()
	return id
}

func (s *Server) takePendingUpload(id, userID string) *pendingUpload {
	pendingUploads.Lock()
	defer pendingUploads.Unlock()
	up := pendingUploads.byID[id]
	if up == nil || up.userID != userID {
		return nil
	}
	delete(pendingUploads.byID, id)
	return up
}

// expirePendingUploads deletes held files nobody decided on. Run periodically.
func (s *Server) expirePendingUploads() {
	pendingUploads.Lock()
	var stale []*pendingUpload
	for id, up := range pendingUploads.byID {
		if time.Since(up.heldAt) > pendingUploadTTL {
			stale = append(stale, up)
			delete(pendingUploads.byID, id)
		}
	}
	pendingUploads.Unlock()
	for _, up := range stale {
		os.Remove(filepath.Join(s.dataDir, up.savedPath))
	}
}

func (s *Server) startPendingUploadSweeper() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			s.expirePendingUploads()
		}
	}()
}

// handleConfirmUpload: POST /api/uploads/pending/{id} — add the held file
// after all. Same result as an upload that found no duplicates.
func (s *Server) handleConfirmUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	up := s.takePendingUpload(r.PathValue("id"), u.ID)
	if up == nil {
		writeErr(w, http.StatusNotFound, "that upload is no longer waiting — please upload the file again")
		return
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, up.savedPath)); err != nil {
		writeErr(w, http.StatusNotFound, "that upload is no longer waiting — please upload the file again")
		return
	}
	s.finalizeUpload(w, r, up)
}

// handleDiscardUpload: DELETE /api/uploads/pending/{id} — the person chose
// not to add it; the held file goes.
func (s *Server) handleDiscardUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if up := s.takePendingUpload(r.PathValue("id"), u.ID); up != nil {
		os.Remove(filepath.Join(s.dataDir, up.savedPath))
	}
	writeNoContent(w)
}

// backfillFileHashes computes hashes for tracks stored before duplicate
// detection existed, one at a time in the background.
func (s *Server) backfillFileHashes() {
	rows, err := s.db.Query(`SELECT id, file_path FROM tracks WHERE COALESCE(file_hash, '') = ''`)
	if err != nil {
		return
	}
	type row struct{ id, path string }
	var todo []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.path) == nil {
			todo = append(todo, r)
		}
	}
	rows.Close()
	for _, r := range todo {
		h, err := fileSHA256(filepath.Join(s.dataDir, r.path))
		if err != nil {
			continue
		}
		_, _ = s.db.Exec(`UPDATE tracks SET file_hash = ? WHERE id = ?`, h, r.id)
	}
}
