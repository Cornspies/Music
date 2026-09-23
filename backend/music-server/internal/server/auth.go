package server

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const defaultSessionDays = 30

const defaultSessionMaxDays = 90

const mediaTokenTTL = 6 * time.Hour

const refreshedTokenHeader = "X-Refreshed-Token"

const (
	audienceSession = "session"
	audienceMedia   = "media"
)

const (
	roleUser  = "user"
	roleAdmin = "admin"
)

func sessionTTL() time.Duration {
	raw := config.Value("MUSIC_SESSION_DAYS")
	if raw == "" {
		return defaultSessionDays * 24 * time.Hour
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		return defaultSessionDays * 24 * time.Hour
	}
	if days <= 0 {
		return 3650 * 24 * time.Hour
	}
	return time.Duration(days) * 24 * time.Hour
}

func sessionMaxTTL() time.Duration {
	raw := config.Value("MUSIC_SESSION_MAX_DAYS")
	if raw == "" {
		return defaultSessionMaxDays * 24 * time.Hour
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		return defaultSessionMaxDays * 24 * time.Hour
	}
	if days <= 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}

type authRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	InviteCode string `json:"inviteCode"`
}

const minPasswordLength = 8

const settingRegistrationOpen = "registration_open"

func (s *Server) registrationOpen() bool {
	if raw := strings.ToLower(strings.TrimSpace(config.Value("MUSIC_ALLOW_REGISTRATION"))); raw != "" {
		switch raw {
		case "0", "false", "no", "off":
			return false
		default:
			return true
		}
	}
	if v, ok := s.getSetting(settingRegistrationOpen); ok {
		return v == "1"
	}
	if config.Value("MUSIC_INVITE_CODE") != "" {
		return true
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE id <> ?`, deletedUserID).Scan(&n); err != nil {
		logErr("count users", err)
		return false
	}
	return n == 0
}

func inviteRequired() bool { return config.Value("MUSIC_INVITE_CODE") != "" }

func registrationPinnedByEnv() bool {
	return strings.TrimSpace(config.Value("MUSIC_ALLOW_REGISTRATION")) != ""
}

func (s *Server) getSetting(key string) (string, bool) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

func (s *Server) putSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

type publicUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"createdAt"`
	HasAvatar bool   `json:"hasAvatar"`
	Role      string `json:"role"`
}

type authInfo struct {
	version int
	role    string
}

var authInfoCache sync.Map

func (s *Server) lookupAuthInfo(userID string) (authInfo, error) {
	if v, ok := authInfoCache.Load(userID); ok {
		return v.(authInfo), nil
	}
	var info authInfo
	err := s.db.QueryRow(`SELECT token_version, role FROM users WHERE id = ?`, userID).
		Scan(&info.version, &info.role)
	if err != nil {
		return authInfo{}, err
	}
	authInfoCache.Store(userID, info)
	return info, nil
}

func forgetAuthInfo(userID string) { authInfoCache.Delete(userID) }

func (s *Server) bumpTokenVersion(userID string) error {
	_, err := s.db.Exec(`UPDATE users SET token_version = token_version + 1 WHERE id = ?`, userID)
	forgetAuthInfo(userID)
	return err
}

func (s *Server) issueToken(u *User, origIAT time.Time) (string, error) {
	info, err := s.lookupAuthInfo(u.ID)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if origIAT.IsZero() {
		origIAT = now
	}
	exp := now.Add(sessionTTL())
	if maxTTL := sessionMaxTTL(); maxTTL > 0 {
		if hard := origIAT.Add(maxTTL); hard.Before(exp) {
			exp = hard
		}
	}
	claims := jwt.MapClaims{
		"sub":  u.ID,
		"usr":  u.Username,
		"aud":  audienceSession,
		"iat":  now.Unix(),
		"oiat": origIAT.Unix(),
		"exp":  exp.Unix(),
		"ver":  info.version,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(s.secret)
}

func (s *Server) issueMediaToken(u *User) (string, time.Time, error) {
	info, err := s.lookupAuthInfo(u.ID)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	exp := now.Add(mediaTokenTTL)
	claims := jwt.MapClaims{
		"sub": u.ID,
		"usr": u.Username,
		"aud": audienceMedia,
		"iat": now.Unix(),
		"exp": exp.Unix(),
		"ver": info.version,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(s.secret)
	return signed, exp, err
}

func (s *Server) authUser(r *http.Request, mediaRoute bool) (*User, error) {
	raw := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimSpace(h[7:])
	}
	if raw == "" && mediaRoute {
		raw = r.URL.Query().Get("token")
	}
	if raw == "" {
		return nil, errors.New("missing token")
	}
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	sub, _ := claims["sub"].(string)
	usr, _ := claims["usr"].(string)
	if sub == "" {
		return nil, errors.New("invalid subject")
	}
	aud, _ := claims["aud"].(string)
	if aud == "" {
		aud = audienceSession
	}
	if aud == audienceMedia && !mediaRoute {
		return nil, errors.New("media token not accepted here")
	}
	if aud != audienceMedia && aud != audienceSession {
		return nil, errors.New("unknown audience")
	}

	info, err := s.lookupAuthInfo(sub)
	if err != nil {
		return nil, errors.New("unknown user")
	}
	ver := 0
	if v, ok := claims["ver"].(float64); ok {
		ver = int(v)
	}
	if ver != info.version {
		return nil, errors.New("token revoked")
	}

	var expiresAt time.Time
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		expiresAt = exp.Time
	}
	var origIAT time.Time
	if v, ok := claims["oiat"].(float64); ok {
		origIAT = time.Unix(int64(v), 0)
	} else if iat, err := claims.GetIssuedAt(); err == nil && iat != nil {
		origIAT = iat.Time
	}
	return &User{
		ID: sub, Username: usr, Role: info.role,
		ExpiresAt: expiresAt, OrigIssuedAt: origIAT, IsMediaToken: aud == audienceMedia,
	}, nil
}

func (s *Server) requireAuth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return s.requireAuthKind(next, false)
}

