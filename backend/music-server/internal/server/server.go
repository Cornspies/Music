package server

import (
        "context"
        "crypto/rand"
        "database/sql"
        "encoding/json"
        "fmt"
        "log"
        "net/http"
        "os"
        "path/filepath"
        "strings"
        "time"

        "music-server/internal/config"
)

// Server holds all dependencies for the music API.
type Server struct {
        db      *sql.DB
        secret  []byte
        dataDir string
        upDir   string
        covDir  string
        avaDir  string
}

// Options carries settings that come from configuration rather than the
// data directory's own layout.
type Options struct {
        // JWTSecret, when non-empty, is used instead of the generated
        // <dataDir>/secret.txt. Must be at least 32 characters; only the first
        // 32 bytes are used, matching the generated secret's length.
        JWTSecret string
}

// New opens the database, applies migrations and prepares directories.
func New(dataDir string, opts Options) (*Server, error) {
        if err := os.MkdirAll(dataDir, 0o755); err != nil {
                return nil, err
        }
        upDir := filepath.Join(dataDir, "uploads")
        covDir := filepath.Join(dataDir, "covers")
        avaDir := filepath.Join(dataDir, "avatars")
        for _, d := range []string{upDir, covDir, avaDir} {
                if err := os.MkdirAll(d, 0o755); err != nil {
                        return nil, err
                }
        }

        db, err := openDB(filepath.Join(dataDir, "music.db"))
        if err != nil {
                return nil, err
        }

        // A configured secret wins, so instances can share one without
        // syncing secret.txt between them.
        var secret []byte
        if opts.JWTSecret != "" {
                if len(opts.JWTSecret) < 32 {
                        return nil, fmt.Errorf("configured JWT secret must be at least 32 characters")
                }
                secret = []byte(opts.JWTSecret)[:32]
        } else {
                secret, err = loadOrCreateSecret(filepath.Join(dataDir, "secret.txt"))
                if err != nil {
                        return nil, err
                }
        }

        return &Server{db: db, secret: secret, dataDir: dataDir, upDir: upDir, covDir: covDir, avaDir: avaDir}, nil
}

