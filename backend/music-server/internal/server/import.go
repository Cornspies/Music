package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Playlist import: paste a playlist link from a catalogue that has them
// (YouTube, through yt-dlp) and every track comes into the library, into a
// playlist of the same name. Fifty tracks take a few minutes, so an import
// is a background job the clients poll: it lists the playlist, then goes
// through the entries one by one — a track already in the library is only
// linked into the playlist, anything else is fetched and ingested like a
// single "Add". One entry failing doesn't stop the rest. Jobs live in
// memory: a restart ends a running import, and running it again picks up
// where it left off, since existing tracks are skipped and the playlist is
// reused by name.

// playlistSource is a catalogue whose links can point at whole playlists.
type playlistSource interface {
	source
	// playlistID extracts the playlist id from a link; "" when the link
	// isn't a playlist on this catalogue.
	playlistID(raw string) string
	// listPlaylist returns the playlist's title and its entries, in order.
	listPlaylist(ctx context.Context, id string) (catalogPlaylist, error)
}

// catalogPlaylist is a listed playlist: its playable entries, and how many
// it has in all when the listing was cut short (Total > len(Entries)).
type catalogPlaylist struct {
	Title   string
	Total   int
	Entries []discoverResult
}

// PlaylistImport is an import job as the clients see it.
type PlaylistImport struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	// ListID is the catalogue's id for the playlist being imported.
	ListID string `json:"listId"`
	// Title is the playlist's name, known once it has been listed.
	Title string `json:"title"`
	// State: queued, listing, running, done, cancelled or failed.
	State string `json:"state"`
	// Total entries; Done of them handled so far, as Added (fetched),
	// Existing (already in the library) or Failed.
	Total    int `json:"total"`
	Done     int `json:"done"`
	Added    int `json:"added"`
	Existing int `json:"existing"`
	Failed   int `json:"failed"`
	// Current is the title being fetched right now.
	Current string `json:"current,omitempty"`
	// PlaylistID is the library playlist the tracks go into.
	PlaylistID string `json:"playlistId,omitempty"`
	// Error is why the whole import stopped, when it did.
	Error    string          `json:"error,omitempty"`
	Failures []ImportFailure `json:"failures"`
	// ExistingTracks are the entries that were in the library already (the
	// same video, added before) and were only linked into the playlist.
	ExistingTracks []ImportExisting `json:"existingTracks"`
	// PossibleDuplicates are entries that were added although a track in
	// the library looks like the same song — for someone to go through.
	PossibleDuplicates []ImportDuplicate `json:"possibleDuplicates"`
	StartedAt          string            `json:"startedAt"`
	FinishedAt         string            `json:"finishedAt,omitempty"`
}

// ImportDuplicate pairs an added track with the one it may duplicate.
type ImportDuplicate struct {
	Title          string `json:"title"`
	TrackID        string `json:"trackId"`
	ExistingID     string `json:"existingId"`
	ExistingTitle  string `json:"existingTitle"`
	ExistingArtist string `json:"existingArtist"`
}

// ImportFailure is one entry that couldn't be added.
type ImportFailure struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// ImportExisting is one entry whose video was in the library already.
type ImportExisting struct {
	Title   string `json:"title"`
	TrackID string `json:"trackId"`
	Reason  string `json:"reason"`
}

const (
	importQueued    = "queued"
	importListing   = "listing"
	importRunning   = "running"
	importDone      = "done"
	importCancelled = "cancelled"
	importFailed    = "failed"

	// Finished imports stay visible for a while, then go.
	importRetention = time.Hour
	importListTime  = 90 * time.Second
	importFetchTime = 3 * time.Minute
)

type importJob struct {
	mu sync.Mutex
	PlaylistImport
	user       *User
	src        playlistSource
	listID     string
	cancel     context.CancelFunc
	finishedAt time.Time
}

func (j *importJob) snapshot() PlaylistImport {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.PlaylistImport
	out.Failures = append([]ImportFailure{}, j.Failures...)
	out.ExistingTracks = append([]ImportExisting{}, j.ExistingTracks...)
	out.PossibleDuplicates = append([]ImportDuplicate{}, j.PossibleDuplicates...)
	return out
}

func (j *importJob) update(fn func(p *PlaylistImport)) {
	j.mu.Lock()
	fn(&j.PlaylistImport)
	j.mu.Unlock()
}

func (j *importJob) finish(state, errMsg string) {
	j.mu.Lock()
	j.State, j.Error, j.Current = state, errMsg, ""
	j.finishedAt = time.Now()
	j.FinishedAt = nowRFC3339()
	j.mu.Unlock()
}

func (j *importJob) active() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.State == importQueued || j.State == importListing || j.State == importRunning
}

var imports = struct {
	sync.Mutex
	byID  map[string]*importJob
	queue chan *importJob
}{byID: map[string]*importJob{}, queue: make(chan *importJob, 32)}

