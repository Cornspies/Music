package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"music-server/internal/config"
)

// YouTube — through yt-dlp (https://github.com/yt-dlp/yt-dlp), which the
// server runs as a subprocess: one call searches, another downloads the
// audio stream of a video. It is off unless MUSIC_YTDLP names the binary,
// because it is a different kind of source from NCS: nothing on YouTube is
// offered for download, so keeping a copy of what one would otherwise
// stream is a choice for whoever runs the server, and against YouTube's
// terms. yt-dlp also breaks whenever YouTube changes its player, so it
// wants updating (`yt-dlp -U`), and current players need a JavaScript
// runtime on the server (Deno or Node; MUSIC_YTDLP_ARGS="--js-runtimes node").
//
// The audio comes as YouTube serves it — AAC in M4A at ~128 kbit/s when
// there is such a stream, Opus otherwise — never re-encoded. Tags are what
// the video offers: proper artist/album/year on the auto-generated "Topic"
// uploads, otherwise a guess split out of the video title, which is why the
// new track then goes on the metadata queue like an upload does.
//
// A pasted link is understood too: a video link searches for just that
// video, a playlist link lists the playlist, which the import (import.go)
// then fetches track by track into a playlist of the same name.

type ytdlpSource struct {
	bin   string
	extra []string // MUSIC_YTDLP_ARGS, appended to every call
}

const (
	ytdlpPageSize = 15
	// ytsearch has no paging; page n asks for n pages' worth and keeps the
	// last, so deep paging costs more each time. Four pages is plenty.
	ytdlpMaxPages = 4
	// Audio only, the AAC stream when there is one (plays everywhere without
	// a compatible copy), else the best there is.
	ytdlpDefaultFormat = "bestaudio[ext=m4a]/bestaudio/best"
	ytdlpMaxFileSize   = "200m" // keep in step with maxDiscoverBytes
)

// ytdlpSlots bounds concurrent downloads; each is a Python process plus a
// few MB/s of bandwidth.
var ytdlpSlots = make(chan struct{}, 2)

// newYTDLPSource reads MUSIC_YTDLP: unset means no YouTube source; a path
// or command name is looked up; a plain "1"/"true" means yt-dlp on PATH.
func newYTDLPSource() (*ytdlpSource, error) {
	raw := config.Value("MUSIC_YTDLP")
	if raw == "" {
		return nil, nil
	}
	name := raw
	if config.Flag("MUSIC_YTDLP") {
		name = "yt-dlp"
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("MUSIC_YTDLP=%q: %w", raw, err)
	}
	return &ytdlpSource{bin: bin, extra: strings.Fields(config.Value("MUSIC_YTDLP_ARGS"))}, nil
}

func (ytdlpSource) key() string  { return "youtube" }
func (ytdlpSource) name() string { return "YouTube" }

// coverAllowed limits the artwork proxy to YouTube's thumbnail hosts.
func (ytdlpSource) coverAllowed(u string) bool {
	return strings.HasPrefix(u, "https://i.ytimg.com/") || strings.HasPrefix(u, "https://i9.ytimg.com/")
}

// version is what `yt-dlp --version` prints, for the start-up log.
func (y ytdlpSource) version() string {
	out, err := exec.Command(y.bin, "--version").Output()
	if err != nil {
		return "unknown version"
	}
	return strings.TrimSpace(string(out))
}

// run executes yt-dlp with the standing options and returns stdout. stderr
// is folded into the error; on success its warnings (a missing JS runtime,
// say) are logged once per call so they reach whoever runs the server.
func (y ytdlpSource) run(ctx context.Context, what string, args ...string) ([]byte, error) {
	full := append([]string{"--ignore-config", "--no-playlist", "--no-progress"}, y.extra...)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, y.bin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("yt-dlp %s: %w", what, ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("yt-dlp %s: %s", what, ytdlpReason(stderr.String(), err))
	}
	if warning := ytdlpLine(stderr.String(), "WARNING:"); warning != "" {
		log.Printf("yt-dlp %s: %s", what, warning)
	}
	return stdout.Bytes(), nil
}

