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
)

// SoundCloud — Creative Commons tracks only.
//
// Artists on SoundCloud choose a licence per track. Those who pick a
// Creative Commons one (or put the track into the public domain) allow it to
// be copied, so keeping it in a library is fine; the "BY" part asks for
// credit, which the track's artist and the licence badge in the results
// give. Everything else on SoundCloud is all-rights-reserved and is never
// offered, searched or fetched here: search asks SoundCloud for shareable
// tracks only, and the licence is checked again before every download, so
// a hand-made request for any other track is refused.
//
// SoundCloud has no open API for new applications, so this talks to the one
// its own web app uses (api-v2), with the public client id the web app
// ships in its scripts — found and cached the way yt-dlp does it, and looked
// up again whenever the API stops accepting it. Like NCS, this is a place
// that can break when someone else changes their website.
//
// Audio: the 160 kbit/s AAC stream where SoundCloud has one (HLS; its
// fragmented-MP4 segments are joined, and remuxed into an ordinary .m4a when
// ffmpeg is there), otherwise the 128 kbit/s MP3. The artist's original
// upload would be better still, but SoundCloud only hands that to signed-in
// accounts.

type soundcloudSource struct{}

const (
	scAPIBase  = "https://api-v2.soundcloud.com"
	scSiteURL  = "https://soundcloud.com/"
	scPageSize = 20
	scMaxPages = 10
	// A browser's user agent: the web app's API answers the web app.
	scUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
	// Caps on what one HLS playlist may ask for.
	scMaxSegments = 3000
)

var (
	scScriptRe     = regexp.MustCompile(`<script[^>]+src="(https://a-v2\.sndcdn\.com/assets/[^"]+\.js)"`)
	scClientIDRe   = regexp.MustCompile(`client_id\s*:\s*"([0-9a-zA-Z]{32})"`)
	scTrackIDRe    = regexp.MustCompile(`^[0-9]{1,20}$`)
	scArtworkHost  = regexp.MustCompile(`^https://i[0-9]\.sndcdn\.com/`)
	scArtworkLarge = regexp.MustCompile(`-large(\.[a-z]+)$`)
	scMapURIRe     = regexp.MustCompile(`URI="([^"]+)"`)
)

var scClient struct {
	sync.Mutex
	id string
}

func (soundcloudSource) key() string  { return "soundcloud" }
func (soundcloudSource) name() string { return "SoundCloud" }

// coverAllowed limits the artwork proxy to SoundCloud's image CDN.
func (soundcloudSource) coverAllowed(u string) bool { return scArtworkHost.MatchString(u) }

// scIsCreativeCommons says whether a SoundCloud licence lets the track be
// copied: any Creative Commons variant, or the public-domain dedication.
func scIsCreativeCommons(license string) bool {
	return strings.HasPrefix(license, "cc-") || license == "no-rights-reserved"
}

// scTrack is the part of api-v2's track object this uses.
type scTrack struct {
	ID                 int64  `json:"id"`
	Title              string `json:"title"`
	Genre              string `json:"genre"`
	License            string `json:"license"`
	Duration           int64  `json:"duration"` // milliseconds
	ArtworkURL         string `json:"artwork_url"`
	PermalinkURL       string `json:"permalink_url"`
	ReleaseDate        string `json:"release_date"`
	CreatedAt          string `json:"created_at"`
	Policy             string `json:"policy"`
	Streamable         bool   `json:"streamable"`
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

// title drops a leading "Artist - " that uploads often carry in the title
// itself, when it names the same artist.
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

// cover is the 500-pixel artwork, or the uploader's picture when the track
// has none.
func (t *scTrack) cover() string {
	art := t.ArtworkURL
	if art == "" {
		art = t.User.AvatarURL
	}
	return scArtworkLarge.ReplaceAllString(art, "-t500x500$1")
}

// fullStream reports whether the whole track can be streamed, not just a
// preview.
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

// ---- the API -------------------------------------------------------------------

func scGet(ctx context.Context, client *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", scUserAgent)
	return client.Do(req)
}

// scClientID returns the web app's client id, finding it again when refresh
// is set (the API refused the cached one) or none is known yet: the last
// script on the home page that mentions it has it.
func scClientID(ctx context.Context, refresh bool) (string, error) {
	scClient.Lock()
	defer scClient.Unlock()
	if scClient.id != "" && !refresh {
		return scClient.id, nil
	}
	page, err := scFetchText(ctx, scSiteURL, 2<<20)
	if err != nil {
		return "", fmt.Errorf("soundcloud home page: %w", err)
	}
	scripts := scScriptRe.FindAllStringSubmatch(page, -1)
	for i := len(scripts) - 1; i >= 0; i-- {
		js, err := scFetchText(ctx, scripts[i][1], 8<<20)
		if err != nil {
			continue
		}
		if m := scClientIDRe.FindStringSubmatch(js); m != nil {
			scClient.id = m[1]
			return scClient.id, nil
		}
	}
	return "", errors.New("soundcloud: no client id in the web app's scripts (the site changed?)")
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

// scAPI calls api-v2 and decodes the answer into out. A refused client id
// is looked up again once.
func scAPI(ctx context.Context, rawURL string, q url.Values, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		id, err := scClientID(ctx, attempt > 0)
		if err != nil {
			return err
		}
		q.Set("client_id", id)
		resp, err := scGet(ctx, metadataLookupClient, rawURL+"?"+q.Encode())
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return &statusError{http.StatusNotFound, "that track isn't on SoundCloud any more"}
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("soundcloud: status %d", resp.StatusCode)
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
		resp.Body.Close()
		return err
	}
	return errors.New("soundcloud refused the client id")
}

// ---- search ----------------------------------------------------------------------

func (soundcloudSource) search(ctx context.Context, query string, page int) ([]discoverResult, bool, error) {
	if page > scMaxPages {
		return nil, false, nil
	}
	var out struct {
		Collection []scTrack `json:"collection"`
		NextHref   string    `json:"next_href"`
	}
	q := url.Values{
		"q": {query},
		// SoundCloud's own filter: tracks whose licence allows sharing,
		// which is every Creative Commons variant and the public domain.
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
		// Checked here too rather than trusting the filter alone, and
		// previews (full tracks only for paying listeners) left out.
		if !scIsCreativeCommons(t.License) || !t.fullStream() {
			continue
		}
		results = append(results, t.result())
	}
	return results, out.NextHref != "" && page < scMaxPages, nil
}

// ---- download --------------------------------------------------------------------

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
	album := title // singles, like NCS: the title keeps the Albums view sensible
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

// scDownloadAudio saves the best full-length stream into dir and returns
// its path and extension: the AAC stream when there is one, else the MP3.
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
		logErr("soundcloud aac stream", err) // the MP3 is the fallback
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

// scResolve turns a transcoding into the signed URL of the stream itself.
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

// scDownloadHLS fetches an HLS playlist of fragmented-MP4 segments and joins
// the initialisation segment and the media segments into one file — itself
// a valid fragmented MP4 — then remuxes it into an ordinary .m4a when ffmpeg
// is available, so every player seeks in it without trouble.
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
		return joined, nil // the joined file plays too
	}
	os.Remove(joined)
	return remuxed, nil
}

// scSaveURL downloads u into path, at most limit bytes.
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

// scAppendURL appends the body of u to w, refusing more than limit bytes.
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