func (s *Server) startImportWorker() {
	go func() {
		for job := range imports.queue {
			func() {
				defer func() {
					if v := recover(); v != nil {
						log.Printf("PANIC in playlist import %s (recovered): %v\n%s", job.ID, v, debug.Stack())
						job.finish(importFailed, "the import stopped unexpectedly")
					}
				}()
				s.runImport(job)
			}()
		}
	}()
}

// userImports lists a user's jobs, newest first, dropping finished ones
// that have aged out.
func userImports(userID string) []PlaylistImport {
	imports.Lock()
	defer imports.Unlock()
	out := []PlaylistImport{}
	for id, j := range imports.byID {
		snap := j.snapshot()
		if !j.active() && time.Since(j.finishedAt) > importRetention {
			delete(imports.byID, id)
			continue
		}
		if j.user.ID == userID {
			out = append(out, snap)
		}
	}
	for i := 1; i < len(out); i++ { // insertion sort: a handful of jobs at most
		for k := i; k > 0 && out[k].StartedAt > out[k-1].StartedAt; k-- {
			out[k], out[k-1] = out[k-1], out[k]
		}
	}
	return out
}

// handleStartImport: POST /api/discover/import {"source":"youtube","url":"…"}.
// Answers 202 with the job; 200 with the running one when this playlist is
// already being imported for the caller.
func (s *Server) handleStartImport(w http.ResponseWriter, r *http.Request) {
	if discoverDisabled() {
		writeErr(w, http.StatusNotFound, "discover is disabled on this server")
		return
	}
	u := userFrom(r)
	var body struct {
		Source string `json:"source"`
		URL    string `json:"url"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	src, ok := findSource(body.Source).(playlistSource)
	if !ok {
		writeErr(w, http.StatusBadRequest, "this catalogue has no playlists to import")
		return
	}
	listID := src.playlistID(body.URL)
	if listID == "" {
		writeErr(w, http.StatusBadRequest, "that isn't a "+src.name()+" playlist link")
		return
	}

	imports.Lock()
	for _, j := range imports.byID {
		if j.user.ID == u.ID && j.listID == listID && j.src.key() == src.key() && j.active() {
			imports.Unlock()
			writeJSON(w, http.StatusOK, j.snapshot())
			return
		}
	}
	job := &importJob{
		PlaylistImport: PlaylistImport{
			ID: uuid.NewString(), Source: src.key(), ListID: listID, State: importQueued,
			Failures: []ImportFailure{}, ExistingTracks: []ImportExisting{}, PossibleDuplicates: []ImportDuplicate{},
			StartedAt: nowRFC3339(),
		},
		user: u, src: src, listID: listID,
	}
	select {
	case imports.queue <- job:
		imports.byID[job.ID] = job
	default:
		imports.Unlock()
		writeErr(w, http.StatusTooManyRequests, "too many imports are waiting; try again later")
		return
	}
	imports.Unlock()
	writeJSON(w, http.StatusAccepted, job.snapshot())
}

// handleListImports: GET /api/discover/imports — the caller's imports,
// running and recently finished.
func (s *Server) handleListImports(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userImports(userFrom(r).ID))
}

// handleCancelImport: DELETE /api/discover/imports/{id} — stops after the
// entry in progress; what came in stays.
func (s *Server) handleCancelImport(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	imports.Lock()
	j := imports.byID[r.PathValue("id")]
	imports.Unlock()
	if j == nil || j.user.ID != u.ID {
		writeErr(w, http.StatusNotFound, "no such import")
		return
	}
	j.mu.Lock()
	cancel := j.cancel
	if j.State == importQueued {
		j.State = importCancelled
		j.finishedAt = time.Now()
		j.FinishedAt = nowRFC3339()
	}
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	writeNoContent(w)
}

func (s *Server) runImport(job *importJob) {
	if !job.active() {
		return // cancelled while queued
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.update(func(p *PlaylistImport) { p.State = importListing })
	job.mu.Lock()
	job.cancel = cancel
	job.mu.Unlock()
	defer cancel()

	listCtx, listCancel := context.WithTimeout(ctx, importListTime)
	pl, err := job.src.listPlaylist(listCtx, job.listID)
	listCancel()
	if err != nil {
		if ctx.Err() != nil {
			job.finish(importCancelled, "")
			return
		}
		logErr("import list", err)
		job.finish(importFailed, friendlyImportError(err, "couldn't read the playlist"))
		return
	}
	entries := pl.Entries
	if len(entries) == 0 {
		job.finish(importFailed, "the playlist has no tracks that can be fetched")
		return
	}
	title := trimDefault(pl.Title, job.src.name()+" playlist")
	playlistID, err := s.playlistForImport(job.user.ID, title)
	if err != nil {
		logErr("import playlist", err)
		job.finish(importFailed, "couldn't create the playlist")
		return
	}
	job.update(func(p *PlaylistImport) {
		p.Title, p.Total, p.PlaylistID, p.State = title, len(entries), playlistID, importRunning
	})

	for _, e := range entries {
		if ctx.Err() != nil {
			job.finish(importCancelled, "")
			return
		}
		job.update(func(p *PlaylistImport) { p.Current = e.Title })
		trackID, added, why, lookalike, err := s.importEntry(ctx, job, e)
		if err != nil {
			if ctx.Err() != nil {
				job.finish(importCancelled, "")
				return
			}
			var status *statusError
			if errors.As(err, &status) && status.code == http.StatusInsufficientStorage {
				// Every further download would fail the same way.
				job.finish(importFailed, status.msg)
				return
			}
			logErr("import "+e.Title, err)
			job.update(func(p *PlaylistImport) {
				p.Done++
				p.Failed++
				p.Failures = append(p.Failures, ImportFailure{Title: e.Title, Reason: friendlyImportError(err, "couldn't fetch it")})
			})
			continue
		}
		if err := s.linkIntoPlaylist(playlistID, trackID); err != nil {
			logErr("import link", err)
		}
		job.update(func(p *PlaylistImport) {
			p.Done++
			if added {
				p.Added++
				if lookalike != nil {
					p.PossibleDuplicates = append(p.PossibleDuplicates, ImportDuplicate{
						Title: e.Title, TrackID: trackID,
						ExistingID: lookalike.ID, ExistingTitle: lookalike.Title, ExistingArtist: lookalike.Artist,
					})
				}
			} else {
				p.Existing++
				p.ExistingTracks = append(p.ExistingTracks, ImportExisting{Title: e.Title, TrackID: trackID, Reason: why})
			}
		})
	}
	job.finish(importDone, "")
	snap := job.snapshot()
	log.Printf("import: %q done — %d added (%d possible duplicates), %d already there, %d failed",
		snap.Title, snap.Added, len(snap.PossibleDuplicates), snap.Existing, snap.Failed)
}

// importEntry makes sure one entry is in the library: the existing track
// when the very same video was added before (why says so), or a fresh
// download. A download that looks like a song already in the library still
// goes ahead — a wrong guess would lose someone a song — but the lookalike
// is returned so the import can point it out.
func (s *Server) importEntry(ctx context.Context, job *importJob, e discoverResult) (id string, added bool, why string, lookalike *Track, err error) {
	var existing string
	if err := s.db.QueryRow(`SELECT id FROM tracks WHERE source = ? AND source_id = ?`, job.src.key(), e.ID).Scan(&existing); err == nil {
		return existing, false, "the same video was added before", nil, nil
	}
	lookalike = s.sameSongInLibrary(job.user.ID, e)
	fetchCtx, cancel := context.WithTimeout(ctx, importFetchTime)
	defer cancel()
	dl, err := job.src.fetch(fetchCtx, e.ID)
	if err != nil {
		return "", false, "", nil, err
	}
	defer dl.Body.Close()
	id, err = s.ingestDownload(fetchCtx, job.user, job.src.key(), e.ID, dl)
	if err != nil {
		return "", false, "", nil, err
	}
	return id, true, "", lookalike, nil
}

// sameSongInLibrary looks for a track that is the same song as a listing
// entry — an upload of it, or another video of it — the way the upload
// duplicate check does, but forgiving the noise of a video title. The
// artist is trusted only when the title itself names one ("Artist - Title");
// a channel name is a hint, never a veto.
func (s *Server) sameSongInLibrary(userID string, e discoverResult) *Track {
	artist, title := parseYouTubeTitle(e.Title, "")
	title = trimDefault(title, e.Title)
	veto := artist != ""
	artist = trimDefault(artist, e.Artist)
	matches := s.findSameSong(userID, title, artist, e.Duration, true, veto)
	if len(matches) == 0 {
		return nil
	}
	return &matches[0].Track
}

// playlistForImport returns the user's playlist of that name, creating it
// when there is none — so importing the same list again fills in what's
// missing rather than making "Name (2)".
func (s *Server) playlistForImport(userID, name string) (string, error) {
	if len(name) > 100 {
		name = strings.TrimSpace(name[:100])
	}
	var id string
	err := s.db.QueryRow(`SELECT id FROM playlists WHERE user_id = ? AND name = ? ORDER BY created_at LIMIT 1`, userID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	id = uuid.NewString()
	_, err = s.db.Exec(`INSERT INTO playlists (id, user_id, name, created_at) VALUES (?,?,?,?)`, id, userID, name, nowRFC3339())
	return id, err
}

// linkIntoPlaylist appends the track unless it is in the playlist already.
func (s *Server) linkIntoPlaylist(playlistID, trackID string) error {
	var already int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`, playlistID, trackID).Scan(&already); err != nil {
		return err
	}
	if already > 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO playlist_tracks (playlist_id, track_id, position)
		SELECT ?, ?, COALESCE(MAX(position), 0) + 1 FROM playlist_tracks WHERE playlist_id = ?`, playlistID, trackID, playlistID)
	return err
}

// friendlyImportError keeps the messages meant for people (statusError)
// and hides the rest behind fallback.
func friendlyImportError(err error, fallback string) string {
	var status *statusError
	if errors.As(err, &status) {
		return status.msg
	}
	return fallback
}
