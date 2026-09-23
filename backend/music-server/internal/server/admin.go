package server

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
)

// Admin: user management and runtime settings. The first account on a server
// is its admin; admins can promote others. Everything here is behind
// requireAdmin.

// AdminUser is a user as the admin screen sees them.
type AdminUser struct {
	publicUser
	TrackCount int   `json:"trackCount"`
	BytesUsed  int64 `json:"bytesUsed"`
	// LastPlayedAt is the most recent listen, or null.
	LastPlayedAt *string `json:"lastPlayedAt"`
}

func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT u.id, u.username, u.created_at, u.has_avatar, u.role,
		       (SELECT COUNT(1) FROM tracks t WHERE t.user_id = u.id),
		       (SELECT COALESCE(SUM(t.file_size + COALESCE(t.alt_size, 0)), 0) FROM tracks t WHERE t.user_id = u.id),
		       (SELECT MAX(played_at) FROM play_events pe WHERE pe.user_id = u.id)
		FROM users u WHERE u.id <> ? ORDER BY u.created_at ASC`, deletedUserID)
	if err != nil {
		s.internalErr(w, "list users", err)
		return
	}
	defer rows.Close()

	users := []AdminUser{}
	for rows.Next() {
		var u AdminUser
		var hasAvatar int
		var last sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.CreatedAt, &hasAvatar, &u.Role, &u.TrackCount, &u.BytesUsed, &last); err != nil {
			s.internalErr(w, "scan user", err)
			return
		}
		u.HasAvatar = hasAvatar == 1
		if last.Valid && last.String != "" {
			v := last.String
			u.LastPlayedAt = &v
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

// handleAdminDeleteUser removes an account and everything it uploaded. The
// files go with it — a shared library still shouldn't keep audio nobody is
// accountable for. Admins can't delete themselves, so a server always keeps
// at least one.
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r)
	id := r.PathValue("id")
	if id == me.ID {
		writeErr(w, http.StatusBadRequest, "you can't delete your own account from here")
		return
	}
	if id == deletedUserID {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	var role string
	err := s.db.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}

	// Their tracks first, one by one, so files and tombstones are handled the
	// same way as a normal delete rather than left behind by the cascade.
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE user_id = ?`, id)
	if err != nil {
		s.internalErr(w, "list user tracks", err)
		return
	}
	var trackIDs []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err == nil {
			trackIDs = append(trackIDs, tid)
		}
	}
	rows.Close()
	for _, tid := range trackIDs {
		if err := s.removeTrack(tid); err != nil {
			s.internalErr(w, "delete user track", err)
			return
		}
	}

	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		s.internalErr(w, "delete user", err)
		return
	}
	forgetAuthInfo(id)
	os.Remove(s.avatarPath(id))
	writeNoContent(w)
}

type adminUserPatch struct {
	Role *string `json:"role"`
	// SignOut revokes every session the user has.
	SignOut bool `json:"signOut"`
}

// handleAdminUpdateUser changes a user's role and/or signs them out
// everywhere. Admins can't demote themselves — promote someone else first.
func (s *Server) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r)
	id := r.PathValue("id")
	var body adminUserPatch
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE id = ? AND id <> ?`, id, deletedUserID).Scan(&exists); err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	if exists == 0 {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if body.Role != nil {
		role := strings.ToLower(strings.TrimSpace(*body.Role))
		if role != roleUser && role != roleAdmin {
			writeErr(w, http.StatusBadRequest, "role must be 'user' or 'admin'")
			return
		}
		if id == me.ID && role != roleAdmin {
			writeErr(w, http.StatusBadRequest, "promote someone else before stepping down")
			return
		}
		if _, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id); err != nil {
			s.internalErr(w, "update role", err)
			return
		}
		forgetAuthInfo(id)
	}
	if body.SignOut {
		if err := s.bumpTokenVersion(id); err != nil {
			s.internalErr(w, "sign out user", err)
			return
		}
	}
	writeNoContent(w)
}

// AdminSettings is the runtime configuration an admin can change without
// restarting the server.
type AdminSettings struct {
	RegistrationOpen bool `json:"registrationOpen"`
	// RegistrationPinned is true when MUSIC_ALLOW_REGISTRATION is set in the
	// environment, in which case the toggle is read-only.
	RegistrationPinned bool `json:"registrationPinned"`
	InviteRequired     bool `json:"inviteRequired"`
	// Informational: what the server can do, so the screen can explain
	// missing features.
	TranscodingAvailable bool `json:"transcodingAvailable"`
	// UserQuotaBytes is the per-user storage cap, 0 for none.
	UserQuotaBytes int64 `json:"userQuotaBytes"`
	// MetadataLookupPaused holds the lookup queue: new tracks still join
	// it, none are looked up until it is resumed.
	MetadataLookupPaused bool `json:"metadataLookupPaused"`
}

func (s *Server) adminSettings() AdminSettings {
	return AdminSettings{
		RegistrationOpen:     s.registrationOpen(),
		RegistrationPinned:   registrationPinnedByEnv(),
		InviteRequired:       inviteRequired(),
		TranscodingAvailable: TranscodingAvailable(),
		UserQuotaBytes:       userQuotaBytes(),
		MetadataLookupPaused: s.metadataPaused(),
	}
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.adminSettings())
}

func (s *Server) handleAdminUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RegistrationOpen     *bool `json:"registrationOpen"`
		MetadataLookupPaused *bool `json:"metadataLookupPaused"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	if body.RegistrationOpen != nil {
		if registrationPinnedByEnv() {
			writeErr(w, http.StatusConflict, "registration is fixed by MUSIC_ALLOW_REGISTRATION in the server's environment")
			return
		}
		v := "0"
		if *body.RegistrationOpen {
			v = "1"
		}
		if err := s.putSetting(settingRegistrationOpen, v); err != nil {
			s.internalErr(w, "save setting", err)
			return
		}
	}
	if p := body.MetadataLookupPaused; p != nil && *p != s.metadataPaused() {
		v, verb := "0", "resumed"
		if *p {
			v, verb = "1", "paused"
		}
		if err := s.putSetting(settingMetadataPaused, v); err != nil {
			s.internalErr(w, "save setting", err)
			return
		}
		log.Printf("metadata queue: %s by %s", verb, userFrom(r).Username)
		s.wakeMetadataWorker()
	}
	writeJSON(w, http.StatusOK, s.adminSettings())
}
