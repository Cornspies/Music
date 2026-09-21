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
	"time"

	"music-server/internal/config"

	"github.com/google/uuid"
)

const maxUploadBytes = 512 << 20 // 512 MB

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
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Artist       string  `json:"artist"`
	Album        string  `json:"album"`
	Genre        string  `json:"genre"`
	Year         int     `json:"year"`
	TrackNo      int     `json:"trackNo"`
	Duration     float64 `json:"duration"`
	MimeType     string  `json:"mimeType"`
	FileSize     int64   `json:"fileSize"`
	HasCover     bool    `json:"hasCover"`
	Plays        int     `json:"plays"`
	LastPlayedAt *string `json:"lastPlayedAt"`
	IsFavorite   bool    `json:"isFavorite"`
	CreatedAt    string  `json:"createdAt"`
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
	// UpdatedAt changes whenever tags, cover or the compatible copy do,
	// so clients can sync deltas with GET /api/tracks/changes.
	UpdatedAt string `json:"updatedAt"`
	// Loudness is the integrated loudness in LUFS (EBU R128), for volume
	// normalisation. Null until the background analysis has run.
	Loudness *float64 `json:"loudness"`
	// Source and SourceID identify the catalogue a track was added from
	// via Discover ("ncs"); both empty for ordinary uploads.
	Source   string `json:"source"`
	SourceID string `json:"sourceId"`
	// CoverColor is a "#rrggbb" colour representative of the cover art,
	// "" until computed (or when there is no cover).
	CoverColor string `json:"coverColor"`
	// Trimmed is true when the audio was cut and the upload is kept for
	// "restore original".
	Trimmed bool `json:"trimmed"`
	// MetadataStatus is the online lookup's state for this track:
	// "queued", "running", "done", or "" when none was ever queued.
	MetadataStatus string `json:"metadataStatus"`
	// HasWaveform is true once GET /api/tracks/{id}/waveform has data.
	HasWaveform bool `json:"hasWaveform"`
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
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".mp4":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".wav":  "audio/wav",
	".wave": "audio/wav",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/ogg",
	// .ogx is RFC 5334's "Ogg multiplexed", normally application/ogg. It is
	// served as audio/ogg because this is an audio player: browsers and
	// ExoPlayer both sniff the actual container, and declaring audio/ogg
	// means an <audio> element will play the audio stream of a file that
	// also happens to carry something else.
	".ogx":  "audio/ogg",
	".spx":  "audio/ogg",
	".weba": "audio/webm",
	".webm": "audio/webm",
	".wma":  "audio/x-ms-wma",
	".aiff": "audio/aiff",
	".aif":  "audio/aiff",
	".alac": "audio/mp4",
	".m4b":  "audio/mp4",
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
       COALESCE(t.alt_path, ''), COALESCE(t.alt_mime, ''), COALESCE(t.alt_size, 0),
       COALESCE(t.updated_at, ''), t.loudness, (COALESCE(t.waveform_json, '') <> ''),
       COALESCE(t.source, ''), COALESCE(t.source_id, ''), COALESCE(t.cover_color, ''),
       COALESCE(mj.state, '')
