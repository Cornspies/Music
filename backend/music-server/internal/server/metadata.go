package server

import (
        "context"
        "database/sql"
        "errors"
        "fmt"
        "net/http"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "time"
)

// MetadataCandidate is the JSON shape of a single lookup result offered to
// the user in the "pick the right result" dialog.
type MetadataCandidate struct {
        ID         string  `json:"id"`
        Source     string  `json:"source"`
        Title      string  `json:"title"`
        Artist     string  `json:"artist"`
        Album      string  `json:"album"`
        Genre      string  `json:"genre"`
        Year       int     `json:"year"`
        Duration   float64 `json:"duration"`
        HasArtwork bool    `json:"hasArtwork"`
        // Confidence is the 0-1 match score; the list is sorted by it, best first.
        Confidence float64 `json:"confidence"`
        // Applied marks the candidate that was automatically applied at upload
        // time (at most one, and only when it cleared the confidence threshold).
        Applied bool `json:"applied"`
}

// MetadataCandidatesResponse is returned by GET .../metadata/candidates.
type MetadataCandidatesResponse struct {
        // AutoApplied is true when one of the candidates was confident enough to
        // be applied automatically during upload.
        AutoApplied bool                `json:"autoApplied"`
        Candidates  []MetadataCandidate `json:"candidates"`
        // LookupDisabled is true when online lookups are switched off server-side.
        LookupDisabled bool `json:"lookupDisabled"`
        // Pending is true while the lookup is still queued or running; the
        // candidates are then what has been found so far. Poll again later.
        Pending bool `json:"pending"`
        // QueuePosition is 1 when the track is next (or being looked up now).
        QueuePosition int `json:"queuePosition,omitempty"`
        // Partial is true when the finished lookup was missing a provider.
        Partial bool `json:"partial"`
        // Attempts counts lookups made so far, for "retrying (attempt 3)" wording.
        Attempts int `json:"attempts"`
        // RetryAt is when a lookup missing a provider will be tried again
        // (RFC 3339), while it is parked.
        RetryAt string `json:"retryAt,omitempty"`
        // Paused is true while the lookup waits on an admin's pause of the
        // queue.
        Paused bool `json:"paused"`
}

func toJSONCandidates(cands []scoredCandidate, appliedID string) []MetadataCandidate {
        out := make([]MetadataCandidate, 0, len(cands))
        for _, c := range cands {
                out = append(out, MetadataCandidate{
                        ID: c.id, Source: c.source, Title: c.title, Artist: c.artist,
                        Album: c.album, Genre: c.genre, Year: c.year, Duration: c.durationSec,
                        HasArtwork: c.artURL() != "", Confidence: c.score, Applied: c.id == appliedID,
                })
        }
        return out
}

// handleTrackMetadataCandidates returns the metadata matches found for a
// track, best first, from its lookup job. A track without a job (older than
// the queue, or never looked up) gets one queued here, and the response says
// "pending" until the worker has been through it — clients poll.
func (s *Server) handleTrackMetadataCandidates(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        // The review belongs to whoever may apply a result — and opening it
        // queues a lookup, which others shouldn't be able to trigger for
        // every track in the library.
        t, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
                writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit it")
                return
        }
        if metadataLookupDisabled() {
                writeJSON(w, http.StatusOK, MetadataCandidatesResponse{
                        Candidates: []MetadataCandidate{}, LookupDisabled: true,
                })
                return
        }

        job, err := s.loadMetadataJob(id)
        if errors.Is(err, sql.ErrNoRows) {
                if err := s.enqueueMetadataLookup(id, reasonView, u.ID); err != nil {
                        s.internalErr(w, "queue lookup", err)
                        return
                }
                writeJSON(w, http.StatusOK, MetadataCandidatesResponse{
                        Candidates: []MetadataCandidate{}, Pending: true, QueuePosition: s.queuePosition(id),
                        Paused: s.metadataPaused(),
                })
                return
        } else if err != nil {
                s.internalErr(w, "load lookup", err)
                return
        }
        resp := MetadataCandidatesResponse{
                AutoApplied: job.AppliedID != "",
                Candidates:  toJSONCandidates(job.Candidates, job.AppliedID),
                Pending:     job.State != jobDone,
                Partial:     job.Partial,
                Attempts:    job.Attempts,
        }
        if resp.Pending {
                resp.QueuePosition = s.queuePosition(id)
                resp.Paused = s.metadataPaused()
                if job.State == jobQueued && !job.NotBefore.IsZero() && job.NotBefore.After(time.Now()) {
                        resp.RetryAt = job.NotBefore.UTC().Format(time.RFC3339)
                }
        }
        writeJSON(w, http.StatusOK, resp)
}