func (s *Server) requireMediaAuth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return s.requireAuthKind(next, true)
}

func (s *Server) requireAuthKind(next func(http.ResponseWriter, *http.Request), media bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.authUser(r, media)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if !u.IsMediaToken {
			s.maybeRefreshToken(w, u)
		}
		next(w, withUser(r, u))
	}
}

func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != roleAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r)
	})
}

func (s *Server) maybeRefreshToken(w http.ResponseWriter, u *User) {
	if u.ExpiresAt.IsZero() {
		return
	}
	ttl := sessionTTL()
	if time.Until(u.ExpiresAt) > ttl/2 {
		return
	}
	if maxTTL := sessionMaxTTL(); maxTTL > 0 && !u.OrigIssuedAt.IsZero() {
		if hard := u.OrigIssuedAt.Add(maxTTL); !hard.After(u.ExpiresAt.Add(time.Minute)) {
			return
		}
	}
	fresh, err := s.issueToken(u, u.OrigIssuedAt)
	if err != nil {
		logErr("refresh token", err)
		return
	}
	w.Header().Set(refreshedTokenHeader, fresh)
}

type AuthConfig struct {
	RegistrationOpen  bool `json:"registrationOpen"`
	InviteRequired    bool `json:"inviteRequired"`
	MinPasswordLength int  `json:"minPasswordLength"`
}

