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
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	fieldLookupTimeout = 40 * time.Second
	fieldLookupAlbums  = 12
	fieldLookupSlack   = 20.0
)

type FieldOption struct {
	Source   string  `json:"source"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	AlbumArt string  `json:"albumArtist,omitempty"`
	Year     int     `json:"year"`
	Genre    string  `json:"genre"`
	Type     string  `json:"type"`
	Cover    string  `json:"cover,omitempty"`
	Duration float64 `json:"duration"`
	URL      string  `json:"url,omitempty"`
}

type LookupSource struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func fieldLookupSources() []LookupSource {
	out := []LookupSource{}
	if !metadataLookupDisabled() {
		out = append(out,
			LookupSource{"deezer", "Deezer"},
			LookupSource{"musicbrainz", "MusicBrainz"},
			LookupSource{"itunes", "Apple Music"},
		)
	}
	if _, ok := findSource("youtube").(songSearcher); ok {
		out = append(out, LookupSource{"youtube", "YouTube Music"})
	}
	if findSource("soundcloud") != nil {
		out = append(out, LookupSource{"soundcloud", "SoundCloud"})
	}
	return out
}

func (s *Server) handleFieldLookupSources(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, fieldLookupSources())
}

func (s *Server) handleFieldLookup(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, _, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	if !canEditTrack(u, t) {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
		return
	}
	q := r.URL.Query()
	key := q.Get("source")
	if !slices.ContainsFunc(fieldLookupSources(), func(s LookupSource) bool { return s.Key == key }) {
		writeErr(w, http.StatusBadRequest, "that source isn't available on this server")
		return
	}
	title := trimDefault(strings.TrimSpace(q.Get("title")), t.Title)
	artist := trimDefault(strings.TrimSpace(q.Get("artist")), dropPlaceholder(t.Artist, "Unknown Artist"))
	if strings.TrimSpace(title) == "" {
		writeErr(w, http.StatusBadRequest, "the track needs a title to look it up")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), fieldLookupTimeout)
	defer cancel()
	var opts []FieldOption
	var err error
	switch key {
	case "deezer":
		opts, err = lookupDeezerReleases(ctx, title, artist)
	case "musicbrainz":
		opts, err = lookupMusicBrainzReleases(ctx, title, artist)
	case "itunes":
		opts, err = lookupItunesReleases(ctx, title, artist)
	case "youtube", "soundcloud":
		opts, err = lookupSourceReleases(ctx, findSource(key), title, artist)
	}
	if err != nil {
		if ctx.Err() != nil && r.Context().Err() == nil {
			writeErr(w, http.StatusGatewayTimeout, "the lookup took too long; try again")
			return
		}
		var status *statusError
		if errors.As(err, &status) {
			writeErr(w, status.code, status.msg)
			return
		}
		if errors.Is(err, errMusicBrainzBusy) {
			writeErr(w, http.StatusServiceUnavailable, "MusicBrainz is busy right now; try again in a minute")
			return
		}
		logErr("field lookup ("+key+")", err)
		writeErr(w, http.StatusBadGateway, "the source didn't answer; try again in a moment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": filterFieldOptions(opts, title, artist, t.Duration)})
}

func filterFieldOptions(opts []FieldOption, title, artist string, duration float64) []FieldOption {
	out := []FieldOption{}
	seen := map[string]bool{}
	for _, o := range opts {
		o.Album = strings.TrimSpace(o.Album)
		if !sameSongTitle(o.Title, title) {
			continue
		}
		if artist != "" && o.Artist != "" && !artistsOverlap(o.Artist, artist) &&
			!strings.Contains(normalizeForMatch(o.Artist), artistKey(artist)) &&
			!strings.Contains(artistKey(artist), normalizeForMatch(o.Artist)) {
			continue
		}
		if duration > 0 && o.Duration > 0 && math.Abs(duration-o.Duration) > fieldLookupSlack {
			continue
		}
		key := strings.Join([]string{normalizeForMatch(o.Album), strconv.Itoa(o.Year), normalizeForMatch(o.Artist), normalizeForMatch(o.Genre)}, "|")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, o)
	}
	slices.SortStableFunc(out, func(a, b FieldOption) int {
		switch {
		case a.Year != 0 && b.Year == 0:
			return -1
		case a.Year == 0 && b.Year != 0:
			return 1
		case a.Year != b.Year:
			return a.Year - b.Year
		case releaseRank(a.Type) != releaseRank(b.Type):
			return releaseRank(a.Type) - releaseRank(b.Type)
		}
		return strings.Compare(strings.ToLower(a.Album), strings.ToLower(b.Album))
	})
	return out
}

func releaseRank(kind string) int {
	switch kind {
	case "album":
		return 0
	case "single", "ep":
		return 1
	case "", "other":
		return 2
	case "compile":
		return 4
	}
	return 3
}

func sameSongTitle(theirs, ours string) bool {
	core := coreTitle(ours)
	if core == "" || coreTitle(theirs) != core {
		return false
	}
	a, b := normalizeForMatch(theirs), normalizeForMatch(ours)
	for _, w := range variantWords {
		if hasWord(a, w) != hasWord(b, w) {
			return false
		}
	}
	return true
}

func quoteSearch(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, " ") + `"`
}

