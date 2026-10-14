package deezer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	deezerItemRe = regexp.MustCompile(`(?i)(?:^|[\s"'<(]|https?://)(?:[a-z0-9-]+\.)*deezer\.com/(?:[a-z]{2}(?:-[a-z]{2})?/)?(track|album|playlist|artist)/(\d+)`)
	numericIDRe  = regexp.MustCompile(`^\d{1,20}$`)
)

type deezerAPIError struct {
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func (c *Client) publicGet(ctx context.Context, endpoint string, q url.Values, out any) error {
	u := endpoint
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = legacyAPIURL + endpoint
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + q.Encode()
	}

	req, err := c.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deezer api status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}

	var apiErr deezerAPIError
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != nil {
		msg := apiErr.Error.Message
		if msg == "" {
			msg = apiErr.Error.Type
		}
		if msg == "" {
			msg = fmt.Sprintf("code %d", apiErr.Error.Code)
		}
		return fmt.Errorf("deezer api error: %s", msg)
	}

	return json.Unmarshal(body, out)
}

var pageRetryDelay = 500 * time.Millisecond

func (c *Client) collectPages(ctx context.Context, initialData []json.RawMessage, nextURL string) ([]json.RawMessage, error) {
	entries := append([]json.RawMessage{}, initialData...)
	visited := make(map[string]bool)

	currentNext := nextURL
	for currentNext != "" {
		if len(entries) > 100000 {
			break
		}

		parsed, err := url.Parse(currentNext)
		if err != nil {
			break
		}
		parsed.Scheme = "https"
		cleanURL := parsed.String()
		if !IsAllowedDeezerURL(cleanURL) {
			break
		}

		if visited[cleanURL] || len(visited) >= 1000 {
			break
		}
		visited[cleanURL] = true

		var page struct {
			Data []json.RawMessage `json:"data"`
			Next string            `json:"next"`
		}

		err = c.publicGet(ctx, cleanURL, nil, &page)
		if err != nil && ctx.Err() == nil {
			select {
			case <-time.After(pageRetryDelay):
				err = c.publicGet(ctx, cleanURL, nil, &page)
			case <-ctx.Done():
			}
		}
		if err != nil {
			return entries, fmt.Errorf("page %d: %w", len(visited), err)
		}

		entries = append(entries, page.Data...)
		currentNext = page.Next
	}

	return entries, nil
}

type rawAPITrack struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	Readable      *bool   `json:"readable"`
	Duration      float64 `json:"duration"`
	Link          string  `json:"link"`
	TrackPosition int     `json:"track_position"`
	DiskNumber    int     `json:"disk_number"`
	ReleaseDate   string  `json:"release_date"`
	Preview       string  `json:"preview"`
	Artist        struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
	Album struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		CoverSmall  string `json:"cover_small"`
		CoverMedium string `json:"cover_medium"`
		CoverBig    string `json:"cover_big"`
		CoverXL     string `json:"cover_xl"`
	} `json:"album"`
}

func (r *rawAPITrack) toTrack() Track {
	return Track{
		ID:            r.ID,
		Title:         strings.TrimSpace(r.Title),
		Artist:        strings.TrimSpace(r.Artist.Name),
		ArtistID:      r.Artist.ID,
		Album:         strings.TrimSpace(r.Album.Title),
		AlbumID:       r.Album.ID,
		Duration:      r.Duration,
		TrackPosition: r.TrackPosition,
		DiskNumber:    r.DiskNumber,
		ReleaseDate:   r.ReleaseDate,
		CoverSmall:    r.Album.CoverSmall,
		CoverMedium:   r.Album.CoverMedium,
		CoverBig:      r.Album.CoverBig,
		CoverXL:       r.Album.CoverXL,
		Link:          r.Link,
		Preview:       r.Preview,
	}
}

func (c *Client) SearchTracks(ctx context.Context, query string, limit, index int) ([]Track, int, bool, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"q":     {query},
		"limit": {strconv.Itoa(limit)},
		"index": {strconv.Itoa(index)},
	}

	var resp struct {
		Data  []rawAPITrack `json:"data"`
		Total int           `json:"total"`
		Next  string        `json:"next"`
	}

	if err := c.publicGet(ctx, "/search/track", q, &resp); err != nil {
		return nil, 0, false, err
	}

	tracks := make([]Track, 0, len(resp.Data))
	for i := range resp.Data {
		t := &resp.Data[i]
		if t.Readable != nil && !*t.Readable {
			continue
		}
		tracks = append(tracks, t.toTrack())
	}

	return tracks, resp.Total, resp.Next != "", nil
}