// candidateArtworkTimeout bounds a single thumbnail fetch. It is shorter than
// a full lookup because a dialog opens a request per candidate at once, and a
// slow provider should cost one placeholder, not a hung dialog.
const candidateArtworkTimeout = 10 * time.Second

// handleCandidateArtwork serves a candidate's cover art so the review dialog
// can show a thumbnail next to each suggestion. The image is fetched from the
// provider on demand through the SSRF-guarded lookup client and never stored:
// choosing the candidate downloads it again for real via applyCandidate.
//
// Anything that stops a thumbnail being shown answers 404 — an expired
// candidate list, a provider that has no image after all, a fetch failure —
// so both clients fall back to their placeholder through the same path.
func (s *Server) handleCandidateArtwork(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")
        candidateID := r.PathValue("candidateId")

        t, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
                writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit it")
                return
        }

        cands, err := s.jobCandidates(id)
        if err != nil {
                writeErr(w, http.StatusNotFound, "no suggestions for this track — reload them to see artwork")
                return
        }
        var artURL string
        for i := range cands {
                if cands[i].id == candidateID {
                        artURL = cands[i].artURL()
                        break
                }
        }
        if artURL == "" {
                writeErr(w, http.StatusNotFound, "no artwork for this result")
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), candidateArtworkTimeout)
        defer cancel()
        b, err := fetchArtwork(ctx, artURL)
        if err != nil {
                // The client hung up (dialog closed) — nothing to report.
                if r.Context().Err() == nil {
                        logErr("candidate artwork", fmt.Errorf("candidate %s: %w", candidateID, err))
                }
                writeErr(w, http.StatusNotFound, "artwork unavailable")
                return
        }

        // Same rule as the track cover endpoint: these bytes come from a third
        // party and are served same-origin with the app, so only recognised raster
        // image types go out, and nosniff stops the browser second-guessing that.
        ct := http.DetectContentType(b)
        if !allowedImageTypes[ct] {
                logErr("candidate artwork", fmt.Errorf("candidate %s: refusing to serve artwork of type %q", candidateID, ct))
                writeErr(w, http.StatusNotFound, "artwork unavailable")
                return
        }

        // Candidate IDs are content hashes, so the same ID always maps to the same
        // artwork URL: safe to cache well past the candidate list's own lifetime.
        etag := `"cand-` + candidateID + `"`
        w.Header().Set("Content-Type", ct)
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Cache-Control", "private, max-age=86400")
        w.Header().Set("ETag", etag)
        if r.Header.Get("If-None-Match") == etag {
                w.WriteHeader(http.StatusNotModified)
                return
        }
        w.Header().Set("Content-Length", strconv.Itoa(len(b)))
        w.WriteHeader(http.StatusOK)
        if r.Method != http.MethodHead {
                _, _ = w.Write(b)
        }
}

func dropPlaceholder(v, placeholder string) string {
        if strings.EqualFold(strings.TrimSpace(v), placeholder) {
                return ""
        }
        return v
}