func leadArtistName(artist string) string {
	if parts := splitArtists(artist); len(parts) > 0 {
		return parts[0]
	}
	return artist
}

func lookupDeezerReleases(ctx context.Context, title, artist string) ([]FieldOption, error) {
	var found struct {
		Data []dzpubTrack `json:"data"`
	}
	query := strings.TrimSpace(leadArtistName(artist) + " " + title)
	if err := dzpubGet(ctx, "/search?limit=50&q="+url.QueryEscape(query), &found); err != nil {
		return nil, err
	}
	type albumInfo struct {
		ReleaseDate string `json:"release_date"`
		RecordType  string `json:"record_type"`
		CoverBig    string `json:"cover_big"`
		Artist      struct {
			Name string `json:"name"`
		} `json:"artist"`
		Genres struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		} `json:"genres"`
	}
	albums := map[int64]*albumInfo{}
	out := []FieldOption{}
	for _, t := range found.Data {
		o := FieldOption{
			Source: "Deezer", Title: t.Title, Artist: t.Artist.Name, Album: t.Album.Title,
			Cover: t.Album.CoverMedium, Duration: float64(t.Duration),
			URL: "https://www.deezer.com/album/" + strconv.FormatInt(t.Album.ID, 10),
		}
		if t.Album.ID != 0 && normalizeForMatch(t.Title) != "" {
			info, ok := albums[t.Album.ID]
			if !ok && len(albums) < fieldLookupAlbums && sameSongTitle(t.Title, title) {
				info = &albumInfo{}
				if err := dzpubGet(ctx, "/album/"+strconv.FormatInt(t.Album.ID, 10), info); err != nil {
					info = nil
				}
				albums[t.Album.ID] = info
			}
			if info != nil {
				o.Year, o.Type = parseReleaseYear(info.ReleaseDate), info.RecordType
				o.AlbumArt = albumArtistFor(info.Artist.Name, t.Artist.Name)
				if info.CoverBig != "" {
					o.Cover = info.CoverBig
				}
				if len(info.Genres.Data) > 0 {
					o.Genre = info.Genres.Data[0].Name
				}
			}
		}
		out = append(out, o)
	}
	return out, nil
}

type mbReleaseSearch struct {
	Recordings []struct {
		Title        string `json:"title"`
		Length       int    `json:"length"`
		ArtistCredit []struct {
			Name       string `json:"name"`
			JoinPhrase string `json:"joinphrase"`
		} `json:"artist-credit"`
		Releases []struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			Date         string `json:"date"`
			Status       string `json:"status"`
			ReleaseGroup struct {
				PrimaryType    string   `json:"primary-type"`
				SecondaryTypes []string `json:"secondary-types"`
			} `json:"release-group"`
		} `json:"releases"`
	} `json:"recordings"`
}

func lookupMusicBrainzReleases(ctx context.Context, title, artist string) ([]FieldOption, error) {
	if mbInCooldown() {
		return nil, errMusicBrainzBusy
	}
	query := "recording:" + quoteSearch(title)
	if artist != "" {
		query += " AND artist:" + quoteSearch(leadArtistName(artist))
	}
	var parsed mbReleaseSearch
	if err := musicbrainzGet(ctx, url.Values{"query": {query}, "fmt": {"json"}, "limit": {"25"}}, &parsed); err != nil {
		return nil, err
	}
	out := []FieldOption{}
	for _, rec := range parsed.Recordings {
		var credit strings.Builder
		for _, c := range rec.ArtistCredit {
			credit.WriteString(c.Name + c.JoinPhrase)
		}
		for _, rel := range rec.Releases {
			if rel.Status != "" && rel.Status != "Official" {
				continue
			}
			kind := strings.ToLower(rel.ReleaseGroup.PrimaryType)
			if slices.Contains(rel.ReleaseGroup.SecondaryTypes, "Compilation") {
				kind = "compile"
			}
			out = append(out, FieldOption{
				Source: "MusicBrainz", Title: rec.Title, Artist: credit.String(), Album: rel.Title,
				Year: parseReleaseYear(rel.Date), Type: kind, Duration: float64(rec.Length) / 1000,
				Cover: "https://coverartarchive.org/release/" + rel.ID + "/front-250",
				URL:   "https://musicbrainz.org/release/" + rel.ID,
			})
		}
	}
	return out, nil
}

