package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dhowden/tag"

	"music-server/internal/config"
)

// Lyrics.
//
// Resolved lazily the first time someone asks for a track's lyrics, then kept
// in the lyrics table. Sources, in order: the file's own tag (USLT / LYRICS —
// the uploader's copy wins), LRCLIB (https://lrclib.net, a free and open
// lyrics database with a plain JSON API, no key), or a text someone pasted in
// by hand. A miss is remembered too, so a track that has no lyrics anywhere
// doesn't cost a network round-trip on every open; it's retried after a
// while in case LRCLIB has gained it since.

const (
	lyricsSourceEmbedded = "embedded"
	lyricsSourceLRCLIB   = "lrclib"
	lyricsSourceManual   = "manual"
	lyricsSourceNone     = "none"

	// A miss is cached this long before LRCLIB is asked again.
	lyricsMissTTL = 7 * 24 * time.Hour

	// LRCLIB asks clients to identify themselves.
	lrclibUserAgent = "Music-SelfHosted-MusicPlayer/1.0 (https://github.com/cornspies/music-player)"
)

// lrclibBase is a variable so tests can point it at a stub.
var lrclibBase = "https://lrclib.net/api"

// LyricLine is one time-stamped line of synced lyrics.
type LyricLine struct {
	// T is the offset in seconds at which the line starts.
	T    float64 `json:"t"`
	Line string  `json:"line"`
}

// Lyrics is what GET /api/tracks/{id}/lyrics returns.
type Lyrics struct {
	// Plain is the whole text, one line per row, for tracks with no timing
	// information (and as the fallback when Synced is present).
	Plain string `json:"plain"`
	// Synced is nil when only plain text is known.
	Synced []LyricLine `json:"synced"`
	// Source is "embedded", "lrclib" or "manual"; "none" means nothing was
	// found anywhere (the response is then a 404 with this body).
	Source string `json:"source"`
	// Instrumental is LRCLIB's word that the track has no lyrics on purpose.
	Instrumental bool `json:"instrumental"`
	// Editable is true for the uploader and admins.
	Editable bool `json:"editable"`
}

type lyricsRow struct {
	plain, synced, source string
	fetchedAt             time.Time
	instrumental          bool
}

func lyricsLookupDisabled() bool { return config.Flag("MUSIC_DISABLE_LYRICS_LOOKUP") }

func (s *Server) readLyricsRow(trackID string) (*lyricsRow, error) {
	var row lyricsRow
	var fetched string
	var instrumental int
	err := s.db.QueryRow(`SELECT plain, synced, source, fetched_at, instrumental FROM lyrics WHERE track_id = ?`, trackID).
		Scan(&row.plain, &row.synced, &row.source, &fetched, &instrumental)
	if err != nil {
		return nil, err
	}
	row.fetchedAt, _ = time.Parse(time.RFC3339, fetched)
	row.instrumental = instrumental == 1
	return &row, nil
}