func (c *Client) SearchAlbums(ctx context.Context, query string, limit, index int) ([]Album, int, bool, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"q":     {query},
		"limit": {strconv.Itoa(limit)},
		"index": {strconv.Itoa(index)},
	}

	var resp struct {
		Data []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			CoverSmall  string `json:"cover_small"`
			CoverMedium string `json:"cover_medium"`
			CoverBig    string `json:"cover_big"`
			CoverXL     string `json:"cover_xl"`
			NbTracks    int    `json:"nb_tracks"`
			Artist      struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"data"`
		Total int    `json:"total"`
		Next  string `json:"next"`
	}

	if err := c.publicGet(ctx, "/search/album", q, &resp); err != nil {
		return nil, 0, false, err
	}

	albums := make([]Album, len(resp.Data))
	for i, a := range resp.Data {
		albums[i] = Album{
			ID:          a.ID,
			Title:       strings.TrimSpace(a.Title),
			Artist:      strings.TrimSpace(a.Artist.Name),
			ArtistID:    a.Artist.ID,
			CoverSmall:  a.CoverSmall,
			CoverMedium: a.CoverMedium,
			CoverBig:    a.CoverBig,
			CoverXL:     a.CoverXL,
			NbTracks:    a.NbTracks,
		}
	}

	return albums, resp.Total, resp.Next != "", nil
}

func (c *Client) SearchPlaylists(ctx context.Context, query string, limit, index int) ([]Playlist, int, bool, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{
		"q":     {query},
		"limit": {strconv.Itoa(limit)},
		"index": {strconv.Itoa(index)},
	}

	var resp struct {
		Data []struct {
			ID          int64  `json:"id"`
			Title       string `json:"title"`
			CoverSmall  string `json:"picture_small"`
			CoverMedium string `json:"picture_medium"`
			CoverBig    string `json:"picture_big"`
			CoverXL     string `json:"picture_xl"`
			NbTracks    int    `json:"nb_tracks"`
			User        struct {
				Name string `json:"name"`
			} `json:"user"`
		} `json:"data"`
		Total int    `json:"total"`
		Next  string `json:"next"`
	}

	if err := c.publicGet(ctx, "/search/playlist", q, &resp); err != nil {
		return nil, 0, false, err
	}

	playlists := make([]Playlist, len(resp.Data))
	for i, p := range resp.Data {
		playlists[i] = Playlist{
			ID:          p.ID,
			Title:       strings.TrimSpace(p.Title),
			Creator:     strings.TrimSpace(p.User.Name),
			CoverSmall:  p.CoverSmall,
			CoverMedium: p.CoverMedium,
			CoverBig:    p.CoverBig,
			CoverXL:     p.CoverXL,
			NbTracks:    p.NbTracks,
		}
	}

	return playlists, resp.Total, resp.Next != "", nil
}

func (c *Client) GetTrack(ctx context.Context, id string) (*Track, error) {
	if !IsNumericID(id) {
		return nil, fmt.Errorf("invalid track id %q", id)
	}
	var raw rawAPITrack
	if err := c.publicGet(ctx, "/track/"+id, nil, &raw); err != nil {
		return nil, err
	}
	t := raw.toTrack()
	return &t, nil
}

func (c *Client) GetAlbum(ctx context.Context, id string) (*Album, error) {
	if !IsNumericID(id) {
		return nil, fmt.Errorf("invalid album id %q", id)
	}
	var albumData struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		CoverSmall  string `json:"cover_small"`
		CoverMedium string `json:"cover_medium"`
		CoverBig    string `json:"cover_big"`
		CoverXL     string `json:"cover_xl"`
		ReleaseDate string `json:"release_date"`
		NbTracks    int    `json:"nb_tracks"`
		Artist      struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"artist"`
		Genres struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		} `json:"genres"`
		Tracks struct {
			Data []rawAPITrack `json:"data"`
			Next string        `json:"next"`
		} `json:"tracks"`
	}

	if err := c.publicGet(ctx, "/album/"+id, nil, &albumData); err != nil {
		return nil, err
	}

	genre := ""
	if len(albumData.Genres.Data) > 0 {
		genre = albumData.Genres.Data[0].Name
	}

	album := &Album{
		ID:          albumData.ID,
		Title:       strings.TrimSpace(albumData.Title),
		Artist:      strings.TrimSpace(albumData.Artist.Name),
		ArtistID:    albumData.Artist.ID,
		CoverSmall:  albumData.CoverSmall,
		CoverMedium: albumData.CoverMedium,
		CoverBig:    albumData.CoverBig,
		CoverXL:     albumData.CoverXL,
		ReleaseDate: albumData.ReleaseDate,
		Genre:       genre,
		NbTracks:    albumData.NbTracks,
	}

	rawItems := make([]json.RawMessage, len(albumData.Tracks.Data))
	for i, t := range albumData.Tracks.Data {
		b, _ := json.Marshal(t)
		rawItems[i] = b
	}

	allRaw, _ := c.collectPages(ctx, rawItems, albumData.Tracks.Next)

	tracks := make([]Track, 0, len(allRaw))
	for _, raw := range allRaw {
		var item rawAPITrack
		if json.Unmarshal(raw, &item) == nil {
			t := item.toTrack()
			if t.Album == "" {
				t.Album = album.Title
			}
			if t.AlbumID == 0 {
				t.AlbumID = album.ID
			}
			if t.ReleaseDate == "" {
				t.ReleaseDate = album.ReleaseDate
			}
			if t.Genre == "" {
				t.Genre = album.Genre
			}
			if t.CoverBig == "" {
				t.CoverBig = album.CoverBig
				t.CoverXL = album.CoverXL
				t.CoverMedium = album.CoverMedium
				t.CoverSmall = album.CoverSmall
			}
			tracks = append(tracks, t)
		}
	}
	album.Tracks = tracks
	return album, nil
}

