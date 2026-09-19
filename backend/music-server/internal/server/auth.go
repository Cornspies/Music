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

// defaultSessionDays is how long a token stays valid without being used.
// Sessions are sliding: every authenticated request that is past the halfway
// mark gets a fresh token back, so the window keeps moving forward and you
// stay signed in indefinitely as long as you open the app now and then.
// A token that is genuinely abandoned (or stolen and sat on) still expires.
const defaultSessionDays = 30

// defaultSessionMaxDays is the absolute ceiling on a session's life,
// counted from the original sign-in rather than the last refresh. Sliding
// alone would let a stolen token be kept alive forever by whoever holds it;
// this makes them sign in again eventually no matter what.
const defaultSessionMaxDays = 90

// mediaTokenTTL is the life of a media token: a short, read-only token that
// clients put in <audio>/<img> URLs instead of the session token, so a copied
// or logged URL is worth a few hours of listening rather than the whole
// account. Long enough that a track started near the end of one token's life
// can still seek (every range request re-authenticates with the same URL).
const mediaTokenTTL = 6 * time.Hour

// refreshedTokenHeader carries a renewed token back to the client.
const refreshedTokenHeader = "X-Refreshed-Token"

// Audience values telling the two token kinds apart.
const (
	audienceSession = "session"
	audienceMedia   = "media"
)

// Roles. Admins can manage users and runtime settings and may edit or
// delete anyone's track.
const (
	roleUser  = "user"
	roleAdmin = "admin"
)

// sessionTTL resolves MUSIC_SESSION_DAYS, falling back to the default.
// Set it to 0 or a negative number for a very long session (10 years), which
// is effectively "never sign me out".
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

// sessionMaxTTL resolves MUSIC_SESSION_MAX_DAYS: how long a session may be
// kept alive by refreshes before a real sign-in is required. 0 or negative
// disables the ceiling.
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
	Username string `json:"username"`
	Password string `json:"password"`
	// InviteCode is checked only when MUSIC_INVITE_CODE is configured.
	InviteCode string `json:"inviteCode"`
}

// minPasswordLength applies to new accounts and password changes. Existing
// shorter passwords keep working until they are changed.
const minPasswordLength = 8

// settingRegistrationOpen is the settings-table key an admin toggles.
const settingRegistrationOpen = "registration_open"

// registrationOpen reports whether new accounts may be created.
//
// Precedence: MUSIC_ALLOW_REGISTRATION when set (an explicit env value pins
// it and disables the admin toggle), then the admin's runtime setting, then a
// default that is open while the server has no accounts at all (so the first
// person can sign up) or an invite code is configured, and closed otherwise.
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
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n); err != nil {
		logErr("count users", err)
		return false
	}
	return n == 0
}

func inviteRequired() bool { return config.Value("MUSIC_INVITE_CODE") != "" }

// registrationPinnedByEnv is true when the environment decides and the admin
// toggle would have no effect.
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

// ---- token version / role cache ----
//
// Every request needs the user's current token version (to reject revoked
// tokens) and role. Both change rarely, so they're cached per user and the
// entry is dropped whenever this process changes either. Single-process by
// design: the SQLite backend already assumes one server per database.

type authInfo struct {
	version int
	role    string
}

var authInfoCache sync.Map // user id -> authInfo

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

// bumpTokenVersion invalidates every token the user currently holds.
func (s *Server) bumpTokenVersion(userID string) error {
	_, err := s.db.Exec(`UPDATE users SET token_version = token_version + 1 WHERE id = ?`, userID)
	forgetAuthInfo(userID)
	return err
}

// ---- token issue / verify ----

// issueToken mints a session token. origIAT is the original sign-in time
// carried across refreshes, so the absolute lifetime is measured from the
// real sign-in; zero means "this is a fresh sign-in".
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

// issueMediaToken mints the short-lived, read-only token used in media URLs.
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

// authUser extracts and validates the token from the Authorization header,
// or — on media routes only — the "token" query parameter that <audio>/<img>
// elements have to use. Media tokens are accepted only on media routes;
// session tokens work everywhere.
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
	// Tokens from before audiences existed carry none; treat them as
	// session tokens so nobody is signed out by the upgrade.
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
	// Tokens issued before versioning have no "ver" and are read as 0,
	// which matches the column default — so they stay valid until the user
	// changes their password or signs out everywhere.
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

