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
	"syscall"
	"time"
	"unicode"

	"music-server/internal/config"
)

// metadataLookupTimeout bounds a whole online-lookup attempt (every source,
// plus an artwork download), so a slow or unreachable network never holds an
// upload response open for long.
const metadataLookupTimeout = 15 * time.Second

// Each individual HTTP call gets its own shorter timeout so one hung
// request can't eat the entire budget on its own.
// metadataLookupClient talks to third-party metadata providers and downloads
// artwork from URLs those providers hand back. Those URLs are not ours, so the
// client refuses to connect to private/loopback addresses: otherwise a hostile
// or compromised provider response could redirect the server into its own
// network (an SSRF), which is especially unwelcome on a self-hosted box.
var metadataLookupClient = &http.Client{
        Timeout: 5 * time.Second,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
                if len(via) >= 5 {
                        return fmt.Errorf("too many redirects")
                }
                return nil
        },
        Transport: &http.Transport{
                // Control runs after DNS resolution, for every connection the
                // transport makes — including ones reached through a redirect —
                // so a hostname that resolves to a private address is still
                // rejected. This is the check that actually closes the SSRF.
                DialContext: (&net.Dialer{
                        Timeout: 5 * time.Second,
                        Control: dialGuard,
                }).DialContext,
        },
}

// dialGuard rejects connections to addresses that are not publicly routable.
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
        return nil
}

// acceptScoreThreshold is the minimum confidence (0-1) a candidate needs
// before we'll auto-apply it at upload time. Below this, enrichment is a
// no-op and the file keeps whatever metadata it already had — the user can
// still pick any candidate manually from the review dialog afterwards.
const acceptScoreThreshold = 0.5

// metadataCandidate is a normalized result from any of the lookup sources.
type metadataCandidate struct {
	title, artist, album, genre string
	year                        int
	durationSec                 float64
	artworkURL                  string
	// coverArtArchiveReleaseID is set instead of artworkURL for MusicBrainz
	// results, which don't carry artwork directly — the Cover Art Archive
	// is queried lazily, only when a candidate is actually applied.
	coverArtArchiveReleaseID string
	source                   string
}

// scoredCandidate pairs a candidate with its confidence and a stable ID the
// frontend can send back to apply it.
type scoredCandidate struct {
	metadataCandidate
	id    string
	score float64
}

// artURL resolves the image URL for a candidate, expanding a MusicBrainz
// release ID into a Cover Art Archive URL when needed.
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

// ---- candidate cache -------------------------------------------------------
//
// Candidates found during upload are kept briefly so the review dialog can
// show them without re-querying every provider. A cache miss is never fatal:
// the handler just re-runs the search.

const candidateCacheTTL = 30 * time.Minute
const candidateCacheMaxEntries = 500

type candidateCacheEntry struct {
	candidates []scoredCandidate
	appliedID  string // ID of the candidate auto-applied at upload, if any
	expires    time.Time
}

var candidateCache = struct {
	mu      sync.Mutex
	entries map[string]candidateCacheEntry
}{entries: map[string]candidateCacheEntry{}}

func cacheCandidates(trackID string, cands []scoredCandidate, appliedID string) {
	if trackID == "" {
		return
	}
	candidateCache.mu.Lock()
	defer candidateCache.mu.Unlock()

	now := time.Now()
	// Opportunistically evict expired entries, and hard-cap total size so a
	// long-running server can't grow this map without bound.
	for k, v := range candidateCache.entries {
		if now.After(v.expires) {
			delete(candidateCache.entries, k)
		}
	}
	if len(candidateCache.entries) >= candidateCacheMaxEntries {
		for k := range candidateCache.entries {
			delete(candidateCache.entries, k)
			if len(candidateCache.entries) < candidateCacheMaxEntries {
				break
			}
		}
	}
	candidateCache.entries[trackID] = candidateCacheEntry{
		candidates: cands, appliedID: appliedID, expires: now.Add(candidateCacheTTL),
	}
}

func cachedCandidates(trackID string) (candidateCacheEntry, bool) {
	candidateCache.mu.Lock()
	defer candidateCache.mu.Unlock()
	e, ok := candidateCache.entries[trackID]
	if !ok || time.Now().After(e.expires) {
		return candidateCacheEntry{}, false
	}
	return e, true
}

func forgetCandidates(trackID string) {
	candidateCache.mu.Lock()
	defer candidateCache.mu.Unlock()
	delete(candidateCache.entries, trackID)
}

// ---- lookup ----------------------------------------------------------------

func metadataLookupDisabled() bool {
	return config.MetadataLookupDisabled()
}