FROM tracks t
LEFT JOIN users u ON u.id = t.user_id
LEFT JOIN play_stats ps ON ps.track_id = t.id AND ps.user_id = ?
LEFT JOIN metadata_jobs mj ON mj.track_id = t.id`

// trackSelectArgs returns the leading bind values trackSelect expects.
// Append any further WHERE arguments after these.
func trackSelectArgs(userID string) []any { return []any{userID, userID} }

func scanTrack(scan func(dest ...any) error) (*Track, string, string, error) {
	var (
		t              Track
		filePath       string
		fileName       string
		hasCover       int
		isFavorite     int
		lastPlayedAt   sql.NullString
		uploaderAvatar int
		altPath        string
		loudness       sql.NullFloat64
		hasWaveform    int
	)
	err := scan(
		&t.ID, &t.Title, &t.Artist, &t.Album, &t.Genre, &t.Year, &t.TrackNo, &t.Duration,
		&t.MimeType, &t.FileSize, &hasCover, &t.Plays, &lastPlayedAt, &isFavorite,
		&t.CreatedAt, &filePath, &fileName,
		&t.UploadedBy.ID, &t.UploadedBy.Username, &uploaderAvatar,
		&altPath, &t.CompatibleMime, &t.CompatibleSize,
		&t.UpdatedAt, &loudness, &hasWaveform,
		&t.Source, &t.SourceID, &t.CoverColor, &t.MetadataStatus,
	)
	if err != nil {
		return nil, "", "", err
	}
	if loudness.Valid {
		v := loudness.Float64
		t.Loudness = &v
	}
	t.HasWaveform = hasWaveform == 1
	t.HasCover = hasCover == 1
	t.IsFavorite = isFavorite == 1
	t.UploadedBy.HasAvatar = uploaderAvatar == 1
	t.HasCompatible = altPath != ""
	if lastPlayedAt.Valid && lastPlayedAt.String != "" {
		t.LastPlayedAt = &lastPlayedAt.String
	}
	t.Trimmed = trimmedTracks.has(t.ID)
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

	// Optional paging. Without ?limit the whole library comes back, as it
	// always has; with it, X-Total-Count says how many match in all so a
	// client can page without a second request.
	if limit, offset, ok := pageParams(r); ok {
		var total int
		countQuery := `SELECT COUNT(1) FROM tracks t WHERE 1 = 1`
		countArgs := []any{}
		if q != "" {
			like := "%" + likeEscape(q) + "%"
			countQuery += ` AND (t.title LIKE ? ESCAPE '\' OR t.artist LIKE ? ESCAPE '\' OR t.album LIKE ? ESCAPE '\')`
			countArgs = append(countArgs, like, like, like)
		}
		if err := s.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
			s.internalErr(w, "count tracks", err)
			return
		}
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}

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

// pageParams reads ?limit and ?offset. ok is false when no limit was given,
// meaning "everything". The limit is capped so a client can't ask for a
// million rows in one go by accident.
func pageParams(r *http.Request) (limit, offset int, ok bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, 0, false
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, 0, false
	}
	if limit > 1000 {
		limit = 1000
	}
	if o, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset"))); err == nil && o > 0 {
		offset = o
	}
	return limit, offset, true
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

	// Per-user storage cap, checked up front against what's already stored
	// so a full account is refused before 250 MB have been written to disk;
	// the exact check happens once the file size is known.
	if quota := userQuotaBytes(); quota > 0 {
		used, err := s.bytesUsedBy(u.ID)
		if err != nil {
			s.internalErr(w, "check quota", err)
			return
		}
		if used >= quota {
			writeErr(w, http.StatusInsufficientStorage,
				fmt.Sprintf("storage limit reached (%s of %s used)", humanBytes(used), humanBytes(quota)))
			return
		}
	}

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
	if quota := userQuotaBytes(); quota > 0 {
		used, _ := s.bytesUsedBy(u.ID)
		if fi, err := os.Stat(absPath); err == nil && used+fi.Size() > quota {
			os.Remove(absPath)
			writeErr(w, http.StatusInsufficientStorage,
				fmt.Sprintf("this file would exceed your storage limit (%s of %s used)", humanBytes(used), humanBytes(quota)))
			return
		}
	}

	up := &pendingUpload{savedPath: savedPath, fileName: fileName, mimeType: mimeType, clientDur: clientDur, userID: u.ID}
	up.hash, _ = fileSHA256(absPath)
	up.meta = readMetadata(absPath)

	// The same song may already be in the library — the identical file,
	// or another rip of it. Unless the client said "add anyway", hold the
	// file and let the person decide with the existing tracks in front
	// of them; nothing needs to be uploaded twice.
	if r.URL.Query().Get("force") == "" {
		if dup := s.findDuplicates(u.ID, up); len(dup.Matches) > 0 {
			dup.PendingID = s.holdUpload(up)
			writeJSON(w, http.StatusConflict, dup)
			return
		}
	}
	s.finalizeUpload(w, r, up)
}

