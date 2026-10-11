package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	dzpubBase      = "https://api.deezer.com"
	dzpubInterval  = 120 * time.Millisecond
	dzpubCacheTTL  = 6 * time.Hour
	dzpubCacheSize = 3000
	catalogMaxRels = 300
)

var errCatalogOff = &statusError{http.StatusNotFound, "online lookups are turned off on this server"}

var dzpub = struct {
	sync.Mutex
	next  time.Time
	cache map[string]dzpubCached
}{cache: map[string]dzpubCached{}}

type dzpubCached struct {
	body []byte
	at   time.Time
}

type dzpubError struct {
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func dzpubGet(ctx context.Context, path string, out any) error {
	if metadataLookupDisabled() {
		return errCatalogOff
	}
	dzpub.Lock()
	if c, ok := dzpub.cache[path]; ok && time.Since(c.at) < dzpubCacheTTL {
		dzpub.Unlock()
		return json.Unmarshal(c.body, out)
	}
	dzpub.Unlock()

	for attempt := 0; ; attempt++ {
		dzpub.Lock()
		now := time.Now()
		wait := dzpub.next.Sub(now)
		dzpub.next = now.Add(max(wait, 0) + dzpubInterval)
		dzpub.Unlock()
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dzpubBase+path, nil)
		if err != nil {
			return err
		}
		resp, err := metadataLookupClient.Do(req)
		if err != nil {
			return fmt.Errorf("deezer catalogue: %w", err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("deezer catalogue: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("deezer catalogue: status %d", resp.StatusCode)
		}
		var apiErr dzpubError
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != nil {
			if apiErr.Error.Code == 4 && attempt < 2 {
				select {
				case <-time.After(time.Second):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			if apiErr.Error.Code == 800 {
				return &statusError{http.StatusNotFound, "Deezer doesn't know that one"}
			}
			return fmt.Errorf("deezer catalogue: %s", apiErr.Error.Message)
		}
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("deezer catalogue: %w", err)
		}
		dzpub.Lock()
		if len(dzpub.cache) >= dzpubCacheSize {
			for k, c := range dzpub.cache {
				if time.Since(c.at) >= dzpubCacheTTL || len(dzpub.cache) >= dzpubCacheSize {
					delete(dzpub.cache, k)
				}
			}
		}
		dzpub.cache[path] = dzpubCached{body: body, at: time.Now()}
		dzpub.Unlock()
		return nil
	}
}

type dzpubArtist struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	PictureMedium string `json:"picture_medium"`
	PictureBig    string `json:"picture_big"`
	NbAlbum       int    `json:"nb_album"`
	NbFan         int    `json:"nb_fan"`
}

type dzpubAlbum struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	CoverMedium string `json:"cover_medium"`
	CoverBig    string `json:"cover_big"`
	ReleaseDate string `json:"release_date"`
	RecordType  string `json:"record_type"`
	NbTracks    int    `json:"nb_tracks"`
	Artist      struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

type dzpubTrack struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	Duration      int    `json:"duration"`
	TrackPosition int    `json:"track_position"`
	DiskNumber    int    `json:"disk_number"`
	Artist        struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
	Contributors []struct {
		Name string `json:"name"`
	} `json:"contributors"`
	Album struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		CoverMedium string `json:"cover_medium"`
	} `json:"album"`
}

type CatalogArtist struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Picture    string `json:"picture,omitempty"`
	Fans       int    `json:"fans"`
	Releases   int    `json:"releases"`
	TrackCount int    `json:"trackCount"`
}

type CatalogRelease struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Type       string `json:"type"`
	Year       int    `json:"year"`
	Cover      string `json:"cover,omitempty"`
	TrackCount int    `json:"trackCount"`
}

type CatalogTrack struct {
	ID       int64   `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album,omitempty"`
	AlbumID  int64   `json:"albumId,omitempty"`
	Cover    string  `json:"cover,omitempty"`
	Duration float64 `json:"duration"`
	Position int     `json:"position,omitempty"`
	Disc     int     `json:"disc,omitempty"`
	TrackID  string  `json:"trackId,omitempty"`
}

