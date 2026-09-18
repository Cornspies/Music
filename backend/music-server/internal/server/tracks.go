package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const maxUploadBytes = 250 << 20 // 250 MB

// allowedImageTypes are the only content types served from the cover endpoint.
// SVG is deliberately absent: it is an executable document in a browser.
// safeCoverBytes reports whether embedded/remote artwork is a real raster
// image we are willing to store and serve back.
func safeCoverBytes(b []byte) bool {
        return len(b) > 0 && allowedImageTypes[http.DetectContentType(b)]
}

var allowedImageTypes = map[string]bool{
        "image/jpeg": true,
        "image/png":  true,
        "image/gif":  true,
        "image/webp": true,
        "image/bmp":  true,
}

// Track is the JSON representation of an uploaded track.
type Track struct {
        ID           string   `json:"id"`
        Title        string   `json:"title"`
        Artist       string   `json:"artist"`
        Album        string   `json:"album"`
        Genre        string   `json:"genre"`
        Year         int      `json:"year"`
        TrackNo      int      `json:"trackNo"`
        Duration     float64  `json:"duration"`
        MimeType     string   `json:"mimeType"`
        FileSize     int64    `json:"fileSize"`
        HasCover     bool     `json:"hasCover"`
        Plays        int      `json:"plays"`
        LastPlayedAt *string  `json:"lastPlayedAt"`
        IsFavorite   bool     `json:"isFavorite"`
        CreatedAt    string   `json:"createdAt"`
        // Uploader of the file. The library is shared between all users, so
        // every track carries who contributed it.
        UploadedBy Uploader `json:"uploadedBy"`
        // A second copy in a format every platform can play, made at upload
        // time for sources iOS can't decode. The original is always kept and
        // remains the default; clients that can't play mimeType should stream
        // with ?version=compatible.
        HasCompatible  bool   `json:"hasCompatible"`
        CompatibleMime string `json:"compatibleMimeType"`
        CompatibleSize int64  `json:"compatibleFileSize"`
}

// Uploader is the small public profile shown next to a track.
type Uploader struct {
        ID        string `json:"id"`
        Username  string `json:"username"`
        HasAvatar bool   `json:"hasAvatar"`
}

// supportedExtensions lists what uploads accept, derived from extMime so the
// message can never drift from the actual rule.
func supportedExtensions() string {
        exts := make([]string, 0, len(extMime))
        for ext := range extMime {
                exts = append(exts, strings.TrimPrefix(ext, "."))
        }
        sort.Strings(exts)
        return strings.Join(exts, ", ")
}

var extMime = map[string]string{
        ".mp3":   "audio/mpeg",
        ".m4a":   "audio/mp4",
        ".mp4":   "audio/mp4",
        ".aac":   "audio/aac",
        ".flac":  "audio/flac",
        ".wav":   "audio/wav",
        ".wave":  "audio/wav",
        ".ogg":   "audio/ogg",
        ".oga":   "audio/ogg",
        ".opus":  "audio/ogg",
        // .ogx is RFC 5334's "Ogg multiplexed", normally application/ogg. It is
        // served as audio/ogg because this is an audio player: browsers and
        // ExoPlayer both sniff the actual container, and declaring audio/ogg
        // means an <audio> element will play the audio stream of a file that
        // also happens to carry something else.
        ".ogx":   "audio/ogg",
        ".spx":   "audio/ogg",
        ".weba":  "audio/webm",
        ".webm":  "audio/webm",
        ".wma":   "audio/x-ms-wma",
        ".aiff":  "audio/aiff",
        ".aif":   "audio/aiff",
        ".alac":  "audio/mp4",
        ".m4b":   "audio/mp4",
}

