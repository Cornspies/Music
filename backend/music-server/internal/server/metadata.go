package server

import (
        "context"
        "fmt"
        "net/http"
        "os"
        "path/filepath"
        "strings"
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
// track, best first. Candidates found during upload are served from a short
// lived cache; on a miss (or for an older track the user wants to re-tag) the
// search is re-run live against every provider.
func (s *Server) handleTrackMetadataCandidates(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        track, _, fileName, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        if metadataLookupDisabled() {
                writeJSON(w, http.StatusOK, MetadataCandidatesResponse{
                        Candidates: []MetadataCandidate{}, LookupDisabled: true, AutoApplied: false,
                })
                return
        }

        if entry, hit := cachedCandidates(id); hit {
                writeJSON(w, http.StatusOK, MetadataCandidatesResponse{
                        AutoApplied:    entry.appliedID != "",
                        Candidates:     toJSONCandidates(entry.candidates, entry.appliedID),
                        LookupDisabled: false,
                })
                return
        }

        // Cache miss — search again using what the track currently has. Placeholder
        // values written at upload ("Unknown Artist"/"Unknown Album", or a title
        // that's just the filename) are deliberately treated as absent so they
        // don't poison the query or the match scoring.
        meta := fileMetadata{
                title:    trackSearchTitle(track.Title, fileName),
                artist:   strings.TrimSpace(dropPlaceholder(track.Artist, "Unknown Artist")),
                album:    strings.TrimSpace(dropPlaceholder(track.Album, "Unknown Album")),
                duration: track.Duration,
        }

        ctx, cancel := context.WithTimeout(r.Context(), metadataLookupTimeout)
        defer cancel()

        cands, outcome := lookupCandidates(ctx, meta, fileName)

        // The client hung up while the providers were still answering. What came
        // back is an arbitrary subset, so caching it would hand that truncated
        // list to the next request instead of searching again.
        if r.Context().Err() != nil {
                return
        }

        // A lookup that lost providers is cached only briefly, so a rate limit
        // during upload doesn't pin the dialog to a short list for half an hour.
        cacheCandidates(id, cands, "", outcome.cacheTTL())
        writeJSON(w, http.StatusOK, MetadataCandidatesResponse{
                Candidates: toJSONCandidates(cands, ""),
                // Nothing was auto-applied on this path: these are suggestions
                // for a track the user is re-tagging by hand.
                AutoApplied:    false,
                LookupDisabled: false,
        })
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

        track, _, fileName, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        // Everyone can see every track, but its tags belong to whoever uploaded
        // it. Without this the UPDATE below would quietly match zero rows and
        // still report success.
        if track.UploadedBy.ID != u.ID {
                writeErr(w, http.StatusForbidden, "only the person who uploaded this track can change its details")
                return
        }

        entry, hit := cachedCandidates(id)
        if !hit {
                // The cached list expired between viewing and choosing — re-run the
                // search so a stale dialog still resolves instead of erroring out.
                ctx, cancel := context.WithTimeout(r.Context(), metadataLookupTimeout)
                defer cancel()
                meta := fileMetadata{
                        title:    trackSearchTitle(track.Title, fileName),
                        artist:   strings.TrimSpace(dropPlaceholder(track.Artist, "Unknown Artist")),
                        album:    strings.TrimSpace(dropPlaceholder(track.Album, "Unknown Album")),
                        duration: track.Duration,
                }
                cands, outcome := lookupCandidates(ctx, meta, fileName)
                entry = candidateCacheEntry{candidates: cands}
                cacheCandidates(id, cands, "", outcome.cacheTTL())
        }

        var chosen *scoredCandidate
        for i := range entry.candidates {
                if entry.candidates[i].id == body.CandidateID {
                        chosen = &entry.candidates[i]
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
                `UPDATE tracks SET title = ?, artist = ?, album = ?, genre = ?, year = ?, has_cover = ?
                 WHERE id = ? AND user_id = ?`,
                title, artist, album, genre, year, boolToInt(hasCover), id, u.ID,
        ); err != nil {
                s.internalErr(w, "apply metadata", err)
                return
        }

        // The user has resolved this track; drop the cached suggestions so a
        // later request re-searches against the corrected metadata.
        forgetCandidates(id)

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