func catalogArtistFrom(a dzpubArtist) CatalogArtist {
	return CatalogArtist{ID: a.ID, Name: a.Name, Picture: a.PictureBig, Fans: a.NbFan, Releases: a.NbAlbum}
}

func catalogReleaseFrom(a dzpubAlbum, artist string) CatalogRelease {
	return CatalogRelease{
		ID: a.ID, Title: a.Title, Artist: trimDefault(a.Artist.Name, artist), Type: a.RecordType,
		Year: parseReleaseYear(a.ReleaseDate), Cover: a.CoverBig, TrackCount: a.NbTracks,
	}
}

func catalogTrackFrom(t dzpubTrack) CatalogTrack {
	names := []string{}
	for _, c := range t.Contributors {
		if c.Name != "" {
			names = append(names, c.Name)
		}
	}
	artist := t.Artist.Name
	if len(names) > 1 {
		artist = strings.Join(names, ", ")
	}
	return CatalogTrack{
		ID: t.ID, Title: t.Title, Artist: artist, Album: t.Album.Title, AlbumID: t.Album.ID,
		Cover: t.Album.CoverMedium, Duration: float64(t.Duration), Position: t.TrackPosition, Disc: t.DiskNumber,
	}
}

type libraryIndex map[string][]*Track

func (s *Server) loadLibraryIndex(userID string) (libraryIndex, error) {
	rows, err := s.db.Query(`SELECT t.id, t.title, t.artist, t.duration FROM tracks t WHERE `+visibleTo("t.user_id"), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := libraryIndex{}
	for rows.Next() {
		t := &Track{}
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Duration); err != nil {
			return nil, err
		}
		if key := coreTitle(t.Title); key != "" {
			idx[key] = append(idx[key], t)
		}
	}
	return idx, rows.Err()
}

func (idx libraryIndex) find(title, artist string, duration float64) string {
	want := normalizeForMatch(title)
	for _, t := range idx[coreTitle(title)] {
		if normalizeForMatch(t.Title) != want && !sameCoreTitle(t.Title, title) {
			continue
		}
		have := dropPlaceholder(t.Artist, "Unknown Artist")
		agree := artistsOverlap(have, artist) ||
			strings.Contains(normalizeForMatch(title), artistKey(have)) && artistKey(have) != ""
		lengthsKnown := duration > 0 && t.Duration > 0
		if lengthsKnown && math.Abs(t.Duration-duration) > 4 {
			continue
		}
		if agree || have == "" && lengthsKnown {
			return t.ID
		}
	}
	return ""
}

func (s *Server) markCatalogTracks(userID string, tracks []CatalogTrack) {
	idx, err := s.loadLibraryIndex(userID)
	if err != nil {
		logErr("catalogue library index", err)
		return
	}
	for i := range tracks {
		tracks[i].TrackID = idx.find(tracks[i].Title, tracks[i].Artist, tracks[i].Duration)
	}
}

func writeCatalogErr(w http.ResponseWriter, err error) {
	var status *statusError
	if errors.As(err, &status) {
		writeErr(w, status.code, status.msg)
		return
	}
	logErr("catalogue", err)
	writeErr(w, http.StatusBadGateway, "Deezer's catalogue didn't answer; try again in a moment")
}