// trackSelect carries two leading bind values, both the viewing user's id:
// the favourites EXISTS in the select list, then the play_stats join. Always
// build the argument list with trackSelectArgs so the order can't drift.
//
// plays / lastPlayedAt are per-listener (play_stats), not library-wide: the
// library is shared, but "your most played" should mean yours.
const trackSelect = `
SELECT t.id, t.title, t.artist, t.album, t.genre, t.year, t.track_no, t.duration,
       t.mime_type, t.file_size, t.has_cover,
       COALESCE(ps.plays, 0), ps.last_played_at,
       EXISTS(SELECT 1 FROM favorites f WHERE f.user_id = ? AND f.track_id = t.id),
       t.created_at, t.file_path, t.file_name,
       t.user_id, COALESCE(u.username, ''), COALESCE(u.has_avatar, 0),
       COALESCE(t.alt_path, ''), COALESCE(t.alt_mime, ''), COALESCE(t.alt_size, 0)
FROM tracks t
LEFT JOIN users u ON u.id = t.user_id
LEFT JOIN play_stats ps ON ps.track_id = t.id AND ps.user_id = ?`

// trackSelectArgs returns the leading bind values trackSelect expects.
// Append any further WHERE arguments after these.
func trackSelectArgs(userID string) []any { return []any{userID, userID} }

func scanTrack(scan func(dest ...any) error) (*Track, string, string, error) {
        var (
                t             Track
                filePath      string
                fileName      string
                hasCover      int
                isFavorite    int
                lastPlayedAt  sql.NullString
                uploaderAvatar int
                altPath        string
        )
        err := scan(
                &t.ID, &t.Title, &t.Artist, &t.Album, &t.Genre, &t.Year, &t.TrackNo, &t.Duration,
                &t.MimeType, &t.FileSize, &hasCover, &t.Plays, &lastPlayedAt, &isFavorite,
                &t.CreatedAt, &filePath, &fileName,
                &t.UploadedBy.ID, &t.UploadedBy.Username, &uploaderAvatar,
                &altPath, &t.CompatibleMime, &t.CompatibleSize,
        )
        if err != nil {
                return nil, "", "", err
        }
        t.HasCover = hasCover == 1
        t.IsFavorite = isFavorite == 1
        t.UploadedBy.HasAvatar = uploaderAvatar == 1
        t.HasCompatible = altPath != ""
        if lastPlayedAt.Valid && lastPlayedAt.String != "" {
                t.LastPlayedAt = &lastPlayedAt.String
        }
        return &t, filePath, fileName, nil
}

func (s *Server) getTrack(w http.ResponseWriter, _ *http.Request, userID, id string) (*Track, string, string, bool) {
        // The library is shared: any signed-in user can read any track.
        row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(userID), id)...)
        t, fp, fn, err := scanTrack(row.Scan)
        if errors.Is(err, sql.ErrNoRows) {
                writeErr(w, http.StatusNotFound, "track not found")
                return nil, "", "", false
        } else if err != nil {
                s.internalErr(w, "fetch track", err)
                return nil, "", "", false
        }
        return t, fp, fn, true
}

// ---- list ----

func (s *Server) handleListTracks(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        q := strings.TrimSpace(r.URL.Query().Get("q"))
        sort := r.URL.Query().Get("sort")

        order := "t.created_at DESC"
        switch sort {
        case "title":
                order = "t.title COLLATE NOCASE ASC"
        case "artist":
                order = "t.artist COLLATE NOCASE ASC, t.album COLLATE NOCASE ASC, t.track_no ASC"
        case "plays":
                order = "COALESCE(ps.plays, 0) DESC, t.created_at DESC"
        case "created":
                order = "t.created_at DESC"
        case "recent":
                // Most recently played first; never-played tracks last.
                order = "(ps.last_played_at IS NULL) ASC, ps.last_played_at DESC, t.created_at DESC"
        }

        query := trackSelect + ` WHERE 1 = 1`
        args := trackSelectArgs(u.ID)
        if q != "" {
                like := "%" + likeEscape(q) + "%"
                query += ` AND (t.title LIKE ? ESCAPE '\' OR t.artist LIKE ? ESCAPE '\' OR t.album LIKE ? ESCAPE '\')`
                args = append(args, like, like, like)
        }
        query += " ORDER BY " + order

        rows, err := s.db.Query(query, args...)
        if err != nil {
                s.internalErr(w, "list tracks", err)
                return
        }
        defer rows.Close()

        tracks := []*Track{}
        for rows.Next() {
                t, _, _, err := scanTrack(rows.Scan)
                if err != nil {
                        s.internalErr(w, "scan track", err)
                        return
                }
                tracks = append(tracks, t)
        }
        if err := rows.Err(); err != nil {
                s.internalErr(w, "iterate tracks", err)
                return
        }
        writeJSON(w, http.StatusOK, tracks)
}

