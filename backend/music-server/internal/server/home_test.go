package server

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSimilarScoresRelatedArtistsAndSharedPlaylists(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	d := &homeData{
		tracks:  map[string]*homeTrack{},
		artists: map[string]*homeArtist{},
		related: map[string]map[string]float64{
			"a": {"c": 1, "d": 0.5},
			"b": {"c": 0.5, "e": 0.1, "f": 0.7},
		},
		now:     now,
		userID:  "listener",
		daySeed: now.Format("2006-01-02"),
	}
	track := func(id string, artists ...string) *homeTrack {
		tr := &homeTrack{id: id, artists: artists}
		d.tracks[id] = tr
		for _, key := range artists {
			if d.artists[key] == nil {
				d.artists[key] = &homeArtist{key: key, name: strings.ToUpper(key)}
			}
			d.artists[key].tracks = append(d.artists[key].tracks, tr)
		}
		return tr
	}
	play := func(tr *homeTrack, seconds float64) {
		tr.lastHeard = now.Add(-time.Hour)
		d.events = append(d.events, homeEvent{track: tr, at: tr.lastHeard, listened: seconds})
	}
	// "a" is heard for 1.5 plays, "b" for half of that.
	a1, a2, b1 := track("a1", "a"), track("a2", "a"), track("b1", "b")
	play(a1, 180)
	play(a2, 90)
	play(b1, 135)

	track("cd", "c", "d") // 1.0 for c, 0.4 for d, 0.1 never heard
	track("c1", "c")      // 0.8 from a, 0.2 from b, 0.1 never heard
	track("a3", "a")      // 0.5 for the heard artist, 0.1 never heard
	track("d1", "d")      // 0.4 from a, 0.1 never heard
	track("e1", "e")      // 0.04 from b, 0.1 never heard: below the cut
	// 0.28 from b and nothing else: just above the cut.
	track("f1", "f").lastHeard = now.AddDate(0, -3, 0)
	inThree := track("z3", "z")
	inTwo := track("z2", "z")
	inOne := track("z1", "z")
	d.lists = [][]*homeTrack{
		{a1, a2, b1, inThree}, // three played neighbours: 0.3
		{a1, inTwo},
		{b1, inTwo}, // one in each of two playlists: 0.2
		{a2, inOne}, // 0.1: below the cut
	}

	got := d.similar(7)
	// The last two are closer together than the random part of the score.
	want := []string{"cd", "c1", "a3", "d1", "z3", "f1", "z2"}
	if len(got.TrackIDs) == len(want) {
		slices.Sort(got.TrackIDs[5:])
	}
	if !slices.Equal(got.TrackIDs, want) {
		t.Errorf("similar tracks: %v, want %v", got.TrackIDs, want)
	}
	if !slices.Equal(got.BasedOn, []string{"A", "B"}) {
		t.Errorf("based on %v, want [A B]", got.BasedOn)
	}
}

func TestHomeIgnoresPlaylistsOver400Tracks(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"small", "huge"} {
		exec(`INSERT INTO playlists (id, user_id, name, created_at) VALUES (?,?,?,?)`, id, userID, id, nowRFC3339())
	}
	add := func(list, track string, position int) {
		exec(`INSERT INTO playlist_tracks (playlist_id, track_id, position) VALUES (?,?,?)`, list, track, position)
	}
	add("small", "shape", 0)
	add("small", "lights", 1)
	for i, id := range []string{"shape", "perfect", "swapped"} {
		add("huge", id, i)
	}
	for i := 3; i <= 400; i++ {
		id := fmt.Sprintf("filler-%d", i)
		exec(`INSERT INTO tracks (id, user_id, title, artist, album, file_name, file_path, mime_type, file_size, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, userID, id, "Filler", "Unknown Album", id+".mp3", "uploads/"+id+".mp3", "audio/mpeg", 1, nowRFC3339())
		add("huge", id, i)
	}
	played := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	for _, id := range []string{"shape", "perfect"} {
		exec(`INSERT INTO play_events (user_id, track_id, played_at, listened_seconds) VALUES (?,?,?,?)`, userID, id, played, 180)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	d, err := s.loadHomeData(userID)
	if err != nil {
		t.Fatal(err)
	}
	d.buildRelated()
	got := d.similar(7)
	// Two played tracks share the 401-track playlist with everything else in
	// it; were it counted, all of those would be suggested.
	if !slices.Equal(got.TrackIDs, []string{"lights"}) {
		t.Errorf("similar tracks: %v, want only the one from the small playlist", got.TrackIDs)
	}
	if !slices.Equal(got.BasedOn, []string{"Ed Sheeran"}) {
		t.Errorf("based on %v, want [Ed Sheeran]", got.BasedOn)
	}
}