// Routes builds the HTTP handler with auth, CORS and logging.
func (s *Server) Routes() http.Handler {
        mux := http.NewServeMux()

        mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
                writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "music-server"})
        })
        mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
                writeJSON(w, http.StatusOK, map[string]any{
                        "service": "music-server",
                        "version": "1.0.0",
                })
        })

        // Auth (public)
        mux.HandleFunc("GET /api/auth/config", s.handleAuthConfig)
        mux.HandleFunc("GET /api/auth/invite/{code}", s.handleCheckInvite)
        mux.HandleFunc("POST /api/auth/register", s.handleRegister)
        mux.HandleFunc("POST /api/auth/login", s.handleLogin)
        mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))
        mux.HandleFunc("POST /api/auth/password", s.requireAuth(s.handleChangePassword))
        mux.HandleFunc("GET /api/auth/media-token", s.requireAuth(s.handleMediaToken))
        mux.HandleFunc("POST /api/auth/logout-all", s.requireAuth(s.handleLogoutAll))

        // Tracks
        mux.HandleFunc("GET /api/tracks", s.requireAuth(s.handleListTracks))
        mux.HandleFunc("POST /api/tracks", s.requireAuth(s.handleUploadTrack))
        mux.HandleFunc("PATCH /api/tracks/{id}", s.requireAuth(s.handleUpdateTrack))
        mux.HandleFunc("DELETE /api/tracks/{id}", s.requireAuth(s.handleDeleteTrack))
        // Media routes: fetched by <audio>/<img> and native players that can't
        // send headers, so these also accept the short-lived media token and
        // the ?token= query parameter. Nothing else does.
        mux.HandleFunc("GET /api/tracks/{id}/stream", s.requireMediaAuth(s.handleStreamTrack))
        mux.HandleFunc("GET /api/tracks/{id}/download", s.requireMediaAuth(s.handleDownloadTrack))
        mux.HandleFunc("GET /api/tracks/{id}/cover", s.requireMediaAuth(s.handleTrackCover))
        mux.HandleFunc("GET /api/tracks/{id}/waveform", s.requireAuth(s.handleTrackWaveform))
        mux.HandleFunc("GET /api/tracks/{id}/lyrics", s.requireAuth(s.handleTrackLyrics))
        mux.HandleFunc("GET /api/discover/sources", s.requireAuth(s.handleDiscoverSources))
        mux.HandleFunc("GET /api/discover/search", s.requireAuth(s.handleDiscoverSearch))
        mux.HandleFunc("POST /api/discover/add", s.requireAuth(s.handleDiscoverAdd))
        mux.HandleFunc("GET /api/discover/artwork", s.requireMediaAuth(s.handleDiscoverArtwork))
        mux.HandleFunc("PUT /api/tracks/{id}/lyrics", s.requireAuth(s.handleSetLyrics))
        mux.HandleFunc("GET /api/tracks/changes", s.requireAuth(s.handleTrackChanges))
        mux.HandleFunc("POST /api/uploads/pending/{id}", s.requireAuth(s.handleConfirmUpload))
        mux.HandleFunc("DELETE /api/uploads/pending/{id}", s.requireAuth(s.handleDiscardUpload))
        mux.HandleFunc("POST /api/tracks/{id}/play", s.requireAuth(s.handleTrackPlay))
        mux.HandleFunc("POST /api/tracks/{id}/listened", s.requireAuth(s.handleTrackListened))
        mux.HandleFunc("POST /api/auth/avatar", s.requireAuth(s.handleUploadAvatar))
        mux.HandleFunc("DELETE /api/auth/avatar", s.requireAuth(s.handleDeleteAvatar))
        mux.HandleFunc("GET /api/users/{id}/avatar", s.requireMediaAuth(s.handleUserAvatar))
        mux.HandleFunc("GET /api/tracks/{id}/metadata/candidates", s.requireAuth(s.handleTrackMetadataCandidates))
        mux.HandleFunc("POST /api/tracks/{id}/metadata/lookup", s.requireAuth(s.handleTrackMetadataLookup))
        mux.HandleFunc("GET /api/tracks/{id}/silence", s.requireAuth(s.handleTrackSilence))
        mux.HandleFunc("POST /api/tracks/{id}/trim", s.requireAuth(s.handleTrackTrim))
        mux.HandleFunc("POST /api/tracks/{id}/trim/restore", s.requireAuth(s.handleTrackRestore))
        mux.HandleFunc("POST /api/tracks/{id}/metadata/youtube", s.requireAuth(s.handleTrackMetadataFromYouTube))
        mux.HandleFunc("GET /api/metadata/queue", s.requireAuth(s.handleMetadataQueue))
        mux.HandleFunc("GET /api/tracks/{id}/metadata/candidates/{candidateId}/artwork", s.requireMediaAuth(s.handleCandidateArtwork))
        mux.HandleFunc("POST /api/tracks/{id}/metadata/apply", s.requireAuth(s.handleApplyTrackMetadata))

        // Listening history
        mux.HandleFunc("GET /api/history", s.requireAuth(s.handleHistory))

        // Admin
        mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleAdminListUsers))
        mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAdmin(s.handleAdminDeleteUser))
        mux.HandleFunc("PATCH /api/admin/users/{id}", s.requireAdmin(s.handleAdminUpdateUser))
        mux.HandleFunc("GET /api/admin/settings", s.requireAdmin(s.handleAdminGetSettings))
        mux.HandleFunc("PATCH /api/admin/settings", s.requireAdmin(s.handleAdminUpdateSettings))
        mux.HandleFunc("GET /api/admin/invites", s.requireAdmin(s.handleAdminListInvites))
        mux.HandleFunc("POST /api/admin/invites", s.requireAdmin(s.handleAdminCreateInvite))
        mux.HandleFunc("POST /api/admin/invites/{code}/revoke", s.requireAdmin(s.handleAdminRevokeInvite))
        mux.HandleFunc("DELETE /api/admin/invites/{code}", s.requireAdmin(s.handleAdminDeleteInvite))

        // Aggregates
        mux.HandleFunc("GET /api/albums", s.requireAuth(s.handleListAlbums))
        mux.HandleFunc("GET /api/artists", s.requireAuth(s.handleListArtists))

        // Playlists
        mux.HandleFunc("GET /api/playlists", s.requireAuth(s.handleListPlaylists))
        mux.HandleFunc("GET /api/playlists/public", s.requireAuth(s.handleListPublicPlaylists))
        mux.HandleFunc("POST /api/playlists", s.requireAuth(s.handleCreatePlaylist))
        mux.HandleFunc("GET /api/playlists/{id}", s.requireAuth(s.handleGetPlaylist))
        mux.HandleFunc("PATCH /api/playlists/{id}", s.requireAuth(s.handleUpdatePlaylist))
        mux.HandleFunc("DELETE /api/playlists/{id}", s.requireAuth(s.handleDeletePlaylist))
        mux.HandleFunc("POST /api/playlists/{id}/pin", s.requireAuth(s.handlePinPlaylist))
        mux.HandleFunc("DELETE /api/playlists/{id}/pin", s.requireAuth(s.handleUnpinPlaylist))
        mux.HandleFunc("POST /api/playlists/{id}/tracks", s.requireAuth(s.handleAddPlaylistTrack))
        mux.HandleFunc("DELETE /api/playlists/{id}/tracks/{trackId}", s.requireAuth(s.handleRemovePlaylistTrack))

        // Playback position ("continue listening")
        mux.HandleFunc("GET /api/playback", s.requireAuth(s.handleGetPlayback))
        mux.HandleFunc("PUT /api/playback", s.requireAuth(s.handlePutPlayback))

        // Favorites
        mux.HandleFunc("GET /api/favorites", s.requireAuth(s.handleListFavorites))
        mux.HandleFunc("POST /api/favorites/{trackId}", s.requireAuth(s.handleAddFavorite))
        mux.HandleFunc("DELETE /api/favorites/{trackId}", s.requireAuth(s.handleRemoveFavorite))

        // Search & stats
        mux.HandleFunc("GET /api/search", s.requireAuth(s.handleSearch))
        mux.HandleFunc("GET /api/stats", s.requireAuth(s.handleStats))

        return s.middleware(mux)
}

