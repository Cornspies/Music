package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	catalogSongsTimeout  = 75 * time.Second
	catalogSongsReleases = 120
	catalogSongsSCPages  = 3
	catalogSongsSlack    = 8.0
)

type CatalogSong struct {
	CatalogTrack
	Year  int              `json:"year,omitempty"`
	Found []discoverResult `json:"found"`
}

type CatalogSongSource struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}

var catalogSongSourceNames = map[string]string{
	"deezer":     "Deezer",
	"youtube":    "YouTube Music",
	"soundcloud": "SoundCloud",
}

type catalogHit struct {
	discoverResult
	year     int
	position int
	disc     int
	deezerID int64
}

type catalogSongLister struct {
	key  string
	list func(ctx context.Context) ([]catalogHit, error)
}

func (s *Server) handleCatalogArtistSongs(w http.ResponseWriter, r *http.Request) {
	id, ok := catalogID(w, r.PathValue("id"))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), catalogSongsTimeout)
	defer cancel()
	var artist dzpubArtist
	if err := dzpubGet(ctx, "/artist/"+id, &artist); err != nil {
		writeCatalogErr(w, err)
		return
	}
	name := artist.Name

	listers := []catalogSongLister{
		{"deezer", func(ctx context.Context) ([]catalogHit, error) { return deezerArtistSongs(ctx, id, name) }},
	}
	if !discoverDisabled() {
		if yt, ok := findSource("youtube").(songSearcher); ok {
			listers = append(listers, catalogSongLister{"youtube", func(ctx context.Context) ([]catalogHit, error) {
				return searcherArtistSongs(ctx, yt, name)
			}})
		}
		if sc := findSource("soundcloud"); sc != nil {
			listers = append(listers, catalogSongLister{"soundcloud", func(ctx context.Context) ([]catalogHit, error) {
				return soundcloudArtistSongs(ctx, sc, name)
			}})
		}
	}

	results := make([][]catalogHit, len(listers))
	sources := make([]CatalogSongSource, len(listers))
	var wg sync.WaitGroup
	for i, l := range listers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := l.list(ctx)
			src := CatalogSongSource{Key: l.key, Name: catalogSongSourceNames[l.key]}
			if err != nil {
				src.Error = catalogSongError(l.key, err)
				if !errors.Is(err, context.DeadlineExceeded) {
					logErr("catalogue songs ("+l.key+")", err)
				}
			}
			kept := hits[:0]
			for _, h := range hits {
				if artistsOverlap(h.Artist, name) || hasArtist(h.Artist, name) {
					kept = append(kept, h)
				}
			}
			src.Count = len(kept)
			results[i], sources[i] = kept, src
		}()
	}
	wg.Wait()

	songs := mergeCatalogSongs(results)
	idx, err := s.loadLibraryIndex(userFrom(r).ID)
	if err != nil {
		s.internalErr(w, "catalogue library index", err)
		return
	}
	for i := range songs {
		song := &songs[i]
		song.TrackID = idx.find(song.Title, song.Artist, song.Duration)
		for _, f := range song.Found {
			if song.TrackID != "" {
				break
			}
			song.TrackID = idx.find(f.Title, f.Artist, f.Duration)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": songs, "sources": sources})
}

func catalogSongError(key string, err error) string {
	var status *statusError
	switch {
	case errors.As(err, &status):
		return status.msg
	case errors.Is(err, context.DeadlineExceeded):
		return "took too long"
	default:
		return catalogSongSourceNames[key] + " didn't answer"
	}
}

type songKey struct{ norm, core, tags string }

func songKeyOf(title string) songKey {
	return songKey{normalizeForMatch(title), coreTitle(title), titleTags(title)}
}

func (k songKey) same(o songKey) bool {
	return k.norm != "" && k.norm == o.norm || k.core != "" && k.core == o.core && k.tags == o.tags
}

func mergeCatalogSongs(lists [][]catalogHit) []CatalogSong {
	songs := []CatalogSong{}
	keys := []songKey{}
	byCore := map[string][]int{}
	byNorm := map[string][]int{}
	for _, hits := range lists {
		for _, h := range hits {
			if strings.TrimSpace(h.Title) == "" {
				continue
			}
			key := songKeyOf(h.Title)
			at := -1
			for _, i := range slices.Concat(byNorm[key.norm], byCore[key.core]) {
				s := songs[i]
				if key.same(keys[i]) && (s.Duration == 0 || h.Duration == 0 || math.Abs(s.Duration-h.Duration) <= catalogSongsSlack) {
					at = i
					break
				}
			}
			if at < 0 {
				byNorm[key.norm] = append(byNorm[key.norm], len(songs))
				byCore[key.core] = append(byCore[key.core], len(songs))
				keys = append(keys, key)
				songs = append(songs, CatalogSong{
					CatalogTrack: CatalogTrack{
						ID: h.deezerID, Title: h.Title, Artist: h.Artist, Album: h.Album, Cover: h.CoverURL,
						Duration: h.Duration, Position: h.position, Disc: h.disc,
					},
					Year:  h.year,
					Found: []discoverResult{h.discoverResult},
				})
				continue
			}
			s := &songs[at]
			if !slices.ContainsFunc(s.Found, func(f discoverResult) bool { return f.Source == h.Source }) {
				s.Found = append(s.Found, h.discoverResult)
			}
			if s.ID == 0 {
				s.ID = h.deezerID
			}
			if s.Album == "" {
				s.Album = h.Album
			}
			if s.Cover == "" {
				s.Cover = h.CoverURL
			}
			if s.Duration == 0 {
				s.Duration = h.Duration
			}
			if h.year > 0 && (s.Year == 0 || h.year < s.Year) {
				s.Year = h.year
			}
		}
	}
	slices.SortStableFunc(songs, func(a, b CatalogSong) int {
		switch {
		case a.Year != b.Year && (a.Year == 0 || b.Year == 0):
			if a.Year == 0 {
				return 1
			}
			return -1
		case a.Year != b.Year:
			return b.Year - a.Year
		default:
			return len(b.Found) - len(a.Found)
		}
	})
	return songs
}

func deezerArtistSongs(ctx context.Context, id, name string) ([]catalogHit, error) {
	hits := []catalogHit{}
	add := func(t dzpubTrack, album dzpubAlbum) {
		c := catalogTrackFrom(t)
		if album.ID != 0 {
			c.Album, c.Cover = album.Title, album.CoverMedium
		}
		hits = append(hits, catalogHit{
			discoverResult: discoverResult{
				Source: "deezer", ID: fmt.Sprint(t.ID), Title: c.Title, Artist: c.Artist, Album: c.Album,
				Duration: c.Duration, CoverURL: c.Cover, PageURL: fmt.Sprintf("https://www.deezer.com/track/%d", t.ID),
			},
			year: parseReleaseYear(album.ReleaseDate), position: c.Position, disc: c.Disc, deezerID: t.ID,
		})
	}
	var top struct {
		Data []dzpubTrack `json:"data"`
	}
	if err := dzpubGet(ctx, "/artist/"+id+"/top?limit=100", &top); err != nil {
		return hits, err
	}
	for _, t := range top.Data {
		add(t, dzpubAlbum{})
	}
	releases := []dzpubAlbum{}
	for index := 0; index < catalogMaxRels; index += 100 {
		var page struct {
			Data  []dzpubAlbum `json:"data"`
			Total int          `json:"total"`
		}
		if err := dzpubGet(ctx, fmt.Sprintf("/artist/%s/albums?limit=100&index=%d", id, index), &page); err != nil {
			return hits, err
		}
		for _, a := range page.Data {
			if a.RecordType != "compile" {
				releases = append(releases, a)
			}
		}
		if len(page.Data) < 100 || index+100 >= page.Total {
			break
		}
	}
	slices.SortStableFunc(releases, func(a, b dzpubAlbum) int { return strings.Compare(a.ReleaseDate, b.ReleaseDate) })
	if len(releases) > catalogSongsReleases {
		releases = releases[len(releases)-catalogSongsReleases:]
	}
	for _, a := range releases {
		var list struct {
			Data []dzpubTrack `json:"data"`
		}
		if err := dzpubGet(ctx, fmt.Sprintf("/album/%d/tracks?limit=300", a.ID), &list); err != nil {
			return hits, err
		}
		for _, t := range list.Data {
			add(t, a)
		}
	}
	slices.SortStableFunc(hits, func(a, b catalogHit) int {
		if a.year == b.year {
			return 0
		}
		if a.year == 0 {
			return 1
		}
		if b.year == 0 {
			return -1
		}
		return a.year - b.year
	})
	return hits, nil
}

func searcherArtistSongs(ctx context.Context, src songSearcher, name string) ([]catalogHit, error) {
	results, err := src.searchSongs(ctx, name)
	if err != nil {
		return nil, err
	}
	return resultHits(results), nil
}

func soundcloudArtistSongs(ctx context.Context, src source, name string) ([]catalogHit, error) {
	all := []discoverResult{}
	for page := 1; page <= catalogSongsSCPages; page++ {
		results, more, err := src.search(ctx, name, page)
		if err != nil {
			if len(all) > 0 {
				break
			}
			return nil, err
		}
		all = append(all, results...)
		if !more {
			break
		}
	}
	return resultHits(all), nil
}

func resultHits(results []discoverResult) []catalogHit {
	hits := make([]catalogHit, 0, len(results))
	for _, r := range results {
		hits = append(hits, catalogHit{discoverResult: r, year: r.Year})
	}
	return hits
}
