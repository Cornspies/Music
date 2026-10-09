package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"music-server/internal/config"
	"music-server/internal/deezer"
)

const (
	deezerPageSize = 25
)

var (
	deezerTrackLinkRe    = regexp.MustCompile(`deezer\.com/(?:[a-z]{2}(?:-[a-z]{2})?/)?track/(\d+)`)
	deezerAlbumLinkRe    = regexp.MustCompile(`deezer\.com/(?:[a-z]{2}(?:-[a-z]{2})?/)?album/(\d+)`)
	deezerPlaylistLinkRe = regexp.MustCompile(`deezer\.com/(?:[a-z]{2}(?:-[a-z]{2})?/)?playlist/(\d+)`)
	deezerIDRe           = regexp.MustCompile(`^\d{1,20}$`)
)

type deezerSource struct {
	client  *deezer.Client
	yt      *ytdlpSource
	dataDir string
}

func newDeezerSource(dataDir string, yt *ytdlpSource) *deezerSource {
	arl := config.Value("MUSIC_DEEZER_ARL")
	quality := config.Value("MUSIC_DEEZER_QUALITY")

	client, err := deezer.NewClient(arl, quality)
	if err != nil {
		logErr("deezer init", err)
	}

	return &deezerSource{
		client:  client,
		yt:      yt,
		dataDir: dataDir,
	}
}

func (d *deezerSource) describe() string {
	if d.client == nil {
		return "Deezer enabled (catalog search only)"
	}

	user := d.client.User()
	prefQuality := d.client.PreferredQuality()

	if user != nil {
		accType := "Premium/HiFi"
		if user.IsFreeAccount {
			accType = "Free"
		}
		return fmt.Sprintf("Deezer enabled (%s %s, signed in as %s)", prefQuality, accType, user.Name)
	}

	if d.client.HasARL() {
		return fmt.Sprintf("Deezer enabled (%s, authenticated)", prefQuality)
	}

	return fmt.Sprintf("Deezer enabled (%s, anonymous)", prefQuality)
}

func (d *deezerSource) key() string  { return "deezer" }
func (d *deezerSource) name() string { return "Deezer" }
func (d *deezerSource) takesLinks()  {}

func (d *deezerSource) coverAllowed(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "dzcdn.net" || strings.HasSuffix(host, ".dzcdn.net") ||
		host == "deezer.com" || strings.HasSuffix(host, ".deezer.com")
}

func (d *deezerSource) playlistID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "playlist:") || strings.HasPrefix(raw, "album:") {
		return raw
	}

	if m := deezerPlaylistLinkRe.FindStringSubmatch(raw); m != nil {
		return "playlist:" + m[1]
	}
	if m := deezerAlbumLinkRe.FindStringSubmatch(raw); m != nil {
		return "album:" + m[1]
	}

	// If numeric ID, treat as playlist ID
	if deezerIDRe.MatchString(raw) {
		return "playlist:" + raw
	}

	// Short links
	if strings.Contains(raw, "deezer.page.link") || strings.Contains(raw, "link.deezer.com") {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if kind, id, err := d.client.ParseURL(ctx, raw); err == nil {
			if kind == "playlist" || kind == "album" {
				return kind + ":" + id
			}
		}
	}

	return ""
}

