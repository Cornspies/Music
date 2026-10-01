package server

import "net/http"

func demoScope(ownerCol string) string {
	return `COALESCE((SELECT id FROM users WHERE id = ` + ownerCol + ` AND role = '` + roleDemo + `'), '')`
}

func sameLibrary(ownerCol string) string {
	return demoScope(ownerCol) + ` = ` + demoScope("?")
}

func visibleTo(ownerCol string) string {
	return `EXISTS (SELECT 1 FROM users v WHERE v.id = ? AND (v.role = '` + roleAdmin + `' OR ` +
		demoScope(ownerCol) + ` = CASE WHEN v.role = '` + roleDemo + `' THEN v.id ELSE '' END))`
}

func (s *Server) canSeeTrack(w http.ResponseWriter, viewerID, trackID string) bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM tracks t WHERE t.id = ? AND `+visibleTo("t.user_id"),
		trackID, viewerID).Scan(&n); err != nil {
		s.internalErr(w, "check track", err)
		return false
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "track not found")
		return false
	}
	return true
}
