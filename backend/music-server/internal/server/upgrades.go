package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"music-server/internal/config"
)

const (
	upgradeMargin     = 1.25
	upgradeMaxErrors  = 5
	upgradeSearchHits = 6
	upgradeFetchLimit = 5 * time.Minute
)

type audioQuality struct {
	Codec string `json:"codec"`
	Kbps  int    `json:"kbps"`
}

var codecWeight = map[string]float64{
	"mp3":    1.0,
	"aac":    1.3,
	"vorbis": 1.3,
	"opus":   1.5,
}

func (q audioQuality) lossless() bool { return q.Codec == "flac" || q.Codec == "wav" }

func (q audioQuality) effective() float64 {
	if q.lossless() {
		return math.Inf(1)
	}
	w, ok := codecWeight[q.Codec]
	if !ok {
		w = 1
	}
	return float64(q.Kbps) * w
}

type qualityProber interface {
	probe(ctx context.Context, id string) (audioQuality, error)
	bestQuality() audioQuality
}

func (ncsSource) bestQuality() audioQuality { return audioQuality{"mp3", 128} }

func (ncsSource) probe(context.Context, string) (audioQuality, error) {
	return audioQuality{"mp3", 128}, nil
}

func (soundcloudSource) bestQuality() audioQuality { return audioQuality{"aac", 160} }

func (soundcloudSource) probe(ctx context.Context, id string) (audioQuality, error) {
	if !scTrackIDRe.MatchString(id) {
		return audioQuality{}, fmt.Errorf("not a SoundCloud track id")
	}
	var t scTrack
	if err := scAPI(ctx, scAPIBase+"/tracks/"+id, url.Values{}, &t); err != nil {
		return audioQuality{}, err
	}
	if why := t.unavailable(); why != "" {
		return audioQuality{}, errors.New(why)
	}
	if aac, _ := t.streams(); aac != nil {
		return audioQuality{"aac", 160}, nil
	}
	return audioQuality{"mp3", 128}, nil
}

func (*ytdlpSource) bestQuality() audioQuality { return audioQuality{"aac", 256} }