func (d *deezerSource) listPlaylist(ctx context.Context, id string) (catalogPlaylist, error) {
	if d.client == nil {
		return catalogPlaylist{}, &statusError{http.StatusServiceUnavailable, "Deezer client is unavailable"}
	}

	if strings.HasPrefix(id, "album:") {
		albumID := strings.TrimPrefix(id, "album:")
		album, err := d.client.GetAlbum(ctx, albumID)
		if err != nil {
			return catalogPlaylist{}, fmt.Errorf("deezer album: %w", err)
		}

		entries := make([]discoverResult, len(album.Tracks))
		for i, t := range album.Tracks {
			entries[i] = deezerTrackToResult(t)
		}

		title := album.Title
		if album.Artist != "" {
			title = album.Artist + " - " + title
		}

		return catalogPlaylist{
			Title:   title,
			Total:   album.NbTracks,
			Entries: entries,
		}, nil
	}

	playlistID := strings.TrimPrefix(id, "playlist:")
	pl, err := d.client.GetPlaylist(ctx, playlistID)
	if err != nil {
		// Fallback: maybe it was an album ID without prefix
		if album, aerr := d.client.GetAlbum(ctx, playlistID); aerr == nil {
			entries := make([]discoverResult, len(album.Tracks))
			for i, t := range album.Tracks {
				entries[i] = deezerTrackToResult(t)
			}
			title := album.Title
			if album.Artist != "" {
				title = album.Artist + " - " + title
			}
			return catalogPlaylist{
				Title:   title,
				Total:   album.NbTracks,
				Entries: entries,
			}, nil
		}
		return catalogPlaylist{}, fmt.Errorf("deezer playlist: %w", err)
	}

	entries := make([]discoverResult, len(pl.Tracks))
	for i, t := range pl.Tracks {
		entries[i] = deezerTrackToResult(t)
	}

	return catalogPlaylist{
		Title:   pl.Title,
		Total:   pl.NbTracks,
		Entries: entries,
	}, nil
}

func deezerTrackToResult(t deezer.Track) discoverResult {
	cover := t.BestCoverURL()
	return discoverResult{
		Source:     "deezer",
		ID:         strconv.FormatInt(t.ID, 10),
		Title:      strings.TrimSpace(t.Title),
		Artist:     strings.TrimSpace(t.Artist),
		Album:      strings.TrimSpace(t.Album),
		Genre:      strings.TrimSpace(t.Genre),
		Year:       parseReleaseYear(t.ReleaseDate),
		Duration:   t.Duration,
		CoverURL:   cover,
		PageURL:    t.Link,
		PreviewURL: t.Preview,
	}
}

func (d *deezerSource) search(ctx context.Context, query string, page int) ([]discoverResult, bool, error) {
	if d.client == nil {
		return nil, false, &statusError{http.StatusServiceUnavailable, "Deezer client is unavailable"}
	}

	trimmed := strings.TrimSpace(query)

	// Check if query is a single track link
	if m := deezerTrackLinkRe.FindStringSubmatch(trimmed); m != nil {
		if page > 1 {
			return nil, false, nil
		}
		t, err := d.client.GetTrack(ctx, m[1])
		if err != nil {
			return nil, false, err
		}
		return []discoverResult{deezerTrackToResult(*t)}, false, nil
	}

	// Check for short link or other Deezer URLs
	if strings.Contains(trimmed, "deezer.com") || strings.Contains(trimmed, "deezer.page.link") {
		kind, id, err := d.client.ParseURL(ctx, trimmed)
		if err == nil {
			if page > 1 {
				return nil, false, nil
			}
			switch kind {
			case "track":
				t, err := d.client.GetTrack(ctx, id)
				if err != nil {
					return nil, false, err
				}
				return []discoverResult{deezerTrackToResult(*t)}, false, nil
			case "album":
				album, err := d.client.GetAlbum(ctx, id)
				if err != nil {
					return nil, false, err
				}
				results := make([]discoverResult, len(album.Tracks))
				for i, t := range album.Tracks {
					results[i] = deezerTrackToResult(t)
				}
				return results, false, nil
			case "playlist":
				pl, err := d.client.GetPlaylist(ctx, id)
				if err != nil {
					return nil, false, err
				}
				results := make([]discoverResult, len(pl.Tracks))
				for i, t := range pl.Tracks {
					results[i] = deezerTrackToResult(t)
				}
				return results, false, nil
			}
		}
	}

	// Standard text search
	index := (page - 1) * deezerPageSize
	tracks, total, hasNext, err := d.client.SearchTracks(ctx, query, deezerPageSize, index)
	if err != nil {
		return nil, false, err
	}

	_ = total
	results := make([]discoverResult, len(tracks))
	for i, t := range tracks {
		results[i] = deezerTrackToResult(t)
	}

	return results, hasNext, nil
}

