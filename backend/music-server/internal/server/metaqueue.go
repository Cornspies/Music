package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"
)

const (
	jobQueued  = "queued"
	jobRunning = "running"
	jobDone    = "done"

	reasonUpload = "upload"
	reasonManual = "manual"
	reasonView   = "view"

	queuedLookupTimeout = 40 * time.Second
)

func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{time.Minute, 10 * time.Minute, time.Hour, 12 * time.Hour}
	if attempt-1 < len(delays) {
		return delays[attempt-1]
	}
	return 24 * time.Hour
}

type metadataJob struct {
	TrackID      string
	State        string
	Reason       string
	RequestedBy  string
	Attempts     int
	NotBefore    time.Time
	CreatedAt    time.Time
	FinishedAt   time.Time
	AppliedID    string
	Candidates   []scoredCandidate
	Partial      bool
	LastError    string
	CoverApplied bool
}

type persistedCandidate struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Genre    string  `json:"genre"`
	Year     int     `json:"year"`
	Duration float64 `json:"duration"`
	Artwork  string  `json:"artwork,omitempty"`
	CAA      string  `json:"caa,omitempty"`
	Score    float64 `json:"score"`
}

func marshalCandidates(cands []scoredCandidate) string {
	out := make([]persistedCandidate, 0, len(cands))
	for _, c := range cands {
		out = append(out, persistedCandidate{
			ID: c.id, Source: c.source, Title: c.title, Artist: c.artist, Album: c.album, Genre: c.genre,
			Year: c.year, Duration: c.durationSec, Artwork: c.artworkURL, CAA: c.coverArtArchiveReleaseID, Score: c.score,
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func unmarshalCandidates(raw string) []scoredCandidate {
	var in []persistedCandidate
	if json.Unmarshal([]byte(raw), &in) != nil {
		return nil
	}
	out := make([]scoredCandidate, 0, len(in))
	for _, p := range in {
		out = append(out, scoredCandidate{
			metadataCandidate: metadataCandidate{
				title: p.Title, artist: p.Artist, album: p.Album, genre: p.Genre, year: p.Year,
				durationSec: p.Duration, artworkURL: p.Artwork, coverArtArchiveReleaseID: p.CAA, source: p.Source,
			},
			id: p.ID, score: p.Score,
		})
	}
	return out
}

const settingMetadataPaused = "metadata_paused"

func (s *Server) metadataPaused() bool {
	v, _ := s.getSetting(settingMetadataPaused)
	return v == "1"
}

var metaQueue struct {
	wake chan struct{}
	once sync.Once
}

func (s *Server) wakeMetadataWorker() {
	metaQueue.once.Do(func() { metaQueue.wake = make(chan struct{}, 1) })
	select {
	case metaQueue.wake <- struct{}{}:
	default:
	}
}

func (s *Server) enqueueMetadataLookup(trackID, reason, requestedBy string) error {
	if metadataLookupDisabled() {
		return nil
	}
	if reason != reasonManual {
		var state string
		err := s.db.QueryRow(`SELECT state FROM metadata_jobs WHERE track_id = ?`, trackID).Scan(&state)
		if err == nil && state != jobDone {
			return nil
		}
	}
	_, err := s.db.Exec(`
		INSERT INTO metadata_jobs (track_id, state, reason, requested_by, attempts, not_before, created_at, finished_at, applied_id, candidates_json, partial, last_error)
		VALUES (?, ?, ?, ?, 0, '', ?, '', '', '[]', 0, '')
		ON CONFLICT(track_id) DO UPDATE SET state = excluded.state, reason = excluded.reason, requested_by = excluded.requested_by,
			attempts = 0, not_before = '', created_at = excluded.created_at, finished_at = '', applied_id = '',
			candidates_json = '[]', partial = 0, last_error = '', cover_applied = 0`,
		trackID, jobQueued, reason, requestedBy, nowRFC3339())
	if err != nil {
		return err
	}
	s.wakeMetadataWorker()
	return nil
}

func (s *Server) loadMetadataJob(trackID string) (*metadataJob, error) {
	var j metadataJob
	var notBefore, created, finished, cands string
	var partial, coverApplied int
	err := s.db.QueryRow(`SELECT track_id, state, reason, requested_by, attempts, not_before, created_at, finished_at, applied_id, candidates_json, partial, last_error, cover_applied
		FROM metadata_jobs WHERE track_id = ?`, trackID).
		Scan(&j.TrackID, &j.State, &j.Reason, &j.RequestedBy, &j.Attempts, &notBefore, &created, &finished, &j.AppliedID, &cands, &partial, &j.LastError, &coverApplied)
	if err != nil {
		return nil, err
	}
	j.NotBefore, _ = time.Parse(time.RFC3339, notBefore)
	j.CreatedAt, _ = time.Parse(time.RFC3339, created)
	j.FinishedAt, _ = time.Parse(time.RFC3339, finished)
	j.Candidates = unmarshalCandidates(cands)
	j.Partial = partial == 1
	j.CoverApplied = coverApplied == 1
	return &j, nil
}

func (s *Server) queuePosition(trackID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM metadata_jobs WHERE state IN (?, ?) AND (not_before = '' OR not_before <= ?)
		AND created_at <= (SELECT created_at FROM metadata_jobs WHERE track_id = ?)`,
		jobQueued, jobRunning, nowRFC3339(), trackID).Scan(&n)
	return max(n, 1)
}

func (s *Server) startMetadataWorker() {
	if metadataLookupDisabled() {
		return
	}
	_, _ = s.db.Exec(`UPDATE metadata_jobs SET state = ? WHERE state = ?`, jobQueued, jobRunning)
	s.wakeMetadataWorker()
	go s.metadataWorker()
}

func (s *Server) metadataWorker() {
	for {
		if s.metadataPaused() {
			select {
			case <-metaQueue.wake:
			case <-time.After(time.Minute):
			}
			continue
		}
		id, wait, ok := s.nextMetadataJob()
		if !ok {
			if wait <= 0 || wait > 30*time.Second {
				wait = 30 * time.Second
			}
			select {
			case <-metaQueue.wake:
			case <-time.After(wait):
			}
			continue
		}
		s.runMetadataJobSafely(id)
	}
}

func (s *Server) runMetadataJobSafely(id string) {
	defer func() {
		if v := recover(); v != nil {
			log.Printf("PANIC in metadata lookup for track %s (recovered): %v\n%s", id, v, debug.Stack())
			_, _ = s.db.Exec(`UPDATE metadata_jobs SET state = ?, not_before = '', finished_at = ?, last_error = ? WHERE track_id = ?`,
				jobDone, nowRFC3339(), "the lookup failed", id)
		}
	}()
	s.runMetadataJob(id)
}

func (s *Server) nextMetadataJob() (id string, wait time.Duration, ok bool) {
	now := time.Now().UTC()
	err := s.db.QueryRow(`SELECT track_id FROM metadata_jobs WHERE state = ? AND (not_before = '' OR not_before <= ?) ORDER BY created_at ASC LIMIT 1`,
		jobQueued, now.Format(time.RFC3339)).Scan(&id)
	if err == nil {
		return id, 0, true
	}
	var nb string
	if err := s.db.QueryRow(`SELECT MIN(not_before) FROM metadata_jobs WHERE state = ? AND not_before <> ''`, jobQueued).Scan(&nb); err == nil {
		if t, err := time.Parse(time.RFC3339, nb); err == nil {
			return "", time.Until(t) + time.Second, false
		}
	}
	return "", 0, false
}

func (s *Server) runMetadataJob(id string) {
	job, err := s.loadMetadataJob(id)
	if err != nil {
		return
	}
	if _, err := s.db.Exec(`UPDATE metadata_jobs SET state = ? WHERE track_id = ?`, jobRunning, id); err != nil {
		return
	}

	var t Track
	var filePath, fileName string
	row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(""), id)...)
	tp, fp, fn, err := scanTrack(row.Scan)
	if err != nil {
		return
	}
	t, filePath, fileName = *tp, fp, fn

	meta := fileMetadata{
		title:    s.knownTitle(t.Title, filePath, fileName),
		artist:   strings.TrimSpace(dropPlaceholder(t.Artist, "Unknown Artist")),
		album:    strings.TrimSpace(dropPlaceholder(t.Album, "Unknown Album")),
		genre:    t.Genre,
		year:     t.Year,
		duration: t.Duration,
	}

	ctx, cancel := context.WithTimeout(context.Background(), queuedLookupTimeout)
	cands, outcome := lookupCandidates(ctx, meta, fileName)
	cancel()

	attempt := job.Attempts + 1

	appliedID := job.AppliedID
	_, wantArtist, wantTitle := buildSearchPlan(meta, fileName)
	identified := wantArtist != "" && wantTitle != ""
	coverApplied := job.CoverApplied
	if job.Reason == reasonUpload && identified && len(cands) > 0 && cands[0].score >= acceptScoreThreshold {
		replaceCover := t.Source == "youtube" && !coverApplied && cands[0].score >= coverMatchThreshold
		applyCtx, applyCancel := context.WithTimeout(context.Background(), 20*time.Second)
		applied, replaced := s.autoApplyCandidate(applyCtx, &t, meta, cands[0], replaceCover)
		applyCancel()
		if applied && appliedID == "" {
			appliedID = cands[0].id
		}
		coverApplied = coverApplied || replaced
	} else if len(cands) > 0 && job.Reason == reasonUpload && !outcome.degraded() {
		why := fmt.Sprintf("no confident match (best %.2f from %s)", cands[0].score, cands[0].source)
		if !identified {
			why = "artist or title unknown"
		}
		log.Printf("metadata queue: %s for %q — leaving tags as they are, %d suggestion(s) in the review",
			why, fileName, len(cands))
	}

	if outcome.degraded() {
		delay := retryDelay(attempt)
		log.Printf("metadata queue: %q incomplete (%d provider(s) missing), retry %d in %s",
			fileName, outcome.failed, attempt, delay)
		_, _ = s.db.Exec(`UPDATE metadata_jobs SET state = ?, attempts = ?, not_before = ?, applied_id = ?, candidates_json = ?, partial = 1, last_error = ?, cover_applied = ? WHERE track_id = ?`,
			jobQueued, attempt, time.Now().UTC().Add(delay).Format(time.RFC3339), appliedID, marshalCandidates(cands),
			fmt.Sprintf("%d provider(s) didn't answer; retrying", outcome.failed), boolToInt(coverApplied), id)
		return
	}

	_, _ = s.db.Exec(`UPDATE metadata_jobs SET state = ?, attempts = ?, not_before = '', finished_at = ?, applied_id = ?, candidates_json = ?, partial = 0, last_error = '', cover_applied = ? WHERE track_id = ?`,
		jobDone, attempt, nowRFC3339(), appliedID, marshalCandidates(cands), boolToInt(coverApplied), id)
}

func (s *Server) autoApplyCandidate(ctx context.Context, t *Track, meta fileMetadata, best scoredCandidate, replaceCover bool) (applied, coverReplaced bool) {
	if t.HasCover && !replaceCover {
		meta.picture = []byte{1}
	} else {
		meta.picture = nil
	}
	filled := applyCandidate(ctx, &meta, best.metadataCandidate, false)
	if len(filled) == 0 {
		return false, false
	}
	hasCover := t.HasCover
	if (!t.HasCover || replaceCover) && len(meta.picture) > 1 {
		if safeCoverBytes(meta.picture) {
			if err := os.WriteFile(filepath.Join(s.covDir, t.ID+".img"), meta.picture, 0o644); err == nil {
				hasCover = true
				coverReplaced = replaceCover
			}
		}
	}
	title := trimDefault(meta.title, t.Title)
	artist := trimDefault(meta.artist, "Unknown Artist")
	album := trimDefault(meta.album, "Unknown Album")
	if _, err := s.db.Exec(`UPDATE tracks SET title = ?, artist = ?, album = ?, genre = ?, year = ?, has_cover = ?, updated_at = ? WHERE id = ?`,
		title, artist, album, meta.genre, meta.year, boolToInt(hasCover), nowRFC3339(), t.ID); err != nil {
		logErr("metadata queue apply", err)
		return false, false
	}
	log.Printf("metadata queue: filled %s for %q from %s match %q by %q (confidence %.2f)",
		strings.Join(filled, ", "), t.Title, best.source, best.title, best.artist, best.score)
	s.forgetLookedUpLyrics(t.ID)
	if coverReplaced || (hasCover && !t.HasCover) {
		go s.updateCoverColor(t.ID)
	}
	return true, coverReplaced
}

type MetadataQueueStatus struct {
	Queued  int `json:"queued"`
	Running *struct {
		TrackID string `json:"trackId"`
		Title   string `json:"title"`
	} `json:"running"`
	NextRetryAt string `json:"nextRetryAt,omitempty"`
	Paused      bool   `json:"paused"`
	Disabled    bool   `json:"disabled"`
}

func (s *Server) handleMetadataQueue(w http.ResponseWriter, _ *http.Request) {
	st := MetadataQueueStatus{Disabled: metadataLookupDisabled(), Paused: s.metadataPaused()}
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM metadata_jobs WHERE state = ?`, jobQueued).Scan(&st.Queued)
	var id, title string
	if err := s.db.QueryRow(`SELECT j.track_id, COALESCE(t.title, '') FROM metadata_jobs j LEFT JOIN tracks t ON t.id = j.track_id WHERE j.state = ? LIMIT 1`, jobRunning).Scan(&id, &title); err == nil {
		st.Running = &struct {
			TrackID string `json:"trackId"`
			Title   string `json:"title"`
		}{id, title}
	}
	var runnable int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM metadata_jobs WHERE state = ? AND (not_before = '' OR not_before <= ?)`, jobQueued, nowRFC3339()).Scan(&runnable)
	if st.Queued > 0 && runnable == 0 && st.Running == nil && !st.Paused {
		var nb string
		if err := s.db.QueryRow(`SELECT MIN(not_before) FROM metadata_jobs WHERE state = ? AND not_before <> ''`, jobQueued).Scan(&nb); err == nil {
			st.NextRetryAt = nb
		}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleTrackMetadataLookup(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	t, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
		return
	}
	if metadataLookupDisabled() {
		writeErr(w, http.StatusConflict, "online metadata lookup is disabled on this server")
		return
	}
	if err := s.enqueueMetadataLookup(id, reasonManual, u.ID); err != nil {
		s.internalErr(w, "queue lookup", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "position": s.queuePosition(id), "paused": s.metadataPaused()})
}

var errNoJob = errors.New("no metadata job")

func (s *Server) jobCandidates(trackID string) ([]scoredCandidate, error) {
	j, err := s.loadMetadataJob(trackID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoJob
	}
	if err != nil {
		return nil, err
	}
	return j.Candidates, nil
}

var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func youtubeVideoID(raw string) string {
	raw = strings.TrimSpace(raw)
	if youtubeIDRe.MatchString(raw) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	host = strings.TrimPrefix(host, "m.")
	host = strings.TrimPrefix(host, "music.")
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "youtube-nocookie.com":
		if v := u.Query().Get("v"); v != "" {
			id = v
		} else {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 {
				switch parts[0] {
				case "shorts", "embed", "live", "v":
					id = parts[1]
				}
			}
		}
	}
	if !youtubeIDRe.MatchString(id) {
		return ""
	}
	return id
}

type youtubeVideoDetails struct {
	Items []struct {
		ID      string `json:"id"`
		Snippet struct {
			Title        string `json:"title"`
			ChannelTitle string `json:"channelTitle"`
			PublishedAt  string `json:"publishedAt"`
			Thumbnails   struct {
				Maxres struct{ URL string } `json:"maxres"`
				High   struct{ URL string } `json:"high"`
				Medium struct{ URL string } `json:"medium"`
			} `json:"thumbnails"`
		} `json:"snippet"`
		ContentDetails struct {
			Duration string `json:"duration"`
		} `json:"contentDetails"`
	} `json:"items"`
}

func youtubeVideoCandidate(ctx context.Context, videoID string) (*metadataCandidate, error) {
	apiKey := config.YouTubeAPIKey()
	if apiKey == "" {
		return nil, errors.New("YouTube isn't configured on this server (MUSIC_YOUTUBE_API_KEY)")
	}
	q := url.Values{"part": {"snippet,contentDetails"}, "id": {videoID}, "key": {apiKey}}
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
	var parsed youtubeVideoDetails
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Items) == 0 {
		return nil, errors.New("that video doesn't exist or isn't public")
	}
	it := parsed.Items[0]
	artist, title := parseYouTubeTitle(it.Snippet.Title, it.Snippet.ChannelTitle)
	if title == "" {
		title = it.Snippet.Title
	}
	thumb := it.Snippet.Thumbnails.Maxres.URL
	if thumb == "" {
		thumb = it.Snippet.Thumbnails.High.URL
	}
	if thumb == "" {
		thumb = it.Snippet.Thumbnails.Medium.URL
	}
	return &metadataCandidate{
		title: title, artist: artist,
		year:        parseReleaseYear(it.Snippet.PublishedAt),
		durationSec: parseISO8601Duration(it.ContentDetails.Duration),
		artworkURL:  thumb,
		source:      "YouTube",
	}, nil
}

func (s *Server) handleTrackMetadataFromYouTube(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")
	t, filePath, fileName, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	videoID := youtubeVideoID(body.URL)
	if videoID == "" {
		writeErr(w, http.StatusBadRequest, "that doesn't look like a YouTube link")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := youtubeVideoCandidate(ctx, videoID)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}

	meta := fileMetadata{
		title:    s.knownTitle(t.Title, filePath, fileName),
		artist:   strings.TrimSpace(dropPlaceholder(t.Artist, "Unknown Artist")),
		duration: t.Duration,
	}
	query, wantArtist, wantTitle := buildSearchPlan(meta, fileName)
	sc := scoredCandidate{metadataCandidate: *c, id: candidateID(*c), score: scoreCandidate(*c, wantArtist, wantTitle, query, t.Duration)}

	job, err := s.loadMetadataJob(id)
	var cands []scoredCandidate
	if err == nil {
		for _, x := range job.Candidates {
			if x.id != sc.id {
				cands = append(cands, x)
			}
		}
	}
	cands = append([]scoredCandidate{sc}, cands...)
	if _, err := s.db.Exec(`
		INSERT INTO metadata_jobs (track_id, state, reason, requested_by, attempts, not_before, created_at, finished_at, applied_id, candidates_json, partial, last_error)
		VALUES (?, ?, ?, ?, 1, '', ?, ?, '', ?, 0, '')
		ON CONFLICT(track_id) DO UPDATE SET candidates_json = excluded.candidates_json`,
		id, jobDone, reasonManual, u.ID, nowRFC3339(), nowRFC3339(), marshalCandidates(cands)); err != nil {
		s.internalErr(w, "store candidate", err)
		return
	}
	writeJSON(w, http.StatusOK, toJSONCandidates([]scoredCandidate{sc}, "")[0])
}
