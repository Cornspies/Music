package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"

	"music-server/internal/config"
)

const metadataLookupTimeout = 15 * time.Second

var metadataLookupClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: dialGuard,
		}).DialContext,
	},
}

func dialGuard(network, address string, c syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("blocked address %q", address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("blocked address %q", address)
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("refusing to connect to non-public address %s", ip)
	}
	for _, n := range nonPublicNets {
		if n.Contains(ip) {
			return fmt.Errorf("refusing to connect to non-public address %s", ip)
		}
	}
	return nil
}

var nonPublicNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"100.64.0.0/10", "198.18.0.0/15", "192.0.0.0/24", "64:ff9b::/96", "64:ff9b:1::/48"} {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}()

const acceptScoreThreshold = 0.5

const coverMatchThreshold = 0.75

type metadataCandidate struct {
	title, artist, album, genre string
	year                        int
	durationSec                 float64
	artworkURL                  string
	coverArtArchiveReleaseID    string
	source                      string
}

type scoredCandidate struct {
	metadataCandidate
	id    string
	score float64
}

func (c metadataCandidate) artURL() string {
	if c.artworkURL != "" {
		return c.artworkURL
	}
	if c.coverArtArchiveReleaseID != "" {
		return "https://coverartarchive.org/release/" + c.coverArtArchiveReleaseID + "/front-500"
	}
	return ""
}

func candidateID(c metadataCandidate) string {
	sum := sha1.Sum([]byte(strings.Join([]string{
		c.source, c.title, c.artist, c.album, strconv.Itoa(int(c.durationSec)),
	}, "\x00")))
	return hex.EncodeToString(sum[:])[:12]
}

func metadataLookupDisabled() bool {
	return config.MetadataLookupDisabled()
}

type lookupOutcome struct {
	failed    int
	cancelled bool
}

func (o lookupOutcome) degraded() bool { return o.failed > 0 || o.cancelled }

