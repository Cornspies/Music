package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"music-server/internal/config"
)

// Discover: search an online catalogue of freely downloadable music and add
// a track to the library with one click. The server does the download, so
// the file arrives tagged, with cover art, and goes through the same
// pipeline as an upload (transcode, analysis, quota).
//
// Catalogues implement source: NCS (ncs.go) always, YouTube through yt-dlp
// (ytdlp.go) when the server is configured for it. A track remembers where
// it came from (tracks.source / source_id), which is how results are marked
// "already in the library" and duplicates are refused.

// source is an online catalogue that can be searched and downloaded from.
type source interface {
	// key is the short id used in the API ("ncs").
	key() string
	// name is what people see.
	name() string
	// search returns one page of matches and whether there is a next page.
	search(ctx context.Context, query string, page int) ([]discoverResult, bool, error)
	// fetch downloads one track. The caller closes the body.
	fetch(ctx context.Context, id string) (*sourceDownload, error)
	// coverAllowed says whether the artwork proxy may fetch this URL: only
	// the catalogue's own image host, so the proxy can't be pointed elsewhere.
	coverAllowed(u string) bool
}

// discoverResult is one search hit, as the clients see it.
type discoverResult struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Genre  string `json:"genre,omitempty"`
	Year   int    `json:"year,omitempty"`
	// Duration in seconds, when the catalogue lists it.
	Duration float64 `json:"duration,omitempty"`
	CoverURL string  `json:"coverUrl,omitempty"`
	// PageURL is the track's page on the catalogue's own site.
	PageURL string `json:"pageUrl,omitempty"`
	// PreviewURL streams the track from the catalogue for a listen before
	// adding; empty when the catalogue offers none.
	PreviewURL string `json:"previewUrl,omitempty"`
	// TrackID is set when this result is already in the library.
	TrackID string `json:"trackId,omitempty"`
}

// sourceDownload is a track being pulled from a catalogue.
type sourceDownload struct {
	Body     io.ReadCloser
	FileName string // with the audio extension; decides the MIME type
	Size     int64  // -1 when unknown
	// Tags for the library entry, since downloads are often untagged files.
	Title, Artist, Album, Genre string
	Year                        int
	// Duration in seconds when the catalogue knows it; only MP3s can be
	// measured without ffprobe.
	Duration float64
	CoverURL string
	// SquareCover crops the middle square out of a wide image (a video
	// thumbnail) instead of storing it as is.
	SquareCover bool
	// LookupMetadata puts the new track on the metadata queue, for sources
	// whose tags are guesses rather than a catalogue's record.
	LookupMetadata bool
}

// DiscoverSource describes a catalogue to the clients.
type DiscoverSource struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Playlists says a pasted playlist link can be imported (import.go).
	Playlists bool `json:"playlists"`
}

// DiscoverSearch is what GET /api/discover/search returns.
type DiscoverSearch struct {
	Results []discoverResult `json:"results"`
	Page    int              `json:"page"`
	HasMore bool             `json:"hasMore"`
	// Playlist is set when the query was a playlist link: the results are
	// its tracks, and the whole thing can be imported.
	Playlist *DiscoverPlaylist `json:"playlist,omitempty"`
}

// DiscoverPlaylist names a catalogue playlist behind a pasted link. Count
// is how many of its tracks are listed (and would be imported); Total how
// many it has, when a long one was cut short.
type DiscoverPlaylist struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
	Total int    `json:"total,omitempty"`
}

var discoverSources = []source{ncsSource{}}

// initDiscoverSources adds the optional sources once the environment is
// known (package init runs before .env is read). Called at start-up.
func (s *Server) initDiscoverSources() {
	yt, err := newYTDLPSource(s.dataDir)
	if err != nil {
		logErr("discover", fmt.Errorf("YouTube source is off: %w", err))
		return
	}
	if yt != nil {
		discoverSources = append(discoverSources, yt)
		log.Printf("discover: %s", yt.describe())
	}
}

func discoverDisabled() bool { return config.Flag("MUSIC_DISABLE_DISCOVER") }

