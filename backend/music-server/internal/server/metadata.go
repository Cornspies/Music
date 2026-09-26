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
	Confidence float64 `json:"confidence"`
	Applied    bool    `json:"applied"`
}

type MetadataCandidatesResponse struct {
	AutoApplied    bool                `json:"autoApplied"`
	Candidates     []MetadataCandidate `json:"candidates"`
	LookupDisabled bool                `json:"lookupDisabled"`
	Pending        bool                `json:"pending"`
	QueuePosition  int                 `json:"queuePosition,omitempty"`
	Partial        bool                `json:"partial"`
	Attempts       int                 `json:"attempts"`
	RetryAt        string              `json:"retryAt,omitempty"`
	Paused         bool                `json:"paused"`
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

func (s *Server) handleTrackMetadataCandidates(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")

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

const candidateArtworkTimeout = 10 * time.Second

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
		if r.Context().Err() == nil {
			logErr("candidate artwork", fmt.Errorf("candidate %s: %w", candidateID, err))
		}
		writeErr(w, http.StatusNotFound, "artwork unavailable")
		return
	}

	ct := http.DetectContentType(b)
	if !allowedImageTypes[ct] {
		logErr("candidate artwork", fmt.Errorf("candidate %s: refusing to serve artwork of type %q", candidateID, ct))
		writeErr(w, http.StatusNotFound, "artwork unavailable")
		return
	}

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

func trackSearchTitle(title, fileName string) string {
	t := strings.TrimSpace(title)
	base := strings.TrimSpace(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
	if t == "" || strings.EqualFold(t, base) {
		return ""
	}
	return t
}

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
	if err := s.applyChosenCandidate(ctx, track, chosen, coverChosen); err != nil {
		s.internalErr(w, "apply metadata", err)
		return
	}

	t, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) applyChosenCandidate(ctx context.Context, track *Track, chosen *scoredCandidate, coverSource string) error {
	id := track.ID
	meta := fileMetadata{}
	applyCandidate(ctx, &meta, chosen.metadataCandidate, true)

	hasCover, wroteCover := track.HasCover, false
	if len(meta.picture) > 0 {
		if !safeCoverBytes(meta.picture) {
			logErr("cover art", fmt.Errorf("track %s: discarding fetched artwork that is not a raster image", id))
		} else if err := os.WriteFile(filepath.Join(s.covDir, id+".img"), meta.picture, 0o644); err == nil {
			hasCover, wroteCover = true, true
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
		`UPDATE tracks SET title = ?, artist = ?, album = ?, genre = ?, year = ?, has_cover = ?, updated_at = ?,
		cover_source = CASE WHEN ? = 1 THEN ? ELSE cover_source END WHERE id = ?`,
		title, artist, album, genre, year, boolToInt(hasCover), nowRFC3339(), boolToInt(wroteCover), coverSource, id,
	); err != nil {
		return err
	}

	_, _ = s.db.Exec(`UPDATE metadata_jobs SET applied_id = ? WHERE track_id = ?`, chosen.id, id)
	s.forgetLookedUpLyrics(id)
	if len(meta.picture) > 0 && hasCover {
		go s.updateCoverColor(id)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
