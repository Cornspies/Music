package server

import "net/http"

// Demo accounts (role "demo") are for people outside the household, such as
// the App Store's reviewers. A demo account has a library of its own: it sees
// only what it uploads itself, and apart from admins nobody else sees that.
// Everything a user can see (tracks, playlists, search, stats) is filtered
// through visibleTo.

// demoScope is the ID of the demo account that owns a row, or an empty string
// when the owner is an ordinary account. ownerCol is an SQL expression for the
// owner's user ID, such as t.user_id, or ? for a parameter.
func demoScope(ownerCol string) string {
	return `COALESCE((SELECT id FROM users WHERE id = ` + ownerCol + ` AND role = '` + roleDemo + `'), '')`
}

// sameLibrary is an SQL condition on an owner column (tracks.user_id or
// playlists.user_id). It takes the viewer's user ID as its one argument and
// holds when owner and viewer are the same demo account, or when neither is a
// demo account. Checks for what is already in the library use it, so an admin
// isn't told a song is there when only a demo account has it.
func sameLibrary(ownerCol string) string {
	return demoScope(ownerCol) + ` = ` + demoScope("?")
}

// visibleTo is sameLibrary widened for admins, who see demo accounts' uploads
// and playlists too. It also takes the viewer's user ID as its one argument.
func visibleTo(ownerCol string) string {
	return `EXISTS (SELECT 1 FROM users v WHERE v.id = ? AND (v.role = '` + roleAdmin + `' OR ` +
		demoScope(ownerCol) + ` = CASE WHEN v.role = '` + roleDemo + `' THEN v.id ELSE '' END))`
}

// canSeeTrack answers 404 unless the viewer may see the track.
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