func findSource(key string) source {
	for _, s := range discoverSources {
		if s.key() == key {
			return s
		}
	}
	return nil
}

// maxDiscoverBytes caps one catalogue download; NCS files are 5–15 MB.
const maxDiscoverBytes = 200 << 20

// discoverClient downloads from catalogues. Longer timeout than the metadata
// lookups (a whole file comes down), same private-network guard.
var discoverClient = &http.Client{
	Timeout:       2 * time.Minute,
	CheckRedirect: metadataLookupClient.CheckRedirect,
	Transport:     metadataLookupClient.Transport,
}

func (s *Server) handleDiscoverSources(w http.ResponseWriter, _ *http.Request) {
	out := []DiscoverSource{}
	if !discoverDisabled() {
		for _, src := range discoverSources {
			_, playlists := src.(playlistSource)
			out = append(out, DiscoverSource{Key: src.key(), Name: src.name(), Playlists: playlists})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDiscoverSearch: GET /api/discover/search?source=ncs&q=…&page=1
func (s *Server) handleDiscoverSearch(w http.ResponseWriter, r *http.Request) {
	if discoverDisabled() {
		writeErr(w, http.StatusNotFound, "discover is disabled on this server")
		return
	}
	src := findSource(r.URL.Query().Get("source"))
	if src == nil {
		writeErr(w, http.StatusBadRequest, "unknown source")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, DiscoverSearch{Results: []discoverResult{}, Page: 1})
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	// A playlist link lists the playlist, so it can be looked over and then
	// imported as a whole.
	if ps, ok := src.(playlistSource); ok {
		if listID := ps.playlistID(q); listID != "" {
			if page > 1 {
				writeJSON(w, http.StatusOK, DiscoverSearch{Results: []discoverResult{}, Page: page})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), importListTime)
			defer cancel()
			pl, err := ps.listPlaylist(ctx, listID)
			if err != nil {
				logErr("discover playlist", err)
				writeErr(w, http.StatusBadGateway, friendlyImportError(err, src.name()+" didn't answer; try again in a moment"))
				return
			}
			s.markInLibrary(pl.Entries)
			info := &DiscoverPlaylist{ID: listID, Title: trimDefault(pl.Title, src.name()+" playlist"), Count: len(pl.Entries)}
			if pl.Total > len(pl.Entries) {
				info.Total = pl.Total
			}
			writeJSON(w, http.StatusOK, DiscoverSearch{Results: pl.Entries, Page: 1, Playlist: info})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	results, more, err := src.search(ctx, q, page)
	if err != nil {
		logErr("discover search", err)
		writeErr(w, http.StatusBadGateway, src.name()+" didn't answer; try again in a moment")
		return
	}
	if results == nil {
		results = []discoverResult{}
	}
	s.markInLibrary(results)
	writeJSON(w, http.StatusOK, DiscoverSearch{Results: results, Page: page, HasMore: more})
}

// handleDiscoverArtwork: GET /api/discover/artwork?source=ncs&u=<url>.
// Result thumbnails go through here rather than straight to the catalogue's
// CDN, so the page's img-src policy stays 'self' and the browser never talks
// to a third party. A media route: <img> can't send headers.
func (s *Server) handleDiscoverArtwork(w http.ResponseWriter, r *http.Request) {
	src := findSource(r.URL.Query().Get("source"))
	u := r.URL.Query().Get("u")
	if src == nil || !src.coverAllowed(u) {
		writeErr(w, http.StatusBadRequest, "not a catalogue image")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	b, err := fetchArtwork(ctx, u)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "image unavailable")
		return
	}
	ct := http.DetectContentType(b)
	if !allowedImageTypes[ct] {
		writeErr(w, http.StatusBadGateway, "not an image")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// markInLibrary fills TrackID on results that were added before.
func (s *Server) markInLibrary(results []discoverResult) {
	for i := range results {
		var id string
		err := s.db.QueryRow(`SELECT id FROM tracks WHERE source = ? AND source_id = ?`,
			results[i].Source, results[i].ID).Scan(&id)
		if err == nil {
			results[i].TrackID = id
		}
	}
}

// handleDiscoverAdd: POST /api/discover/add {"source":"ncs","id":"…"}.
// Answers 200 with the existing track when it is already in the library,
// 201 with the new one otherwise.
func (s *Server) handleDiscoverAdd(w http.ResponseWriter, r *http.Request) {
	if discoverDisabled() {
		writeErr(w, http.StatusNotFound, "discover is disabled on this server")
		return
	}
	u := userFrom(r)
	var body struct {
		Source string `json:"source"`
		ID     string `json:"id"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	src := findSource(body.Source)
	if src == nil || strings.TrimSpace(body.ID) == "" {
		writeErr(w, http.StatusBadRequest, "unknown source or track")
		return
	}

	var existing string
	if err := s.db.QueryRow(`SELECT id FROM tracks WHERE source = ? AND source_id = ?`, src.key(), body.ID).Scan(&existing); err == nil {
		if t, _, _, ok := s.getTrack(w, r, u.ID, existing); ok {
			writeJSON(w, http.StatusOK, t)
		}
		return
	}

	if quota := userQuotaBytes(); quota > 0 {
		used, err := s.bytesUsedBy(u.ID)
		if err != nil {
			s.internalErr(w, "check quota", err)
			return
		}
		if used >= quota {
			writeErr(w, http.StatusInsufficientStorage,
				fmt.Sprintf("storage limit reached (%s of %s used)", humanBytes(used), humanBytes(quota)))
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	dl, err := src.fetch(ctx, body.ID)
	var status *statusError
	if err != nil {
		logErr("discover fetch", err)
		if errors.As(err, &status) {
			writeErr(w, status.code, status.msg)
			return
		}
		writeErr(w, http.StatusBadGateway, "couldn't download from "+src.name()+"; try again later")
		return
	}
	defer dl.Body.Close()

	t, err := s.ingestDownload(ctx, u, src.key(), body.ID, dl)
	if errors.As(err, &status) {
		writeErr(w, status.code, status.msg)
		return
	}
	if err != nil {
		s.internalErr(w, "discover add", err)
		return
	}
	out, _, _, ok := s.getTrack(w, r, u.ID, t)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// ingestDownload stores a catalogue download exactly as an upload would be
// stored — file under uploads/, cover under covers/, a tracks row — then
// kicks off the compatible copy and analysis. Returns the new track id.
func (s *Server) ingestDownload(ctx context.Context, u *User, sourceKey, sourceID string, dl *sourceDownload) (string, error) {
	ext := strings.ToLower(filepath.Ext(dl.FileName))
	mimeType, ok := extMime[ext]
	if !ok {
		return "", &statusError{http.StatusBadGateway, "the catalogue sent a file format this server doesn't accept (" + ext + ")"}
	}
	id := uuid.NewString()
	rel := filepath.Join("uploads", id+ext)
	absPath := filepath.Join(s.dataDir, rel)
	dst, err := os.Create(absPath)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(dst, io.LimitReader(dl.Body, maxDiscoverBytes+1))
	closeErr := dst.Close()
	switch {
	case copyErr != nil:
		os.Remove(absPath)
		return "", fmt.Errorf("download: %w", copyErr)
	case closeErr != nil:
		os.Remove(absPath)
		return "", closeErr
	case n == 0:
		os.Remove(absPath)
		return "", &statusError{http.StatusBadGateway, "the catalogue sent an empty file"}
	case n > maxDiscoverBytes:
		os.Remove(absPath)
		return "", &statusError{http.StatusBadGateway, "the file is larger than this server accepts"}
	}
	if quota := userQuotaBytes(); quota > 0 {
		used, _ := s.bytesUsedBy(u.ID)
		if used+n > quota {
			os.Remove(absPath)
			return "", &statusError{http.StatusInsufficientStorage,
				fmt.Sprintf("this track would exceed your storage limit (%s of %s used)", humanBytes(used), humanBytes(quota))}
		}
	}

	hash, _ := fileSHA256(absPath)
	// The catalogue's listing is the curated record; the file's own tags,
	// when there are any, tend to be sloppier ("Artist - Title [NCS
	// Release]" as the title) and only fill what the listing lacks.
	meta := readMetadata(absPath)
	title := trimDefault(dl.Title, trimDefault(meta.title, strings.TrimSuffix(dl.FileName, ext)))
	artist := trimDefault(dl.Artist, trimDefault(meta.artist, "Unknown Artist"))
	album := trimDefault(dl.Album, trimDefault(meta.album, "Unknown Album"))
	genre := trimDefault(dl.Genre, meta.genre)
	year := dl.Year
	if year == 0 {
		year = meta.year
	}
	duration := meta.duration
	if duration <= 0 {
		duration = dl.Duration
	}
	if duration <= 0 {
		duration = probeDuration(absPath)
	}

	hasCover := false
	cover := shrinkCover(meta.picture)
	if len(cover) == 0 && dl.CoverURL != "" {
		if b, err := fetchArtwork(ctx, dl.CoverURL); err == nil {
			if dl.SquareCover {
				b = squareCrop(b)
			}
			cover = shrinkCover(b)
		} else {
			logErr("discover cover", err)
		}
	}
	if len(cover) > 0 && safeCoverBytes(cover) {
		if err := os.WriteFile(filepath.Join(s.covDir, id+".img"), cover, 0o644); err == nil {
			hasCover = true
		}
	}

	createdAt := nowRFC3339()
	_, err = s.db.Exec(`INSERT INTO tracks
		(id, user_id, title, artist, album, genre, year, track_no, duration,
		 file_name, file_path, mime_type, file_size, has_cover, plays, created_at,
		 alt_path, alt_mime, alt_size, updated_at, source, source_id, file_hash)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?,?,?,?)`,
		id, u.ID, title, artist, album, genre, year, meta.trackNo, duration,
		sanitizeFilename(dl.FileName), rel, mimeType, n, hasCover, createdAt,
		"", "", 0, createdAt, sourceKey, sourceID, hash)
	if err != nil {
		os.Remove(absPath)
		os.Remove(filepath.Join(s.covDir, id+".img"))
		return "", err
	}
	s.startCompatibleCopy(id, rel)
	s.queueAnalysis(id)
	if hasCover {
		go s.updateCoverColor(id)
	}
	if dl.LookupMetadata {
		if err := s.enqueueMetadataLookup(id, reasonUpload, u.ID); err != nil {
			logErr("queue metadata lookup", err)
		}
	}
	return id, nil
}

// Catalogues hand out print-resolution art (NCS: 3000×3000, ~4 MB). Every
// list row fetches the cover, so that's brought down to what screens use.
const maxCoverEdge = 1000

// shrinkCover returns a JPEG no larger than maxCoverEdge on a side, or the
// input unchanged when it is already small enough or can't be decoded (the
// raster check later decides whether it's served at all).
func shrinkCover(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= maxCoverEdge && cfg.Height <= maxCoverEdge {
		return raw
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxAvatarPixels {
		return raw
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	b := src.Bounds()
	scale := float64(maxCoverEdge) / float64(max(b.Dx(), b.Dy()))
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	// Box filter: each output pixel averages the block of source pixels it
	// covers, which is what you want when shrinking by 3× or more.
	for y := 0; y < h; y++ {
		y0 := b.Min.Y + y*b.Dy()/h
		y1 := max(y0+1, b.Min.Y+(y+1)*b.Dy()/h)
		for x := 0; x < w; x++ {
			x0 := b.Min.X + x*b.Dx()/w
			x1 := max(x0+1, b.Min.X+(x+1)*b.Dx()/w)
			var r, g, bl, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr>>8), g+uint64(cg>>8), bl+uint64(cb>>8), n+1
				}
			}
			out.Set(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(bl / n), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 88}); err != nil {
		return raw
	}
	return buf.Bytes()
}