// ytdlpReason picks yt-dlp's own explanation out of stderr, which is more
// useful than "exit status 1".
func ytdlpReason(stderr string, err error) string {
	if line := ytdlpLine(stderr, "ERROR:"); line != "" {
		return line
	}
	return err.Error()
}

// ytdlpLine returns the last stderr line starting with prefix, without the
// prefix and yt-dlp's "[youtube] id:" tag, cut to a sensible length.
func ytdlpLine(stderr, prefix string) string {
	found := ""
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			found = strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	if i := strings.Index(found, "] "); i >= 0 && strings.HasPrefix(found, "[") {
		found = found[i+2:]
	}
	if i := strings.Index(found, ": "); i >= 0 && i < 14 && youtubeIDRe.MatchString(found[:i]) {
		found = found[i+2:]
	}
	if len(found) > 200 {
		found = found[:200] + "…"
	}
	return found
}

// ---- search --------------------------------------------------------------------

type ytdlpEntry struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Channel    string   `json:"channel"`
	Uploader   string   `json:"uploader"`
	Duration   *float64 `json:"duration"`
	LiveStatus string   `json:"live_status"`
}

func (y ytdlpSource) search(ctx context.Context, query string, page int) ([]discoverResult, bool, error) {
	if page > ytdlpMaxPages {
		return nil, false, nil
	}
	// A pasted video link: just that video. (Playlist links are the search
	// handler's business, since the answer carries the playlist's name.)
	if id := youtubeVideoID(query); id != "" && strings.Contains(query, "/") {
		if page > 1 {
			return nil, false, nil
		}
		out, err := y.run(ctx, "lookup", "--skip-download", "-j", "https://www.youtube.com/watch?v="+id)
		if err != nil {
			return nil, false, err
		}
		var e ytdlpEntry
		if err := json.Unmarshal(out, &e); err != nil {
			return nil, false, fmt.Errorf("yt-dlp lookup: %w", err)
		}
		if r, ok := y.result(e); ok {
			return []discoverResult{r}, false, nil
		}
		return []discoverResult{}, false, nil
	}

	want := ytdlpPageSize * page
	out, err := y.run(ctx, "search", "--flat-playlist", "--skip-download", "--dump-single-json",
		fmt.Sprintf("ytsearch%d:%s", want, query))
	if err != nil {
		return nil, false, err
	}
	var parsed struct {
		Entries []ytdlpEntry `json:"entries"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, false, fmt.Errorf("yt-dlp search: %w", err)
	}
	all := parsed.Entries
	if len(all) <= (page-1)*ytdlpPageSize {
		return nil, false, nil
	}
	results := []discoverResult{}
	for _, e := range all[(page-1)*ytdlpPageSize:] {
		if r, ok := y.result(e); ok {
			results = append(results, r)
		}
	}
	return results, len(all) >= want && page < ytdlpMaxPages, nil
}

// result shapes one listing entry; false for what can't be fetched (streams
// and premieres, and the private or deleted videos a playlist may hold).
func (y ytdlpSource) result(e ytdlpEntry) (discoverResult, bool) {
	title := strings.TrimSpace(e.Title)
	if !youtubeIDRe.MatchString(e.ID) || e.LiveStatus == "is_live" || e.LiveStatus == "is_upcoming" ||
		title == "[Private video]" || title == "[Deleted video]" {
		return discoverResult{}, false
	}
	r := discoverResult{
		Source: y.key(),
		ID:     e.ID,
		// Some playlist listings come without titles; the download itself
		// carries them, so the entry is still worth having.
		Title:   trimDefault(title, "YouTube video "+e.ID),
		Artist:  trimDefault(e.Channel, e.Uploader),
		PageURL: "https://www.youtube.com/watch?v=" + e.ID,
		// mqdefault is the 16:9 thumbnail without letterbox bars.
		CoverURL: "https://i.ytimg.com/vi/" + e.ID + "/mqdefault.jpg",
	}
	if e.Duration != nil {
		r.Duration = *e.Duration
	}
	return r, true
}

// ---- playlists ---------------------------------------------------------------

// ytdlpMaxPlaylist caps an import; YouTube playlists can hold 5,000.
const ytdlpMaxPlaylist = 500

var youtubePlaylistIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{2,}$`)

// youtubePlaylistID extracts the playlist id from a link: playlist?list=…,
// a watch link carrying list=…, or the same on music.youtube.com. Anything
// without a list is not a playlist link.
func youtubePlaylistID(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "list=") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	host = strings.TrimPrefix(host, "m.")
	host = strings.TrimPrefix(host, "music.")
	if host != "youtube.com" && host != "youtu.be" {
		return ""
	}
	id := u.Query().Get("list")
	if !youtubePlaylistIDRe.MatchString(id) {
		return ""
	}
	return id
}

