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

const (
	lyricsSourceEmbedded = "embedded"
	lyricsSourceLRCLIB   = "lrclib"
	lyricsSourceManual   = "manual"
	lyricsSourceNone     = "none"

	lyricsMissTTL = 7 * 24 * time.Hour

	lrclibUserAgent = "Music-SelfHosted-MusicPlayer/1.0 (https://github.com/cornspies/music-player)"
)

var lrclibBase = "https://lrclib.net/api"

type LyricLine struct {
	T    float64 `json:"t"`
	Line string  `json:"line"`
}

type Lyrics struct {
	Plain        string      `json:"plain"`
	Synced       []LyricLine `json:"synced"`
	Source       string      `json:"source"`
	Instrumental bool        `json:"instrumental"`
	Editable     bool        `json:"editable"`
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

func (s *Server) forgetLookedUpLyrics(trackID string) {
	_, _ = s.db.Exec(`DELETE FROM lyrics WHERE track_id = ? AND source IN (?, ?)`,
		trackID, lyricsSourceLRCLIB, lyricsSourceNone)
}

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

func lrclibLookup(ctx context.Context, t *Track) (*lyricsRow, error) {
	if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.Artist) == "" {
		return nil, nil
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

var lrcStamp = regexp.MustCompile(`\[(\d+):(\d{1,2}(?:\.\d+)?)\]`)

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
