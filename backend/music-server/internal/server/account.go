package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	deletedUserID   = "deleted-user"
	deletedUsername = "deleted user"
)

func (s *Server) ensureDeletedUser(tx *sql.Tx) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO users (id, username, password_hash, created_at, role) VALUES (?,?,?,?,?)`,
		deletedUserID, deletedUsername, "!", nowRFC3339(), roleUser)
	return err
}

// checkPassword confirms a signed-in account's password before a change that
// needs it, so a stolen session alone can't take the account over.
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, userID, password string) bool {
	if locked, retry := accountLocked(accountKey(userID)); locked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many wrong passwords, try again later")
		return false
	}
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
		noteAccountFailure(accountKey(userID))
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return false
	}
	clearAccountFailures(accountKey(userID))
	return true
}

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

	// Other accounts' uploads stay in the library under the deleted-user
	// placeholder; a demo account's never joined it, and handing them over
	// would publish them, so they go with the account.
	if u.Role == roleDemo {
		if err := s.removeTracksOf(u.ID); err != nil {
			s.internalErr(w, "delete tracks", err)
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
	s.forgetListener(u.ID)
	os.Remove(s.avatarPath(u.ID))
	clearAuthFailures(r)
	writeNoContent(w)
}

func (s *Server) publicUserByID(id string) (publicUser, error) {
	var out publicUser
	var hasAvatar int
	err := s.db.QueryRow(`SELECT id, username, created_at, has_avatar, role FROM users WHERE id = ?`, id).
		Scan(&out.ID, &out.Username, &out.CreatedAt, &hasAvatar, &out.Role)
	out.HasAvatar = hasAvatar == 1
	return out, err
}
