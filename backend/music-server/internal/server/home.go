package server

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	artistInfoFresh   = 14 * 24 * time.Hour
	artistInfoPerCall = 20
	homeHistoryDays   = 180
	mixCount          = 6
	mixMaxTracks      = 35
	mixMinTracks      = 8
	sessionGap        = 30 * time.Minute
)

type relatedArtist struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
}

type artistInfo struct {
	name    string
	id      int64
	picture string
	related []relatedArtist
}

var artistInfoQueue = struct {
	sync.Mutex
	pending map[string]string
	running bool
}{pending: map[string]string{}}

func (s *Server) loadArtistInfo() map[string]artistInfo {
	out := map[string]artistInfo{}
	rows, err := s.db.Query(`SELECT artist_key, name, deezer_id, picture, related_json FROM artist_info`)
	if err != nil {
		logErr("load artist info", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var key, rel string
		var info artistInfo
		if rows.Scan(&key, &info.name, &info.id, &info.picture, &rel) != nil {
			continue
		}
		_ = json.Unmarshal([]byte(rel), &info.related)
		out[key] = info
	}
	return out
}

func (s *Server) wantArtistInfo(names []string) bool {
	if metadataLookupDisabled() || len(names) == 0 {
		return false
	}
	cutoff := time.Now().Add(-artistInfoFresh).UTC().Format(time.RFC3339)
	queued := false
	artistInfoQueue.Lock()
	defer artistInfoQueue.Unlock()
	for _, name := range names {
		key := artistKey(name)
		if key == "" {
			continue
		}
		if _, ok := artistInfoQueue.pending[key]; ok {
			queued = true
			continue
		}
		var fetched string
		err := s.db.QueryRow(`SELECT fetched_at FROM artist_info WHERE artist_key = ?`, key).Scan(&fetched)
		if err == nil && fetched > cutoff {
			continue
		}
		artistInfoQueue.pending[key] = name
		queued = true
	}
	if queued && !artistInfoQueue.running && len(artistInfoQueue.pending) > 0 {
		artistInfoQueue.running = true
		go s.runArtistInfo()
	}
	return queued
}

func (s *Server) runArtistInfo() {
	defer logPanic("artist info")
	for {
		artistInfoQueue.Lock()
		key, name := "", ""
		for k, n := range artistInfoQueue.pending {
			key, name = k, n
			break
		}
		if key == "" {
			artistInfoQueue.running = false
			artistInfoQueue.Unlock()
			return
		}
		artistInfoQueue.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		info, err := fetchArtistInfo(ctx, name)
		cancel()
		if err != nil {
			logErr("artist info for "+name, err)
		} else {
			rel, _ := json.Marshal(info.related)
			if _, err := s.db.Exec(`INSERT INTO artist_info (artist_key, name, deezer_id, picture, related_json, fetched_at)
				VALUES (?,?,?,?,?,?)
				ON CONFLICT(artist_key) DO UPDATE SET name = excluded.name, deezer_id = excluded.deezer_id,
					picture = excluded.picture, related_json = excluded.related_json, fetched_at = excluded.fetched_at`,
				key, info.name, info.id, info.picture, string(rel), nowRFC3339()); err != nil {
				logErr("save artist info", err)
			}
		}
		artistInfoQueue.Lock()
		delete(artistInfoQueue.pending, key)
		artistInfoQueue.Unlock()
		if err != nil && metadataLookupDisabled() {
			artistInfoQueue.Lock()
			clear(artistInfoQueue.pending)
			artistInfoQueue.running = false
			artistInfoQueue.Unlock()
			return
		}
	}
}

func fetchArtistInfo(ctx context.Context, name string) (artistInfo, error) {
	var found struct {
		Data []dzpubArtist `json:"data"`
	}
	if err := dzpubGet(ctx, "/search/artist?limit=5&q="+url.QueryEscape(name), &found); err != nil {
		return artistInfo{}, err
	}
	info := artistInfo{name: name, related: []relatedArtist{}}
	want := artistKey(name)
	for _, a := range found.Data {
		if artistKey(a.Name) == want {
			info.id, info.name, info.picture = a.ID, a.Name, a.PictureBig
			break
		}
	}
	if info.id == 0 {
		return info, nil
	}
	var related struct {
		Data []dzpubArtist `json:"data"`
	}
	if err := dzpubGet(ctx, "/artist/"+strconv.FormatInt(info.id, 10)+"/related?limit="+strconv.Itoa(artistInfoPerCall), &related); err != nil {
		return artistInfo{}, err
	}
	for _, a := range related.Data {
		info.related = append(info.related, relatedArtist{ID: a.ID, Name: a.Name, Picture: a.PictureBig})
	}
	return info, nil
}

type homeTrack struct {
	id        string
	artists   []string
	genre     string
	createdAt string
	hasCover  bool
	plays     int
	fav       bool
	lastHeard time.Time
}

type homeArtist struct {
	key      string
	name     string
	tracks   []*homeTrack
	score    float64
	listens  int
	spelling map[string]int
}

type homeData struct {
	tracks   map[string]*homeTrack
	artists  map[string]*homeArtist
	events   []homeEvent
	related  map[string]map[string]float64
	info     map[string]artistInfo
	coPlay   map[string]map[string]int
	now      time.Time
	userID   string
	daySeed  string
	maxScore float64
}

type homeEvent struct {
	track    *homeTrack
	at       time.Time
	listened float64
}

type HomeMix struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle"`
	Seed          string   `json:"seed,omitempty"`
	TrackIDs      []string `json:"trackIds"`
	CoverTrackIDs []string `json:"coverTrackIds"`
}

