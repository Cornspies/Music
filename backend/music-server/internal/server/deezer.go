package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	deezerAPIBase  = "https://api.deezer.com"
	deezerPageSize = 25
)

var (
	deezerTrackLinkRe = regexp.MustCompile(`deezer\.com/(?:[a-z]{2}(?:-[a-z]{2})?/)?track/(\d+)`)
	deezerIDRe        = regexp.MustCompile(`^\d{1,20}$`)
)

type deezerSource struct {
	yt *ytdlpSource
}

func (deezerSource) key() string  { return "deezer" }
func (deezerSource) name() string { return "Deezer" }
func (deezerSource) takesLinks()  {}

func (deezerSource) coverAllowed(u string) bool {
	for _, host := range []string{"https://e-cdns-images.dzcdn.net/", "https://cdn-images.dzcdn.net/", "https://cdns-images.dzcdn.net/"} {
		if strings.HasPrefix(u, host) {
			return true
		}
	}
	return false
}

type deezerArtist struct {
	Name string `json:"name"`
}

type deezerAlbum struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	CoverBig    string `json:"cover_big"`
	CoverXL     string `json:"cover_xl"`
	ReleaseDate string `json:"release_date"`
	Genres      struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	} `json:"genres"`
}

type deezerTrack struct {
	ID            int64        `json:"id"`
	Title         string       `json:"title"`
	Readable      *bool        `json:"readable"`
	Duration      float64      `json:"duration"`
	Link          string       `json:"link"`
	TrackPosition int          `json:"track_position"`
	ReleaseDate   string       `json:"release_date"`
	Artist        deezerArtist `json:"artist"`
	Album         deezerAlbum  `json:"album"`
}

type deezerError struct {
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func deezerGet(ctx context.Context, path string, q url.Values, out any) error {
	u := deezerAPIBase + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deezer: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var failed deezerError
	if json.Unmarshal(body, &failed) == nil && failed.Error != nil {
		if failed.Error.Code == 800 {
			return &statusError{http.StatusNotFound, "that track isn't on Deezer"}
		}
		return fmt.Errorf("deezer: %s", failed.Error.Message)
	}
	return json.Unmarshal(body, out)
}

func (t *deezerTrack) result() discoverResult {
	return discoverResult{
		Source:   "deezer",
		ID:       strconv.FormatInt(t.ID, 10),
		Title:    strings.TrimSpace(t.Title),
		Artist:   strings.TrimSpace(t.Artist.Name),
		Album:    strings.TrimSpace(t.Album.Title),
		Year:     parseReleaseYear(t.ReleaseDate),
		Duration: t.Duration,
		CoverURL: t.Album.CoverBig,
		PageURL:  t.Link,
	}
}

func (d deezerSource) search(ctx context.Context, query string, page int) ([]discoverResult, bool, error) {
	if m := deezerTrackLinkRe.FindStringSubmatch(query); m != nil {
		if page > 1 {
			return nil, false, nil
		}
		var t deezerTrack
		if err := deezerGet(ctx, "/track/"+m[1], nil, &t); err != nil {
			return nil, false, err
		}
		return []discoverResult{t.result()}, false, nil
	}
	var parsed struct {
		Data  []deezerTrack `json:"data"`
		Total int           `json:"total"`
		Next  string        `json:"next"`
	}
	q := url.Values{
		"q":     {query},
		"index": {strconv.Itoa((page - 1) * deezerPageSize)},
		"limit": {strconv.Itoa(deezerPageSize)},
	}
	if err := deezerGet(ctx, "/search", q, &parsed); err != nil {
		return nil, false, err
	}
	results := []discoverResult{}
	for i := range parsed.Data {
		t := &parsed.Data[i]
		if t.Readable != nil && !*t.Readable {
			continue
		}
		results = append(results, t.result())
	}
	return results, parsed.Next != "", nil
}

func (d deezerSource) fetch(ctx context.Context, id string) (*sourceDownload, error) {
	if !deezerIDRe.MatchString(id) {
		return nil, &statusError{http.StatusBadRequest, "not a Deezer track id"}
	}
	var t deezerTrack
	if err := deezerGet(ctx, "/track/"+id, nil, &t); err != nil {
		return nil, err
	}
	var album deezerAlbum
	if t.Album.ID > 0 {
		albumCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := deezerGet(albumCtx, "/album/"+strconv.FormatInt(t.Album.ID, 10), nil, &album); err != nil {
			logErr("deezer album", err)
		}
		cancel()
	}

	want := upgradeTrack{title: t.Title, artist: t.Artist.Name, duration: t.Duration}
	var match *discoverResult
	for _, query := range []string{t.Artist.Name + " " + t.Title, t.Artist.Name + " " + t.Title + " audio"} {
		results, _, err := d.yt.search(ctx, query, 1)
		if err != nil {
			return nil, &statusError{http.StatusBadGateway, "couldn't search YouTube for this song: " + ytdlpFriendly(err, d.yt.jar != "")}
		}
		for i := range results {
			if i < upgradeSearchHits && sameRecording(want, results[i]) {
				match = &results[i]
				break
			}
		}
		if match != nil {
			break
		}
	}
	if match == nil {
		return nil, &statusError{http.StatusNotFound, "couldn't find this exact song on YouTube to download"}
	}

	dl, err := d.yt.fetch(ctx, match.ID)
	if err != nil {
		return nil, err
	}
	title, artist := strings.TrimSpace(t.Title), strings.TrimSpace(t.Artist.Name)
	dl.FileName = sanitizeFilename(artist+" - "+title) + filepath.Ext(dl.FileName)
	dl.Title, dl.Artist = title, artist
	dl.Album = trimDefault(strings.TrimSpace(t.Album.Title), dl.Album)
	if year := parseReleaseYear(trimDefault(t.ReleaseDate, album.ReleaseDate)); year > 0 {
		dl.Year = year
	}
	if len(album.Genres.Data) > 0 {
		dl.Genre = album.Genres.Data[0].Name
	}
	dl.TrackNo = t.TrackPosition
	dl.Duration = t.Duration
	if cover := trimDefault(t.Album.CoverXL, t.Album.CoverBig); cover != "" {
		dl.CoverURL, dl.SquareCover, dl.CatalogueCover = cover, false, true
	}
	dl.LookupMetadata = false
	return dl, nil
}