// middleware adds logging then CORS handling around the mux.
func (s *Server) middleware(next http.Handler) http.Handler {
        logMW := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                start := time.Now()
                sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
                next.ServeHTTP(sw, r)
                // Position saves arrive every few seconds from every playing client
                // and would drown everything else in the log; they're only logged
                // when something went wrong with one.
                positionSave := r.Method == http.MethodPut && r.URL.Path == "/api/playback" && sw.code < 400
                if r.URL.Path != "/healthz" && !positionSave {
                        log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.code, time.Since(start).Round(time.Millisecond))
                }
        })

        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                // Never let a browser second-guess a declared content type. The
                // cover endpoint serves bytes that originate in uploaded files.
                w.Header().Set("X-Content-Type-Options", "nosniff")

                // CORS is opt-in. The web app reaches the API through a
                // same-origin rewrite, so no cross-origin access is needed by
                // default; set MUSIC_CORS_ORIGINS (comma-separated, or "*") only
                // if you have another client that needs it.
                if origin := r.Header.Get("Origin"); origin != "" && corsAllows(origin) {
                        w.Header().Set("Access-Control-Allow-Origin", origin)
                        w.Header().Set("Vary", "Origin")
                        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
                        w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, If-None-Match")
                        w.Header().Set("Access-Control-Expose-Headers", "Content-Range, Accept-Ranges, Content-Length, Content-Type, ETag, X-Total-Count, "+refreshedTokenHeader)
                }
                if r.Method == http.MethodOptions {
                        w.WriteHeader(http.StatusNoContent)
                        return
                }
                logMW.ServeHTTP(w, r)
        })
}

