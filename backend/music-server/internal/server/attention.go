package server

import (
	"net/http"
	"strings"
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
	query := `SELECT id, artist, album, has_cover, cover_source FROM tracks WHERE attention_dismissed = 0`
	var args []any
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
	if _, err := s.db.Exec(`UPDATE tracks SET attention_dismissed = 1 WHERE id = ?`, id); err != nil {
		s.internalErr(w, "dismiss attention", err)
		return
	}
	writeNoContent(w)
}