// requireMediaAuth is requireAuth for routes that browsers and native players
// fetch without headers: these also accept media tokens and the ?token=
// query parameter.
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

// requireAdmin is requireAuth plus a role check.
func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != roleAdmin {
			writeErr(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r)
	})
}

// maybeRefreshToken slides the session forward. Once a token is past the
// halfway point of its life, the response carries a replacement that clients
// store in place of the old one. Refreshing on the second half rather than on
// every request keeps tokens stable enough to cache and avoids re-signing on
// every image and range request. The original sign-in time is carried
// across so the absolute ceiling still applies.
func (s *Server) maybeRefreshToken(w http.ResponseWriter, u *User) {
	if u.ExpiresAt.IsZero() {
		return
	}
	ttl := sessionTTL()
	if time.Until(u.ExpiresAt) > ttl/2 {
		return
	}
	// Once the absolute ceiling is what's limiting the token, a refresh
	// would only hand back the same expiry — don't re-sign on every request.
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

// AuthConfig tells a client what the sign-up form should look like before it
// asks the user for anything. Unauthenticated by design — it exposes only
// policy, never whether a particular username exists.
type AuthConfig struct {
	// RegistrationOpen is false when new accounts are disabled entirely.
	RegistrationOpen bool `json:"registrationOpen"`
	// InviteRequired is true when MUSIC_INVITE_CODE is set, so the client
	// should show an invite-code field.
	InviteRequired bool `json:"inviteRequired"`
	// MinPasswordLength lets clients validate before a round trip and keeps
	// their message in step with the server's rule.
	MinPasswordLength int `json:"minPasswordLength"`
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
	// A single-use invite link (see invites.go) is its holder's ticket in,
	// whatever the general policy says: it works while sign-ups are closed
	// and stands in for MUSIC_INVITE_CODE. It's only consumed once the
	// account has actually been created, so a typo in the password doesn't
	// burn the link.
	inviteLink := strings.TrimSpace(req.InviteCode)
	usingLink := s.invitePending(inviteLink)

	// Otherwise registration is open only if you allow it. On a server
	// shared with friends you typically create the accounts once and then
	// close it, or hand out MUSIC_INVITE_CODE instead of leaving signup open
	// to anyone who finds the URL.
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
	// The first account on a server is its admin.
	role := roleUser
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&total); err == nil && total == 0 {
		role = roleAdmin
	}
	if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash, created_at, role) VALUES (?,?,?,?,?)`,
		id, username, string(hash), createdAt, role); err != nil {
		s.internalErr(w, "insert user", err)
		return
	}
	if usingLink {
		// Two people racing on the same link: the second UPDATE matches no
		// row, and that account is rolled back.
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

// dummyHash is compared against when the username doesn't exist, so a
// missing account costs the same bcrypt work as a wrong password and the
// response time can't be used to enumerate usernames.
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
	err := s.db.QueryRow(`SELECT id, password_hash, created_at, has_avatar, role FROM users WHERE username = ?`, username).
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
	u := userFrom(r)
	var created string
	var hasAvatar int
	err := s.db.QueryRow(`SELECT created_at, has_avatar FROM users WHERE id = ?`, u.ID).Scan(&created, &hasAvatar)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "user no longer exists")
		return
	} else if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	writeJSON(w, http.StatusOK, publicUser{ID: u.ID, Username: u.Username, CreatedAt: created, HasAvatar: hasAvatar == 1, Role: u.Role})
}

// MediaToken is what GET /api/auth/media-token returns.
type MediaToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// handleMediaToken hands out a short-lived token for media URLs. Clients
// refresh it before it expires; it is never written to persistent storage
// and can't be used for anything but streaming, covers and avatars.
func (s *Server) handleMediaToken(w http.ResponseWriter, r *http.Request) {
	tok, exp, err := s.issueMediaToken(userFrom(r))
	if err != nil {
		s.internalErr(w, "sign media token", err)
		return
	}
	writeJSON(w, http.StatusOK, MediaToken{Token: tok, ExpiresAt: exp.UTC().Format(time.RFC3339)})
}

// handleLogoutAll signs the user out on every device by invalidating every
// token issued so far, including the one making this request.
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

// handleChangePassword verifies the current password, then updates the
// stored bcrypt hash. Every existing token is revoked — the whole point of
// changing a password is usually that someone else might have the old
// session — and a fresh one is returned so this client stays signed in.
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

	// Same rule as register.
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