func lookupItunesReleases(ctx context.Context, title, artist string) ([]FieldOption, error) {
	term := strings.TrimSpace(leadArtistName(artist) + " " + title)
	q := url.Values{"term": {term}, "media": {"music"}, "entity": {"song"}, "limit": {"50"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, itunesSearchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes search: status %d", resp.StatusCode)
	}
	var parsed struct {
		Results []struct {
			TrackName         string  `json:"trackName"`
			ArtistName        string  `json:"artistName"`
			CollectionName    string  `json:"collectionName"`
			CollectionArtist  string  `json:"collectionArtistName"`
			CollectionViewURL string  `json:"collectionViewUrl"`
			PrimaryGenreName  string  `json:"primaryGenreName"`
			ReleaseDate       string  `json:"releaseDate"`
			ArtworkURL100     string  `json:"artworkUrl100"`
			TrackTimeMillis   float64 `json:"trackTimeMillis"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := []FieldOption{}
	for _, r := range parsed.Results {
		album, kind := r.CollectionName, "album"
		for suffix, k := range map[string]string{" - Single": "single", " - EP": "ep"} {
			if strings.HasSuffix(album, suffix) {
				album, kind = strings.TrimSuffix(album, suffix), k
			}
		}
		out = append(out, FieldOption{
			Source: "Apple Music", Title: r.TrackName, Artist: r.ArtistName, Album: album,
			AlbumArt: albumArtistFor(r.CollectionArtist, r.ArtistName),
			Year:     parseReleaseYear(r.ReleaseDate), Genre: r.PrimaryGenreName, Type: kind,
			Cover:    strings.Replace(r.ArtworkURL100, "100x100bb", "600x600bb", 1),
			Duration: r.TrackTimeMillis / 1000, URL: r.CollectionViewURL,
		})
	}
	return out, nil
}

func lookupSourceReleases(ctx context.Context, src source, title, artist string) ([]FieldOption, error) {
	query := strings.TrimSpace(leadArtistName(artist) + " " + title)
	var results []discoverResult
	var err error
	if songs, ok := src.(songSearcher); ok {
		results, err = songs.searchSongs(ctx, query)
	} else {
		results, _, err = src.search(ctx, query, 1)
	}
	if err != nil {
		return nil, err
	}
	out := []FieldOption{}
	for _, r := range results {
		cover := r.CoverURL
		if src.key() == "youtube" {
			cover = "https://i.ytimg.com/vi/" + r.ID + "/hqdefault.jpg"
		}
		out = append(out, FieldOption{
			Source: src.name(), Title: r.Title, Artist: r.Artist, Album: r.Album, Year: r.Year,
			Genre: r.Genre, Cover: cover, Duration: r.Duration, URL: r.PageURL,
		})
	}
	return out, nil
}

var artworkHosts = []string{
	"dzcdn.net", "mzstatic.com", "coverartarchive.org", "archive.org",
	"i.ytimg.com", "lh3.googleusercontent.com", "sndcdn.com",
}

func allowedArtworkURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range artworkHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func (s *Server) handleSetCoverFromURL(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, _, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	if !canEditTrack(u, t) {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its cover")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if !allowedArtworkURL(body.URL) {
		writeErr(w, http.StatusBadRequest, "that isn't a cover from a known catalogue")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	raw, err := fetchArtwork(ctx, body.URL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't download that cover")
		return
	}
	if strings.EqualFold(mustHost(body.URL), "i.ytimg.com") {
		raw = squareCrop(raw)
	}
	if !safeCoverBytes(raw) {
		writeErr(w, http.StatusBadGateway, "that cover isn't an image")
		return
	}
	if err := os.WriteFile(s.coverPath(t.ID), shrinkCover(raw), 0o644); err != nil {
		s.internalErr(w, "save cover", err)
		return
	}
	s.removeThumbnails(t.ID)
	if _, err := s.db.Exec(`UPDATE tracks SET has_cover = 1, cover_source = ?, updated_at = ? WHERE id = ?`,
		coverChosen, nowRFC3339(), t.ID); err != nil {
		s.internalErr(w, "record cover", err)
		return
	}
	go s.updateCoverColor(t.ID)
	out, _, _, ok := s.getTrack(w, r, u.ID, t.ID)
	if ok {
		writeJSON(w, http.StatusOK, out)
	}
}

func mustHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
