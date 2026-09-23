package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type soundcloudSource struct{}

const (
	scAPIBase     = "https://api-v2.soundcloud.com"
	scSiteURL     = "https://soundcloud.com/"
	scWidgetURL   = "https://w.soundcloud.com/player/?url=https%3A//api.soundcloud.com/tracks/293"
	scRetryAfter  = 5 * time.Minute
	scPageSize    = 20
	scMaxPages    = 10
	scUserAgent   = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
	scMaxSegments = 3000
)

var (
	scScriptRe       = regexp.MustCompile(`<script[^>]+src="(https://a-v2\.sndcdn\.com/assets/[^"]+\.js)"`)
	scWidgetScriptRe = regexp.MustCompile(`<script[^>]+src="(https://widget\.sndcdn\.com/[^"]+\.js)"`)
	scHydratedIDRe   = regexp.MustCompile(`"hydratable":"apiClient","data":\{"id":"([0-9a-zA-Z]{32})"`)
	scClientIDRe     = regexp.MustCompile(`client_id\s*:[^,}]{0,100}`)
	scQuotedIDRe     = regexp.MustCompile(`"([0-9a-zA-Z]{32})"`)
	scTrackIDRe      = regexp.MustCompile(`^[0-9]{1,20}$`)
	scArtworkHost    = regexp.MustCompile(`^https://i[0-9]\.sndcdn\.com/`)
	scArtworkLarge   = regexp.MustCompile(`-large(\.[a-z]+)$`)
	scMapURIRe       = regexp.MustCompile(`URI="([^"]+)"`)
)

var scClient struct {
	sync.Mutex
	id       string
	failedAt time.Time
}

var scNoRedirectClient = &http.Client{
	Timeout:       15 * time.Second,
	Transport:     metadataLookupClient.Transport,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

var errSCRefused = &statusError{http.StatusBadGateway, "SoundCloud is turning down every client id it hands out right now — try again later"}

func (soundcloudSource) takesLinks() {}

func (soundcloudSource) key() string  { return "soundcloud" }
func (soundcloudSource) name() string { return "SoundCloud" }

func (soundcloudSource) coverAllowed(u string) bool { return scArtworkHost.MatchString(u) }

func scIsCreativeCommons(license string) bool {
	return strings.HasPrefix(license, "cc-") || license == "no-rights-reserved"
}

type scTrack struct {
	ID                 int64  `json:"id"`
	Title              string `json:"title"`
	Genre              string `json:"genre"`
	License            string `json:"license"`
	Duration           int64  `json:"duration"`
	ArtworkURL         string `json:"artwork_url"`
	PermalinkURL       string `json:"permalink_url"`
	ReleaseDate        string `json:"release_date"`
	CreatedAt          string `json:"created_at"`
	Policy             string `json:"policy"`
	Streamable         bool   `json:"streamable"`
	Sharing            string `json:"sharing"`
	TrackAuthorization string `json:"track_authorization"`
	User               struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	PublisherMetadata *struct {
		Artist     string `json:"artist"`
		AlbumTitle string `json:"album_title"`
	} `json:"publisher_metadata"`
	Media struct {
		Transcodings []scTranscoding `json:"transcodings"`
	} `json:"media"`
}

type scTranscoding struct {
	URL     string `json:"url"`
	Preset  string `json:"preset"`
	Snipped bool   `json:"snipped"`
	Format  struct {
		Protocol string `json:"protocol"`
		MimeType string `json:"mime_type"`
	} `json:"format"`
}

func (t *scTrack) artist() string {
	if t.PublisherMetadata != nil && strings.TrimSpace(t.PublisherMetadata.Artist) != "" {
		return strings.TrimSpace(t.PublisherMetadata.Artist)
	}
	return strings.TrimSpace(t.User.Username)
}

func (t *scTrack) title() string {
	title := strings.TrimSpace(t.Title)
	if a := t.artist(); a != "" {
		for _, sep := range []string{" - ", " – ", " — "} {
			if prefix := a + sep; len(title) > len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
				return strings.TrimSpace(title[len(prefix):])
			}
		}
	}
	return title
}