func (c *Client) GetPlaylist(ctx context.Context, id string) (*Playlist, error) {
	if !IsNumericID(id) {
		return nil, fmt.Errorf("invalid playlist id %q", id)
	}
	var plData struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		CoverSmall  string `json:"picture_small"`
		CoverMedium string `json:"picture_medium"`
		CoverBig    string `json:"picture_big"`
		CoverXL     string `json:"picture_xl"`
		NbTracks    int    `json:"nb_tracks"`
		Creator     struct {
			Name string `json:"name"`
		} `json:"creator"`
		Tracks struct {
			Data []rawAPITrack `json:"data"`
			Next string        `json:"next"`
		} `json:"tracks"`
	}

	if err := c.publicGet(ctx, "/playlist/"+id, nil, &plData); err != nil {
		return nil, err
	}

	pl := &Playlist{
		ID:          plData.ID,
		Title:       strings.TrimSpace(plData.Title),
		Creator:     strings.TrimSpace(plData.Creator.Name),
		CoverSmall:  plData.CoverSmall,
		CoverMedium: plData.CoverMedium,
		CoverBig:    plData.CoverBig,
		CoverXL:     plData.CoverXL,
		NbTracks:    plData.NbTracks,
	}

	rawItems := make([]json.RawMessage, len(plData.Tracks.Data))
	for i, t := range plData.Tracks.Data {
		b, _ := json.Marshal(t)
		rawItems[i] = b
	}

	allRaw, _ := c.collectPages(ctx, rawItems, plData.Tracks.Next)

	tracks := make([]Track, 0, len(allRaw))
	for _, raw := range allRaw {
		var item rawAPITrack
		if json.Unmarshal(raw, &item) == nil {
			tracks = append(tracks, item.toTrack())
		}
	}
	pl.Tracks = tracks
	return pl, nil
}

func (c *Client) ParseURL(ctx context.Context, raw string) (kind, id string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", errors.New("empty url")
	}

	if short, ok := shortLinkURL(trimmed); ok {
		trimmed = c.resolveShortLink(ctx, short)
	}

	if kind, id, ok := MatchURL(trimmed); ok {
		return kind, id, nil
	}

	return "", "", errors.New("not a recognized deezer url")
}

func MatchURL(s string) (kind, id string, ok bool) {
	if m := deezerItemRe.FindStringSubmatch(s); m != nil {
		return strings.ToLower(m[1]), m[2], true
	}
	return "", "", false
}

var shortLinkHosts = map[string]bool{"deezer.page.link": true, "link.deezer.com": true}

func isShortLinkHost(u *url.URL) bool {
	return shortLinkHosts[strings.ToLower(u.Hostname())]
}

func shortLinkURL(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !isShortLinkHost(u) {
		return "", false
	}
	u.Scheme = "https"
	return u.String(), true
}

const maxShortLinkHops = 5

func (c *Client) resolveShortLink(ctx context.Context, link string) string {
	hopClient := *c.http
	hopClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	cur := link
	for i := 0; i < maxShortLinkHops; i++ {
		if _, _, ok := MatchURL(cur); ok {
			return cur
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, cur, nil)
		if err != nil {
			return cur
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := hopClient.Do(req)
		if err != nil {
			return cur
		}
		resp.Body.Close()

		loc := resp.Header.Get("Location")
		if loc == "" {
			return cur
		}
		next, err := req.URL.Parse(loc)
		if err != nil || next.Scheme != "https" || !(isShortLinkHost(next) || IsAllowedDeezerURL(next.String())) {
			return cur
		}
		cur = next.String()
	}
	return cur
}

func IsNumericID(s string) bool {
	return numericIDRe.MatchString(s)
}
