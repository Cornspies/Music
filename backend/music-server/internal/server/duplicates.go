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

	for _, m := range s.findSameSong(userID, title, up.meta.artist, duration, false, false) {
		t := m.Track
		add(&t, m.Reason)
	}
	return out
}

// Brackets and "feat." carry what a video title adds to a song's name —
// "(Official Video)", "[NCS Release]", "(feat. X)" — and are dropped for
// the loose comparison.
var (
	bracketRe = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
	featRe    = regexp.MustCompile(`(?i)\s+(feat|ft)\.?\s.*$`)
)

func coreTitle(s string) string {
	return normalizeForMatch(featRe.ReplaceAllString(bracketRe.ReplaceAllString(s, ""), ""))
}

// findSameSong finds library tracks that look like the same recording as
// (title, artist, duration): the same title and artist, or the same title
// and a length within a few seconds (the artist tag is often missing on one
// side). With loose, the titles may also agree with their bracketed parts
// dropped — for video titles — but then only together with the length. With
// artistVeto, a clearly different artist on both sides rules a match out
// even when the lengths agree; it's set when the artist is trustworthy (an
// "Artist - Title" split) rather than a channel name.
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
	// Candidates by cheap SQL prefilter (case-insensitive title), then the
	// real comparison in Go with the same normaliser the metadata scoring
	// uses, so "Song (Remastered 2011)" and "song - remastered 2011" agree.
	rows, err := s.db.Query(trackSelect+` WHERE lower(t.title) LIKE ?`, append(trackSelectArgs(userID), "%"+firstWord(prefilter)+"%")...)
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
			// Same name, different artist: a cover or a namesake.
		case lengthsAgree:
			// Same title and the same length: near-certainly the same song.
			out = append(out, DuplicateMatch{Track: *t, Reason: "same song"})
		}
	}
	return out
}

// sameSongPair says whether two library tracks look like the same
// recording, by the rules of findSameSong with the artist veto on (tags on
// both sides, so a different artist means a different song), and names the
// rule that matched.
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

// DuplicateGroup is a set of library tracks that look like one song.
type DuplicateGroup struct {
	Reason string  `json:"reason"`
	Tracks []Track `json:"tracks"`
}

// handleLibraryDuplicates: GET /api/library/duplicates — every group of
// tracks in the library that look like the same song, for someone to go
// through and thin out. Groups are built by joining pairs that match, so
// three copies with slightly different titles end up together.
func (s *Server) handleLibraryDuplicates(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := s.db.Query(trackSelect+` ORDER BY t.created_at ASC`, trackSelectArgs(u.ID)...)
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

	// Only tracks sharing a core title can pair up, so compare within those
	// buckets rather than every track with every other.
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
			continue // alone
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