// pendingUpload is a stored file that hasn't become a track yet: either
// about to (the normal path) or waiting for the uploader to confirm that a
// duplicate is wanted.
type pendingUpload struct {
	savedPath, fileName, mimeType string
	clientDur                     float64
	userID                        string
	hash                          string
	meta                          fileMetadata
	heldAt                        time.Time
}

// finalizeUpload turns a stored file into a library track and answers the
// request with it.
func (s *Server) finalizeUpload(w http.ResponseWriter, r *http.Request, up *pendingUpload) {
	u := userFrom(r)
	absPath := filepath.Join(s.dataDir, up.savedPath)
	meta := up.meta
	savedPath, fileName, mimeType, clientDur := up.savedPath, up.fileName, up.mimeType, up.clientDur
	id := strings.TrimSuffix(filepath.Base(savedPath), filepath.Ext(savedPath))
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
                 alt_path, alt_mime, alt_size, updated_at, file_hash)
                VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?,?,?)`,
		id, u.ID, title, artist, album, meta.genre, meta.year, meta.trackNo, duration,
		fileName, savedPath, mimeType, size, hasCover, createdAt,
		"", "", 0, createdAt, up.hash)
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

	// Whatever the file's tags left out is looked up online by the queue
	// worker, after the response: a bulk upload never waits on providers.
	if err := s.enqueueMetadataLookup(id, reasonUpload, u.ID); err != nil {
		logErr("queue metadata lookup", err)
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
	// refresh instead. Loudness/waveform analysis queues behind it.
	s.startCompatibleCopy(id, savedPath)
	s.queueAnalysis(id)
	if hasCover {
		go s.updateCoverColor(id)
	}
}

// userQuotaBytes resolves MUSIC_MAX_BYTES_PER_USER (accepts plain bytes or a
// suffix: 500M, 20G). 0 means unlimited, which is the default.
func userQuotaBytes() int64 {
	raw := strings.ToUpper(strings.TrimSpace(config.Value("MUSIC_MAX_BYTES_PER_USER")))
	if raw == "" {
		return 0
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(raw, "G"):
		mult, raw = 1<<30, strings.TrimSuffix(raw, "G")
	case strings.HasSuffix(raw, "M"):
		mult, raw = 1<<20, strings.TrimSuffix(raw, "M")
	case strings.HasSuffix(raw, "K"):
		mult, raw = 1<<10, strings.TrimSuffix(raw, "K")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n * mult
}

// bytesUsedBy sums the originals and compatible copies a user has uploaded.
func (s *Server) bytesUsedBy(userID string) (int64, error) {
	var used int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(file_size + COALESCE(alt_size, 0)), 0) FROM tracks WHERE user_id = ?`, userID).Scan(&used)
	return used, err
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(1<<20))
	default:
		return fmt.Sprintf("%d KB", n/1024)
	}
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
			`UPDATE tracks SET alt_path = ?, alt_mime = ?, alt_size = ?, updated_at = ? WHERE id = ?`,
			alt.relPath, alt.mime, alt.size, nowRFC3339(), trackID)
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

	// Everyone can see every track, but only the uploader (or an admin)
	// may edit it — so "missing" and "not yours" are genuinely different
	// answers now.
	var ownerID string
	switch err := s.db.QueryRow(`SELECT user_id FROM tracks WHERE id = ?`, id).Scan(&ownerID); {
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, http.StatusNotFound, "track not found")
		return
	case err != nil:
		s.internalErr(w, "check track", err)
		return
	case ownerID != u.ID && !u.IsAdmin():
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can edit it")
		return
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, nowRFC3339())
	query := `UPDATE tracks SET ` + strings.Join(sets, ", ") + ` WHERE id = ?`
	args = append(args, id)
	if _, err := s.db.Exec(query, args...); err != nil {
		s.internalErr(w, "update track", err)
		return
	}
	// Corrected tags are the usual reason a lyrics lookup missed.
	s.forgetLookedUpLyrics(id)
	t, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	id := r.PathValue("id")

	t, _, _, ok := s.getTrack(w, r, u.ID, id)
	if !ok {
		return
	}
	if t.UploadedBy.ID != u.ID && !u.IsAdmin() {
		writeErr(w, http.StatusForbidden, "only the person who uploaded this track can delete it")
		return
	}
	if err := s.removeTrack(id); err != nil {
		s.internalErr(w, "delete track", err)
		return
	}
	writeNoContent(w)
}

