package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func statsFor(t *testing.T, s *Server, userID string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleStats(rec, withUser(httptest.NewRequest("GET", "/api/stats", nil), &User{ID: userID, Role: roleUser}))
	out := map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", rec.Code, rec.Body.String())
	}
	return out
}

func listedIDs(list any) []string {
	ids := []string{}
	items, _ := list.([]any)
	for _, item := range items {
		track, _ := item.(map[string]any)
		id, _ := track["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

func mustExec(t *testing.T, s *Server, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func addTestTrack(t *testing.T, s *Server, id, userID, artist string) {
	t.Helper()
	mustExec(t, s, `INSERT INTO tracks (id, user_id, title, artist, album, duration, file_name, file_path, mime_type, file_size, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, userID, id, artist, "Unknown Album", 100, id+".mp3", "uploads/"+id+".mp3", "audio/mpeg", 1, "2026-01-01T00:00:00Z")
}

func TestStatsListsAndTotals(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	mustExec(t, s, `INSERT INTO users (id, username, password_hash, created_at, role) VALUES ('demo-user', 'demo', '!', ?, ?)`,
		nowRFC3339(), roleDemo)
	addTestTrack(t, s, "hidden", "demo-user", "Nobody")
	mustExec(t, s, `UPDATE tracks SET created_at = '2026-01-01T00:00:00Z'`)
	mustExec(t, s, `UPDATE tracks SET created_at = '2026-02-01T00:00:00Z' WHERE id = 'perfect'`)

	for _, p := range []struct {
		id    string
		plays int
		last  any
	}{
		{"shape", 5, "2026-03-01T11:00:00Z"},
		{"perfect", 5, "2026-03-01T10:00:00Z"},
		{"lights", 5, "2026-03-01T08:00:00Z"},
		{"home", 0, "2026-03-01T08:00:00Z"},
		// A count from before plays were recorded one by one.
		{"swapped", 1, "2026-03-05T00:00:00Z"},
		{"hidden", 9, "2026-03-01T12:00:00Z"},
	} {
		mustExec(t, s, `INSERT INTO play_stats (user_id, track_id, plays, last_played_at) VALUES (?,?,?,?)`, userID, p.id, p.plays, p.last)
	}
	for _, e := range []struct {
		id, at   string
		listened float64
	}{
		{"lights", "2026-03-01T08:00:00Z", 20},
		{"home", "2026-03-01T08:00:00Z", 10},
		{"shape", "2026-03-01T09:00:00Z", 100},
		{"perfect", "2026-03-01T10:00:00Z", 30},
		{"shape", "2026-03-01T11:00:00Z", 50},
		{"hidden", "2026-03-01T12:00:00Z", 500},
	} {
		mustExec(t, s, `INSERT INTO play_events (user_id, track_id, played_at, listened_seconds) VALUES (?,?,?,?)`, userID, e.id, e.at, e.listened)
	}
	mustExec(t, s, `INSERT INTO favorites (user_id, track_id, created_at) VALUES (?, 'shape', ?)`, userID, nowRFC3339())
	mustExec(t, s, `INSERT INTO playlists (id, user_id, name, created_at) VALUES ('list', ?, 'List', ?)`, userID, nowRFC3339())

	stats := statsFor(t, s, userID)
	// Most plays first, then the newer upload; a tie in both keeps the order
	// the tracks were added in. No plays and other libraries stay out.
	if got, want := listedIDs(stats["mostPlayed"]), []string{"perfect", "shape", "lights", "swapped"}; !slices.Equal(got, want) {
		t.Errorf("most played: %v, want %v", got, want)
	}
	// By the newest recorded play, same second by id; a counter alone is not
	// a play.
	if got, want := listedIDs(stats["recentlyPlayed"]), []string{"shape", "perfect", "home", "lights"}; !slices.Equal(got, want) {
		t.Errorf("recently played: %v, want %v", got, want)
	}
	for key, want := range map[string]float64{
		"trackCount": 5, "totalDuration": 1353, "albumCount": 1, "artistCount": 5,
		"favoriteCount": 1, "playlistCount": 1, "playCount": 5, "listenedSeconds": 210,
	} {
		if got, _ := stats[key].(float64); got != want {
			t.Errorf("%s: %v, want %v", key, stats[key], want)
		}
	}
}

func TestRecentlyPlayedCutsASecondSharedByManyTracksByID(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	mustExec(t, s, `INSERT INTO play_events (user_id, track_id, played_at, listened_seconds) VALUES (?, 'shape', '2026-03-01T07:00:00Z', 0)`, userID)
	want := []string{"lights"}
	for i := 11; i >= 0; i-- {
		id := fmt.Sprintf("same-%02d", i)
		addTestTrack(t, s, id, userID, "Same Second")
		mustExec(t, s, `INSERT INTO play_events (user_id, track_id, played_at, listened_seconds) VALUES (?, ?, '2026-03-01T08:00:00Z', 0)`, userID, id)
		if i < 9 {
			want = append(want, id)
		}
	}
	slices.Sort(want[1:])
	mustExec(t, s, `INSERT INTO play_events (user_id, track_id, played_at, listened_seconds) VALUES (?, 'lights', '2026-03-01T09:00:00Z', 0)`, userID)

	if got := listedIDs(statsFor(t, s, userID)["recentlyPlayed"]); !slices.Equal(got, want) {
		t.Errorf("recently played: %v, want %v", got, want)
	}
}

func TestArtistCountMatchesTheArtistList(t *testing.T) {
	_, s := newTestClient(t)
	userID := seedLibrary(t, s)
	for i, credit := range []string{"", "Unknown Artist", "ED SHEERAN", "Ed Sheeran & The Weeknd", "Tom &amp; Jerry", "Tom feat. Jerry", "!!!"} {
		addTestTrack(t, s, fmt.Sprintf("credit-%d", i), userID, credit)
	}
	artists, err := s.libraryArtists(userID, "")
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.artistCount(userID)
	if err != nil {
		t.Fatal(err)
	}
	// The five seeded ones, Unknown Artist, Tom, Jerry and "!!!".
	if count != len(artists) || count != 9 {
		t.Errorf("artist count %d, artist list has %d, want 9", count, len(artists))
	}
}