// trackSearchTitle ignores a stored title that's just the bare filename,
// since that's the upload fallback rather than a real tag.
func trackSearchTitle(title, fileName string) string {
        t := strings.TrimSpace(title)
        base := strings.TrimSpace(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
        if t == "" || strings.EqualFold(t, base) {
                return ""
        }
        return t
}

// knownTitle is a track's title as a lookup should see it: "" only when it
// is the upload's fallback, the filename standing in for a missing tag. A
// title that merely equals the filename is kept when the file's own title
// tag says it too — files are often named after the song ("Numb.mp3"), and
// a lookup may fill what's missing but must never replace a real tag.
func (s *Server) knownTitle(title, filePath, fileName string) string {
        if t := trackSearchTitle(title, fileName); t != "" {
                return t
        }
        title = strings.TrimSpace(title)
        if title == "" {
                return ""
        }
        if tags := readMetadata(filepath.Join(s.dataDir, filePath)); strings.TrimSpace(tags.title) != "" {
                return title
        }
        return ""
}

// handleApplyTrackMetadata applies a user-chosen candidate to a track,
// overwriting title/artist/album/genre/year and the cover art with whatever
// that candidate provides. Unlike the automatic upload path this intentionally
// overwrites existing values — the user explicitly picked this result.
func (s *Server) handleApplyTrackMetadata(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        var body struct {
                CandidateID string `json:"candidateId"`
        }
        if err := jsonDecodeStrict(r, w, &body); err != nil {
                return
        }
        body.CandidateID = strings.TrimSpace(body.CandidateID)
        if body.CandidateID == "" {
                writeErr(w, http.StatusBadRequest, "candidateId is required")
                return
        }

        track, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        // Everyone can see every track, but its tags belong to whoever uploaded
        // it. Without this the UPDATE below would quietly match zero rows and
        // still report success.
        if track.UploadedBy.ID != u.ID && !u.IsAdmin() {
                writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
                return
        }

        cands, err := s.jobCandidates(id)
        if err != nil {
                writeErr(w, http.StatusNotFound, "that result is no longer available — reload the suggestions and try again")
                return
        }

        var chosen *scoredCandidate
        for i := range cands {
                if cands[i].id == body.CandidateID {
                        chosen = &cands[i]
                        break
                }
        }
        if chosen == nil {
                writeErr(w, http.StatusNotFound, "that result is no longer available — reload the suggestions and try again")
                return
        }

        ctx, cancel := context.WithTimeout(r.Context(), metadataLookupTimeout)
        defer cancel()

        // Start from empty so overwrite mode fills everything the candidate has.
        meta := fileMetadata{}
        applyCandidate(ctx, &meta, chosen.metadataCandidate, true)

        hasCover := track.HasCover
        if len(meta.picture) > 0 {
                if !safeCoverBytes(meta.picture) {
                        logErr("cover art", fmt.Errorf("track %s: discarding fetched artwork that is not a raster image", id))
                } else if err := os.WriteFile(filepath.Join(s.covDir, id+".img"), meta.picture, 0o644); err == nil {
                        hasCover = true
                } else {
                        logErr("save cover", err)
                }
        }

        title := trimDefault(meta.title, track.Title)
        artist := trimDefault(meta.artist, "Unknown Artist")
        album := trimDefault(meta.album, "Unknown Album")
        genre := meta.genre
        if genre == "" {
                genre = track.Genre
        }
        year := meta.year
        if year == 0 {
                year = track.Year
        }

        if _, err := s.db.Exec(
                `UPDATE tracks SET title = ?, artist = ?, album = ?, genre = ?, year = ?, has_cover = ?, updated_at = ?
                 WHERE id = ?`,
                title, artist, album, genre, year, boolToInt(hasCover), nowRFC3339(), id,
        ); err != nil {
                s.internalErr(w, "apply metadata", err)
                return
        }

        // The user has resolved this track. The suggestions stay (they're the
        // job row) but the applied one is marked so the dialog shows it.
        _, _ = s.db.Exec(`UPDATE metadata_jobs SET applied_id = ? WHERE track_id = ?`, chosen.id, id)
        s.forgetLookedUpLyrics(id)
        if len(meta.picture) > 0 && hasCover {
                go s.updateCoverColor(id)
        }

        t, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        writeJSON(w, http.StatusOK, t)
}

func boolToInt(b bool) int {
        if b {
                return 1
        }
        return 0
}