func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := struct {
		Artists  []CatalogArtist  `json:"artists"`
		Releases []CatalogRelease `json:"releases"`
	}{Artists: []CatalogArtist{}, Releases: []CatalogRelease{}}
	if q == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var artists struct {
		Data []dzpubArtist `json:"data"`
	}
	if err := dzpubGet(ctx, "/search/artist?limit=8&q="+url.QueryEscape(q), &artists); err != nil {
		writeCatalogErr(w, err)
		return
	}
	var albums struct {
		Data []dzpubAlbum `json:"data"`
	}
	if err := dzpubGet(ctx, "/search/album?limit=12&q="+url.QueryEscape(q), &albums); err != nil {
		writeCatalogErr(w, err)
		return
	}
	counts := s.artistTrackCounts(userFrom(r).ID)
	for _, a := range artists.Data {
		c := catalogArtistFrom(a)
		c.TrackCount = counts[artistKey(a.Name)]
		out.Artists = append(out.Artists, c)
	}
	for _, a := range albums.Data {
		out.Releases = append(out.Releases, catalogReleaseFrom(a, ""))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) artistTrackCounts(userID string) map[string]int {
	counts := map[string]int{}
	artists, err := s.libraryArtists(userID, "")
	if err != nil {
		return counts
	}
	for _, a := range artists {
		counts[artistKey(a.Name)] = a.TrackCount
	}
	return counts
}

func catalogID(w http.ResponseWriter, raw string) (string, bool) {
	if !deezerIDRe.MatchString(raw) {
		writeErr(w, http.StatusBadRequest, "not a catalogue id")
		return "", false
	}
	return raw, true
}

func (s *Server) handleCatalogArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r.PathValue("id"))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var artist dzpubArtist
	if err := dzpubGet(ctx, "/artist/"+id, &artist); err != nil {
		writeCatalogErr(w, err)
		return
	}
	var top struct {
		Data []dzpubTrack `json:"data"`
	}
	if err := dzpubGet(ctx, "/artist/"+id+"/top?limit=10", &top); err != nil {
		writeCatalogErr(w, err)
		return
	}
	releases := []CatalogRelease{}
	for index := 0; index < catalogMaxRels; index += 100 {
		var page struct {
			Data  []dzpubAlbum `json:"data"`
			Total int          `json:"total"`
		}
		if err := dzpubGet(ctx, fmt.Sprintf("/artist/%s/albums?limit=100&index=%d", id, index), &page); err != nil {
			writeCatalogErr(w, err)
			return
		}
		for _, a := range page.Data {
			releases = append(releases, catalogReleaseFrom(a, artist.Name))
		}
		if len(page.Data) < 100 || len(releases) >= page.Total {
			break
		}
	}
	topTracks := make([]CatalogTrack, 0, len(top.Data))
	for _, t := range top.Data {
		topTracks = append(topTracks, catalogTrackFrom(t))
	}
	u := userFrom(r)
	s.markCatalogTracks(u.ID, topTracks)
	out := catalogArtistFrom(artist)
	out.TrackCount = s.artistTrackCounts(u.ID)[artistKey(artist.Name)]
	writeJSON(w, http.StatusOK, map[string]any{"artist": out, "top": topTracks, "releases": releases})
}

func (s *Server) handleCatalogRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r.PathValue("id"))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var album dzpubAlbum
	if err := dzpubGet(ctx, "/album/"+id, &album); err != nil {
		writeCatalogErr(w, err)
		return
	}
	var list struct {
		Data []dzpubTrack `json:"data"`
	}
	if err := dzpubGet(ctx, "/album/"+id+"/tracks?limit=300", &list); err != nil {
		writeCatalogErr(w, err)
		return
	}
	tracks := make([]CatalogTrack, 0, len(list.Data))
	for _, t := range list.Data {
		c := catalogTrackFrom(t)
		c.Album, c.AlbumID, c.Cover = album.Title, album.ID, album.CoverMedium
		tracks = append(tracks, c)
	}
	s.markCatalogTracks(userFrom(r).ID, tracks)
	writeJSON(w, http.StatusOK, map[string]any{"release": catalogReleaseFrom(album, ""), "tracks": tracks})
}

func (s *Server) handleCatalogImage(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("u")
	if !allowedArtworkURL(u) {
		writeErr(w, http.StatusBadRequest, "not a catalogue image")
		return
	}
	serveArtwork(w, r, u)
}