func (t *scTrack) year() int {
	for _, d := range []string{t.ReleaseDate, t.CreatedAt} {
		if len(d) >= 4 {
			if y, err := strconv.Atoi(d[:4]); err == nil && y > 1900 {
				return y
			}
		}
	}
	return 0
}

func (t *scTrack) cover() string {
	art := t.ArtworkURL
	if art == "" {
		art = t.User.AvatarURL
	}
	return scArtworkLarge.ReplaceAllString(art, "-t500x500$1")
}

func (t *scTrack) fullStream() bool {
	if t.Policy == "BLOCK" || !t.Streamable {
		return false
	}
	for _, tc := range t.Media.Transcodings {
		if !tc.Snipped {
			return true
		}
	}
	return false
}

func (t *scTrack) result() discoverResult {
	return discoverResult{
		Source:   "soundcloud",
		ID:       strconv.FormatInt(t.ID, 10),
		Title:    t.title(),
		Artist:   t.artist(),
		Genre:    strings.TrimSpace(t.Genre),
		Year:     t.year(),
		Duration: float64(t.Duration) / 1000,
		CoverURL: t.cover(),
		PageURL:  t.PermalinkURL,
		License:  t.License,
	}
}

func scGet(ctx context.Context, client *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", scUserAgent)
	return client.Do(req)
}

func scClientIDsIn(js string) []string {
	var ids []string
	for _, m := range scClientIDRe.FindAllString(js, -1) {
		for _, q := range scQuotedIDRe.FindAllStringSubmatch(m, -1) {
			ids = append(ids, q[1])
		}
	}
	return ids
}

func scWidgetClientIDs(ctx context.Context) ([]string, error) {
	page, err := scFetchText(ctx, scWidgetURL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("soundcloud widget: %w", err)
	}
	var ids []string
	for _, m := range scWidgetScriptRe.FindAllStringSubmatch(page, -1) {
		if js, err := scFetchText(ctx, m[1], 8<<20); err == nil {
			ids = append(ids, scClientIDsIn(js)...)
		}
	}
	return ids, nil
}

func scSiteClientIDs(ctx context.Context) ([]string, error) {
	page, err := scFetchText(ctx, scSiteURL, 2<<20)
	if err != nil {
		return nil, fmt.Errorf("soundcloud home page: %w", err)
	}
	var ids []string
	if m := scHydratedIDRe.FindStringSubmatch(page); m != nil {
		ids = append(ids, m[1])
	}
	scripts := scScriptRe.FindAllStringSubmatch(page, -1)
	for i := len(scripts) - 1; i >= 0; i-- {
		if js, err := scFetchText(ctx, scripts[i][1], 8<<20); err == nil {
			if found := scClientIDsIn(js); len(found) > 0 {
				return append(ids, found...), nil
			}
		}
	}
	return ids, nil
}

func scFetchText(ctx context.Context, u string, limit int64) (string, error) {
	resp, err := scGet(ctx, metadataLookupClient, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: status %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return string(b), err
}

func scCall(ctx context.Context, rawURL string, q url.Values, id string, out any) (bool, error) {
	q.Set("client_id", id)
	resp, err := scGet(ctx, metadataLookupClient, rawURL+"?"+q.Encode())
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	case http.StatusNotFound:
		return true, &statusError{http.StatusNotFound, "that track isn't on SoundCloud any more"}
	default:
		return true, fmt.Errorf("soundcloud: status %d", resp.StatusCode)
	}
}

func scIDWorks(ctx context.Context, id string) bool {
	var out struct{}
	ok, err := scCall(ctx, scAPIBase+"/search/tracks", url.Values{"q": {"a"}, "limit": {"1"}}, id, &out)
	return ok && err == nil
}