func (s *Server) handleAuthConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, AuthConfig{
		RegistrationOpen:  s.registrationOpen(),
		InviteRequired:    config.Value("MUSIC_INVITE_CODE") != "",
		MinPasswordLength: minPasswordLength,
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if blocked, retry := rateLimitAuth(r); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}

	var req authRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	inviteLink := strings.TrimSpace(req.InviteCode)
	usingLink := s.invitePending(inviteLink)

	if !usingLink && !s.registrationOpen() {
		if inviteLink != "" {
			noteAuthFailure(r)
			writeErr(w, http.StatusForbidden, "this invite link has expired or was already used")
			return
		}
		writeErr(w, http.StatusForbidden, "registration is closed on this server")
		return
	}
	username := strings.TrimSpace(req.Username)
	if !validUsername(username) {
		writeErr(w, http.StatusBadRequest, "username must be 3-32 chars (letters, digits, _ - .)")
		return
	}
	if len(req.Password) < minPasswordLength || len(req.Password) > 128 {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("password must be %d-128 characters", minPasswordLength))
		return
	}
	if code := config.Value("MUSIC_INVITE_CODE"); code != "" && !usingLink {
		if subtle.ConstantTimeCompare([]byte(inviteLink), []byte(code)) != 1 {
			noteAuthFailure(r)
			writeErr(w, http.StatusForbidden, "a valid invite code is required")
			return
		}
	}
	username = strings.ToLower(username)

	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE username = ?`, username).Scan(&exists); err != nil {
		s.internalErr(w, "check username", err)
		return
	}
	if exists > 0 {
		writeErr(w, http.StatusConflict, "username already taken")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		s.internalErr(w, "hash password", err)
		return
	}
	id := uuid.NewString()
	createdAt := nowRFC3339()
	role := roleUser
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE id <> ?`, deletedUserID).Scan(&total); err == nil && total == 0 {
		role = roleAdmin
	}
	if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash, created_at, role) VALUES (?,?,?,?,?)`,
		id, username, string(hash), createdAt, role); err != nil {
		s.internalErr(w, "insert user", err)
		return
	}
	if usingLink {
		if err := s.consumeInvite(inviteLink, id); err != nil {
			_, _ = s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
			noteAuthFailure(r)
			writeErr(w, http.StatusForbidden, "this invite link has expired or was already used")
			return
		}
	}
	noteRegistration(r)

	user := &User{ID: id, Username: username, Role: role}
	token, err := s.issueToken(user, time.Time{})
	if err != nil {
		s.internalErr(w, "sign token", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": token,
		"user":  publicUser{ID: id, Username: username, CreatedAt: createdAt, Role: role},
	})
}

var dummyHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)
	return h
}()

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if blocked, retry := rateLimitAuth(r); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many sign-in attempts, try again later")
		return
	}

	var req authRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))

	var (
		id        string
		hash      string
		created   string
		hasAvatar int
		role      string
	)
	err := s.db.QueryRow(`SELECT id, password_hash, created_at, has_avatar, role FROM users WHERE username = ? AND id <> ?`,
		username, deletedUserID).
		Scan(&id, &hash, &created, &hasAvatar, &role)
	if errors.Is(err, sql.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		noteAuthFailure(r)
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	} else if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		noteAuthFailure(r)
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	clearAuthFailures(r)

	token, err := s.issueToken(&User{ID: id, Username: username, Role: role}, time.Time{})
	if err != nil {
		s.internalErr(w, "sign token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  publicUser{ID: id, Username: username, CreatedAt: created, HasAvatar: hasAvatar == 1, Role: role},
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.publicUserByID(userFrom(r).ID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "user no longer exists")
		return
	} else if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

type MediaToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

func (s *Server) handleMediaToken(w http.ResponseWriter, r *http.Request) {
	tok, exp, err := s.issueMediaToken(userFrom(r))
	if err != nil {
		s.internalErr(w, "sign media token", err)
		return
	}
	writeJSON(w, http.StatusOK, MediaToken{Token: tok, ExpiresAt: exp.UTC().Format(time.RFC3339)})
}

func (s *Server) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	if err := s.bumpTokenVersion(userFrom(r).ID); err != nil {
		s.internalErr(w, "revoke tokens", err)
		return
	}
	writeNoContent(w)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var req changePasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeErr(w, http.StatusBadRequest, "currentPassword and newPassword are required")
		return
	}

	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, u.ID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "user no longer exists")
		return
	} else if err != nil {
		s.internalErr(w, "lookup password", err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.CurrentPassword)) != nil {
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}

	if len(req.NewPassword) < minPasswordLength {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("new password must be at least %d characters", minPasswordLength))
		return
	}
	if len(req.NewPassword) > 128 {
		writeErr(w, http.StatusBadRequest, "new password must be at most 128 characters")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		s.internalErr(w, "hash password", err)
		return
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ?, token_version = token_version + 1 WHERE id = ?`,
		string(newHash), u.ID); err != nil {
		s.internalErr(w, "update password", err)
		return
	}
	forgetAuthInfo(u.ID)

	token, err := s.issueToken(u, time.Time{})
	if err != nil {
		s.internalErr(w, "sign token", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) internalErr(w http.ResponseWriter, ctx string, err error) {
	logErr(ctx, err)
	writeErr(w, http.StatusInternalServerError, "internal server error")
}