type HomeArtist struct {
	Name         string `json:"name"`
	TrackCount   int    `json:"trackCount"`
	Listens      int    `json:"listens"`
	CoverTrackID string `json:"coverTrackId,omitempty"`
	Picture      string `json:"picture,omitempty"`
}

type HomeSuggestion struct {
	Name       string `json:"name"`
	DeezerID   int64  `json:"deezerId,omitempty"`
	Picture    string `json:"picture,omitempty"`
	TrackCount int    `json:"trackCount"`
	Because    string `json:"because"`
}

type HomeSimilar struct {
	TrackIDs []string `json:"trackIds"`
	BasedOn  []string `json:"basedOn"`
}

type HomeFeed struct {
	Mixes     []HomeMix              `json:"mixes"`
	Top       []HomeArtist           `json:"topArtists"`
	Suggested []HomeSuggestion       `json:"suggestedArtists"`
	Similar   map[string]HomeSimilar `json:"similar"`
	Pending   bool                   `json:"pending"`
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	d, err := s.loadHomeData(u.ID)
	if err != nil {
		s.internalErr(w, "home", err)
		return
	}
	top := d.rankedArtists()
	seeds := make([]string, 0, 12)
	for _, a := range top {
		if len(seeds) == cap(seeds) {
			break
		}
		seeds = append(seeds, a.name)
	}
	pending := s.wantArtistInfo(seeds)
	d.info = s.loadArtistInfo()
	d.buildRelated()

	feed := HomeFeed{
		Mixes:     d.mixes(top),
		Top:       d.topArtists(top),
		Suggested: d.suggestions(top),
		Similar: map[string]HomeSimilar{
			"week":  d.similar(7),
			"month": d.similar(30),
		},
		Pending: pending,
	}
	writeJSON(w, http.StatusOK, feed)
}