// corsAllows reports whether an Origin is permitted by MUSIC_CORS_ORIGINS.
// Unset means "no cross-origin access", which is the right default for a
// server sitting behind the app's own reverse proxy.
func corsAllows(origin string) bool {
        raw := config.Value("MUSIC_CORS_ORIGINS")
        if raw == "" {
                return false
        }
        for _, allowed := range strings.Split(raw, ",") {
                allowed = strings.TrimSpace(allowed)
                if allowed == "*" || strings.EqualFold(allowed, origin) {
                        return true
                }
        }
        return false
}

type statusWriter struct {
        http.ResponseWriter
        code int
}

func (w *statusWriter) WriteHeader(code int) {
        w.code = code
        w.ResponseWriter.WriteHeader(code)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(code)
        if v != nil {
                _ = json.NewEncoder(w).Encode(v)
        }
}

func writeErr(w http.ResponseWriter, code int, msg string) {
        writeJSON(w, code, map[string]string{"error": msg})
}

func writeNoContent(w http.ResponseWriter) {
        w.WriteHeader(http.StatusNoContent)
}

// User is the authenticated principal stored in request context.
type User struct {
        ID       string
        Username string
        // Role is "user" or "admin", read from the database (not the token)
        // so a promotion takes effect on the next request.
        Role string
        // ExpiresAt is the "exp" claim of the token this request was
        // authenticated with. requireAuth uses it to decide when to hand the
        // client a freshly minted token (see sliding sessions in auth.go).
        ExpiresAt time.Time
        // OrigIssuedAt is when the session was originally signed in, carried
        // across refreshes so the absolute session ceiling holds.
        OrigIssuedAt time.Time
        // IsMediaToken is true when the request authenticated with a
        // short-lived media token rather than a session.
        IsMediaToken bool
}

// IsAdmin reports whether the user may manage the server.
func (u *User) IsAdmin() bool { return u != nil && u.Role == roleAdmin }

func logErr(ctx string, err error) {
        log.Printf("ERROR %s: %v", ctx, err)
}

type ctxKey int

const userKey ctxKey = 1

func userFrom(r *http.Request) *User {
        u, _ := r.Context().Value(userKey).(*User)
        return u
}

func withUser(r *http.Request, u *User) *http.Request {
        return r.WithContext(context.WithValue(r.Context(), userKey, u))
}

func validUsername(name string) bool {
        if len(name) < 3 || len(name) > 32 {
                return false
        }
        for _, c := range name {
                ok := c == '_' || c == '-' || c == '.' ||
                        (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
                if !ok {
                        return false
                }
        }
        return true
}

func nowRFC3339() string {
        return time.Now().UTC().Format(time.RFC3339)
}

// timeZero disables Last-Modified in http.ServeContent (we use Cache-Control + ETag).
var timeZero time.Time

func trimDefault(s, def string) string {
        s = strings.TrimSpace(s)
        if s == "" {
                return def
        }
        return s
}

func loadOrCreateSecret(path string) ([]byte, error) {
        if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
                return b[:32], nil
        }
        buf := make([]byte, 32)
        if _, err := rand.Read(buf); err != nil {
                return nil, err
        }
        hex := make([]byte, 64)
        const digits = "0123456789abcdef"
        for i, b := range buf {
                hex[i*2] = digits[b>>4]
                hex[i*2+1] = digits[b&0x0f]
        }
        if err := os.WriteFile(path, hex, 0o600); err != nil {
                return nil, err
        }
        return hex[:32], nil
}
