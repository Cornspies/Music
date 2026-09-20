package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Metadata lookup queue.
//
// Online lookups used to run inside the upload request. That made every
// upload wait on four providers, and a bulk upload of a few hundred files
// ran straight into MusicBrainz's rate limit and YouTube's daily quota —
// after which the tracks silently got whatever the degraded lookup found.
//
// Now an upload stores the file with its own tags and puts the track on this
// queue. A single worker takes jobs one at a time (which is also what keeps
// the providers' rate limits happy), and when a lookup comes back
// incomplete because a provider was throttled or down, the job goes back on
// the queue with a delay instead of being written off. What the lookup has
// found so far is kept, so the review dialog can already show it. The queue
// lives in SQLite, so a restart mid-upload loses nothing.
//
// Jobs are also how a track gets re-checked later: an uploader or admin can
// queue a fresh lookup for anything in the library.

const (
	jobQueued  = "queued"
	jobRunning = "running"
	jobDone    = "done"

	// reasonUpload lookups auto-apply a confident match into the still-empty
	// tags. The other reasons only gather suggestions for someone to pick.
	reasonUpload = "upload"
	reasonManual = "manual"
	reasonView   = "view"

	// A queued lookup may take longer than the upload-time budget did: the
	// worker isn't holding anyone's request open.
	queuedLookupTimeout = 40 * time.Second
	// After this many incomplete attempts the job is closed with what it has;
	// a manual re-lookup can always try again.
	maxLookupAttempts = 8
)

// retryDelay grows with each incomplete attempt. MusicBrainz's cooldown is a
// minute, so the first retries are short; the later ones space out so a
// day-long YouTube quota exhaustion doesn't burn the whole schedule at once.
func retryDelay(attempt int) time.Duration {
	delays := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	if attempt-1 < len(delays) {
		return delays[attempt-1]
	}
	return 15 * time.Minute
}

// metadataJob is one row of metadata_jobs.
type metadataJob struct {
	TrackID     string
	State       string
	Reason      string
	RequestedBy string
	Attempts    int
	NotBefore   time.Time
	CreatedAt   time.Time
	FinishedAt  time.Time
	AppliedID   string
	Candidates  []scoredCandidate
	// Partial is true when a provider was missing from the last attempt.
	Partial   bool
	LastError string
}

// persistedCandidate is the JSON form of a scoredCandidate in the job row.
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

// metaQueue is the worker's wake-up signal; enqueueing pokes it.
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

