package server

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
)

type AdminUser struct {
	publicUser
	TrackCount   int     `json:"trackCount"`
	BytesUsed    int64   `json:"bytesUsed"`
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

	s.removePlaylistCoversOf(id)
	if err := s.removeTracksOf(id); err != nil {
		s.internalErr(w, "delete user tracks", err)
		return
	}

	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		s.internalErr(w, "delete user", err)
		return
	}
	forgetAuthInfo(id)
	s.forgetListener(id)
	os.Remove(s.avatarPath(id))
	writeNoContent(w)
}

type adminUserPatch struct {
	Role    *string `json:"role"`
	SignOut bool    `json:"signOut"`
}

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
		if role != roleUser && role != roleAdmin && role != roleDemo {
			writeErr(w, http.StatusBadRequest, "role must be 'user', 'admin' or 'demo'")
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

type AdminSettings struct {
	RegistrationOpen     bool  `json:"registrationOpen"`
	RegistrationPinned   bool  `json:"registrationPinned"`
	InviteRequired       bool  `json:"inviteRequired"`
	TranscodingAvailable bool  `json:"transcodingAvailable"`
	UserQuotaBytes       int64 `json:"userQuotaBytes"`
	MetadataLookupPaused bool  `json:"metadataLookupPaused"`
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