func scWorkingID(ctx context.Context, suspect string) (string, error) {
	scClient.Lock()
	defer scClient.Unlock()
	if scClient.id != "" && scClient.id != suspect {
		return scClient.id, nil
	}
	if suspect != "" && scIDWorks(ctx, suspect) {
		scClient.id, scClient.failedAt = suspect, time.Time{}
		return suspect, nil
	}
	if time.Since(scClient.failedAt) < scRetryAfter {
		return "", errSCRefused
	}
	for _, find := range []func(context.Context) ([]string, error){scWidgetClientIDs, scSiteClientIDs} {
		ids, err := find(ctx)
		if err != nil {
			logErr("soundcloud client id", err)
			continue
		}
		for _, id := range ids {
			if id != suspect && scIDWorks(ctx, id) {
				scClient.id, scClient.failedAt = id, time.Time{}
				return id, nil
			}
		}
	}
	scClient.id, scClient.failedAt = "", time.Now()
	return "", errSCRefused
}

func scAPI(ctx context.Context, rawURL string, q url.Values, out any) error {
	id, err := scWorkingID(ctx, "")
	if err != nil {
		return err
	}
	if ok, err := scCall(ctx, rawURL, q, id, out); ok {
		return err
	}
	next, err := scWorkingID(ctx, id)
	if err != nil {
		return err
	}
	if next != id {
		if ok, err := scCall(ctx, rawURL, q, next, out); ok {
			return err
		}
	}
	return &statusError{http.StatusBadGateway, "SoundCloud refused that request (the track may be private or not available in this country)"}
}

func scLink(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "soundcloud.com") || strings.ContainsAny(raw, " \t\n") {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "soundcloud.com", "www.soundcloud.com", "m.soundcloud.com":
		host = "soundcloud.com"
	case "on.soundcloud.com", "api.soundcloud.com":
	case "w.soundcloud.com":
		if inner := u.Query().Get("url"); inner != "" && !strings.Contains(inner, "w.soundcloud.com") {
			return scLink(inner)
		}
		return "", false
	default:
		return "", false
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" {
		return "", false
	}
	return "https://" + host + path, true
}

func scFollowShortLink(ctx context.Context, link string) (string, error) {
	resp, err := scGet(ctx, scNoRedirectClient, link)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc, err := resp.Location()
	if err != nil || resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", &statusError{http.StatusNotFound, "that SoundCloud short link doesn't lead anywhere"}
	}
	full, ok := scLink(loc.String())
	if !ok || strings.HasPrefix(full, "https://on.soundcloud.com/") {
		return "", &statusError{http.StatusUnprocessableEntity, "that short link doesn't lead to a SoundCloud track"}
	}
	return full, nil
}

func scResolveLink(ctx context.Context, link string) (*scTrack, error) {
	if strings.HasPrefix(link, "https://on.soundcloud.com/") {
		full, err := scFollowShortLink(ctx, link)
		if err != nil {
			return nil, err
		}
		link = full
	}
	var out struct {
		Kind string `json:"kind"`
		scTrack
	}
	if err := scAPI(ctx, scAPIBase+"/resolve", url.Values{"url": {link}}, &out); err != nil {
		var status *statusError
		if errors.As(err, &status) && status.code == http.StatusNotFound {
			return nil, &statusError{http.StatusNotFound, "there's no SoundCloud track at that link"}
		}
		return nil, err
	}
	switch out.Kind {
	case "track":
	case "playlist", "system-playlist":
		return nil, &statusError{http.StatusUnprocessableEntity, "that's a SoundCloud playlist — paste a link to a single track"}
	case "user":
		return nil, &statusError{http.StatusUnprocessableEntity, "that's a SoundCloud profile — paste a link to a single track"}
	default:
		return nil, &statusError{http.StatusUnprocessableEntity, "that link isn't a SoundCloud track"}
	}
	t := &out.scTrack
	switch {
	case t.Sharing == "private":
		return nil, &statusError{http.StatusUnprocessableEntity, "that's a private SoundCloud track — only public ones can be added"}
	case !scIsCreativeCommons(t.License):
		return nil, &statusError{http.StatusUnprocessableEntity, "This track isn't Creative Commons"}
	case !t.fullStream():
		return nil, &statusError{http.StatusUnprocessableEntity, "SoundCloud only offers a preview of that track"}
	}
	return t, nil
}