// lookupCandidates searches every configured provider and returns all
// candidates scored against what we know about the file, best first. Errors
// from an individual provider are logged and skipped — one dead source never
// prevents the others from contributing.
func lookupCandidates(ctx context.Context, meta fileMetadata, fallbackName string) []scoredCandidate {
	query, wantArtist, wantTitle := buildSearchPlan(meta, fallbackName)
	if query == "" {
		return nil
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

	// Query every provider at once rather than one after another. They share a
	// single deadline, so sequentially the slowest one starved the rest: with
	// MusicBrainz first, its rate-limit wait plus retries could burn most of the
	// budget and iTunes or YouTube would simply never be asked, which is why
	// suggestions sometimes arrived missing a provider. In parallel the whole
	// lookup costs about as long as the slowest single provider instead of the
	// sum of all of them.
	results := make([][]metadataCandidate, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src source) {
			defer wg.Done()
			cands, err := src.search(ctx, query)
			if err != nil {
				// Rate limiting is an expected, self-healing condition, not a
				// failure worth an ERROR line — the other providers cover it.
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

	// Flattened in the order the providers are declared, so the result is the
	// same on every run regardless of which one answered first.
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
	return out
}

// enrichMissingMetadata fills in whatever's missing from an uploaded track's
// tags (title, artist, album, genre, year, cover art) using the best match
// found across MusicBrainz, Deezer, iTunes and (if configured) YouTube — but
// only if that match is confident enough. Any tag actually embedded in the
// file is left untouched, and if nothing clears the confidence bar the file
// keeps exactly what it had. It returns every candidate found (best first)
// plus the ID of the one applied, so the caller can cache them for the
// user-facing "pick the right result" dialog. Entirely best-effort: no
// network, no match, or any other error is silently non-fatal. Set
// MUSIC_DISABLE_METADATA_LOOKUP to disable lookups altogether.
func (s *Server) enrichMissingMetadata(meta *fileMetadata, fallbackName string) ([]scoredCandidate, string) {
	if metadataLookupDisabled() {
		return nil, ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), metadataLookupTimeout)
	defer cancel()

	cands := lookupCandidates(ctx, *meta, fallbackName)
	if len(cands) == 0 {
		return nil, ""
	}

	best := cands[0]
	if best.score < acceptScoreThreshold {
		log.Printf("metadata lookup: no confident match for %q (best %.2f from %s) — leaving metadata as-is, %d alternative(s) available",
			fallbackName, best.score, best.source, len(cands))
		return cands, ""
	}

	filled := applyCandidate(ctx, meta, best.metadataCandidate, false)
	if len(filled) > 0 {
		log.Printf("metadata lookup: filled %s for %q from %s match %q by %q (confidence %.2f)",
			strings.Join(filled, ", "), fallbackName, best.source, best.title, best.artist, best.score)
	}
	return cands, best.id
}

// applyCandidate copies fields from a candidate into meta. When overwrite is
// false only empty fields are filled (the automatic upload path); when true
// every non-empty candidate field wins (the user explicitly picked it).
// It returns the names of the fields that changed.
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

// buildSearchPlan decides what to search for (query) and what we'll treat
// as ground truth when scoring results (wantArtist/wantTitle). It prefers
// real tags for ground truth, but will still use a filename-derived guess
// to widen the search query — e.g. a title tag with no artist tag still
// benefits from a filename-derived artist hint in the query, without that
// guess being trusted for scoring. If there are no tags at all, the
// filename guess becomes the ground truth too, since it's all we have.
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

// splitArtistTitle tries to pull "Artist" and "Title" out of a filename like
// "Daft Punk - One More Time.mp3" or "01 - Daft Punk - One More Time.flac".
func splitArtistTitle(fallbackName string) (artist, title string, ok bool) {
	name := cleanupFilename(strings.TrimSuffix(fallbackName, filepath.Ext(fallbackName)))
	parts := filenameSepRe.Split(name, -1)
	if len(parts) > 0 && isTrackNumber(parts[0]) {
		parts = parts[1:] // drop a leading track-number part, e.g. "01 - ..."
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

// scoreCandidate rates how confident we should be that a candidate is
// actually the same song as what we're looking for, from 0 to 1. Text
// similarity (title/artist token overlap) carries most of the weight;
// duration proximity is a secondary signal used when we know the file's real
// length (e.g. it breaks ties between a studio and live take of the same
// song). A single generic word as the only title signal (e.g. "Intro") is
// deliberately dampened, since it matches too many unrelated tracks to be
// trusted on its own.
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

	durationScore := 0.5 // neutral when we can't check either side
	if knownDurationSec > 0 && c.durationSec > 0 {
		diff := math.Abs(c.durationSec - knownDurationSec)
		durationScore = math.Max(0, 1-diff/20)
	}
	score := 0.75*textScore + 0.25*durationScore

	// YouTube titles are free-text video names, not curated metadata, so even
	// a good-looking match is less trustworthy than one from a music
	// database. Discount it so it rarely wins auto-apply on its own, while
	// still being offered to the user as a pickable option.
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

// ---- iTunes Search API (https://itunes.apple.com/search) — no key needed ----

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

// ---- Deezer Search API (https://api.deezer.com/search) — no key needed ----

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
		Duration int `json:"duration"` // seconds
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

// ---- MusicBrainz recording search (https://musicbrainz.org/ws/2/recording) ----
// Free and keyless, but their usage policy asks for a descriptive
// User-Agent and at most ~1 request/second. MusicBrainz answers with 503
// (not 429) when you exceed that, so a 503 here is almost always a rate
// limit rather than an outage — mbThrottle paces requests, and a 503 is
// retried with backoff before the source is temporarily given up on.

// var rather than const so tests can point it at a stub server.
var musicbrainzSearchURL = "https://musicbrainz.org/ws/2/recording"

const (
	// Slightly above 1/sec: MusicBrainz measures the rate on their side,
	// so the extra margin absorbs clock and network jitter that would
	// otherwise read as a burst.
	mbMinInterval = 1100 * time.Millisecond
	mbMaxAttempts = 3
	// After exhausting retries, stop querying MusicBrainz entirely for a
	// while. Hammering a rate-limited endpoint only deepens the limit,
	// and the other providers still cover the lookup meanwhile.
	mbCooldownDuration = 60 * time.Second
)

// errMusicBrainzBusy marks a rate-limit outcome so the caller can log it as
// an expected condition rather than a hard failure.
var errMusicBrainzBusy = errors.New("musicbrainz is rate-limiting requests; skipping this source temporarily")

var mbThrottleState struct {
	mu            sync.Mutex
	last          time.Time
	cooldownUntil time.Time
}

// mbThrottle waits until it's safe to issue the next MusicBrainz request,
// serializing callers so concurrent uploads can't burst past the limit.
// It returns ctx.Err() if the caller's deadline passes while waiting.
func mbThrottle(ctx context.Context) error {
	mbThrottleState.mu.Lock()
	defer mbThrottleState.mu.Unlock()

	if wait := time.Until(mbThrottleState.last.Add(mbMinInterval)); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	mbThrottleState.last = time.Now()
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

// mbRetryDelay honours a Retry-After header when the server sends one,
// otherwise falls back to exponential backoff (1s, 2s, ...).
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
		Length       int    `json:"length"` // ms
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

// musicbrainzRequest performs the search, retrying when MusicBrainz reports
// that we're going too fast. Their rate limiter answers 503 (429 is accepted
// here too in case that changes); anything else is returned as a real error.
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
		// MusicBrainz's usage policy requires an identifying User-Agent.
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
	// Still cooling down from a previous rate limit — skip quietly rather
	// than spending part of the lookup budget on a near-certain 503.
	if mbInCooldown() {
		return nil, nil
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

// ---- YouTube Data API v3 ----------------------------------------------------
//
// Unlike the other three, YouTube has no keyless public search endpoint, so
// this source stays disabled unless MUSIC_YOUTUBE_API_KEY is set (free to create
// at https://console.cloud.google.com — enable "YouTube Data API v3").
// Results are video titles rather than curated metadata, so they're parsed
// heuristically and scored with a penalty; they're most useful as a manual
// fallback for tracks the music databases simply don't have.

const (
	youtubeSearchURL = "https://www.googleapis.com/youtube/v3/search"
	youtubeVideosURL = "https://www.googleapis.com/youtube/v3/videos"
)

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
			Duration string `json:"duration"` // ISO-8601, e.g. "PT3M32S"
		} `json:"contentDetails"`
	} `json:"items"`
}

func searchYouTube(ctx context.Context, query string) ([]metadataCandidate, error) {
	apiKey := config.YouTubeAPIKey()
	apiKey = ""
	if apiKey == "" {
		return nil, nil // not configured — silently skip this source
	}

	q := url.Values{
		"part":            {"snippet"},
		"type":            {"video"},
		"videoCategoryId": {"10"}, // "Music"
		"maxResults":      {"5"},
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
	// Durations live on a different endpoint; one batched call covers all
	// hits. A failure here is non-fatal — we just lose the duration signal.
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

// parseISO8601Duration handles the subset YouTube emits, e.g. "PT3M32S".
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

// youtubeNoiseRe strips the promotional clutter that fills video titles —
// "(Official Music Video)", "[HD]", "(Lyrics)", "(Audio)" and friends.
var youtubeNoiseRe = regexp.MustCompile(`(?i)[\(\[][^\)\]]*(official|lyric|audio|video|hd|hq|4k|remaster|visualizer|mv|m/v|full album|free download|explicit|clean)[^\)\]]*[\)\]]`)

// youtubeChannelSuffixRe strips "- Topic" from auto-generated artist
// channels, which are otherwise a very reliable artist signal.
var youtubeChannelSuffixRe = regexp.MustCompile(`(?i)\s*-\s*topic\s*$`)

// parseYouTubeTitle turns a video title into an (artist, title) guess.
// "Daft Punk - One More Time (Official Video)" → ("Daft Punk", "One More Time").
// When the title has no separator, the channel name is used as the artist,
// which works well for official artist and "- Topic" channels.
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

// ---- artwork download, shared by all sources ----

const maxArtworkBytes = 8 << 20 // 8 MB

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