// removeTrack deletes a track row and every file behind it, leaving a
// tombstone so syncing clients learn it's gone. Authorisation is the
// caller's job.
func (s *Server) removeTrack(id string) error {
	// Read the paths before the row goes: afterwards there is nothing
	// left to look them up from and the files would be orphaned.
	var filePath, altPath string
	var hasCover int
	err := s.db.QueryRow(`SELECT file_path, COALESCE(alt_path, ''), has_cover FROM tracks WHERE id = ?`, id).
		Scan(&filePath, &altPath, &hasCover)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO deleted_tracks (id, deleted_at) VALUES (?, ?)`, id, nowRFC3339()); err != nil {
		logErr("tombstone", err)
	}
	os.Remove(filepath.Join(s.dataDir, filePath))
	os.Remove(filepath.Join(s.dataDir, filePath) + ".orig")
	if altPath != "" {
		os.Remove(filepath.Join(s.dataDir, altPath))
	}
	if hasCover == 1 {
		os.Remove(filepath.Join(s.covDir, id+".img"))
	}
	return nil
}

// ---- delta sync ----

// TrackChanges is what GET /api/tracks/changes?since=<RFC3339> returns: every
// track created or modified after the cut-off, the ids of tracks deleted
// since, and the server time to pass back as the next "since".
type TrackChanges struct {
	Tracks  []*Track `json:"tracks"`
	Deleted []string `json:"deleted"`
	Now     string   `json:"now"`
}

func (s *Server) handleTrackChanges(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	since := strings.TrimSpace(r.URL.Query().Get("since"))
	if since != "" {
		if _, err := time.Parse(time.RFC3339, since); err != nil {
			writeErr(w, http.StatusBadRequest, "since must be an RFC 3339 timestamp")
			return
		}
	}
	// Taken before the queries so anything written during them is
	// reported again next time rather than lost in the gap.
	now := nowRFC3339()

	rows, err := s.db.Query(trackSelect+` WHERE t.updated_at > ? ORDER BY t.updated_at ASC`,
		append(trackSelectArgs(u.ID), since)...)
	if err != nil {
		s.internalErr(w, "list changes", err)
		return
	}
	defer rows.Close()
	changes := TrackChanges{Tracks: []*Track{}, Deleted: []string{}, Now: now}
	for rows.Next() {
		t, _, _, err := scanTrack(rows.Scan)
		if err != nil {
			s.internalErr(w, "scan change", err)
			return
		}
		changes.Tracks = append(changes.Tracks, t)
	}
	if err := rows.Err(); err != nil {
		s.internalErr(w, "iterate changes", err)
		return
	}

	if since != "" {
		del, err := s.db.Query(`SELECT id FROM deleted_tracks WHERE deleted_at > ?`, since)
		if err != nil {
			s.internalErr(w, "list deleted", err)
			return
		}
		defer del.Close()
		for del.Next() {
			var id string
			if err := del.Scan(&id); err == nil {
				changes.Deleted = append(changes.Deleted, id)
			}
		}
	}
	writeJSON(w, http.StatusOK, changes)
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
	// The ETag carries updated_at, which moves whenever art is replaced,
	// so a match can be answered before the file is even opened — which is
	// most cover requests once the list has been scrolled once.
	etag := `"cover-` + id + `-` + t.UpdatedAt + `"`
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
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
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	s.recordPlay(u.ID, id)
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