func (soundcloudSource) search(ctx context.Context, query string, page int) ([]discoverResult, bool, error) {
	if page > scMaxPages {
		return nil, false, nil
	}
	if link, ok := scLink(query); ok {
		if page > 1 {
			return nil, false, nil
		}
		t, err := scResolveLink(ctx, link)
		if err != nil {
			return nil, false, err
		}
		return []discoverResult{t.result()}, false, nil
	}
	var out struct {
		Collection []scTrack `json:"collection"`
		NextHref   string    `json:"next_href"`
	}
	q := url.Values{
		"q":              {query},
		"filter.license": {"to_share"},
		"limit":          {strconv.Itoa(scPageSize)},
		"offset":         {strconv.Itoa((page - 1) * scPageSize)},
	}
	if err := scAPI(ctx, scAPIBase+"/search/tracks", q, &out); err != nil {
		return nil, false, err
	}
	results := []discoverResult{}
	for i := range out.Collection {
		t := &out.Collection[i]
		if !scIsCreativeCommons(t.License) || !t.fullStream() {
			continue
		}
		results = append(results, t.result())
	}
	return results, out.NextHref != "" && page < scMaxPages, nil
}

func (soundcloudSource) fetch(ctx context.Context, id string) (*sourceDownload, error) {
	if !scTrackIDRe.MatchString(id) {
		return nil, &statusError{http.StatusBadRequest, "not a SoundCloud track id"}
	}
	var t scTrack
	if err := scAPI(ctx, scAPIBase+"/tracks/"+id, url.Values{}, &t); err != nil {
		return nil, err
	}
	if !scIsCreativeCommons(t.License) {
		return nil, &statusError{http.StatusForbidden, "only Creative Commons tracks can be added from SoundCloud"}
	}
	if !t.fullStream() {
		return nil, &statusError{http.StatusBadGateway, "SoundCloud only offers a preview of this track"}
	}

	dir, err := os.MkdirTemp("", "music-soundcloud-")
	if err != nil {
		return nil, err
	}
	path, ext, err := scDownloadAudio(ctx, &t, dir)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	size := int64(-1)
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	title, artist := t.title(), t.artist()
	album := title
	if t.PublisherMetadata != nil && strings.TrimSpace(t.PublisherMetadata.AlbumTitle) != "" {
		album = strings.TrimSpace(t.PublisherMetadata.AlbumTitle)
	}
	return &sourceDownload{
		Body:           &tempDownload{File: f, dir: dir},
		FileName:       sanitizeFilename(trimDefault(artist, "SoundCloud")+" - "+trimDefault(title, id)) + ext,
		Size:           size,
		Title:          title,
		Artist:         artist,
		Album:          album,
		Genre:          strings.TrimSpace(t.Genre),
		Year:           t.year(),
		Duration:       float64(t.Duration) / 1000,
		CoverURL:       t.cover(),
		LookupMetadata: true,
	}, nil
}

