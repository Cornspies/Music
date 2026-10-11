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
	"slices"
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
		Title        string  `json:"title"`
		Artist       string  `json:"artist"`
		Album        string  `json:"album"`
		Duration     float64 `json:"duration"`
		FileName     string  `json:"fileName"`
		FileSize     int64   `json:"fileSize"`
		MimeType     string  `json:"mimeType"`
		StorageBytes int64   `json:"storageBytes"`
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
	if duration <= 0 {
		duration = probeDuration(filepath.Join(s.dataDir, up.savedPath))
	}
	out.Upload.Title, out.Upload.Artist, out.Upload.Album = title, up.meta.artist, up.meta.album
	out.Upload.Duration, out.Upload.FileName, out.Upload.MimeType = duration, up.fileName, up.mimeType
	if fi, err := os.Stat(filepath.Join(s.dataDir, up.savedPath)); err == nil {
		out.Upload.FileSize = fi.Size()
	}
	out.Upload.StorageBytes = out.Upload.FileSize + compatibleCopyBytes(filepath.Ext(up.savedPath), duration)

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

var packagingTagPrefixes = []string{"feat", "ft", "featuring", "prod", "produced by", "from", "with", "official"}

var packagingTagWords = map[string]bool{
	"official": true, "music": true, "video": true, "audio": true, "lyric": true, "lyrics": true,
	"visualizer": true, "visualiser": true, "hd": true, "hq": true, "4k": true, "1080p": true, "720p": true,
	"explicit": true, "clean": true, "original": true, "mix": true, "version": true, "mv": true,
	"remaster": true, "remastered": true, "full": true, "song": true, "ost": true, "soundtrack": true,
	"score": true, "theme": true, "album": true, "single": true, "release": true, "ncs": true,
	"monstercat": true, "no": true, "copyright": true, "free": true, "download": true,
}

func isPackagingTag(tag string) bool {
	for _, p := range packagingTagPrefixes {
		if tag == p || strings.HasPrefix(tag, p+" ") {
			return true
		}
	}
	for _, w := range strings.Fields(tag) {
		if !packagingTagWords[w] && !(len(w) == 4 && strings.Trim(w, "0123456789") == "") {
			return false
		}
	}
	return true
}

func titleTags(s string) string {
	var tags []string
	for _, m := range bracketRe.FindAllString(s, -1) {
		if tag := normalizeForMatch(m); tag != "" && !isPackagingTag(tag) {
			tags = append(tags, tag)
		}
	}
	slices.Sort(tags)
	return strings.Join(tags, "|")
}

func sameCoreTitle(a, b string) bool {
	core := coreTitle(a)
	return core != "" && core == coreTitle(b) && titleTags(a) == titleTags(b)
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
		core := loose && wantCore != "" && sameCoreTitle(t.Title, title)
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
	core := sameCoreTitle(a.Title, b.Title)
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

func (s *Server) returnPendingUpload(id string, up *pendingUpload) {
	pendingUploads.Lock()
	pendingUploads.byID[id] = up
	pendingUploads.Unlock()
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

func (s *Server) handleReplaceWithUpload(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
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
	pendingID := r.PathValue("id")
	up := s.takePendingUpload(pendingID, u.ID)
	if up == nil {
		writeErr(w, http.StatusNotFound, "that upload is no longer waiting — please upload the file again")
		return
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, up.savedPath)); err != nil {
		writeErr(w, http.StatusNotFound, "that upload is no longer waiting — please upload the file again")
		return
	}
	t, oldRel, _, ok := s.getTrack(w, r, u.ID, body.TrackID)
	if !ok {
		s.returnPendingUpload(pendingID, up)
		return
	}
	if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
		s.returnPendingUpload(pendingID, up)
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can replace it")
		return
	}
	if err := s.replaceTrackAudio(t, oldRel, up); err != nil {
		s.internalErr(w, "replace track audio", err)
		return
	}
	out, _, _, ok := s.getTrack(w, r, u.ID, t.ID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) replaceTrackAudio(t *Track, oldRel string, up *pendingUpload) error {
	id := t.ID
	stopCompatibleCopy(id)
	altRel := s.altPathFor(id)
	pendingAbs := filepath.Join(s.dataDir, up.savedPath)
	newRel := filepath.Join("uploads", id+strings.ToLower(filepath.Ext(up.savedPath)))
	newAbs := filepath.Join(s.dataDir, newRel)
	oldAbs := filepath.Join(s.dataDir, oldRel)

	var size int64
	if fi, err := os.Stat(pendingAbs); err == nil {
		size = fi.Size()
	}
	duration := up.meta.duration
	if duration <= 0 {
		duration = up.clientDur
	}
	if duration <= 0 {
		duration = probeDuration(pendingAbs)
	}
	if duration <= 0 {
		duration = t.Duration
	}

	if err := os.Rename(pendingAbs, newAbs); err != nil {
		os.Remove(pendingAbs)
		return err
	}
	_, err := s.db.Exec(`UPDATE tracks SET file_name = ?, file_path = ?, mime_type = ?, file_size = ?, file_hash = ?, duration = ?,
                alt_path = '', alt_mime = '', alt_size = 0, loudness = NULL, waveform_json = '', analyzed_at = '',
                edges_checked = 0, codec = '', updated_at = ? WHERE id = ?`,
		up.fileName, newRel, up.mimeType, size, up.hash, duration, nowRFC3339(), id)
	if err != nil {
		if newRel != oldRel {
			os.Remove(newAbs)
		}
		return err
	}

	if oldRel != newRel {
		os.Remove(oldAbs)
	}
	if altRel != "" && altRel != newRel {
		os.Remove(filepath.Join(s.dataDir, altRel))
	}
	_, _ = s.db.Exec(`DELETE FROM lyrics WHERE track_id = ? AND source <> ?`, id, lyricsSourceManual)

	if !t.HasCover && len(up.meta.picture) > 0 && safeCoverBytes(up.meta.picture) {
		if err := os.WriteFile(s.coverPath(id), shrinkCover(up.meta.picture), 0o644); err == nil {
			s.removeThumbnails(id)
			_, _ = s.db.Exec(`UPDATE tracks SET has_cover = 1, cover_source = ? WHERE id = ?`, coverFromFile, id)
			go s.updateCoverColor(id)
		} else {
			logErr("save cover", err)
		}
	}

	s.startCompatibleCopy(id, newRel)
	s.queueAnalysis(id)
	return nil
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
