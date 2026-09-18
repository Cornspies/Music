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

// refreshedTokenHeader carries a renewed token back to the client.
const refreshedTokenHeader = "X-Refreshed-Token"

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

type authRequest struct {
        Username string `json:"username"`
        Password string `json:"password"`
        // InviteCode is checked only when MUSIC_INVITE_CODE is configured.
        InviteCode string `json:"inviteCode"`
}

// minPasswordLength applies to new accounts and password changes. Existing
// shorter passwords keep working until they are changed.
const minPasswordLength = 8

// registrationOpen reports whether new accounts may be created. Defaults to
// open so a fresh install can create its first user; close it afterwards with
// MUSIC_ALLOW_REGISTRATION=0, or gate it with MUSIC_INVITE_CODE.
func registrationOpen() bool {
        switch strings.ToLower(strings.TrimSpace(config.Value("MUSIC_ALLOW_REGISTRATION"))) {
        case "0", "false", "no", "off":
                return false
        default:
                return true
        }
}

type publicUser struct {
        ID        string `json:"id"`
        Username  string `json:"username"`
        CreatedAt string `json:"createdAt"`
        HasAvatar bool   `json:"hasAvatar"`
}

func (s *Server) issueToken(u *User) (string, error) {
        claims := jwt.MapClaims{
                "sub": u.ID,
                "usr": u.Username,
                "iat": time.Now().Unix(),
                "exp": time.Now().Add(sessionTTL()).Unix(),
        }
        tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
        return tok.SignedString(s.secret)
}

// authUser extracts and validates the JWT from either the Authorization
// header or the "token" query parameter (used by <audio>/<img> elements).
func (s *Server) authUser(r *http.Request) (*User, error) {
        raw := ""
        if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
                raw = strings.TrimSpace(h[7:])
        }
        if raw == "" {
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
        var expiresAt time.Time
        if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
                expiresAt = exp.Time
        }
        return &User{ID: sub, Username: usr, ExpiresAt: expiresAt}, nil
}

func (s *Server) requireAuth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                u, err := s.authUser(r)
                if err != nil {
                        writeErr(w, http.StatusUnauthorized, "unauthorized")
                        return
                }
                s.maybeRefreshToken(w, u)
                next(w, withUser(r, u))
        }
}

// maybeRefreshToken slides the session forward. Once a token is past the
// halfway point of its life, the response carries a replacement that clients
// store in place of the old one. Refreshing on the second half rather than on
// every request keeps tokens stable enough to cache and avoids re-signing on
// every image and range request.
func (s *Server) maybeRefreshToken(w http.ResponseWriter, u *User) {
        if u.ExpiresAt.IsZero() {
                return
        }
        ttl := sessionTTL()
        if time.Until(u.ExpiresAt) > ttl/2 {
                return
        }
        fresh, err := s.issueToken(u)
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
                RegistrationOpen:  registrationOpen(),
                InviteRequired:    config.Value("MUSIC_INVITE_CODE") != "",
                MinPasswordLength: minPasswordLength,
        })
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
        // Registration is open only if you allow it. On a server shared with
        // friends you typically create the accounts once and then close it, or
        // hand out MUSIC_INVITE_CODE instead of leaving signup open to anyone
        // who finds the URL.
        if !registrationOpen() {
                writeErr(w, http.StatusForbidden, "registration is closed on this server")
                return
        }
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
        if code := config.Value("MUSIC_INVITE_CODE"); code != "" {
                if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.InviteCode)), []byte(code)) != 1 {
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
        if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash, created_at) VALUES (?,?,?,?)`,
                id, username, string(hash), createdAt); err != nil {
                s.internalErr(w, "insert user", err)
                return
        }

        user := &User{ID: id, Username: username}
        token, err := s.issueToken(user)
        if err != nil {
                s.internalErr(w, "sign token", err)
                return
        }
        writeJSON(w, http.StatusCreated, map[string]any{
                "token": token,
                "user":  publicUser{ID: id, Username: username, CreatedAt: createdAt},
        })
}

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
        )
        err := s.db.QueryRow(`SELECT id, password_hash, created_at, has_avatar FROM users WHERE username = ?`, username).
                Scan(&id, &hash, &created, &hasAvatar)
        if errors.Is(err, sql.ErrNoRows) {
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

        token, err := s.issueToken(&User{ID: id, Username: username})
        if err != nil {
                s.internalErr(w, "sign token", err)
                return
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "token": token,
                "user":  publicUser{ID: id, Username: username, CreatedAt: created, HasAvatar: hasAvatar == 1},
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
        writeJSON(w, http.StatusOK, publicUser{ID: u.ID, Username: u.Username, CreatedAt: created, HasAvatar: hasAvatar == 1})
}

type changePasswordRequest struct {
        CurrentPassword string `json:"currentPassword"`
        NewPassword     string `json:"newPassword"`
}

// handleChangePassword verifies the current password, then updates the
// stored bcrypt hash. The JWT stays valid: it only carries the user id and
// username, which do not change.
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
        if _, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(newHash), u.ID); err != nil {
                s.internalErr(w, "update password", err)
                return
        }
        writeNoContent(w)
}

func (s *Server) internalErr(w http.ResponseWriter, ctx string, err error) {
        logErr(ctx, err)
        writeErr(w, http.StatusInternalServerError, "internal server error")
}