func (s *Server) loadHomeData(userID string) (*homeData, error) {
	now := time.Now().UTC()
	d := &homeData{
		tracks:  map[string]*homeTrack{},
		artists: map[string]*homeArtist{},
		related: map[string]map[string]float64{},
		coPlay:  map[string]map[string]int{},
		now:     now,
		userID:  userID,
		daySeed: now.Format("2006-01-02"),
	}
	rows, err := s.db.Query(`SELECT t.id, t.artist, t.genre, t.created_at, t.has_cover,
			COALESCE(ps.plays, 0), CASE WHEN f.track_id IS NULL THEN 0 ELSE 1 END
		FROM tracks t
		LEFT JOIN play_stats ps ON ps.track_id = t.id AND ps.user_id = ?
		LEFT JOIN favorites f ON f.track_id = t.id AND f.user_id = ?
		WHERE `+visibleTo("t.user_id"), userID, userID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		t := &homeTrack{}
		var credit string
		if err := rows.Scan(&t.id, &credit, &t.genre, &t.createdAt, &t.hasCover, &t.plays, &t.fav); err != nil {
			rows.Close()
			return nil, err
		}
		t.genre = strings.ToLower(strings.TrimSpace(t.genre))
		for _, name := range splitArtists(dropPlaceholder(credit, "Unknown Artist")) {
			key := artistKey(name)
			a := d.artists[key]
			if a == nil {
				a = &homeArtist{key: key, spelling: map[string]int{}}
				d.artists[key] = a
			}
			a.tracks = append(a.tracks, t)
			a.spelling[name]++
			t.artists = append(t.artists, key)
		}
		d.tracks[t.id] = t
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, a := range d.artists {
		best := 0
		for name, n := range a.spelling {
			if n > best || n == best && name < a.name {
				a.name, best = name, n
			}
		}
	}

	since := now.AddDate(0, 0, -homeHistoryDays).Format(time.RFC3339)
	ev, err := s.db.Query(`SELECT pe.track_id, pe.played_at, `+listenedExpr+`
		FROM play_events pe JOIN tracks t ON t.id = pe.track_id
		WHERE pe.user_id = ? AND pe.played_at >= ?
		ORDER BY pe.played_at`, userID, since)
	if err != nil {
		return nil, err
	}
	for ev.Next() {
		var id, at string
		var listened float64
		if err := ev.Scan(&id, &at, &listened); err != nil {
			ev.Close()
			return nil, err
		}
		t := d.tracks[id]
		when, perr := time.Parse(time.RFC3339, at)
		if t == nil || perr != nil {
			continue
		}
		d.events = append(d.events, homeEvent{track: t, at: when, listened: listened})
		if when.After(t.lastHeard) {
			t.lastHeard = when
		}
	}
	ev.Close()
	if err := ev.Err(); err != nil {
		return nil, err
	}

	for _, e := range d.events {
		age := now.Sub(e.at).Hours() / 24
		weight := math.Min(e.listened/180, 2) * math.Pow(0.5, age/30)
		for _, key := range e.track.artists {
			a := d.artists[key]
			a.score += weight
			if age <= 90 {
				a.listens++
			}
		}
	}
	for _, a := range d.artists {
		for _, t := range a.tracks {
			if t.fav {
				a.score += 0.5
			}
			a.score += 0.05 * float64(t.plays)
		}
		d.maxScore = math.Max(d.maxScore, a.score)
	}

	pl, err := s.db.Query(`SELECT pt.playlist_id, pt.track_id FROM playlist_tracks pt
		JOIN playlists p ON p.id = pt.playlist_id WHERE p.user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	lists := map[string][]*homeTrack{}
	for pl.Next() {
		var listID, trackID string
		if pl.Scan(&listID, &trackID) == nil {
			if t := d.tracks[trackID]; t != nil {
				lists[listID] = append(lists[listID], t)
			}
		}
	}
	pl.Close()
	for _, list := range lists {
		if len(list) > 400 {
			continue
		}
		for _, a := range list {
			m := d.coPlay[a.id]
			if m == nil {
				m = map[string]int{}
				d.coPlay[a.id] = m
			}
			for _, b := range list {
				if a != b {
					m[b.id]++
				}
			}
		}
		d.addListRelations(list)
	}
	return d, nil
}

func (d *homeData) relate(a, b string, w float64) {
	if a == b || a == "" || b == "" {
		return
	}
	m := d.related[a]
	if m == nil {
		m = map[string]float64{}
		d.related[a] = m
	}
	m[b] = math.Min(m[b]+w, 2)
}

func (d *homeData) addListRelations(list []*homeTrack) {
	set := map[string]bool{}
	for _, t := range list {
		for _, a := range t.artists {
			set[a] = true
		}
	}
	if len(set) < 2 || len(set) > 120 {
		return
	}
	w := math.Min(0.5, 4/float64(len(set)))
	for a := range set {
		for b := range set {
			d.relate(a, b, w)
		}
	}
}

func (d *homeData) buildRelated() {
	for _, t := range d.tracks {
		for _, a := range t.artists {
			for _, b := range t.artists {
				d.relate(a, b, 0.6)
			}
		}
	}
	together := map[[2]string]int{}
	for i := 1; i < len(d.events); i++ {
		prev, cur := d.events[i-1], d.events[i]
		if cur.at.Sub(prev.at) > sessionGap {
			continue
		}
		for _, a := range prev.track.artists {
			for _, b := range cur.track.artists {
				if a != b {
					together[[2]string{min(a, b), max(a, b)}]++
				}
			}
		}
	}
	for pair, n := range together {
		w := 0.5 * float64(n) / float64(n+6)
		d.relate(pair[0], pair[1], w)
		d.relate(pair[1], pair[0], w)
	}
	genres := map[string][]string{}
	for key, a := range d.artists {
		count := map[string]int{}
		for _, t := range a.tracks {
			if t.genre != "" {
				count[t.genre]++
			}
		}
		top, best := "", 0
		for g, n := range count {
			if n > best || n == best && g < top {
				top, best = g, n
			}
		}
		if top != "" {
			genres[top] = append(genres[top], key)
		}
	}
	for _, members := range genres {
		if len(members) > 200 {
			continue
		}
		for _, a := range members {
			for _, b := range members {
				d.relate(a, b, 0.2)
			}
		}
	}
	for key, info := range d.info {
		if _, ok := d.artists[key]; !ok {
			continue
		}
		for i, r := range info.related {
			w := 1 - float64(i)/25
			d.relate(key, artistKey(r.Name), w)
			d.relate(artistKey(r.Name), key, w*0.8)
		}
	}
}

func (d *homeData) rankedArtists() []*homeArtist {
	out := make([]*homeArtist, 0, len(d.artists))
	for _, a := range d.artists {
		if a.score > 0 {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b *homeArtist) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.key, b.key)
	})
	return out
}

func (d *homeData) rng(salt string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(d.userID + "|" + d.daySeed + "|" + salt))
	sum := h.Sum64()
	return rand.New(rand.NewPCG(sum, sum>>17|1))
}

func (d *homeData) trackScore(t *homeTrack, r *rand.Rand) float64 {
	score := math.Log1p(float64(t.plays)) + r.Float64()*1.2
	if t.fav {
		score += 1.5
	}
	if !t.lastHeard.IsZero() && d.now.Sub(t.lastHeard) < 36*time.Hour {
		score -= 1
	}
	return score
}

func (d *homeData) pickTracks(a *homeArtist, n int, used map[string]bool, r *rand.Rand) []*homeTrack {
	type scored struct {
		t *homeTrack
		s float64
	}
	var list []scored
	for _, t := range a.tracks {
		if !used[t.id] {
			list = append(list, scored{t, d.trackScore(t, r)})
		}
	}
	slices.SortFunc(list, func(x, y scored) int {
		if x.s > y.s {
			return -1
		}
		if x.s < y.s {
			return 1
		}
		return strings.Compare(x.t.id, y.t.id)
	})
	out := []*homeTrack{}
	for _, s := range list {
		if len(out) == n {
			break
		}
		out = append(out, s.t)
		used[s.t.id] = true
	}
	return out
}

func spreadArtists(tracks []*homeTrack, r *rand.Rand) []*homeTrack {
	r.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
	out := make([]*homeTrack, 0, len(tracks))
	rest := append([]*homeTrack{}, tracks...)
	lastArtist := ""
	for len(rest) > 0 {
		pick := 0
		for i, t := range rest {
			if len(t.artists) == 0 || t.artists[0] != lastArtist {
				pick = i
				break
			}
		}
		t := rest[pick]
		rest = append(rest[:pick], rest[pick+1:]...)
		out = append(out, t)
		if len(t.artists) > 0 {
			lastArtist = t.artists[0]
		}
	}
	return out
}

func coverIDs(tracks []*homeTrack) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tracks {
		if !t.hasCover || len(t.artists) > 0 && seen[t.artists[0]] {
			continue
		}
		if len(t.artists) > 0 {
			seen[t.artists[0]] = true
		}
		out = append(out, t.id)
		if len(out) == 4 {
			break
		}
	}
	for _, t := range tracks {
		if len(out) == 4 {
			break
		}
		if t.hasCover && !slices.Contains(out, t.id) {
			out = append(out, t.id)
		}
	}
	return out
}

func trackIDs(tracks []*homeTrack) []string {
	out := make([]string, len(tracks))
	for i, t := range tracks {
		out[i] = t.id
	}
	return out
}

func (d *homeData) artistMix(seed *homeArtist, members []*homeArtist) (HomeMix, bool) {
	r := d.rng("mix:" + seed.key)
	pool := map[string]float64{}
	for _, m := range members {
		for other, w := range d.related[m.key] {
			if _, inLib := d.artists[other]; inLib && w >= 0.35 {
				pool[other] = math.Max(pool[other], w)
			}
		}
	}
	for _, m := range members {
		delete(pool, m.key)
	}
	type weighted struct {
		a *homeArtist
		w float64
	}
	var related []weighted
	for key, w := range pool {
		related = append(related, weighted{d.artists[key], w})
	}
	slices.SortFunc(related, func(x, y weighted) int {
		if x.w != y.w {
			if x.w > y.w {
				return -1
			}
			return 1
		}
		return strings.Compare(x.a.key, y.a.key)
	})
	if len(related) > 10 {
		related = related[:10]
	}

	used := map[string]bool{}
	var picked []*homeTrack
	contrib := map[string]int{}
	for _, m := range members {
		got := d.pickTracks(m, 8, used, r)
		picked = append(picked, got...)
		contrib[m.key] += len(got)
	}
	for _, rel := range related {
		if len(picked) >= mixMaxTracks {
			break
		}
		n := min(4, int(math.Ceil(rel.w*3))+1)
		got := d.pickTracks(rel.a, n, used, r)
		picked = append(picked, got...)
		contrib[rel.a.key] += len(got)
	}
	if len(picked) > mixMaxTracks {
		picked = picked[:mixMaxTracks]
	}
	if len(picked) < mixMinTracks {
		return HomeMix{}, false
	}
	picked = spreadArtists(picked, r)

	names := []string{}
	for _, m := range members {
		names = append(names, m.name)
	}
	for _, rel := range related {
		if len(names) >= 3 {
			break
		}
		if contrib[rel.a.key] > 0 {
			names = append(names, rel.a.name)
		}
	}
	subtitle := strings.Join(names, ", ")
	if len(contrib) > len(names) {
		subtitle += " and more"
	}
	return HomeMix{
		ID: "artist-" + seed.key, Kind: "artist", Title: seed.name + " Mix", Subtitle: subtitle, Seed: seed.name,
		TrackIDs: trackIDs(picked), CoverTrackIDs: coverIDs(picked),
	}, true
}

func (d *homeData) mixes(top []*homeArtist) []HomeMix {
	out := []HomeMix{}
	covered := map[string]bool{}
	for _, seed := range top {
		if len(out) == mixCount {
			break
		}
		if covered[seed.key] || len(seed.tracks) < 2 {
			continue
		}
		members := []*homeArtist{seed}
		covered[seed.key] = true
		for _, other := range top[:min(len(top), 15)] {
			if len(members) == 3 {
				break
			}
			if !covered[other.key] && d.related[seed.key][other.key] >= 0.5 {
				members = append(members, other)
				covered[other.key] = true
			}
		}
		if mix, ok := d.artistMix(seed, members); ok {
			out = append(out, mix)
		}
	}
	if mix, ok := d.onRepeat(); ok {
		out = append(out, mix)
	}
	if mix, ok := d.rediscover(); ok {
		out = append(out, mix)
	}
	return out
}

func (d *homeData) onRepeat() (HomeMix, bool) {
	since := d.now.AddDate(0, 0, -30)
	plays := map[*homeTrack]float64{}
	for _, e := range d.events {
		if e.at.After(since) {
			plays[e.track] += math.Min(e.listened/180, 1.5)
		}
	}
	list := make([]*homeTrack, 0, len(plays))
	for t, n := range plays {
		if n >= 1 {
			list = append(list, t)
		}
	}
	slices.SortFunc(list, func(a, b *homeTrack) int {
		if plays[a] != plays[b] {
			if plays[a] > plays[b] {
				return -1
			}
			return 1
		}
		return strings.Compare(a.id, b.id)
	})
	if len(list) > 30 {
		list = list[:30]
	}
	if len(list) < mixMinTracks {
		return HomeMix{}, false
	}
	return HomeMix{
		ID: "on-repeat", Kind: "repeat", Title: "On Repeat", Subtitle: "What you've played most this month",
		TrackIDs: trackIDs(list), CoverTrackIDs: coverIDs(list),
	}, true
}

func (d *homeData) rediscover() (HomeMix, bool) {
	cutoff := d.now.AddDate(0, 0, -60)
	r := d.rng("rediscover")
	type scored struct {
		t *homeTrack
		s float64
	}
	var list []scored
	for _, t := range d.tracks {
		if t.plays < 2 && !t.fav {
			continue
		}
		if !t.lastHeard.IsZero() && t.lastHeard.After(cutoff) {
			continue
		}
		s := math.Log1p(float64(t.plays)) + r.Float64()
		if t.fav {
			s += 1
		}
		list = append(list, scored{t, s})
	}
	slices.SortFunc(list, func(a, b scored) int {
		if a.s > b.s {
			return -1
		}
		if a.s < b.s {
			return 1
		}
		return strings.Compare(a.t.id, b.t.id)
	})
	tracks := []*homeTrack{}
	for _, s := range list {
		if len(tracks) == 30 {
			break
		}
		tracks = append(tracks, s.t)
	}
	if len(tracks) < mixMinTracks {
		return HomeMix{}, false
	}
	tracks = spreadArtists(tracks, r)
	return HomeMix{
		ID: "rediscover", Kind: "rediscover", Title: "Rediscover", Subtitle: "Favorites you haven't played in a while",
		TrackIDs: trackIDs(tracks), CoverTrackIDs: coverIDs(tracks),
	}, true
}

func (d *homeData) bestCover(a *homeArtist) string {
	best, plays := "", -1
	for _, t := range a.tracks {
		if t.hasCover && t.plays > plays {
			best, plays = t.id, t.plays
		}
	}
	return best
}

func (d *homeData) topArtists(top []*homeArtist) []HomeArtist {
	out := []HomeArtist{}
	for _, a := range top {
		if len(out) == 12 {
			break
		}
		if a.listens == 0 && len(out) >= 6 {
			continue
		}
		item := HomeArtist{Name: a.name, TrackCount: len(a.tracks), Listens: a.listens, CoverTrackID: d.bestCover(a)}
		if info, ok := d.info[a.key]; ok {
			item.Picture = info.picture
		}
		out = append(out, item)
	}
	return out
}

func (d *homeData) suggestions(top []*homeArtist) []HomeSuggestion {
	type cand struct {
		HomeSuggestion
		score float64
		best  float64
	}
	cands := map[string]*cand{}
	seeds := top[:min(len(top), 10)]
	norm := math.Max(d.maxScore, 1e-9)
	for _, seed := range seeds {
		weight := seed.score / norm
		info := d.info[seed.key]
		for i, rel := range info.related {
			key := artistKey(rel.Name)
			if a, ok := d.artists[key]; ok && (a.score > 0.25*seed.score || len(a.tracks) > 3) {
				continue
			}
			contrib := weight * (1 - float64(i)/25)
			if _, inLib := d.artists[key]; inLib {
				contrib *= 0.7
			}
			c := cands[key]
			if c == nil {
				c = &cand{HomeSuggestion: HomeSuggestion{Name: rel.Name, DeezerID: rel.ID, Picture: rel.Picture}}
				if a, ok := d.artists[key]; ok {
					c.TrackCount = len(a.tracks)
				}
				cands[key] = c
			}
			c.score += contrib
			if contrib > c.best {
				c.best, c.Because = contrib, seed.name
			}
		}
		for key, w := range d.related[seed.key] {
			a, ok := d.artists[key]
			if !ok || cands[key] != nil || a.score > 0.25*seed.score || w < 0.6 {
				continue
			}
			cands[key] = &cand{
				HomeSuggestion: HomeSuggestion{Name: a.name, TrackCount: len(a.tracks), Because: seed.name},
				score:          weight * w * 0.5, best: weight * w * 0.5,
			}
		}
	}
	for _, seed := range seeds {
		delete(cands, seed.key)
	}
	list := make([]*cand, 0, len(cands))
	for _, c := range cands {
		list = append(list, c)
	}
	slices.SortFunc(list, func(a, b *cand) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	out := []HomeSuggestion{}
	perSeed := map[string]int{}
	for _, c := range list {
		if len(out) == 12 {
			break
		}
		if perSeed[c.Because] >= 3 {
			continue
		}
		perSeed[c.Because]++
		if c.Picture == "" {
			if info, ok := d.info[artistKey(c.Name)]; ok {
				c.Picture = info.picture
			}
		}
		out = append(out, c.HomeSuggestion)
	}
	return out
}

func (d *homeData) similar(days int) HomeSimilar {
	since := d.now.AddDate(0, 0, -days)
	heard := map[string]float64{}
	genres := map[string]float64{}
	played := map[string]bool{}
	for _, e := range d.events {
		if e.at.Before(since) {
			continue
		}
		w := math.Min(e.listened/180, 1.5)
		played[e.track.id] = true
		for _, a := range e.track.artists {
			heard[a] += w
		}
		if e.track.genre != "" {
			genres[e.track.genre] += w
		}
	}
	out := HomeSimilar{TrackIDs: []string{}, BasedOn: []string{}}
	if len(played) == 0 {
		return out
	}
	maxHeard, maxGenre := 0.0, 0.0
	for _, w := range heard {
		maxHeard = math.Max(maxHeard, w)
	}
	for _, w := range genres {
		maxGenre = math.Max(maxGenre, w)
	}
	r := d.rng("similar:" + strconv.Itoa(days))
	type scored struct {
		t *homeTrack
		s float64
	}
	var list []scored
	for _, t := range d.tracks {
		if played[t.id] {
			continue
		}
		score := 0.0
		for _, a := range t.artists {
			score += 0.5 * heard[a] / maxHeard
			for h, w := range heard {
				score += w / maxHeard * d.related[h][a] * 0.8
			}
		}
		if maxGenre > 0 && t.genre != "" {
			score += 0.3 * genres[t.genre] / maxGenre
		}
		shared := 0
		for other, n := range d.coPlay[t.id] {
			if played[other] {
				shared += n
			}
		}
		score += 0.3 * math.Min(float64(shared), 3) / 3
		if t.lastHeard.IsZero() {
			score += 0.1
		}
		if score < 0.25 {
			continue
		}
		list = append(list, scored{t, score + r.Float64()*0.05})
	}
	slices.SortFunc(list, func(a, b scored) int {
		if a.s > b.s {
			return -1
		}
		if a.s < b.s {
			return 1
		}
		return strings.Compare(a.t.id, b.t.id)
	})
	perArtist := map[string]int{}
	for _, s := range list {
		if len(out.TrackIDs) == 24 {
			break
		}
		lead := ""
		if len(s.t.artists) > 0 {
			lead = s.t.artists[0]
		}
		if perArtist[lead] >= 3 {
			continue
		}
		perArtist[lead]++
		out.TrackIDs = append(out.TrackIDs, s.t.id)
	}
	type hw struct {
		key string
		w   float64
	}
	var hs []hw
	for k, w := range heard {
		hs = append(hs, hw{k, w})
	}
	slices.SortFunc(hs, func(a, b hw) int {
		if a.w != b.w {
			if a.w > b.w {
				return -1
			}
			return 1
		}
		return strings.Compare(a.key, b.key)
	})
	for _, h := range hs {
		if len(out.BasedOn) == 3 {
			break
		}
		out.BasedOn = append(out.BasedOn, d.artists[h.key].name)
	}
	return out
}
