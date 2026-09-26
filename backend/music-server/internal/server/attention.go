package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
)

const (
	coverFromFile      = "file"
	coverFromThumbnail = "thumbnail"
	coverFromCatalogue = "catalogue"
	coverFromMatch     = "match"
	coverChosen        = "chosen"
)

func coverOrigin(hasCover bool, source string) string {
	if !hasCover {
		return ""
	}
	return source
}

type AttentionItem struct {
	TrackID string   `json:"trackId"`
	Reasons []string `json:"reasons"`
}

func (s *Server) handleLibraryAttention(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	query := `SELECT id, artist, album, has_cover, cover_source FROM tracks WHERE attention_dismissed = ?`
	args := []any{boolToInt(r.URL.Query().Get("hidden") == "1")}
	if !u.IsAdmin() {
		query += ` AND user_id = ?`
		args = append(args, u.ID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		s.internalErr(w, "list attention", err)
		return
	}
	defer rows.Close()
	items := []AttentionItem{}
	for rows.Next() {
		var id, artist, album, coverSource string
		var hasCover bool
		if err := rows.Scan(&id, &artist, &album, &hasCover, &coverSource); err != nil {
			s.internalErr(w, "scan attention", err)
			return
		}
		reasons := []string{}
		if strings.TrimSpace(dropPlaceholder(artist, "Unknown Artist")) == "" {
			reasons = append(reasons, "artist")
		}
		if strings.TrimSpace(dropPlaceholder(album, "Unknown Album")) == "" {
			reasons = append(reasons, "album")
		}
		if !hasCover {
			reasons = append(reasons, "cover")
		} else if coverSource == coverFromThumbnail {
			reasons = append(reasons, "thumbnail")
		}
		if len(reasons) > 0 {
			items = append(items, AttentionItem{TrackID: id, Reasons: reasons})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleDismissAttention(w http.ResponseWriter, r *http.Request) {
	s.setAttentionDismissed(w, r, true)
}

func (s *Server) handleRestoreAttention(w http.ResponseWriter, r *http.Request) {
	s.setAttentionDismissed(w, r, false)
}

func (s *Server) setAttentionDismissed(w http.ResponseWriter, r *http.Request, dismissed bool) {
	u := userFrom(r)
	id := r.PathValue("id")
	t, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if !canEditTrack(u, t) {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
		return
	}
	if _, err := s.db.Exec(`UPDATE tracks SET attention_dismissed = ? WHERE id = ?`, boolToInt(dismissed), id); err != nil {
		s.internalErr(w, "update attention", err)
		return
	}
	writeNoContent(w)
}

const bulkApplyThreshold = 0.9

type BulkApplyStatus struct {
	Running bool `json:"running"`
	Total   int  `json:"total"`
	Done    int  `json:"done"`
	Failed  int  `json:"failed"`
}

var bulkApply = struct {
	sync.Mutex
	byUser map[string]*BulkApplyStatus
}{byUser: map[string]*BulkApplyStatus{}}

func (s *Server) handleApplyConfidentMatches(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var body struct {
		TrackIDs []string `json:"trackIds"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if len(body.TrackIDs) > 10000 {
		writeErr(w, http.StatusBadRequest, "too many tracks at once")
		return
	}
	bulkApply.Lock()
	if st := bulkApply.byUser[u.ID]; st != nil && st.Running {
		bulkApply.Unlock()
		writeErr(w, http.StatusConflict, "matches are already being applied")
		return
	}
	bulkApply.Unlock()

	type pick struct {
		track *Track
		cand  scoredCandidate
	}
	var picks []pick
	lookups, uncertain := 0, 0
	for _, id := range body.TrackIDs {
		row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(u.ID), id)...)
		t, _, _, err := scanTrack(row.Scan)
		if err != nil || !canEditTrack(u, t) {
			continue
		}
		job, err := s.loadMetadataJob(id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if !metadataLookupDisabled() && s.enqueueMetadataLookup(id, reasonView, u.ID) == nil {
				lookups++
			} else {
				uncertain++
			}
		case err != nil:
			continue
		case job.State != jobDone:
			lookups++
		case len(job.Candidates) == 0 || job.Candidates[0].score < bulkApplyThreshold || job.AppliedID == job.Candidates[0].id:
			uncertain++
		default:
			picks = append(picks, pick{track: t, cand: job.Candidates[0]})
		}
	}

	status := &BulkApplyStatus{Running: len(picks) > 0, Total: len(picks)}
	bulkApply.Lock()
	bulkApply.byUser[u.ID] = status
	bulkApply.Unlock()

	if len(picks) > 0 {
		work := make(chan pick)
		var wg sync.WaitGroup
		for range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer logPanic("apply confident matches")
				for p := range work {
					ctx, cancel := context.WithTimeout(context.Background(), metadataLookupTimeout)
					err := s.applyChosenCandidate(ctx, p.track, &p.cand, coverFromMatch)
					cancel()
					if err != nil {
						logErr("apply match", err)
					}
					bulkApply.Lock()
					status.Done++
					if err != nil {
						status.Failed++
					}
					bulkApply.Unlock()
				}
			}()
		}
		go func() {
			for _, p := range picks {
				work <- p
			}
			close(work)
			wg.Wait()
			bulkApply.Lock()
			status.Running = false
			bulkApply.Unlock()
		}()
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"applying": len(picks), "lookups": lookups, "uncertain": uncertain})
}

func (s *Server) handleApplyConfidentStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	bulkApply.Lock()
	var st BulkApplyStatus
	if cur := bulkApply.byUser[u.ID]; cur != nil {
		st = *cur
	}
	bulkApply.Unlock()
	writeJSON(w, http.StatusOK, st)
}