// enqueueMetadataLookup puts (or puts back) a track on the queue. A job that
// is already queued or running is left alone unless the reason is manual,
// which restarts it from scratch so "look up again" always means a fresh
// search.
func (s *Server) enqueueMetadataLookup(trackID, reason, requestedBy string) error {
	if metadataLookupDisabled() {
		return nil
	}
	if reason != reasonManual {
		var state string
		err := s.db.QueryRow(`SELECT state FROM metadata_jobs WHERE track_id = ?`, trackID).Scan(&state)
		if err == nil && state != jobDone {
			return nil // already on its way
		}
	}
	_, err := s.db.Exec(`
		INSERT INTO metadata_jobs (track_id, state, reason, requested_by, attempts, not_before, created_at, finished_at, applied_id, candidates_json, partial, last_error)
		VALUES (?, ?, ?, ?, 0, '', ?, '', '', '[]', 0, '')
		ON CONFLICT(track_id) DO UPDATE SET state = excluded.state, reason = excluded.reason, requested_by = excluded.requested_by,
			attempts = 0, not_before = '', created_at = excluded.created_at, finished_at = '', applied_id = '',
			candidates_json = '[]', partial = 0, last_error = ''`,
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
	var partial int
	err := s.db.QueryRow(`SELECT track_id, state, reason, requested_by, attempts, not_before, created_at, finished_at, applied_id, candidates_json, partial, last_error
		FROM metadata_jobs WHERE track_id = ?`, trackID).
		Scan(&j.TrackID, &j.State, &j.Reason, &j.RequestedBy, &j.Attempts, &notBefore, &created, &finished, &j.AppliedID, &cands, &partial, &j.LastError)
	if err != nil {
		return nil, err
	}
	j.NotBefore, _ = time.Parse(time.RFC3339, notBefore)
	j.CreatedAt, _ = time.Parse(time.RFC3339, created)
	j.FinishedAt, _ = time.Parse(time.RFC3339, finished)
	j.Candidates = unmarshalCandidates(cands)
	j.Partial = partial == 1
	return &j, nil
}

// queuePosition is how many jobs are ahead of this one (1 = next up).
func (s *Server) queuePosition(trackID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM metadata_jobs WHERE state IN (?, ?) AND created_at <= (SELECT created_at FROM metadata_jobs WHERE track_id = ?)`,
		jobQueued, jobRunning, trackID).Scan(&n)
	return max(n, 1)
}

// startMetadataWorker recovers jobs interrupted by a restart and runs the
// worker loop.
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
		id, wait, ok := s.nextMetadataJob()
		if !ok {
			// Nothing runnable: sleep until poked, or until the earliest
			// delayed job is due.
			if wait <= 0 || wait > 30*time.Second {
				wait = 30 * time.Second
			}
			select {
			case <-metaQueue.wake:
			case <-time.After(wait):
			}
			continue
		}
		s.runMetadataJob(id)
	}
}

// nextMetadataJob returns the oldest runnable job, or how long until one
// becomes runnable.
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
	var fileName string
	row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(""), id)...)
	tp, _, fn, err := scanTrack(row.Scan)
	if err != nil {
		// The track was deleted while queued; the row cascades away.
		return
	}
	t, fileName = *tp, fn

	// Placeholder values written at upload ("Unknown Artist", a title that's
	// just the filename) count as absent: they'd poison the query and the
	// scoring, and they're exactly what a confident match is allowed to fill.
	meta := fileMetadata{
		title:    trackSearchTitle(t.Title, fileName),
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
	if outcome.degraded() && attempt < maxLookupAttempts {
		delay := retryDelay(attempt)
		log.Printf("metadata queue: %q incomplete (%d provider(s) missing), retry %d in %s",
			fileName, outcome.failed, attempt, delay)
		_, _ = s.db.Exec(`UPDATE metadata_jobs SET state = ?, attempts = ?, not_before = ?, candidates_json = ?, partial = 1, last_error = ? WHERE track_id = ?`,
			jobQueued, attempt, time.Now().UTC().Add(delay).Format(time.RFC3339), marshalCandidates(cands),
			fmt.Sprintf("%d provider(s) didn't answer; retrying", outcome.failed), id)
		return
	}

	appliedID := ""
	if job.Reason == reasonUpload && len(cands) > 0 && cands[0].score >= acceptScoreThreshold {
		applyCtx, applyCancel := context.WithTimeout(context.Background(), 20*time.Second)
		if s.autoApplyCandidate(applyCtx, &t, meta, cands[0]) {
			appliedID = cands[0].id
		}
		applyCancel()
	} else if len(cands) > 0 && job.Reason == reasonUpload {
		log.Printf("metadata queue: no confident match for %q (best %.2f from %s) — leaving tags as-is, %d alternative(s) available",
			fileName, cands[0].score, cands[0].source, len(cands))
	}

	partial := 0
	if outcome.degraded() {
		partial = 1
	}
	_, _ = s.db.Exec(`UPDATE metadata_jobs SET state = ?, attempts = ?, not_before = '', finished_at = ?, applied_id = ?, candidates_json = ?, partial = ?, last_error = '' WHERE track_id = ?`,
		jobDone, attempt, nowRFC3339(), appliedID, marshalCandidates(cands), partial, id)
}

// autoApplyCandidate fills what the file's own tags left empty from the best
// match — never overwriting a real tag — and stores the cover if there was
// none. meta holds the track's current values with placeholders dropped.
func (s *Server) autoApplyCandidate(ctx context.Context, t *Track, meta fileMetadata, best scoredCandidate) bool {
	if t.HasCover {
		meta.picture = []byte{1} // non-empty: keep the cover the file had
	} else {
		meta.picture = nil
	}
	filled := applyCandidate(ctx, &meta, best.metadataCandidate, false)
	if len(filled) == 0 {
		return false
	}
	hasCover := t.HasCover
	if !t.HasCover && len(meta.picture) > 1 {
		if safeCoverBytes(meta.picture) {
			if err := os.WriteFile(filepath.Join(s.covDir, t.ID+".img"), meta.picture, 0o644); err == nil {
				hasCover = true
			}
		}
	}
	title := trimDefault(meta.title, t.Title)
	artist := trimDefault(meta.artist, "Unknown Artist")
	album := trimDefault(meta.album, "Unknown Album")
	if _, err := s.db.Exec(`UPDATE tracks SET title = ?, artist = ?, album = ?, genre = ?, year = ?, has_cover = ?, updated_at = ? WHERE id = ?`,
		title, artist, album, meta.genre, meta.year, boolToInt(hasCover), nowRFC3339(), t.ID); err != nil {
		logErr("metadata queue apply", err)
		return false
	}
	log.Printf("metadata queue: filled %s for %q from %s match %q by %q (confidence %.2f)",
		strings.Join(filled, ", "), t.Title, best.source, best.title, best.artist, best.score)
	s.forgetLookedUpLyrics(t.ID)
	if hasCover && !t.HasCover {
		go s.updateCoverColor(t.ID)
	}
	return true
}

// ---- HTTP ------------------------------------------------------------------

// MetadataQueueStatus is GET /api/metadata/queue.
type MetadataQueueStatus struct {
	// Queued counts jobs waiting, including ones delayed by a retry.
	Queued int `json:"queued"`
	// Running is the track being looked up right now, if any.
	Running *struct {
		TrackID string `json:"trackId"`
		Title   string `json:"title"`
	} `json:"running"`
	// NextRetryAt is when the earliest delayed job will be tried again, when
	// everything left is waiting on a provider.
	NextRetryAt string `json:"nextRetryAt,omitempty"`
	// Disabled mirrors MUSIC_DISABLE_METADATA_LOOKUP.
	Disabled bool `json:"disabled"`
}

func (s *Server) handleMetadataQueue(w http.ResponseWriter, _ *http.Request) {
	st := MetadataQueueStatus{Disabled: metadataLookupDisabled()}
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
	if st.Queued > 0 && runnable == 0 && st.Running == nil {
		var nb string
		if err := s.db.QueryRow(`SELECT MIN(not_before) FROM metadata_jobs WHERE state = ? AND not_before <> ''`, jobQueued).Scan(&nb); err == nil {
			st.NextRetryAt = nb
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// handleTrackMetadataLookup: POST /api/tracks/{id}/metadata/lookup — queue a
// fresh search for a track that's already in the library. Uploader or admin.
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
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "position": s.queuePosition(id)})
}

var errNoJob = errors.New("no metadata job")

// jobCandidates returns the stored suggestions for a track, for the handlers
// that need to resolve a candidate id (artwork, apply).
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
