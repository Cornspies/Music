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

type source interface {
	key() string
	name() string
	search(ctx context.Context, query string, page int) ([]discoverResult, bool, error)
	fetch(ctx context.Context, id string) (*sourceDownload, error)
	coverAllowed(u string) bool
}

type discoverResult struct {
	Source     string  `json:"source"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album,omitempty"`
	Genre      string  `json:"genre,omitempty"`
	Year       int     `json:"year,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	CoverURL   string  `json:"coverUrl,omitempty"`
	PageURL    string  `json:"pageUrl,omitempty"`
	PreviewURL string  `json:"previewUrl,omitempty"`
	License    string  `json:"license,omitempty"`
	TrackID    string  `json:"trackId,omitempty"`
}

type sourceDownload struct {
	Body                        io.ReadCloser
	FileName                    string
	Size                        int64
	Title, Artist, Album, Genre string
	Year, TrackNo               int
	Duration                    float64
	CoverURL                    string
	SquareCover                 bool
	CatalogueCover              bool
	LookupMetadata              bool
}

type linkSource interface{ takesLinks() }

type songSearcher interface {
	searchSongs(ctx context.Context, query string) ([]discoverResult, error)
}

type DiscoverSource struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Playlists bool   `json:"playlists"`
	Links     bool   `json:"links"`
	Songs     bool   `json:"songs"`
}

type DiscoverSearch struct {
	Results  []discoverResult  `json:"results"`
	Page     int               `json:"page"`
	HasMore  bool              `json:"hasMore"`
	Playlist *DiscoverPlaylist `json:"playlist,omitempty"`
}

type DiscoverPlaylist struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
	Total int    `json:"total,omitempty"`
}

var discoverSources = []source{ncsSource{}}

func (s *Server) initDiscoverSources() {
	var list []source
	yt, err := newYTDLPSource(s.dataDir)
	switch {
	case err != nil:
		logErr("discover", fmt.Errorf("YouTube source is off: %w", err))
	case yt != nil:
		list = append(list, yt)
		log.Printf("discover: %s", yt.describe())
	}
	if !config.Flag("MUSIC_DISABLE_DEEZER") {
		dz := newDeezerSource(s.dataDir, yt)
		list = append(list, dz)
		log.Printf("discover: %s", dz.describe())
	}
	list = append(list, ncsSource{})
	if !config.Flag("MUSIC_DISABLE_SOUNDCLOUD") {
		list = append(list, soundcloudSource{})
	}
	discoverSources = list
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

const maxDiscoverBytes = 200 << 20

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
			_, links := src.(linkSource)
			_, songs := src.(songSearcher)
			out = append(out, DiscoverSource{Key: src.key(), Name: src.name(), Playlists: playlists, Links: links, Songs: songs})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

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

	if ps, ok := src.(playlistSource); ok {
		if listID := ps.playlistID(q); listID != "" {
			if page > 1 {
				writeJSON(w, http.StatusOK, DiscoverSearch{Results: []discoverResult{}, Page: page})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), importListTime)
			defer cancel()
			pl, err := ps.listPlaylist(ctx, listID)
			noteSource(src.key(), err)
			if err != nil {
				logErr("discover playlist", err)
				writeErr(w, http.StatusBadGateway, friendlyImportError(err, src.name()+" didn't answer; try again in a moment"))
				return
			}
			s.markInLibrary(userFrom(r).ID, pl.Entries)
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
	var results []discoverResult
	var more bool
	var err error
	if songs, ok := src.(songSearcher); ok && r.URL.Query().Get("only") == "songs" && !strings.Contains(q, "://") {
		if page == 1 {
			results, err = songs.searchSongs(ctx, q)
		}
	} else {
		results, more, err = src.search(ctx, q, page)
	}
	noteSource(src.key(), err)
	if err != nil {
		var status *statusError
		if errors.As(err, &status) {
			if status.code >= 500 {
				logErr("discover search", err)
			}
			writeErr(w, status.code, status.msg)
			return
		}
		logErr("discover search", err)
		writeErr(w, http.StatusBadGateway, src.name()+" didn't answer; try again in a moment")
		return
	}
	if results == nil {
		results = []discoverResult{}
	}
	s.markInLibrary(userFrom(r).ID, results)
	writeJSON(w, http.StatusOK, DiscoverSearch{Results: results, Page: page, HasMore: more})
}

func (s *Server) handleDiscoverArtwork(w http.ResponseWriter, r *http.Request) {
	src := findSource(r.URL.Query().Get("source"))
	u := r.URL.Query().Get("u")
	if src == nil || !src.coverAllowed(u) {
		writeErr(w, http.StatusBadRequest, "not a catalogue image")
		return
	}
	serveArtwork(w, r, u)
}

func serveArtwork(w http.ResponseWriter, r *http.Request, u string) {
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

func (s *Server) markInLibrary(viewerID string, results []discoverResult) {
	for i := range results {
		if id, ok := s.trackFromSource(viewerID, results[i].Source, results[i].ID); ok {
			results[i].TrackID = id
		}
	}
}

func (s *Server) trackFromSource(viewerID, source, sourceID string) (string, bool) {
	var id string
	err := s.db.QueryRow(`SELECT t.id FROM tracks t WHERE t.source = ? AND t.source_id = ? AND `+sameLibrary("t.user_id"),
		source, sourceID, viewerID).Scan(&id)
	return id, err == nil
}

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

	if existing, ok := s.trackFromSource(u.ID, src.key(), body.ID); ok {
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
	noteSource(src.key(), err)
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

func (s *Server) saveDownload(dl *sourceDownload) (id, rel, mimeType string, n int64, err error) {
	ext := strings.ToLower(filepath.Ext(dl.FileName))
	mimeType, ok := extMime[ext]
	if !ok {
		return "", "", "", 0, &statusError{http.StatusBadGateway, "the catalogue sent a file format this server doesn't accept (" + ext + ")"}
	}
	id = uuid.NewString()
	rel = filepath.Join("uploads", id+ext)
	absPath := filepath.Join(s.dataDir, rel)
	dst, err := os.Create(absPath)
	if err != nil {
		return "", "", "", 0, err
	}
	n, copyErr := io.Copy(dst, io.LimitReader(dl.Body, maxDiscoverBytes+1))
	closeErr := dst.Close()
	switch {
	case copyErr != nil:
		err = fmt.Errorf("download: %w", copyErr)
	case closeErr != nil:
		err = closeErr
	case n == 0:
		err = &statusError{http.StatusBadGateway, "the catalogue sent an empty file"}
	case n > maxDiscoverBytes:
		err = &statusError{http.StatusBadGateway, "the file is larger than this server accepts"}
	}
	if err != nil {
		os.Remove(absPath)
		return "", "", "", 0, err
	}
	return id, rel, mimeType, n, nil
}

func (s *Server) ingestDownload(ctx context.Context, u *User, sourceKey, sourceID string, dl *sourceDownload) (string, error) {
	id, rel, mimeType, n, err := s.saveDownload(dl)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(rel))
	absPath := filepath.Join(s.dataDir, rel)
	if quota := userQuotaBytes(); quota > 0 {
		used, _ := s.bytesUsedBy(u.ID)
		if used+n > quota {
			os.Remove(absPath)
			return "", &statusError{http.StatusInsufficientStorage,
				fmt.Sprintf("this track would exceed your storage limit (%s of %s used)", humanBytes(used), humanBytes(quota))}
		}
	}

	hash, _ := fileSHA256(absPath)
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
	coverSource := coverFromFile
	if dl.CatalogueCover {
		cover = nil
	}
	if len(cover) == 0 && dl.CoverURL != "" {
		if b, err := fetchArtwork(ctx, dl.CoverURL); err == nil {
			if dl.SquareCover {
				b = squareCrop(b)
			}
			cover = shrinkCover(b)
			coverSource = coverFromCatalogue
		} else {
			logErr("discover cover", err)
		}
	}
	if len(cover) > 0 && safeCoverBytes(cover) {
		if err := os.WriteFile(filepath.Join(s.covDir, id+".img"), cover, 0o644); err == nil {
			hasCover = true
		}
	}
	switch {
	case !hasCover:
		coverSource = ""
	case sourceKey == "youtube":
		coverSource = coverFromThumbnail
	}
	trackNo := meta.trackNo
	if dl.TrackNo > 0 {
		trackNo = dl.TrackNo
	}

	createdAt := nowRFC3339()
	_, err = s.db.Exec(`INSERT INTO tracks
		(id, user_id, title, artist, album, genre, year, track_no, duration,
		 file_name, file_path, mime_type, file_size, has_cover, plays, created_at,
		 alt_path, alt_mime, alt_size, updated_at, source, source_id, file_hash, cover_source, album_artist)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?,?,?,?,?,?)`,
		id, u.ID, title, artist, album, genre, year, trackNo, duration,
		sanitizeFilename(dl.FileName), rel, mimeType, n, hasCover, createdAt,
		"", "", 0, createdAt, sourceKey, sourceID, hash, coverSource, albumArtistFor(meta.albumArtist, artist))
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

const maxCoverEdge = 1000

func shrinkCover(raw []byte) []byte { return resizeCover(raw, maxCoverEdge) }

func resizeCover(raw []byte, edge int) []byte {
	if len(raw) == 0 {
		return raw
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= edge && cfg.Height <= edge {
		return raw
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxAvatarPixels {
		return raw
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, downscale(src, edge), &jpeg.Options{Quality: 88}); err != nil {
		return raw
	}
	return buf.Bytes()
}

func downscale(src image.Image, edge int) *image.RGBA {
	b := src.Bounds()
	scale := min(1, float64(edge)/float64(max(b.Dx(), b.Dy())))
	w, h := max(1, int(float64(b.Dx())*scale)), max(1, int(float64(b.Dy())*scale))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
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
	return out
}
