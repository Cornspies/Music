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
	"regexp"
	"strings"
	"sync"
	"time"
)

const pendingUploadTTL = 30 * time.Minute

type DuplicateMatch struct {
	Track  Track  `json:"track"`
	Reason string `json:"reason"`
}

type DuplicateResponse struct {
	Error     string           `json:"error"`
	PendingID string           `json:"pendingId"`
	Matches   []DuplicateMatch `json:"matches"`
	Upload    struct {
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
		rows, err := s.db.Query(libraryTrackSelect+` WHERE t.file_hash = ?`, append(trackSelectArgs(userID), up.hash)...)
		if err == nil {
			for rows.Next() {
				if t, _, _, err := scanTrack(rows.Scan); err == nil {
					add(t, "same file")
				}
			}
			rows.Close()
		}
	}

	for _, m := range s.findSameSong(userID, title, up.meta.artist, duration, false, false) {
		t := m.Track
		add(&t, m.Reason)
	}
	return out
}

var (
	bracketRe = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
	featRe    = regexp.MustCompile(`(?i)\s+(feat|ft)\.?\s.*$`)
)

func coreTitle(s string) string {
	return normalizeForMatch(featRe.ReplaceAllString(bracketRe.ReplaceAllString(s, ""), ""))
}

func (s *Server) findSameSong(userID, title, artist string, duration float64, loose, artistVeto bool) []DuplicateMatch {
	wantTitle := normalizeForMatch(title)
	wantCore := coreTitle(title)
	if wantTitle == "" {
		return nil
	}
	wantArtist := normalizeForMatch(artist)
	prefilter := wantTitle
	if loose && wantCore != "" {
		prefilter = wantCore
	}
	rows, err := s.db.Query(libraryTrackSelect+` WHERE lower(t.title) LIKE ?`, append(trackSelectArgs(userID), "%"+firstWord(prefilter)+"%")...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []DuplicateMatch
	for rows.Next() {
		t, _, _, err := scanTrack(rows.Scan)
		if err != nil {
			continue
		}
		exact := normalizeForMatch(t.Title) == wantTitle
		core := loose && wantCore != "" && coreTitle(t.Title) == wantCore
		if !exact && !core {
			continue
		}
		haveArtist := normalizeForMatch(dropPlaceholder(t.Artist, "Unknown Artist"))
		bothKnown := wantArtist != "" && haveArtist != ""
		artistsAgree := bothKnown && (wantArtist == haveArtist ||
			strings.Contains(haveArtist, wantArtist) || strings.Contains(wantArtist, haveArtist))
		lengthsAgree := duration > 0 && t.Duration > 0 && math.Abs(t.Duration-duration) <= 3
		switch {
		case exact && artistsAgree:
			out = append(out, DuplicateMatch{Track: *t, Reason: "same song"})
		case artistVeto && bothKnown && !artistsAgree:
		case lengthsAgree:
			out = append(out, DuplicateMatch{Track: *t, Reason: "same song"})
		}
	}
	return out
}

func sameSongPair(a, b *Track) (bool, string) {
	titleA, titleB := normalizeForMatch(a.Title), normalizeForMatch(b.Title)
	if titleA == "" || titleB == "" {
		return false, ""
	}
	exact := titleA == titleB
	core := coreTitle(a.Title) != "" && coreTitle(a.Title) == coreTitle(b.Title)
	if !exact && !core {
		return false, ""
	}
	artistA := normalizeForMatch(dropPlaceholder(a.Artist, "Unknown Artist"))
	artistB := normalizeForMatch(dropPlaceholder(b.Artist, "Unknown Artist"))
	bothKnown := artistA != "" && artistB != ""
	artistsAgree := bothKnown && (artistA == artistB || strings.Contains(artistA, artistB) || strings.Contains(artistB, artistA))
	lengthsAgree := a.Duration > 0 && b.Duration > 0 && math.Abs(a.Duration-b.Duration) <= 3
	switch {
	case exact && artistsAgree:
		return true, "same title and artist"
	case bothKnown && !artistsAgree:
		return false, ""
	case lengthsAgree:
		return true, "same title and length"
	}
	return false, ""
}

type DuplicateGroup struct {
	Reason string  `json:"reason"`
	Tracks []Track `json:"tracks"`
}

func (s *Server) handleLibraryDuplicates(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := s.db.Query(libraryTrackSelect+` ORDER BY t.created_at ASC`, trackSelectArgs(u.ID)...)
	if err != nil {
		s.internalErr(w, "list tracks", err)
		return
	}
	var tracks []*Track
	for rows.Next() {
		if t, _, _, err := scanTrack(rows.Scan); err == nil {
			tracks = append(tracks, t)
		}
	}
	rows.Close()

	buckets := map[string][]int{}
	for i, t := range tracks {
		if key := coreTitle(t.Title); key != "" {
			buckets[key] = append(buckets[key], i)
		}
	}
	parent := make([]int, len(tracks))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	reasons := map[int]string{}
	for _, idx := range buckets {
		for x := 0; x < len(idx); x++ {
			for y := x + 1; y < len(idx); y++ {
				if ok, why := sameSongPair(tracks[idx[x]], tracks[idx[y]]); ok {
					ra, rb := find(idx[x]), find(idx[y])
					if ra != rb {
						parent[rb] = ra
					}
					if reasons[ra] == "" || why == "same title and artist" {
						reasons[ra] = why
					}
				}
			}
		}
	}
	groupsByRoot := map[int]*DuplicateGroup{}
	var order []int
	for i, t := range tracks {
		root := find(i)
		if root == i && reasons[i] == "" {
			continue
		}
		g := groupsByRoot[root]
		if g == nil {
			g = &DuplicateGroup{Reason: reasons[root]}
			groupsByRoot[root] = g
			order = append(order, root)
		}
		g.Tracks = append(g.Tracks, *t)
	}
	out := []DuplicateGroup{}
	for _, root := range order {
		if g := groupsByRoot[root]; len(g.Tracks) > 1 {
			if g.Reason == "" {
				g.Reason = "same title"
			}
			out = append(out, *g)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i]
	}
	return s
}

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

func (s *Server) handleDiscardUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if up := s.takePendingUpload(r.PathValue("id"), u.ID); up != nil {
		os.Remove(filepath.Join(s.dataDir, up.savedPath))
	}
	writeNoContent(w)
}

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
