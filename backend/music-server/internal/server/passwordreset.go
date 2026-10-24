package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const passwordResetTTL = 24 * time.Hour

type PasswordReset struct {
	Code      string `json:"code"`
	Username  string `json:"username"`
	ExpiresAt string `json:"expiresAt"`
}

type PasswordResetCheck struct {
	Valid     bool   `json:"valid"`
	Username  string `json:"username,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

func hashResetCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func (s *Server) createPasswordReset(userID, createdBy string) (*PasswordReset, error) {
	var username string
	err := s.db.QueryRow(`SELECT username FROM users WHERE id = ? AND id <> ?`, userID, deletedUserID).Scan(&username)
	if err != nil {
		return nil, err
	}
	code, err := newInviteCode()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expires := now.Add(passwordResetTTL).Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	var creator any
	if createdBy != "" {
		creator = createdBy
	}
	if _, err := tx.Exec(`INSERT INTO password_resets (code_hash, user_id, created_by, created_at, expires_at) VALUES (?,?,?,?,?)`,
		hashResetCode(code), userID, creator, now.Format(time.RFC3339), expires); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PasswordReset{Code: code, Username: username, ExpiresAt: expires}, nil
}

func (s *Server) CreatePasswordResetFor(username string) (*PasswordReset, error) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM users WHERE username = ? AND id <> ?`,
		strings.ToLower(strings.TrimSpace(username)), deletedUserID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("no account named %q", username)
	} else if err != nil {
		return nil, err
	}
	return s.createPasswordReset(id, "")
}

func (s *Server) pendingPasswordReset(code string) (userID, username, expiresAt string, err error) {
	err = s.db.QueryRow(`
		SELECT r.user_id, u.username, r.expires_at FROM password_resets r
		JOIN users u ON u.id = r.user_id
		WHERE r.code_hash = ? AND r.expires_at > ?`, hashResetCode(code), nowRFC3339()).
		Scan(&userID, &username, &expiresAt)
	return
}

func (s *Server) handleAdminCreatePasswordReset(w http.ResponseWriter, r *http.Request) {
	reset, err := s.createPasswordReset(r.PathValue("id"), userFrom(r).ID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		s.internalErr(w, "create password reset", err)
		return
	}
	writeJSON(w, http.StatusCreated, reset)
}

func (s *Server) handleCheckPasswordReset(w http.ResponseWriter, r *http.Request) {
	if blocked, retry := rateLimitAuth(r); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	code := strings.TrimSpace(r.PathValue("code"))
	_, username, expiresAt, err := s.pendingPasswordReset(code)
	if errors.Is(err, sql.ErrNoRows) || code == "" {
		noteAuthFailure(r)
		writeJSON(w, http.StatusOK, PasswordResetCheck{Valid: false})
		return
	} else if err != nil {
		s.internalErr(w, "lookup password reset", err)
		return
	}
	writeJSON(w, http.StatusOK, PasswordResetCheck{Valid: true, Username: username, ExpiresAt: expiresAt})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if blocked, retry := rateLimitAuth(r); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	var req struct {
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.Password) < minPasswordLength || len(req.Password) > maxPasswordLength {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("password must be %d-%d characters", minPasswordLength, maxPasswordLength))
		return
	}
	code := strings.TrimSpace(req.Code)
	userID, username, _, err := s.pendingPasswordReset(code)
	if errors.Is(err, sql.ErrNoRows) || code == "" {
		noteAuthFailure(r)
		writeErr(w, http.StatusForbidden, "this reset link has expired or was already used")
		return
	} else if err != nil {
		s.internalErr(w, "lookup password reset", err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.internalErr(w, "hash password", err)
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		s.internalErr(w, "begin", err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM password_resets WHERE code_hash = ? AND expires_at > ?`, hashResetCode(code), nowRFC3339())
	if err != nil {
		s.internalErr(w, "use password reset", err)
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		writeErr(w, http.StatusForbidden, "this reset link has expired or was already used")
		return
	}
	if _, err := tx.Exec(`UPDATE users SET password_hash = ?, token_version = token_version + 1 WHERE id = ?`,
		string(hash), userID); err != nil {
		s.internalErr(w, "update password", err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.internalErr(w, "commit", err)
		return
	}
	forgetAuthInfo(userID)
	clearAuthFailures(r)
	clearAccountFailures(loginKey(username))

	user, err := s.publicUserByID(userID)
	if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	token, err := s.issueToken(&User{ID: user.ID, Username: user.Username, Role: user.Role}, time.Time{})
	if err != nil {
		s.internalErr(w, "sign token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": user})
}