func (d *deezerSource) fetch(ctx context.Context, id string) (*sourceDownload, error) {
	if !deezerIDRe.MatchString(id) {
		return nil, &statusError{http.StatusBadRequest, "not a Deezer track id"}
	}

	if d.client == nil {
		return nil, &statusError{http.StatusServiceUnavailable, "Deezer client is not configured"}
	}

	// 1. Direct native download from Deezer
	dl, dlErr := d.fetchDirect(ctx, id)
	if dlErr == nil {
		return dl, nil
	}

	logErr("deezer direct download", dlErr)

	// 2. Optional YouTube fallback if yt-dlp is available
	if d.yt != nil {
		ytDl, ytErr := d.fetchYTOldFallback(ctx, id)
		if ytErr == nil {
			return ytDl, nil
		}
		logErr("deezer youtube fallback", ytErr)
	}

	return nil, &statusError{
		code: http.StatusBadGateway,
		msg:  fmt.Sprintf("couldn't download from Deezer: %v", dlErr),
	}
}

func (d *deezerSource) fetchDirect(ctx context.Context, id string) (*sourceDownload, error) {
	tmpBase := ""
	if d.dataDir != "" {
		tmpBase = filepath.Join(d.dataDir, "tmp")
		if err := os.MkdirAll(tmpBase, 0o755); err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp(tmpBase, "music-deezer-")
	if err != nil {
		return nil, err
	}

	rawPath := filepath.Join(dir, "raw")
	actualQuality, ext, trackInfo, err := d.client.DownloadTrack(ctx, id, rawPath, maxDiscoverBytes)
	if err != nil {
		os.RemoveAll(dir)
		if errors.Is(err, deezer.ErrFileTooLarge) {
			return nil, &statusError{http.StatusBadGateway, "the file is larger than this server accepts"}
		}
		return nil, err
	}

	// Tag the file using ffmpeg if available
	bin := ffmpegBinary()
	finalPath := filepath.Join(dir, "audio"+ext)
	if bin != "" {
		args := []string{
			"-nostdin", "-loglevel", "error", "-y",
			"-i", rawPath,
		}
		if trackInfo.Title != "" {
			args = append(args, "-metadata", "title="+trackInfo.Title)
		}
		if trackInfo.Artist != "" {
			args = append(args, "-metadata", "artist="+trackInfo.Artist)
		}
		if trackInfo.Album != "" {
			args = append(args, "-metadata", "album="+trackInfo.Album)
		}
		if trackInfo.Genre != "" {
			args = append(args, "-metadata", "genre="+trackInfo.Genre)
		}
		if trackInfo.ReleaseDate != "" {
			args = append(args, "-metadata", "date="+trackInfo.ReleaseDate)
		}
		if trackInfo.TrackPosition > 0 {
			args = append(args, "-metadata", fmt.Sprintf("track=%d", trackInfo.TrackPosition))
		}
		if trackInfo.DiskNumber > 0 {
			args = append(args, "-metadata", fmt.Sprintf("disc=%d", trackInfo.DiskNumber))
		}
		args = append(args, "-c", "copy", finalPath)

		cmd := exec.CommandContext(ctx, bin, args...)
		if msg, cerr := cmd.CombinedOutput(); cerr != nil {
			logErr("deezer ffmpeg tag", fmt.Errorf("%v: %s", cerr, strings.TrimSpace(string(msg))))
			finalPath = rawPath + ext
			_ = os.Rename(rawPath, finalPath)
		} else {
			_ = os.Remove(rawPath)
		}
	} else {
		finalPath = rawPath + ext
		_ = os.Rename(rawPath, finalPath)
	}

	f, err := os.Open(finalPath)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	size := int64(-1)
	if fi, serr := f.Stat(); serr == nil {
		size = fi.Size()
		if size > maxDiscoverBytes {
			f.Close()
			os.RemoveAll(dir)
			return nil, &statusError{http.StatusBadGateway, "the file is larger than this server accepts"}
		}
	}

	title := strings.TrimSpace(trackInfo.Title)
	artist := strings.TrimSpace(trackInfo.Artist)
	album := strings.TrimSpace(trackInfo.Album)
	year := parseReleaseYear(trackInfo.ReleaseDate)

	_ = actualQuality

	return &sourceDownload{
		Body:           &tempDownload{File: f, dir: dir},
		FileName:       sanitizeFilename(artist+" - "+title) + ext,
		Size:           size,
		Title:          title,
		Artist:         artist,
		Album:          album,
		Genre:          trackInfo.Genre,
		Year:           year,
		TrackNo:        trackInfo.TrackPosition,
		Duration:       trackInfo.Duration,
		CoverURL:       trackInfo.BestCoverURL(),
		SquareCover:    false,
		CatalogueCover: true,
		LookupMetadata: false,
	}, nil
}

func (d *deezerSource) fetchYTOldFallback(ctx context.Context, id string) (*sourceDownload, error) {
	track, err := d.client.GetTrack(ctx, id)
	if err != nil {
		return nil, err
	}

	want := upgradeTrack{title: track.Title, artist: track.Artist, duration: track.Duration}
	var match *discoverResult

	for _, query := range []string{track.Artist + " " + track.Title, track.Artist + " " + track.Title + " audio"} {
		results, _, serr := d.yt.search(ctx, query, 1)
		if serr != nil {
			continue
		}
		for i := range results {
			if i < upgradeSearchHits && sameRecording(want, results[i]) {
				match = &results[i]
				break
			}
		}
		if match != nil {
			break
		}
	}

	if match == nil {
		return nil, errors.New("no matching recording on YouTube")
	}

	dl, err := d.yt.fetch(ctx, match.ID)
	if err != nil {
		return nil, err
	}

	title, artist := strings.TrimSpace(track.Title), strings.TrimSpace(track.Artist)
	dl.FileName = sanitizeFilename(artist+" - "+title) + filepath.Ext(dl.FileName)
	dl.Title, dl.Artist = title, artist
	dl.Album = trimDefault(strings.TrimSpace(track.Album), dl.Album)
	if year := parseReleaseYear(track.ReleaseDate); year > 0 {
		dl.Year = year
	}
	if track.Genre != "" {
		dl.Genre = track.Genre
	}
	dl.TrackNo = track.TrackPosition
	dl.Duration = track.Duration
	if cover := track.BestCoverURL(); cover != "" {
		dl.CoverURL, dl.SquareCover, dl.CatalogueCover = cover, false, true
	}
	dl.LookupMetadata = false
	return dl, nil
}

// bestQuality implements qualityProber.
func (d *deezerSource) bestQuality() audioQuality {
	if d.client == nil {
		return audioQuality{"mp3", 128}
	}
	switch d.client.PreferredQuality() {
	case deezer.QualityFLAC:
		return audioQuality{"flac", 1411}
	case deezer.QualityMP3_320:
		return audioQuality{"mp3", 320}
	default:
		return audioQuality{"mp3", 128}
	}
}

// probe implements qualityProber for Deezer tracks.
func (d *deezerSource) probe(ctx context.Context, id string) (audioQuality, error) {
	if !deezerIDRe.MatchString(id) {
		return audioQuality{}, fmt.Errorf("not a Deezer track id")
	}
	if d.client == nil {
		return audioQuality{"mp3", 128}, nil
	}

	trackData, err := d.client.GetTrackData(ctx, id)
	if err != nil {
		return audioQuality{}, err
	}

	_, actualQuality, err := d.client.GetTrackDownloadURL(ctx, trackData, d.client.PreferredQuality(), true)
	if err != nil {
		return audioQuality{}, err
	}

	switch actualQuality {
	case deezer.QualityFLAC:
		return audioQuality{"flac", 1411}, nil
	case deezer.QualityMP3_320:
		return audioQuality{"mp3", 320}, nil
	default:
		return audioQuality{"mp3", 128}, nil
	}
}