func lookupCandidates(ctx context.Context, meta fileMetadata, fallbackName string) ([]scoredCandidate, lookupOutcome) {
	query, wantArtist, wantTitle := buildSearchPlan(meta, fallbackName)
	if query == "" {
		return nil, lookupOutcome{}
	}

	type source struct {
		name   string
		search func(context.Context, string) ([]metadataCandidate, error)
	}
	sources := []source{
		{"MusicBrainz", searchMusicBrainz},
		{"Deezer", searchDeezer},
		{"iTunes", searchItunes},
		{"YouTube", searchYouTube},
	}

	results := make([][]metadataCandidate, len(sources))
	var failed atomic.Int32
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src source) {
			defer wg.Done()
			defer logPanic("metadata lookup (" + src.name + ")")
			cands, err := src.search(ctx, query)
			if err != nil {
				failed.Add(1)
				if errors.Is(err, errMusicBrainzBusy) {
					log.Printf("metadata lookup (%s): %v", src.name, err)
				} else {
					logErr("metadata lookup ("+src.name+")", err)
				}
				return
			}
			results[i] = cands
		}(i, src)
	}
	wg.Wait()

	outcome := lookupOutcome{failed: int(failed.Load()), cancelled: ctx.Err() != nil}

	var out []scoredCandidate
	seen := map[string]bool{}
	for _, cands := range results {
		for _, c := range cands {
			if strings.TrimSpace(c.title) == "" {
				continue
			}
			id := candidateID(c)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, scoredCandidate{
				metadataCandidate: c,
				id:                id,
				score:             scoreCandidate(c, wantArtist, wantTitle, query, meta.duration),
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out, outcome
}

func applyCandidate(ctx context.Context, meta *fileMetadata, c metadataCandidate, overwrite bool) []string {
	filled := []string{}
	if c.title != "" && (overwrite || meta.title == "") {
		meta.title = strings.TrimSpace(c.title)
		filled = append(filled, "title")
	}
	if c.artist != "" && (overwrite || meta.artist == "") {
		meta.artist = strings.TrimSpace(c.artist)
		filled = append(filled, "artist")
	}
	if c.album != "" && (overwrite || meta.album == "") {
		meta.album = strings.TrimSpace(c.album)
		filled = append(filled, "album")
	}
	if c.genre != "" && (overwrite || meta.genre == "") {
		meta.genre = strings.TrimSpace(c.genre)
		filled = append(filled, "genre")
	}
	if c.year > 0 && (overwrite || meta.year == 0) {
		meta.year = c.year
		filled = append(filled, "year")
	}
	if overwrite || len(meta.picture) == 0 {
		if artURL := c.artURL(); artURL != "" {
			if img, err := fetchArtwork(ctx, artURL); err == nil {
				meta.picture = img
				filled = append(filled, "cover art")
			} else {
				logErr("fetch artwork", err)
			}
		}
	}
	return filled
}

func buildSearchPlan(meta fileMetadata, fallbackName string) (query, wantArtist, wantTitle string) {
	wantArtist, wantTitle = meta.artist, meta.title
	fnArtist, fnTitle, fnOK := splitArtistTitle(fallbackName)

	switch {
	case meta.artist != "" && meta.title != "":
		query = meta.artist + " " + meta.title
	case meta.title != "":
		if fnOK {
			query = fnArtist + " " + meta.title
		} else {
			query = meta.title
		}
	case meta.artist != "":
		if fnOK {
			query = meta.artist + " " + fnTitle
		} else {
			query = meta.artist
		}
	case fnOK:
		query = fnArtist + " " + fnTitle
		wantArtist, wantTitle = fnArtist, fnTitle
	default:
		query = cleanupFilename(strings.TrimSuffix(fallbackName, filepath.Ext(fallbackName)))
	}
	return query, wantArtist, wantTitle
}

var filenameSepRe = regexp.MustCompile(`\s*[-–—_]\s*`)

func splitArtistTitle(fallbackName string) (artist, title string, ok bool) {
	name := cleanupFilename(strings.TrimSuffix(fallbackName, filepath.Ext(fallbackName)))
	parts := filenameSepRe.Split(name, -1)
	if len(parts) > 0 && isTrackNumber(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) < 2 {
		return "", "", false
	}
	artist = strings.TrimSpace(parts[0])
	title = strings.TrimSpace(strings.Join(parts[1:], " - "))
	if artist == "" || title == "" {
		return "", "", false
	}
	return artist, title, true
}

func isTrackNumber(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 3 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func cleanupFilename(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	return strings.Join(strings.Fields(name), " ")
}

func scoreCandidate(c metadataCandidate, wantArtist, wantTitle, rawQuery string, knownDurationSec float64) float64 {
	var textScore float64
	switch {
	case wantArtist != "" && wantTitle != "":
		textScore = 0.5*jaccard(tokenSet(wantTitle), tokenSet(c.title)) + 0.5*jaccard(tokenSet(wantArtist), tokenSet(c.artist))
	case wantTitle != "":
		textScore = jaccard(tokenSet(wantTitle), tokenSet(c.title))
		if len(tokenSet(wantTitle)) <= 1 {
			textScore *= 0.4
		}
	case wantArtist != "":
		textScore = jaccard(tokenSet(wantArtist), tokenSet(c.artist))
	default:
		textScore = jaccard(tokenSet(rawQuery), tokenSet(c.artist+" "+c.title))
	}

	durationScore := 0.5
	if knownDurationSec > 0 && c.durationSec > 0 {
		diff := math.Abs(c.durationSec - knownDurationSec)
		durationScore = math.Max(0, 1-diff/20)
	}
	score := 0.75*textScore + 0.25*durationScore

	if c.source == "YouTube" {
		score *= 0.9
	}
	return score
}

func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	lastSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteRune(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func tokenSet(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, tok := range strings.Fields(normalizeForMatch(s)) {
		set[tok] = struct{}{}
	}
	return set
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func parseReleaseYear(releaseDate string) int {
	if len(releaseDate) < 4 {
		return 0
	}
	if y, err := strconv.Atoi(releaseDate[:4]); err == nil && y > 0 && y < 3000 {
		return y
	}
	return 0
}

const itunesSearchURL = "https://itunes.apple.com/search"

type itunesSearchResponse struct {
	Results []struct {
		TrackName        string  `json:"trackName"`
		ArtistName       string  `json:"artistName"`
		CollectionName   string  `json:"collectionName"`
		PrimaryGenreName string  `json:"primaryGenreName"`
		ReleaseDate      string  `json:"releaseDate"`
		ArtworkURL100    string  `json:"artworkUrl100"`
		TrackTimeMillis  float64 `json:"trackTimeMillis"`
	} `json:"results"`
}

func searchItunes(ctx context.Context, query string) ([]metadataCandidate, error) {
	q := url.Values{"term": {query}, "media": {"music"}, "entity": {"song"}, "limit": {"5"}}
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
	var parsed itunesSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]metadataCandidate, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		artURL := r.ArtworkURL100
		if artURL != "" {
			artURL = strings.Replace(artURL, "100x100bb", "600x600bb", 1)
		}
		out = append(out, metadataCandidate{
			title: r.TrackName, artist: r.ArtistName, album: r.CollectionName,
			genre: r.PrimaryGenreName, year: parseReleaseYear(r.ReleaseDate),
			durationSec: r.TrackTimeMillis / 1000, artworkURL: artURL, source: "iTunes",
		})
	}
	return out, nil
}

const deezerSearchURL = "https://api.deezer.com/search"

type deezerSearchResponse struct {
	Data []struct {
		Title  string `json:"title"`
		Artist struct {
			Name string `json:"name"`
		} `json:"artist"`
		Album struct {
			Title    string `json:"title"`
			CoverXL  string `json:"cover_xl"`
			CoverBig string `json:"cover_big"`
			CoverMed string `json:"cover_medium"`
		} `json:"album"`
		Duration int `json:"duration"`
	} `json:"data"`
}

func searchDeezer(ctx context.Context, query string) ([]metadataCandidate, error) {
	q := url.Values{"q": {query}, "limit": {"5"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deezerSearchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deezer search: status %d", resp.StatusCode)
	}
	var parsed deezerSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([]metadataCandidate, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		art := d.Album.CoverXL
		if art == "" {
			art = d.Album.CoverBig
		}
		if art == "" {
			art = d.Album.CoverMed
		}
		out = append(out, metadataCandidate{
			title: d.Title, artist: d.Artist.Name, album: d.Album.Title,
			durationSec: float64(d.Duration), artworkURL: art, source: "Deezer",
		})
	}
	return out, nil
}

var musicbrainzSearchURL = "https://musicbrainz.org/ws/2/recording"

const (
	mbMinInterval      = 1100 * time.Millisecond
	mbMaxAttempts      = 3
	mbCooldownDuration = 60 * time.Second
)

var errMusicBrainzBusy = errors.New("musicbrainz is rate-limiting requests; skipping this source temporarily")

var mbThrottleState struct {
	mu            sync.Mutex
	last          time.Time
	cooldownUntil time.Time
}

func mbThrottle(ctx context.Context) error {
	mbThrottleState.mu.Lock()
	slot := mbThrottleState.last.Add(mbMinInterval)
	if now := time.Now(); slot.Before(now) {
		slot = now
	}
	mbThrottleState.last = slot
	mbThrottleState.mu.Unlock()

	if wait := time.Until(slot); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-t.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func mbInCooldown() bool {
	mbThrottleState.mu.Lock()
	defer mbThrottleState.mu.Unlock()
	return time.Now().Before(mbThrottleState.cooldownUntil)
}

func mbStartCooldown() {
	mbThrottleState.mu.Lock()
	defer mbThrottleState.mu.Unlock()
	mbThrottleState.cooldownUntil = time.Now().Add(mbCooldownDuration)
}

func mbRetryDelay(resp *http.Response, attempt int) time.Duration {
	if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil && secs >= 0 && secs <= 60 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

type musicbrainzSearchResponse struct {
	Recordings []struct {
		Title        string `json:"title"`
		Length       int    `json:"length"`
		ArtistCredit []struct {
			Name string `json:"name"`
		} `json:"artist-credit"`
		Releases []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Date  string `json:"date"`
		} `json:"releases"`
	} `json:"recordings"`
}

func musicbrainzRequest(ctx context.Context, query string) (*musicbrainzSearchResponse, error) {
	q := url.Values{"query": {query}, "fmt": {"json"}, "limit": {"5"}}

	for attempt := 1; attempt <= mbMaxAttempts; attempt++ {
		if err := mbThrottle(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, musicbrainzSearchURL+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Music-SelfHosted-MusicPlayer/1.0 ( self-hosted )")

		resp, err := metadataLookupClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
			delay := mbRetryDelay(resp, attempt)
			resp.Body.Close()
			if attempt == mbMaxAttempts {
				mbStartCooldown()
				return nil, errMusicBrainzBusy
			}
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("musicbrainz search: status %d", resp.StatusCode)
		}

		var parsed musicbrainzSearchResponse
		decErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed)
		resp.Body.Close()
		if decErr != nil {
			return nil, decErr
		}
		return &parsed, nil
	}
	return nil, errMusicBrainzBusy
}

func searchMusicBrainz(ctx context.Context, query string) ([]metadataCandidate, error) {
	if mbInCooldown() {
		return nil, errMusicBrainzBusy
	}

	parsed, err := musicbrainzRequest(ctx, query)
	if err != nil {
		return nil, err
	}

	out := make([]metadataCandidate, 0, len(parsed.Recordings))
	for _, r := range parsed.Recordings {
		artist := ""
		if len(r.ArtistCredit) > 0 {
			artist = r.ArtistCredit[0].Name
		}
		album, year, releaseID := "", 0, ""
		if len(r.Releases) > 0 {
			album = r.Releases[0].Title
			year = parseReleaseYear(r.Releases[0].Date)
			releaseID = r.Releases[0].ID
		}
		out = append(out, metadataCandidate{
			title: r.Title, artist: artist, album: album, year: year,
			durationSec: float64(r.Length) / 1000, coverArtArchiveReleaseID: releaseID, source: "MusicBrainz",
		})
	}
	return out, nil
}

const (
	youtubeSearchURL = "https://www.googleapis.com/youtube/v3/search"
	youtubeVideosURL = "https://www.googleapis.com/youtube/v3/videos"

	youtubeDefaultMaxResults = 15
	youtubeMaxResultsCap     = 50
)

func youtubeMaxResults() int {
	raw := config.Value("MUSIC_YOUTUBE_MAX_RESULTS")
	if raw == "" {
		return youtubeDefaultMaxResults
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return youtubeDefaultMaxResults
	}
	return max(1, min(n, youtubeMaxResultsCap))
}

type youtubeSearchResponse struct {
	Items []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			Title        string `json:"title"`
			ChannelTitle string `json:"channelTitle"`
			Thumbnails   struct {
				High struct {
					URL string `json:"url"`
				} `json:"high"`
				Medium struct {
					URL string `json:"url"`
				} `json:"medium"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
}

type youtubeVideosResponse struct {
	Items []struct {
		ID             string `json:"id"`
		ContentDetails struct {
			Duration string `json:"duration"`
		} `json:"contentDetails"`
	} `json:"items"`
}

func searchYouTube(ctx context.Context, query string) ([]metadataCandidate, error) {
	apiKey := config.YouTubeAPIKey()
	if apiKey == "" {
		return nil, nil
	}

	q := url.Values{
		"part":            {"snippet"},
		"type":            {"video"},
		"videoCategoryId": {"10"},
		"maxResults":      {strconv.Itoa(youtubeMaxResults())},
		"q":               {query},
		"key":             {apiKey},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, youtubeSearchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube search: status %d", resp.StatusCode)
	}
	var parsed youtubeSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		if it.ID.VideoID != "" {
			ids = append(ids, it.ID.VideoID)
		}
	}
	durations := map[string]float64{}
	if len(ids) > 0 {
		if d, err := youtubeDurations(ctx, apiKey, ids); err != nil {
			logErr("youtube durations", err)
		} else {
			durations = d
		}
	}

	out := make([]metadataCandidate, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		artist, title := parseYouTubeTitle(it.Snippet.Title, it.Snippet.ChannelTitle)
		if title == "" {
			continue
		}
		thumb := it.Snippet.Thumbnails.High.URL
		if thumb == "" {
			thumb = it.Snippet.Thumbnails.Medium.URL
		}
		out = append(out, metadataCandidate{
			title: title, artist: artist,
			durationSec: durations[it.ID.VideoID],
			artworkURL:  thumb,
			source:      "YouTube",
		})
	}
	return out, nil
}

func youtubeDurations(ctx context.Context, apiKey string, ids []string) (map[string]float64, error) {
	q := url.Values{"part": {"contentDetails"}, "id": {strings.Join(ids, ",")}, "key": {apiKey}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, youtubeVideosURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube videos: status %d", resp.StatusCode)
	}
	var parsed youtubeVideosResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, it := range parsed.Items {
		out[it.ID] = parseISO8601Duration(it.ContentDetails.Duration)
	}
	return out, nil
}

var iso8601DurationRe = regexp.MustCompile(`^P(?:(\d+)D)?T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

func parseISO8601Duration(s string) float64 {
	m := iso8601DurationRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	atoi := func(v string) float64 {
		if v == "" {
			return 0
		}
		n, _ := strconv.Atoi(v)
		return float64(n)
	}
	return atoi(m[1])*86400 + atoi(m[2])*3600 + atoi(m[3])*60 + atoi(m[4])
}

var youtubeNoiseRe = regexp.MustCompile(`(?i)[\(\[][^\)\]]*(official|lyric|audio|video|hd|hq|4k|remaster|visualizer|mv|m/v|full album|free download|explicit|clean)[^\)\]]*[\)\]]`)

var youtubeChannelSuffixRe = regexp.MustCompile(`(?i)\s*-\s*topic\s*$`)

func parseYouTubeTitle(videoTitle, channelTitle string) (artist, title string) {
	clean := youtubeNoiseRe.ReplaceAllString(videoTitle, " ")
	clean = strings.Join(strings.Fields(clean), " ")
	clean = strings.Trim(clean, " -–—|")

	channel := youtubeChannelSuffixRe.ReplaceAllString(strings.TrimSpace(channelTitle), "")

	parts := filenameSepRe.Split(clean, -1)
	if len(parts) >= 2 {
		a := strings.TrimSpace(parts[0])
		t := strings.TrimSpace(strings.Join(parts[1:], " - "))
		if a != "" && t != "" {
			return a, t
		}
	}
	return channel, clean
}

const maxArtworkBytes = 8 << 20

func fetchArtwork(ctx context.Context, artworkURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artworkURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch artwork: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtworkBytes))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("fetch artwork: empty body")
	}
	return data, nil
}
