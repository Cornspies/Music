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

type playlistSource interface {
	source
	playlistID(raw string) string
	listPlaylist(ctx context.Context, id string) (catalogPlaylist, error)
}

type catalogPlaylist struct {
	Title   string
	Total   int
	Entries []discoverResult
}

type PlaylistImport struct {
	ID                 string            `json:"id"`
	Source             string            `json:"source"`
	ListID             string            `json:"listId"`
	Title              string            `json:"title"`
	State              string            `json:"state"`
	Total              int               `json:"total"`
	Done               int               `json:"done"`
	Added              int               `json:"added"`
	Existing           int               `json:"existing"`
	Failed             int               `json:"failed"`
	Current            string            `json:"current,omitempty"`
	PlaylistID         string            `json:"playlistId,omitempty"`
	Error              string            `json:"error,omitempty"`
	Failures           []ImportFailure   `json:"failures"`
	ExistingTracks     []ImportExisting  `json:"existingTracks"`
	PossibleDuplicates []ImportDuplicate `json:"possibleDuplicates"`
	StartedAt          string            `json:"startedAt"`
	FinishedAt         string            `json:"finishedAt,omitempty"`
}

type ImportDuplicate struct {
	Title          string `json:"title"`
	TrackID        string `json:"trackId"`
	ExistingID     string `json:"existingId"`
	ExistingTitle  string `json:"existingTitle"`
	ExistingArtist string `json:"existingArtist"`
}

type ImportFailure struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

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

	importRetention = time.Hour
	importListTime  = 90 * time.Second
	importFetchTime = 3 * time.Minute
)

type importJob struct {
	mu sync.Mutex
	PlaylistImport
	user       *User
	src        source
	listID     string
	preset     []discoverResult
	noPlaylist bool
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
	for i := 1; i < len(out); i++ {
		for k := i; k > 0 && out[k].StartedAt > out[k-1].StartedAt; k-- {
			out[k], out[k-1] = out[k-1], out[k]
		}
	}
	return out
}

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

func (s *Server) handleListImports(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, userImports(userFrom(r).ID))
}

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
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.update(func(p *PlaylistImport) { p.State = importListing })
	job.mu.Lock()
	job.cancel = cancel
	job.mu.Unlock()
	defer cancel()

	entries, title := job.preset, job.snapshot().Title
	if ps, ok := job.src.(playlistSource); ok && job.preset == nil {
		listCtx, listCancel := context.WithTimeout(ctx, importListTime)
		pl, err := ps.listPlaylist(listCtx, job.listID)
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
		entries, title = pl.Entries, trimDefault(pl.Title, job.src.name()+" playlist")
	}
	if len(entries) == 0 {
		job.finish(importFailed, "the playlist has no tracks that can be fetched")
		return
	}
	playlistID := ""
	if !job.noPlaylist {
		var err error
		playlistID, err = s.playlistForImport(job.user.ID, title)
		if err != nil {
			logErr("import playlist", err)
			job.finish(importFailed, "couldn't create the playlist")
			return
		}
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
		if playlistID != "" {
			if err := s.linkIntoPlaylist(playlistID, trackID); err != nil {
				logErr("import link", err)
			}
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

func (s *Server) importEntry(ctx context.Context, job *importJob, e discoverResult) (id string, added bool, why string, lookalike *Track, err error) {
	if existing, ok := s.trackFromSource(job.user.ID, job.src.key(), e.ID); ok {
		return existing, false, "the same video was added before", nil, nil
	}
	lookalike = s.sameSongInLibrary(job.user.ID, e, job.preset != nil)
	fetchCtx, cancel := context.WithTimeout(ctx, importFetchTime)
	defer cancel()
	dl, err := job.src.fetch(fetchCtx, e.ID)
	noteSource(job.src.key(), err)
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

func (s *Server) sameSongInLibrary(userID string, e discoverResult, clean bool) *Track {
	artist, title := parseYouTubeTitle(e.Title, "")
	title = trimDefault(title, e.Title)
	veto := artist != ""
	artist = trimDefault(artist, e.Artist)
	if clean {
		title, artist, veto = e.Title, e.Artist, e.Artist != ""
	}
	matches := s.findSameSong(userID, title, artist, e.Duration, true, veto)
	if len(matches) == 0 {
		return nil
	}
	return &matches[0].Track
}

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

func friendlyImportError(err error, fallback string) string {
	var status *statusError
	if errors.As(err, &status) {
		return status.msg
	}
	return fallback
}