func (s *Server) writeLyricsRow(trackID string, row lyricsRow) error {
	instr := 0
	if row.instrumental {
		instr = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO lyrics (track_id, plain, synced, source, fetched_at, instrumental) VALUES (?,?,?,?,?,?)
		ON CONFLICT(track_id) DO UPDATE SET plain = excluded.plain, synced = excluded.synced,
			source = excluded.source, fetched_at = excluded.fetched_at, instrumental = excluded.instrumental`,
		trackID, row.plain, row.synced, row.source, nowRFC3339(), instr)
	return err
}

// forgetLookedUpLyrics drops a cached LRCLIB result or miss so the next
// request looks again — for after the tags were corrected, when the old
// search terms were probably why nothing was found. Embedded and hand-typed
// lyrics don't depend on the tags and are kept.
func (s *Server) forgetLookedUpLyrics(trackID string) {
	_, _ = s.db.Exec(`DELETE FROM lyrics WHERE track_id = ? AND source IN (?, ?)`,
		trackID, lyricsSourceLRCLIB, lyricsSourceNone)
}

// resolveLyrics returns the cached row, filling the cache first when it is
// empty or holds a miss old enough to retry.
func (s *Server) resolveLyrics(ctx context.Context, t *Track, filePath string) (*lyricsRow, error) {
	row, err := s.readLyricsRow(t.ID)
	switch {
	case err == nil && row.source != lyricsSourceNone:
		return row, nil
	case err == nil && time.Since(row.fetchedAt) < lyricsMissTTL:
		return row, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}

	fresh := lyricsRow{source: lyricsSourceNone}
	if text := embeddedLyrics(filePath); text != "" {
		fresh.source = lyricsSourceEmbedded
		if lines := parseLRC(text); len(lines) > 0 {
			fresh.synced = text
			fresh.plain = plainFromLines(lines)
		} else {
			fresh.plain = text
		}
	} else if !lyricsLookupDisabled() {
		hit, err := lrclibLookup(ctx, t)
		if err != nil {
			// The provider is down or throttling (LRCLIB sits behind
			// Cloudflare and has the odd 503). That says nothing about
			// whether the lyrics exist, so it must not be cached as a miss
			// — that would hide them for a week. Answer "none" for now and
			// let the next request try again once the backoff has passed.
			logErr("lyrics lookup", err)
			return &fresh, nil
		}
		if hit != nil {
			fresh = *hit
		}
	}
	if err := s.writeLyricsRow(t.ID, fresh); err != nil {
		return nil, err
	}
	return &fresh, nil
}

// purgeStaleLyricsMisses runs once: builds before the backoff logic wrote a
// provider outage into the cache as "no lyrics", so any miss recorded by
// them is dropped and looked up afresh. Keyed in settings so it's a one-off.
func (s *Server) purgeStaleLyricsMisses() {
	const key = "lyrics_misses_purged"
	if _, done := s.getSetting(key); done {
		return
	}
	if _, err := s.db.Exec(`DELETE FROM lyrics WHERE source = ?`, lyricsSourceNone); err != nil {
		logErr("purge lyrics misses", err)
		return
	}
	_ = s.putSetting(key, nowRFC3339())
}

// lrclibBackoff holds the moment LRCLIB may be contacted again after it
// answered with an error. One outage shouldn't turn every "open lyrics"
// into another failing round-trip; the default matches its own Retry-After
// when it sends one.
var lrclibBackoff struct {
	sync.Mutex
	until time.Time
}

const lrclibDefaultBackoff = 2 * time.Minute

func lrclibAvailable() bool {
	lrclibBackoff.Lock()
	defer lrclibBackoff.Unlock()
	return time.Now().After(lrclibBackoff.until)
}

func lrclibPause(retryAfter string) {
	d := lrclibDefaultBackoff
	if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
		d = time.Duration(secs) * time.Second
	}
	lrclibBackoff.Lock()
	lrclibBackoff.until = time.Now().Add(d)
	lrclibBackoff.Unlock()
}

// embeddedLyrics reads the lyrics tag (USLT for ID3, LYRICS for Vorbis) from
// the audio file, or "" when there is none.
func embeddedLyrics(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil || m == nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(m.Lyrics(), "\r\n", "\n"))
}

// ---- LRCLIB ---------------------------------------------------------------

type lrclibRecord struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// lrclibLookup asks LRCLIB for the track. /api/get wants an exact title,
// artist, album and duration and is the precise path; when it 404s, /api/search
// is tried without the album and the closest duration within a few seconds
// is taken. nil, nil means "nothing there".
func lrclibLookup(ctx context.Context, t *Track) (*lyricsRow, error) {
	if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Artist) == "" {
		return nil, nil // nothing to search by
	}
	if !lrclibAvailable() {
		return nil, errors.New("lrclib is unavailable, waiting before asking again")
	}
	q := url.Values{}
	q.Set("track_name", t.Title)
	q.Set("artist_name", t.Artist)
	if t.Album != "" {
		q.Set("album_name", t.Album)
	}
	if t.Duration > 0 {
		q.Set("duration", strconv.Itoa(int(math.Round(t.Duration))))
	}
	var rec lrclibRecord
	found, err := lrclibGet(ctx, "/get?"+q.Encode(), &rec)
	if err != nil {
		return nil, err
	}
	if !found {
		q = url.Values{}
		q.Set("track_name", t.Title)
		q.Set("artist_name", t.Artist)
		var results []lrclibRecord
		if _, err := lrclibGet(ctx, "/search?"+q.Encode(), &results); err != nil {
			return nil, err
		}
		best := pickLRCLIBResult(results, t.Duration)
		if best == nil {
			return nil, nil
		}
		rec = *best
	}
	row := &lyricsRow{
		source:       lyricsSourceLRCLIB,
		instrumental: rec.Instrumental,
		plain:        strings.TrimSpace(rec.PlainLyrics),
		synced:       strings.TrimSpace(rec.SyncedLyrics),
	}
	if row.synced != "" && row.plain == "" {
		row.plain = plainFromLines(parseLRC(row.synced))
	}
	if row.plain == "" && row.synced == "" && !row.instrumental {
		return nil, nil
	}
	return row, nil
}

// pickLRCLIBResult chooses the search hit whose length is closest to the
// track's, preferring one with synced lyrics. Anything more than five seconds
// off is likely a different version (live, radio edit) and is skipped.
func pickLRCLIBResult(results []lrclibRecord, duration float64) *lrclibRecord {
	var usable []lrclibRecord
	for _, r := range results {
		if r.PlainLyrics == "" && r.SyncedLyrics == "" && !r.Instrumental {
			continue
		}
		if duration > 0 && r.Duration > 0 && math.Abs(r.Duration-duration) > 5 {
			continue
		}
		usable = append(usable, r)
	}
	if len(usable) == 0 {
		return nil
	}
	sort.SliceStable(usable, func(i, j int) bool {
		si, sj := usable[i].SyncedLyrics != "", usable[j].SyncedLyrics != ""
		if si != sj {
			return si
		}
		return math.Abs(usable[i].Duration-duration) < math.Abs(usable[j].Duration-duration)
	})
	return &usable[0]
}

// lrclibGet performs one request through the SSRF-guarded lookup client.
// It reports found=false on a 404 rather than an error, since "not in the
// database" is the common, uninteresting case.
func lrclibGet(ctx context.Context, pathAndQuery string, out any) (found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lrclibBase+pathAndQuery, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", lrclibUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := metadataLookupClient.Do(req)
	if err != nil {
		lrclibPause("")
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return false, nil
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		// Their problem, not a verdict on the track: back off and try later.
		lrclibPause(resp.Header.Get("Retry-After"))
		return false, fmt.Errorf("lrclib: status %d (paused)", resp.StatusCode)
	default:
		return false, fmt.Errorf("lrclib: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(out); err != nil {
		return false, fmt.Errorf("lrclib: decode: %w", err)
	}
	return true, nil
}

// ---- LRC ------------------------------------------------------------------

// lrcStamp matches one [mm:ss.xx] (or [mm:ss]) time tag; a line may carry
// several when the same words repeat.
var lrcStamp = regexp.MustCompile(`\[(\d+):(\d{1,2}(?:\.\d+)?)\]`)

// parseLRC turns LRC text into lines ordered by time. Metadata tags like
// [ar:…] have no digits before the colon and are skipped; text with no time
// tags at all yields nothing, which is how callers tell plain from synced.
func parseLRC(text string) []LyricLine {
	var out []LyricLine
	for _, raw := range strings.Split(text, "\n") {
		stamps := lrcStamp.FindAllStringSubmatchIndex(raw, -1)
		if len(stamps) == 0 {
			continue
		}
		line := strings.TrimSpace(raw[stamps[len(stamps)-1][1]:])
		for _, m := range stamps {
			min, _ := strconv.Atoi(raw[m[2]:m[3]])
			sec, _ := strconv.ParseFloat(raw[m[4]:m[5]], 64)
			out = append(out, LyricLine{T: float64(min)*60 + sec, Line: line})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

func plainFromLines(lines []LyricLine) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, l.Line)
	}
	return strings.Join(parts, "\n")
}

// ---- handlers -------------------------------------------------------------

func (s *Server) lyricsResponse(row *lyricsRow, editable bool) Lyrics {
	out := Lyrics{Plain: row.plain, Source: row.source, Instrumental: row.instrumental, Editable: editable}
	if row.synced != "" {
		if lines := parseLRC(row.synced); len(lines) > 0 {
			out.Synced = lines
		}
	}
	return out
}

func (s *Server) canEditLyrics(u *User, t *Track) bool {
	return u.IsAdmin() || t.UploadedBy.ID == u.ID
}

// handleTrackLyrics: GET /api/tracks/{id}/lyrics. 404 (with a body) when no
// source has them, so clients can show "no lyrics" without a second request.
func (s *Server) handleTrackLyrics(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, filePath, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	row, err := s.resolveLyrics(ctx, t, filepath.Join(s.dataDir, filePath))
	if err != nil {
		s.internalErr(w, "lyrics", err)
		return
	}
	out := s.lyricsResponse(row, s.canEditLyrics(u, t))
	if row.source == lyricsSourceNone {
		writeJSON(w, http.StatusNotFound, out)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSetLyrics: PUT /api/tracks/{id}/lyrics with {"text": "..."}. LRC
// time tags in the text make it synced; plain text is stored as is. An empty
// text removes what's stored so the next request looks the lyrics up again.
func (s *Server) handleSetLyrics(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	t, _, _, ok := s.getTrack(w, r, u.ID, r.PathValue("id"))
	if !ok {
		return
	}
	if !s.canEditLyrics(u, t) {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit its lyrics")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	text := strings.TrimSpace(strings.ReplaceAll(body.Text, "\r\n", "\n"))
	if text == "" {
		if _, err := s.db.Exec(`DELETE FROM lyrics WHERE track_id = ?`, t.ID); err != nil {
			s.internalErr(w, "clear lyrics", err)
			return
		}
		writeNoContent(w)
		return
	}
	if len(text) > 64<<10 {
		writeErr(w, http.StatusBadRequest, "lyrics are too long")
		return
	}
	row := lyricsRow{source: lyricsSourceManual}
	if lines := parseLRC(text); len(lines) > 0 {
		row.synced = text
		row.plain = plainFromLines(lines)
	} else {
		row.plain = text
	}
	if err := s.writeLyricsRow(t.ID, row); err != nil {
		s.internalErr(w, "save lyrics", err)
		return
	}
	writeJSON(w, http.StatusOK, s.lyricsResponse(&row, true))
}