func (s *Server) handleCatalogFind(w http.ResponseWriter, r *http.Request) {
	if discoverDisabled() {
		writeErr(w, http.StatusNotFound, "discover is disabled on this server")
		return
	}
	q := r.URL.Query()
	src := findSource(q.Get("source"))
	if src == nil {
		writeErr(w, http.StatusBadRequest, "unknown source")
		return
	}
	title, artist := strings.TrimSpace(q.Get("title")), strings.TrimSpace(q.Get("artist"))
	if title == "" {
		writeErr(w, http.StatusBadRequest, "a title is needed")
		return
	}
	duration, _ := strconv.ParseFloat(q.Get("duration"), 64)
	want := upgradeTrack{title: title, artist: artist, duration: duration}

	var found *discoverResult
	if id := q.Get("id"); src.key() == "deezer" && deezerIDRe.MatchString(id) {
		found = &discoverResult{Source: src.key(), ID: id, Title: title, Artist: artist, Duration: duration,
			PageURL: "https://www.deezer.com/track/" + id}
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		var err error
		found, err = s.findOnSource(ctx, src, want)
		noteSource(src.key(), err)
		if err != nil {
			var status *statusError
			if errors.As(err, &status) {
				writeErr(w, status.code, status.msg)
				return
			}
			logErr("catalogue find", err)
			writeErr(w, http.StatusBadGateway, src.name()+" didn't answer; try again in a moment")
			return
		}
	}
	if found != nil {
		if id, ok := s.trackFromSource(userFrom(r).ID, found.Source, found.ID); ok {
			found.TrackID = id
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": found})
}

func (s *Server) findOnSource(ctx context.Context, src source, want upgradeTrack) (*discoverResult, error) {
	artist := splitArtists(want.artist)
	lead := ""
	if len(artist) > 0 {
		lead = artist[0]
	}
	query := strings.TrimSpace(lead + " " + want.title)
	if songs, ok := src.(songSearcher); ok {
		results, err := songs.searchSongs(ctx, query)
		if err == nil {
			for i := range results {
				if sameRecording(want, results[i]) {
					return &results[i], nil
				}
			}
		} else {
			logErr("catalogue songs search", err)
		}
	}
	return s.searchRecording(ctx, src, want, query)
}

func (s *Server) handleCatalogAdd(w http.ResponseWriter, r *http.Request) {
	if discoverDisabled() {
		writeErr(w, http.StatusNotFound, "discover is disabled on this server")
		return
	}
	var body struct {
		Source  string           `json:"source"`
		Title   string           `json:"title"`
		Entries []discoverResult `json:"entries"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	src := findSource(body.Source)
	if src == nil {
		writeErr(w, http.StatusBadRequest, "unknown source")
		return
	}
	if len(body.Entries) == 0 || len(body.Entries) > 500 {
		writeErr(w, http.StatusBadRequest, "pick between 1 and 500 tracks")
		return
	}
	entries := make([]discoverResult, 0, len(body.Entries))
	for _, e := range body.Entries {
		if strings.TrimSpace(e.ID) == "" {
			continue
		}
		e.Source = src.key()
		entries = append(entries, e)
	}
	u := userFrom(r)
	job := &importJob{
		PlaylistImport: PlaylistImport{
			ID: uuid.NewString(), Source: src.key(), Title: trimDefault(strings.TrimSpace(body.Title), "Tracks from "+src.name()),
			State: importQueued, Total: len(entries),
			Failures: []ImportFailure{}, ExistingTracks: []ImportExisting{}, PossibleDuplicates: []ImportDuplicate{},
			StartedAt: nowRFC3339(),
		},
		user: u, src: src, preset: entries, noPlaylist: true,
	}
	imports.Lock()
	select {
	case imports.queue <- job:
		imports.byID[job.ID] = job
	default:
		imports.Unlock()
		writeErr(w, http.StatusTooManyRequests, "too many imports are waiting; try again later")
		return
	}
	imports.Unlock()
	writeJSON(w, http.StatusAccepted, job.snapshot())
}
