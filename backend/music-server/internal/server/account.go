package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Self-service account changes: a new username, and deleting the account.
//
// The library is shared, so an account's uploads are not really its own to
// take along: when someone deletes their account their tracks stay, handed
// to a built-in placeholder account. Everything personal goes with the row
// (the cascade takes playlists, favourites, history, sessions, share links).
//
// The placeholder can never be signed into or registered: its password hash
// is not a bcrypt hash, its name has a space in it (usernames can't), and it
// is left out of the user list, the "is this the first account" checks and
// sign-in. Tracks it holds show as uploaded by "deleted user" and can be
// managed by admins, like any other track.

const (
	deletedUserID   = "deleted-user"
	deletedUsername = "deleted user"
)

// ensureDeletedUser creates the placeholder account the first time it is
// needed.
func (s *Server) ensureDeletedUser(tx *sql.Tx) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO users (id, username, password_hash, created_at, role) VALUES (?,?,?,?,?)`,
		deletedUserID, deletedUsername, "!", nowRFC3339(), roleUser)
	return err
}

// checkPassword verifies the caller's current password, answering 401 (and
// counting the failure against the sign-in rate limit) when it is wrong.
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, userID, password string) bool {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "user no longer exists")
		return false
	} else if err != nil {
		s.internalErr(w, "lookup password", err)
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		noteAuthFailure(r)
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return false
	}
	return true
}

// handleRenameAccount: POST /api/auth/username {"username", "password"}.
// The username is also the sign-in name, so this asks for the password
// like a password change does — a borrowed session shouldn't be able to
// lock the owner out by renaming the account. Answers with a fresh token
// (the token carries the name) and the updated user.
func (s *Server) handleRenameAccount(w http.ResponseWriter, r *http.Request) {
	if blocked, _ := rateLimitAuth(r); blocked {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	u := userFrom(r)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Username)
	if !validUsername(name) {
		writeErr(w, http.StatusBadRequest, "username must be 3-32 chars (letters, digits, _ - .)")
		return
	}
	name = strings.ToLower(name)
	if !s.checkPassword(w, r, u.ID, req.Password) {
		return
	}
	var taken int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE username = ? AND id <> ?`, name, u.ID).Scan(&taken); err != nil {
		s.internalErr(w, "check username", err)
		return
	}
	if taken > 0 {
		writeErr(w, http.StatusConflict, "username already taken")
		return
	}
	if _, err := s.db.Exec(`UPDATE users SET username = ? WHERE id = ?`, name, u.ID); err != nil {
		// Two renames racing for the same name: the UNIQUE constraint decides.
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErr(w, http.StatusConflict, "username already taken")
			return
		}
		s.internalErr(w, "rename user", err)
		return
	}
	clearAuthFailures(r)

	renamed := *u
	renamed.Username = name
	token, err := s.issueToken(&renamed, u.OrigIssuedAt)
	if err != nil {
		s.internalErr(w, "sign token", err)
		return
	}
	user, err := s.publicUserByID(u.ID)
	if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}

// handleDeleteAccount: POST /api/auth/delete-account {"password"}. The
// tracks stay in the library; the account and everything personal to it
// go. The last admin can't leave while others depend on the server.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	if blocked, _ := rateLimitAuth(r); blocked {
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	u := userFrom(r)
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !s.checkPassword(w, r, u.ID, req.Password) {
		return
	}

	if u.Role == roleAdmin {
		var otherAdmins, others int
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE role = ? AND id <> ? AND id <> ?`,
			roleAdmin, u.ID, deletedUserID).Scan(&otherAdmins)
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE id <> ? AND id <> ?`, u.ID, deletedUserID).Scan(&others)
		if otherAdmins == 0 && others > 0 {
			writeErr(w, http.StatusConflict, "you're the only admin — make someone else an admin first")
			return
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.internalErr(w, "begin", err)
		return
	}
	defer tx.Rollback()
	if err := s.ensureDeletedUser(tx); err != nil {
		s.internalErr(w, "placeholder account", err)
		return
	}
	// updated_at moves so other devices' delta sync picks up the new uploader.
	if _, err := tx.Exec(`UPDATE tracks SET user_id = ?, updated_at = ? WHERE user_id = ?`,
		deletedUserID, nowRFC3339(), u.ID); err != nil {
		s.internalErr(w, "hand over tracks", err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id = ?`, u.ID); err != nil {
		s.internalErr(w, "delete account", err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalErr(w, "commit", err)
		return
	}
	forgetAuthInfo(u.ID)
	os.Remove(s.avatarPath(u.ID))
	clearAuthFailures(r)
	writeNoContent(w)
}

// publicUserByID reads a user as the clients see it, from the database —
// not from the token, whose copy of the name is only as new as the token.
func (s *Server) publicUserByID(id string) (publicUser, error) {
	var out publicUser
	var hasAvatar int
	err := s.db.QueryRow(`SELECT id, username, created_at, has_avatar, role FROM users WHERE id = ?`, id).
		Scan(&out.ID, &out.Username, &out.CreatedAt, &hasAvatar, &out.Role)
	out.HasAvatar = hasAvatar == 1
	return out, err
}