func (y *ytdlpSource) probe(ctx context.Context, id string) (audioQuality, error) {
	if !youtubeIDRe.MatchString(id) {
		return audioQuality{}, fmt.Errorf("not a YouTube video id")
	}
	format := trimDefault(config.Value("MUSIC_YTDLP_FORMAT"), ytdlpDefaultFormat)
	out, err := y.run(ctx, "probe", "--skip-download", "-j", "-f", format, "https://www.youtube.com/watch?v="+id)
	if err != nil {
		return audioQuality{}, err
	}
	var info struct {
		ABR    float64 `json:"abr"`
		TBR    float64 `json:"tbr"`
		ACodec string  `json:"acodec"`
		Ext    string  `json:"ext"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return audioQuality{}, fmt.Errorf("yt-dlp probe: %w", err)
	}
	kbps := info.ABR
	if kbps <= 0 {
		kbps = info.TBR
	}
	codec := strings.ToLower(info.ACodec)
	switch {
	case strings.HasPrefix(codec, "mp4a"), info.Ext == "m4a":
		codec = "aac"
	case strings.HasPrefix(codec, "opus"):
		codec = "opus"
	case strings.HasPrefix(codec, "vorbis"):
		codec = "vorbis"
	case strings.HasPrefix(codec, "mp3"):
		codec = "mp3"
	}
	if kbps <= 0 {
		return audioQuality{}, fmt.Errorf("yt-dlp didn't report a bitrate")
	}
	return audioQuality{codec, int(math.Round(kbps))}, nil
}

func fileQuality(mimeType, path string, size int64, duration float64) audioQuality {
	codec := ""
	switch {
	case strings.EqualFold(filepath.Ext(path), ".opus"), mimeType == "audio/webm":
		codec = "opus"
	case mimeType == "audio/mpeg":
		codec = "mp3"
	case mimeType == "audio/mp4", mimeType == "audio/aac":
		codec = "aac"
	case mimeType == "audio/ogg":
		codec = "vorbis"
	case mimeType == "audio/flac":
		codec = "flac"
	case mimeType == "audio/wav":
		codec = "wav"
	default:
		codec = strings.TrimPrefix(mimeType, "audio/")
	}
	kbps := 0
	if duration > 0 && size > 0 {
		kbps = int(math.Round(float64(size) * 8 / duration / 1000))
	}
	return audioQuality{codec, kbps}
}

type UpgradeCandidate struct {
	Track     Track          `json:"track"`
	Current   audioQuality   `json:"current"`
	Found     discoverResult `json:"found"`
	Quality   audioQuality   `json:"quality"`
	SourceKey string         `json:"source"`
}

type UpgradeStatus struct {
	Running    bool               `json:"running"`
	Source     string             `json:"source"`
	Total      int                `json:"total"`
	Done       int                `json:"done"`
	Skipped    int                `json:"skipped"`
	StartedAt  string             `json:"startedAt,omitempty"`
	FinishedAt string             `json:"finishedAt,omitempty"`
	Error      string             `json:"error,omitempty"`
	Candidates []UpgradeCandidate `json:"candidates"`
	Sources    []DiscoverSource   `json:"sources"`
}

var upgrades = struct {
	sync.Mutex
	status UpgradeStatus
	cancel context.CancelFunc
}{status: UpgradeStatus{Candidates: []UpgradeCandidate{}}}

func upgradeSources() []DiscoverSource {
	out := []DiscoverSource{}
	for _, src := range discoverSources {
		if _, ok := src.(qualityProber); ok {
			out = append(out, DiscoverSource{Key: src.key(), Name: src.name()})
		}
	}
	return out
}

func snapshotUpgrades() UpgradeStatus {
	upgrades.Lock()
	defer upgrades.Unlock()
	st := upgrades.status
	st.Candidates = append([]UpgradeCandidate{}, st.Candidates...)
	st.Sources = upgradeSources()
	return st
}

func (s *Server) handleUpgradeStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, snapshotUpgrades())
}

type upgradeTrack struct {
	id, title, artist, mimeType, path, source string
	size                                      int64
	duration                                  float64
}

func (s *Server) handleUpgradeScan(w http.ResponseWriter, r *http.Request) {
	if discoverDisabled() {
		writeErr(w, http.StatusNotFound, "discover is disabled on this server")
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	src := findSource(body.Source)
	prober, ok := src.(qualityProber)
	if src == nil || !ok {
		writeErr(w, http.StatusBadRequest, "that source can't be searched for better quality")
		return
	}

	rows, err := s.db.Query(`SELECT id, title, artist, mime_type, file_path, file_size, duration, source FROM tracks ORDER BY created_at`)
	if err != nil {
		s.internalErr(w, "list tracks", err)
		return
	}
	var todo []upgradeTrack
	skipped := 0
	ceiling := prober.bestQuality().effective()
	for rows.Next() {
		var t upgradeTrack
		if rows.Scan(&t.id, &t.title, &t.artist, &t.mimeType, &t.path, &t.size, &t.duration, &t.source) != nil {
			continue
		}
		q := fileQuality(t.mimeType, t.path, t.size, t.duration)
		if t.source == src.key() || q.effective()*upgradeMargin > ceiling {
			skipped++
			continue
		}
		todo = append(todo, t)
	}
	rows.Close()

	upgrades.Lock()
	if upgrades.status.Running {
		upgrades.Unlock()
		writeErr(w, http.StatusConflict, "a scan is already running")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	upgrades.cancel = cancel
	upgrades.status = UpgradeStatus{
		Running:    true,
		Source:     src.key(),
		Total:      len(todo),
		Skipped:    skipped,
		StartedAt:  nowRFC3339(),
		Candidates: []UpgradeCandidate{},
	}
	upgrades.Unlock()

	go s.runUpgradeScan(ctx, userFrom(r).ID, src, prober, todo)
	writeJSON(w, http.StatusAccepted, snapshotUpgrades())
}

func (s *Server) runUpgradeScan(ctx context.Context, viewerID string, src source, prober qualityProber, todo []upgradeTrack) {
	defer logPanic("upgrade scan")
	failures := 0
	finish := func(msg string) {
		upgrades.Lock()
		upgrades.status.Running = false
		upgrades.status.FinishedAt = nowRFC3339()
		upgrades.status.Error = msg
		upgrades.cancel = nil
		found := len(upgrades.status.Candidates)
		upgrades.Unlock()
		log.Printf("upgrades: scan on %s finished, %d better version(s) found", src.name(), found)
	}
	for _, t := range todo {
		if ctx.Err() != nil {
			finish("")
			return
		}
		cand, err := s.findUpgrade(ctx, viewerID, src, prober, t)
		if ctx.Err() != nil {
			finish("")
			return
		}
		if err != nil {
			failures++
			logErr("upgrade scan", fmt.Errorf("%s – %s: %w", t.artist, t.title, err))
			if failures >= upgradeMaxErrors {
				finish(src.name() + " stopped answering: " + err.Error())
				return
			}
		} else {
			failures = 0
		}
		upgrades.Lock()
		upgrades.status.Done++
		if cand != nil {
			upgrades.status.Candidates = append(upgrades.status.Candidates, *cand)
		}
		upgrades.Unlock()
		if src.key() != "youtube" {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
			}
		}
	}
	finish("")
}

func (s *Server) findUpgrade(ctx context.Context, viewerID string, src source, prober qualityProber, t upgradeTrack) (*UpgradeCandidate, error) {
	artist := dropPlaceholder(t.artist, "Unknown Artist")
	title := strings.TrimSpace(bracketRe.ReplaceAllString(t.title, ""))
	queries := []string{strings.TrimSpace(artist + " " + title)}
	if artist != "" && title != "" {
		queries = append(queries, title)
	}
	for _, query := range queries {
		match, err := s.searchRecording(ctx, src, t, query)
		if err != nil {
			return nil, err
		}
		if match == nil {
			continue
		}
		if _, taken := s.trackFromAnySource(src.key(), match.ID); taken {
			return nil, nil
		}
		probeCtx, cancel := context.WithTimeout(ctx, time.Minute)
		q, err := prober.probe(probeCtx, match.ID)
		cancel()
		if err != nil {
			return nil, err
		}
		current := fileQuality(t.mimeType, t.path, t.size, t.duration)
		if q.effective() < current.effective()*upgradeMargin {
			return nil, nil
		}
		row := s.db.QueryRow(trackSelect+` WHERE t.id = ?`, append(trackSelectArgs(viewerID), t.id)...)
		track, _, _, err := scanTrack(row.Scan)
		if err != nil {
			return nil, err
		}
		return &UpgradeCandidate{Track: *track, Current: current, Found: *match, Quality: q, SourceKey: src.key()}, nil
	}
	return nil, nil
}

func (s *Server) searchRecording(ctx context.Context, src source, t upgradeTrack, query string) (*discoverResult, error) {
	searchCtx, cancel := context.WithTimeout(ctx, time.Minute)
	results, _, err := src.search(searchCtx, query, 1)
	cancel()
	if err != nil {
		return nil, err
	}
	if len(results) > upgradeSearchHits {
		results = results[:upgradeSearchHits]
	}
	for i := range results {
		if sameRecording(t, results[i]) {
			return &results[i], nil
		}
	}
	return nil, nil
}

func (s *Server) trackFromAnySource(source, sourceID string) (string, bool) {
	var id string
	err := s.db.QueryRow(`SELECT id FROM tracks WHERE source = ? AND source_id = ?`, source, sourceID).Scan(&id)
	return id, err == nil
}

var variantWords = []string{
	"live", "remix", "cover", "sped up", "speed up", "slowed", "nightcore", "8d", "karaoke",
	"instrumental", "acoustic", "reverb", "bass boosted", "extended", "mashup", "reaction", "1 hour", "loop",
}

func hasWord(haystack, word string) bool {
	return strings.Contains(" "+haystack+" ", " "+word+" ")
}

func stripArtistPrefix(title string, artists ...string) string {
	for _, sep := range []string{" - ", " – ", " — ", " | "} {
		i := strings.Index(title, sep)
		if i <= 0 {
			continue
		}
		left := normalizeForMatch(title[:i])
		for _, a := range artists {
			a = normalizeForMatch(strings.TrimSuffix(strings.TrimSpace(a), " - Topic"))
			if a != "" && (strings.Contains(left, a) || strings.Contains(a, left)) {
				return strings.TrimSpace(title[i+len(sep):])
			}
		}
	}
	return title
}

func leadArtist(artist string) string {
	a := normalizeForMatch(featRe.ReplaceAllString(artist, ""))
	for _, sep := range []string{" x ", " and ", " with "} {
		if i := strings.Index(a, sep); i > 0 {
			a = a[:i]
		}
	}
	if i := strings.IndexAny(artist, ",&"); i > 0 {
		a = normalizeForMatch(artist[:i])
	}
	return strings.TrimSpace(a)
}

func sameRecording(t upgradeTrack, r discoverResult) bool {
	want := coreTitle(t.title)
	if want == "" {
		return false
	}
	got := coreTitle(stripArtistPrefix(r.Title, r.Artist, t.artist))
	if got != want && !(len(want) >= 4 && hasWord(got, want)) {
		return false
	}
	ownTitle := normalizeForMatch(t.title)
	theirTitle := normalizeForMatch(r.Title)
	for _, w := range variantWords {
		if hasWord(theirTitle, w) && !hasWord(ownTitle, w) {
			return false
		}
	}
	lengthKnown := t.duration > 0 && r.Duration > 0
	if lengthKnown && math.Abs(t.duration-r.Duration) > 3 {
		return false
	}
	artist := leadArtist(dropPlaceholder(t.artist, "Unknown Artist"))
	if artist == "" {
		return lengthKnown
	}
	return strings.Contains(normalizeForMatch(r.Artist+" "+r.Title), artist)
}

func takeCandidate(trackID string) (UpgradeCandidate, bool) {
	upgrades.Lock()
	defer upgrades.Unlock()
	for i, c := range upgrades.status.Candidates {
		if c.Track.ID == trackID {
			upgrades.status.Candidates = append(upgrades.status.Candidates[:i:i], upgrades.status.Candidates[i+1:]...)
			return c, true
		}
	}
	return UpgradeCandidate{}, false
}

func returnCandidate(c UpgradeCandidate) {
	upgrades.Lock()
	upgrades.status.Candidates = append(upgrades.status.Candidates, c)
	upgrades.Unlock()
}

func (s *Server) handleUpgradeApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID string `json:"trackId"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	c, ok := takeCandidate(body.TrackID)
	if !ok {
		writeErr(w, http.StatusNotFound, "that suggestion is gone; run the scan again")
		return
	}
	src := findSource(c.SourceKey)
	if src == nil {
		writeErr(w, http.StatusNotFound, "that source isn't available any more")
		return
	}
	u := userFrom(r)
	t, oldRel, _, ok := s.getTrack(w, r, u.ID, c.Track.ID)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), upgradeFetchLimit)
	defer cancel()
	dl, err := src.fetch(ctx, c.Found.ID)
	noteSource(src.key(), err)
	var status *statusError
	if err != nil {
		returnCandidate(c)
		logErr("upgrade fetch", err)
		if errors.As(err, &status) {
			writeErr(w, status.code, status.msg)
			return
		}
		writeErr(w, http.StatusBadGateway, "couldn't download from "+src.name()+"; try again later")
		return
	}
	defer dl.Body.Close()

	_, rel, mimeType, _, err := s.saveDownload(dl)
	if err != nil {
		returnCandidate(c)
		if errors.As(err, &status) {
			writeErr(w, status.code, status.msg)
			return
		}
		s.internalErr(w, "save upgrade", err)
		return
	}
	abs := filepath.Join(s.dataDir, rel)
	up := &pendingUpload{savedPath: rel, fileName: sanitizeFilename(dl.FileName), mimeType: mimeType, clientDur: dl.Duration, userID: u.ID}
	up.hash, _ = fileSHA256(abs)
	up.meta = readMetadata(abs)
	if err := s.replaceTrackAudio(t, oldRel, up); err != nil {
		os.Remove(abs)
		returnCandidate(c)
		s.internalErr(w, "replace track audio", err)
		return
	}
	if _, err := s.db.Exec(`UPDATE tracks SET source = ?, source_id = ? WHERE id = ?`, src.key(), c.Found.ID, t.ID); err != nil {
		logErr("record upgrade source", err)
	}
	out, _, _, ok := s.getTrack(w, r, u.ID, t.ID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpgradeDismiss(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TrackID string `json:"trackId"`
	}
	if err := jsonDecodeStrict(r, w, &body); err != nil {
		return
	}
	takeCandidate(body.TrackID)
	writeNoContent(w)
}

func (s *Server) handleUpgradeCancel(w http.ResponseWriter, _ *http.Request) {
	upgrades.Lock()
	if upgrades.cancel != nil {
		upgrades.cancel()
	}
	upgrades.Unlock()
	writeNoContent(w)
}