func likeEscape(s string) string {
        s = strings.ReplaceAll(s, `\`, `\\`)
        s = strings.ReplaceAll(s, `%`, `\%`)
        s = strings.ReplaceAll(s, `_`, `\_`)
        return s
}

// ---- upload ----

func (s *Server) handleUploadTrack(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

        mr, err := r.MultipartReader()
        if err != nil {
                writeErr(w, http.StatusBadRequest, "expected multipart/form-data")
                return
        }

        var (
                savedPath string // relative path under dataDir
                fileName  string
                mimeType  string
                clientDur float64
        )

        for {
                part, err := mr.NextPart()
                if errors.Is(err, io.EOF) {
                        break
                } else if err != nil {
                        writeErr(w, http.StatusBadRequest, "malformed multipart body")
                        return
                }
                field := part.FormName()
                switch field {
                case "file":
                        if savedPath != "" {
                                writeErr(w, http.StatusBadRequest, "only one file per request")
                                return
                        }
                        fname := sanitizeFilename(filepath.Base(part.FileName()))
                        if fname == "" || fname == "." {
                                fname = "upload"
                        }
                        ext := strings.ToLower(filepath.Ext(fname))
                        mt, ok := extMime[ext]
                        if !ok {
                                writeErr(w, http.StatusBadRequest,
                                        "unsupported audio format "+ext+" (allowed: "+supportedExtensions()+")")
                                return
                        }
                        id := uuid.NewString()
                        rel := filepath.Join("uploads", id+ext)
                        dst, err := os.Create(filepath.Join(s.dataDir, rel))
                        if err != nil {
                                s.internalErr(w, "create file", err)
                                return
                        }
                        n, copyErr := io.Copy(dst, part)
                        closeErr := dst.Close()
                        if copyErr != nil || n == 0 {
                                os.Remove(filepath.Join(s.dataDir, rel))
                                if copyErr != nil {
                                        writeErr(w, http.StatusBadRequest, "failed reading file data")
                                        return
                                }
                                writeErr(w, http.StatusBadRequest, "uploaded file is empty")
                                return
                        }
                        if closeErr != nil {
                                os.Remove(filepath.Join(s.dataDir, rel))
                                writeErr(w, http.StatusInternalServerError, "failed storing file")
                                return
                        }
                        savedPath, fileName, mimeType = rel, fname, mt
                case "duration":
                        buf, _ := io.ReadAll(io.LimitReader(part, 32))
                        if d, err := strconv.ParseFloat(strings.TrimSpace(string(buf)), 64); err == nil && d > 0 && d < 100000 {
                                clientDur = d
                        }
                default:
                        // Drain unknown fields so multipart parsing stays valid.
                        _, _ = io.Copy(io.Discard, io.LimitReader(part, 1<<20))
                }
                part.Close()
        }

        if savedPath == "" {
                writeErr(w, http.StatusBadRequest, "missing 'file' field")
                return
        }

        absPath := filepath.Join(s.dataDir, savedPath)
        meta := readMetadata(absPath)
        candidates, appliedID := s.enrichMissingMetadata(&meta, fileName)

        id := strings.TrimSuffix(filepath.Base(savedPath), filepath.Ext(savedPath))
        // Keep the candidates around so the client can offer "not the right
        // song?" without re-querying every provider.
        cacheCandidates(id, candidates, appliedID)
        hasCover := false
        if len(meta.picture) > 0 {
                if !safeCoverBytes(meta.picture) {
                        logErr("cover art", fmt.Errorf("track %s: discarding embedded artwork that is not a raster image", id))
                } else if err := os.WriteFile(filepath.Join(s.covDir, id+".img"), meta.picture, 0o644); err == nil {
                        hasCover = true
                } else {
                        logErr("save cover", err)
                }
        }

        duration := meta.duration
        if duration <= 0 {
                duration = clientDur
        }

        title := trimDefault(meta.title, strings.TrimSuffix(fileName, filepath.Ext(fileName)))
        artist := trimDefault(meta.artist, "Unknown Artist")
        album := trimDefault(meta.album, "Unknown Album")
        createdAt := nowRFC3339()

        fi, _ := os.Stat(absPath)
        var size int64
        if fi != nil {
                size = fi.Size()
        }

        res, err := s.db.Exec(`INSERT INTO tracks
                (id, user_id, title, artist, album, genre, year, track_no, duration,
                 file_name, file_path, mime_type, file_size, has_cover, plays, created_at,
                 alt_path, alt_mime, alt_size)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?)`,
                id, u.ID, title, artist, album, meta.genre, meta.year, meta.trackNo, duration,
                fileName, savedPath, mimeType, size, hasCover, createdAt,
                "", "", 0)
        if err != nil {
                os.Remove(absPath)
                os.Remove(filepath.Join(s.covDir, id+".img"))
                s.internalErr(w, "insert track", err)
                return
        }
        if n, _ := res.RowsAffected(); n != 1 {
                os.Remove(absPath)
                writeErr(w, http.StatusInternalServerError, "failed to store track")
                return
        }

        t, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        writeJSON(w, http.StatusCreated, t)

        // Transcoding runs after the response, not before it. Converting a long
        // file takes minutes, and holding the upload open that long trips the
        // client's idle timeout — the upload appears to fail even though the
        // track was stored fine. The compatible copy shows up on the next
        // refresh instead.
        s.startCompatibleCopy(id, savedPath)
}

// transcodeSlots caps how many conversions run at once. ffmpeg is CPU-hungry
// and a bulk upload would otherwise start one per file and swamp the machine.
var transcodeSlots = make(chan struct{}, 2)

// startCompatibleCopy produces the widely-playable copy in the background and
// records it against the track when it is ready.
func (s *Server) startCompatibleCopy(trackID, originalRel string) {
        if !needsCompatibleCopy[strings.ToLower(filepath.Ext(originalRel))] {
                return
        }
        go func() {
                transcodeSlots <- struct{}{}
                defer func() { <-transcodeSlots }()

                alt := s.makeCompatibleCopy(trackID, originalRel)
                if alt == nil {
                        return
                }
                res, err := s.db.Exec(
                        `UPDATE tracks SET alt_path = ?, alt_mime = ?, alt_size = ? WHERE id = ?`,
                        alt.relPath, alt.mime, alt.size, trackID)
                if err != nil {
                        logErr("record compatible copy", err)
                        return
                }
                // The track may have been deleted while ffmpeg was running; if
                // so nothing references this file, so don't leave it behind.
                if n, _ := res.RowsAffected(); n == 0 {
                        os.Remove(filepath.Join(s.dataDir, alt.relPath))
                        return
                }
                log.Printf("transcode: compatible copy ready for track %s (%d bytes)", trackID, alt.size)
        }()
}

func sanitizeFilename(name string) string {
        name = strings.TrimSpace(name)
        // Control characters out.
        var b strings.Builder
        for _, c := range name {
                if c < 32 {
                        continue
                }
                b.WriteRune(c)
        }
        return b.String()
}

// ---- update / delete ----

func (s *Server) handleUpdateTrack(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        var body struct {
                Title  *string `json:"title"`
                Artist *string `json:"artist"`
                Album  *string `json:"album"`
                Genre  *string `json:"genre"`
                Year   *int    `json:"year"`
        }
        if err := jsonDecodeStrict(r, w, &body); err != nil {
                return
        }

        sets := []string{}
        args := []any{}
        if body.Title != nil {
                if strings.TrimSpace(*body.Title) == "" {
                        writeErr(w, http.StatusBadRequest, "title cannot be empty")
                        return
                }
                sets = append(sets, "title = ?")
                args = append(args, strings.TrimSpace(*body.Title))
        }
        if body.Artist != nil {
                sets = append(sets, "artist = ?")
                args = append(args, trimDefault(*body.Artist, "Unknown Artist"))
        }
        if body.Album != nil {
                sets = append(sets, "album = ?")
                args = append(args, trimDefault(*body.Album, "Unknown Album"))
        }
        if body.Genre != nil {
                sets = append(sets, "genre = ?")
                args = append(args, strings.TrimSpace(*body.Genre))
        }
        if body.Year != nil {
                if *body.Year < 0 || *body.Year > 3000 {
                        writeErr(w, http.StatusBadRequest, "invalid year")
                        return
                }
                sets = append(sets, "year = ?")
                args = append(args, *body.Year)
        }
        if len(sets) == 0 {
                writeErr(w, http.StatusBadRequest, "no fields to update")
                return
        }

        // Everyone can see every track, but only the uploader may edit it —
        // so "missing" and "not yours" are genuinely different answers now.
        var ownerID string
        switch err := s.db.QueryRow(`SELECT user_id FROM tracks WHERE id = ?`, id).Scan(&ownerID); {
        case errors.Is(err, sql.ErrNoRows):
                writeErr(w, http.StatusNotFound, "track not found")
                return
        case err != nil:
                s.internalErr(w, "check track", err)
                return
        case ownerID != u.ID:
                writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit it")
                return
        }

        query := `UPDATE tracks SET ` + strings.Join(sets, ", ") + ` WHERE id = ? AND user_id = ?`
        args = append(args, id, u.ID)
        if _, err := s.db.Exec(query, args...); err != nil {
                s.internalErr(w, "update track", err)
                return
        }
        t, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        t, filePath, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        if t.UploadedBy.ID != u.ID {
                writeErr(w, http.StatusForbidden, "only the person who uploaded this track can delete it")
                return
        }

        // Read the alternate path before the row goes: afterwards there is
        // nothing left to look it up from and the file would be orphaned.
        altPath := s.altPathFor(id)

        if _, err := s.db.Exec(`DELETE FROM tracks WHERE id = ? AND user_id = ?`, id, u.ID); err != nil {
                s.internalErr(w, "delete track", err)
                return
        }
        os.Remove(filepath.Join(s.dataDir, filePath))
        if altPath != "" {
                os.Remove(filepath.Join(s.dataDir, altPath))
        }
        if t.HasCover {
                os.Remove(filepath.Join(s.covDir, id+".img"))
        }
        writeNoContent(w)
}

// ---- streaming ----

func (s *Server) handleStreamTrack(w http.ResponseWriter, r *http.Request) {
        s.serveTrackFile(w, r, false)
}

func (s *Server) handleDownloadTrack(w http.ResponseWriter, r *http.Request) {
        s.serveTrackFile(w, r, true)
}

func (s *Server) serveTrackFile(w http.ResponseWriter, r *http.Request, download bool) {
        u := userFrom(r)
        id := r.PathValue("id")

        t, filePath, fileName, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }

        // The original is the default everywhere. A client that cannot decode
        // it (iOS with an Ogg file, say) asks for ?version=compatible and gets
        // the AAC copy made at upload time, when one exists.
        if wantsCompatible(r) && t.HasCompatible {
                if alt := s.altPathFor(id); alt != "" {
                        filePath = alt
                        t.MimeType = t.CompatibleMime
                        fileName = swapExtension(fileName, compatibleExt)
                }
        }

        f, err := os.Open(filepath.Join(s.dataDir, filePath))
        if err != nil {
                s.internalErr(w, "open track file", err)
                return
        }
        defer f.Close()

        if download {
                w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
        }
        w.Header().Set("Content-Type", t.MimeType)
        w.Header().Set("Cache-Control", "private, max-age=3600")
        // http.ServeContent handles Range / 206 / Content-Length / If-Range.
        http.ServeContent(w, r, fileName, timeZero, f)
}

// wantsCompatible reports whether the caller asked for the widely-playable
// copy. Anything other than "compatible" means the original, so a stray value
// can never silently downgrade what someone hears.
func wantsCompatible(r *http.Request) bool {
        return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("version")), "compatible")
}

// altPathFor reads the stored path of the compatible copy, or "" if there is
// none. Read from the database rather than derived from a naming convention so
// the two can never disagree.
func (s *Server) altPathFor(trackID string) string {
        var path string
        if err := s.db.QueryRow(`SELECT COALESCE(alt_path, '') FROM tracks WHERE id = ?`, trackID).
                Scan(&path); err != nil {
                logErr("lookup alternate audio", err)
                return ""
        }
        return path
}

// swapExtension renames a download so the file the user saves matches what is
// actually inside it.
func swapExtension(name, ext string) string {
        return strings.TrimSuffix(name, filepath.Ext(name)) + ext
}

func (s *Server) handleTrackCover(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")

        t, _, _, ok := s.getTrack(w, r, u.ID, id)
        if !ok {
                return
        }
        if !t.HasCover {
                writeErr(w, http.StatusNotFound, "no cover art")
                return
        }
        b, err := os.ReadFile(filepath.Join(s.covDir, id+".img"))
        if err != nil {
                s.internalErr(w, "read cover", err)
                return
        }
        // Cover bytes come from the uploader's file (an embedded ID3 picture)
        // or a remote provider, so they are untrusted. DetectContentType will
        // happily report text/html for HTML-shaped bytes, and this endpoint is
        // same-origin with the app, so serving that would be stored XSS against
        // every listener. Only recognised raster image types are served, and
        // nosniff stops the browser second-guessing us.
        ct := http.DetectContentType(b)
        if !allowedImageTypes[ct] {
                logErr("cover art", fmt.Errorf("track %s: refusing to serve cover of type %q", id, ct))
                writeErr(w, http.StatusNotFound, "no cover art")
                return
        }
        etag := `"cover-` + id + `"`
        w.Header().Set("Content-Type", ct)
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("Cache-Control", "private, max-age=86400")
        w.Header().Set("ETag", etag)
        if r.Header.Get("If-None-Match") == etag {
                w.WriteHeader(http.StatusNotModified)
                return
        }
        w.Header().Set("Content-Length", strconv.Itoa(len(b)))
        w.WriteHeader(http.StatusOK)
        if r.Method != http.MethodHead {
                _, _ = w.Write(b)
        }
}

func (s *Server) handleTrackPlay(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        id := r.PathValue("id")
        // Any user may play any track, but the count is theirs alone.
        var exists int
        if err := s.db.QueryRow(`SELECT COUNT(1) FROM tracks WHERE id = ?`, id).Scan(&exists); err != nil {
                s.internalErr(w, "check track", err)
                return
        }
        if exists == 0 {
                writeErr(w, http.StatusNotFound, "track not found")
                return
        }
        if _, err := s.db.Exec(`
                INSERT INTO play_stats (user_id, track_id, plays, last_played_at)
                VALUES (?, ?, 1, ?)
                ON CONFLICT(user_id, track_id)
                DO UPDATE SET plays = plays + 1, last_played_at = excluded.last_played_at`,
                u.ID, id, nowRFC3339()); err != nil {
                s.internalErr(w, "count play", err)
                return
        }
        writeNoContent(w)
}

func jsonDecodeStrict(r *http.Request, w http.ResponseWriter, v any) error {
        dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
        if err := dec.Decode(v); err != nil {
                writeErr(w, http.StatusBadRequest, "invalid JSON body")
                return err
        }
        return nil
}