func (y ytdlpSource) playlistID(raw string) string { return youtubePlaylistID(raw) }

// listPlaylist returns the playlist's title and its playable entries, in
// order. Mixes (list=RD…) are endless; yt-dlp returns their first stretch.
func (y ytdlpSource) listPlaylist(ctx context.Context, id string) (catalogPlaylist, error) {
	if !youtubePlaylistIDRe.MatchString(id) {
		return catalogPlaylist{}, &statusError{http.StatusBadRequest, "not a YouTube playlist id"}
	}
	out, err := y.run(ctx, "playlist", "--yes-playlist", "--flat-playlist", "--skip-download", "--dump-single-json",
		"--playlist-end", strconv.Itoa(ytdlpMaxPlaylist), "https://www.youtube.com/playlist?list="+id)
	if err != nil {
		return catalogPlaylist{}, &statusError{http.StatusBadGateway, ytdlpFriendly(err)}
	}
	var parsed struct {
		Title   string       `json:"title"`
		Count   int          `json:"playlist_count"`
		Entries []ytdlpEntry `json:"entries"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return catalogPlaylist{}, fmt.Errorf("yt-dlp playlist: %w", err)
	}
	pl := catalogPlaylist{Title: strings.TrimSpace(parsed.Title), Total: parsed.Count, Entries: []discoverResult{}}
	for _, e := range parsed.Entries {
		if r, ok := y.result(e); ok {
			pl.Entries = append(pl.Entries, r)
		}
	}
	return pl, nil
}

// ---- download ------------------------------------------------------------------

// ytdlpInfo is the part of the video's info JSON the library entry needs.
type ytdlpInfo struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Channel     string   `json:"channel"`
	Uploader    string   `json:"uploader"`
	Artist      string   `json:"artist"`
	Artists     []string `json:"artists"`
	Track       string   `json:"track"`
	Album       string   `json:"album"`
	ReleaseYear int      `json:"release_year"`
	UploadDate  string   `json:"upload_date"` // YYYYMMDD
	Duration    float64  `json:"duration"`
	Thumbnail   string   `json:"thumbnail"`
}

// tempDownload is the fetched file; closing it removes the directory it
// was downloaded into.
type tempDownload struct {
	*os.File
	dir string
}

func (t *tempDownload) Close() error {
	err := t.File.Close()
	os.RemoveAll(t.dir)
	return err
}

func (y ytdlpSource) fetch(ctx context.Context, id string) (*sourceDownload, error) {
	if !youtubeIDRe.MatchString(id) {
		return nil, &statusError{http.StatusBadRequest, "not a YouTube video id"}
	}
	select {
	case ytdlpSlots <- struct{}{}:
		defer func() { <-ytdlpSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	dir, err := os.MkdirTemp("", "music-ytdlp-")
	if err != nil {
		return nil, err
	}
	format := trimDefault(config.Value("MUSIC_YTDLP_FORMAT"), ytdlpDefaultFormat)
	out, err := y.run(ctx, "download",
		"--print-json", "-f", format, "--max-filesize", ytdlpMaxFileSize,
		"-o", filepath.Join(dir, "%(id)s.%(ext)s"),
		"https://www.youtube.com/watch?v="+id)
	if err != nil {
		os.RemoveAll(dir)
		return nil, &statusError{http.StatusBadGateway, ytdlpFriendly(err)}
	}
	var info ytdlpInfo
	if len(out) > 0 {
		if err := json.Unmarshal(out, &info); err != nil {
			logErr("yt-dlp info", err)
		}
	}

	matches, _ := filepath.Glob(filepath.Join(dir, id+".*"))
	if len(matches) == 0 {
		os.RemoveAll(dir)
		return nil, &statusError{http.StatusBadGateway, "yt-dlp didn't produce a file — the video may be longer than the size limit allows"}
	}
	f, err := os.Open(matches[0])
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	size := int64(-1)
	if fi, err := f.Stat(); err == nil {
		size = fi.Size()
	}
	ext := strings.ToLower(filepath.Ext(matches[0]))

	// Topic uploads carry real tags; everything else gets the title split
	// the way the metadata provider splits it ("Artist - Title (Official
	// Video)" → Artist / Title).
	title, artist := strings.TrimSpace(info.Track), ""
	if len(info.Artists) > 0 {
		artist = strings.TrimSpace(info.Artists[0])
	}
	artist = trimDefault(artist, info.Artist)
	if title == "" || artist == "" {
		a, t := parseYouTubeTitle(info.Title, trimDefault(info.Channel, info.Uploader))
		title, artist = trimDefault(title, t), trimDefault(artist, a)
	}
	year := info.ReleaseYear
	if year == 0 && len(info.UploadDate) >= 4 {
		year, _ = strconv.Atoi(info.UploadDate[:4])
	}
	cover := info.Thumbnail
	if cover == "" {
		cover = "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg"
	}
	return &sourceDownload{
		Body:     &tempDownload{File: f, dir: dir},
		FileName: sanitizeFilename(trimDefault(artist, "YouTube")+" - "+trimDefault(title, id)) + ext,
		Size:     size,
		Title:    title,
		Artist:   artist,
		Album:    strings.TrimSpace(info.Album),
		Year:     year,
		Duration: info.Duration,
		CoverURL: cover,
		// A video thumbnail is 16:9; the middle square is the cover (on
		// Topic uploads that is exactly the album art).
		SquareCover:    true,
		LookupMetadata: true,
	}, nil
}

// ytdlpFriendly turns yt-dlp's error text into something the person who
// clicked "Add" can act on.
func ytdlpFriendly(err error) string {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "the download took too long; try again"
	case strings.Contains(low, "sign in to confirm") || strings.Contains(low, "not a bot"):
		return "YouTube is asking this server to sign in (bot check); the person running it can pass cookies through MUSIC_YTDLP_ARGS"
	case strings.Contains(low, "private video"):
		return "that video is private"
	case strings.Contains(low, "age"):
		return "that video is age-restricted and can't be fetched without signing in"
	case strings.Contains(low, "playlist does not exist") || strings.Contains(low, "playlist doesn't exist"):
		return "that playlist doesn't exist or isn't public"
	case strings.Contains(low, "unavailable") || strings.Contains(low, "removed"):
		return "that video isn't available"
	case strings.Contains(low, "requested format is not available"):
		return "no audio stream was available for that video (yt-dlp may need updating or a JavaScript runtime)"
	}
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		msg = msg[i+2:]
	}
	return "couldn't fetch that video: " + msg
}

// squareCrop keeps the middle square of a wide image, re-encoded as JPEG.
// Anything that can't be decoded, or is square already, is returned as is.
func squareCrop(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width == cfg.Height || int64(cfg.Width)*int64(cfg.Height) > maxAvatarPixels {
		return raw
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	sub, ok := src.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return raw
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0 := b.Min.X + (b.Dx()-side)/2
	y0 := b.Min.Y + (b.Dy()-side)/2
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sub.SubImage(image.Rect(x0, y0, x0+side, y0+side)), &jpeg.Options{Quality: 90}); err != nil {
		return raw
	}
	return buf.Bytes()
}