func scDownloadAudio(ctx context.Context, t *scTrack, dir string) (string, string, error) {
	var aac, mp3 *scTranscoding
	for i := range t.Media.Transcodings {
		tc := &t.Media.Transcodings[i]
		if tc.Snipped {
			continue
		}
		switch {
		case tc.Format.Protocol == "hls" && strings.HasPrefix(tc.Preset, "aac_160") && aac == nil:
			aac = tc
		case tc.Format.Protocol == "progressive" && strings.HasPrefix(tc.Format.MimeType, "audio/mpeg") && mp3 == nil:
			mp3 = tc
		}
	}
	if aac != nil {
		path, err := scDownloadHLS(ctx, t, aac, dir)
		if err == nil {
			return path, ".m4a", nil
		}
		logErr("soundcloud aac stream", err)
	}
	if mp3 == nil {
		return "", "", &statusError{http.StatusBadGateway, "SoundCloud offers no stream this server can fetch for that track"}
	}
	streamURL, err := scResolve(ctx, t, mp3)
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, "audio.mp3")
	if err := scSaveURL(ctx, streamURL, path, maxDiscoverBytes); err != nil {
		return "", "", err
	}
	return path, ".mp3", nil
}

func scResolve(ctx context.Context, t *scTrack, tc *scTranscoding) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	q := url.Values{}
	if t.TrackAuthorization != "" {
		q.Set("track_authorization", t.TrackAuthorization)
	}
	if err := scAPI(ctx, tc.URL, q, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.URL, "https://") {
		return "", errors.New("soundcloud: no stream url")
	}
	return out.URL, nil
}

func scDownloadHLS(ctx context.Context, t *scTrack, tc *scTranscoding, dir string) (string, error) {
	playlistURL, err := scResolve(ctx, t, tc)
	if err != nil {
		return "", err
	}
	base, err := url.Parse(playlistURL)
	if err != nil {
		return "", err
	}
	text, err := scFetchText(ctx, playlistURL, 1<<20)
	if err != nil {
		return "", err
	}
	var init string
	var segments []string
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXT-X-KEY"):
			if !strings.Contains(line, "METHOD=NONE") {
				return "", errors.New("soundcloud: encrypted stream")
			}
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE"):
			return "", errors.New("soundcloud: byte-range playlist")
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			m := scMapURIRe.FindStringSubmatch(line)
			if m == nil {
				return "", errors.New("soundcloud: EXT-X-MAP without a URI")
			}
			init = m[1]
		case strings.HasPrefix(line, "#"):
		default:
			segments = append(segments, line)
		}
	}
	if init == "" || len(segments) == 0 || len(segments) > scMaxSegments {
		return "", fmt.Errorf("soundcloud: unexpected playlist (%d segments)", len(segments))
	}

	joined := filepath.Join(dir, "joined.mp4")
	out, err := os.Create(joined)
	if err != nil {
		return "", err
	}
	var written int64
	for _, ref := range append([]string{init}, segments...) {
		u, err := base.Parse(ref)
		if err != nil || u.Scheme != "https" {
			out.Close()
			return "", fmt.Errorf("soundcloud: bad segment url %q", ref)
		}
		n, err := scAppendURL(ctx, u.String(), out, maxDiscoverBytes-written)
		written += n
		if err != nil {
			out.Close()
			return "", err
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}

	bin := ffmpegBinary()
	if bin == "" {
		return joined, nil
	}
	remuxed := filepath.Join(dir, "audio.m4a")
	cmd := exec.CommandContext(ctx, bin, "-nostdin", "-loglevel", "error", "-y",
		"-i", joined, "-map", "0:a:0", "-c", "copy", "-movflags", "+faststart", remuxed)
	if msg, err := cmd.CombinedOutput(); err != nil {
		logErr("soundcloud remux", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(msg))))
		return joined, nil
	}
	os.Remove(joined)
	return remuxed, nil
}

func scSaveURL(ctx context.Context, u, path string, limit int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = scAppendURL(ctx, u, f, limit)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func scAppendURL(ctx context.Context, u string, w io.Writer, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, &statusError{http.StatusBadGateway, "the file is larger than this server accepts"}
	}
	resp, err := scGet(ctx, discoverClient, u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("soundcloud stream: status %d", resp.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err == nil && n > limit {
		return n, &statusError{http.StatusBadGateway, "the file is larger than this server accepts"}
	}
	return n, err
}
