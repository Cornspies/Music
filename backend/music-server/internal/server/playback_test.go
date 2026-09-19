package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) (*Server, *User) {
	t.Helper()
	s, err := New(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	u := &User{ID: "u1", Username: "alice"}
	if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash, created_at) VALUES (?,?,?,?)`,
		u.ID, u.Username, "x", nowRFC3339()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		if _, err := s.db.Exec(`INSERT INTO tracks
			(id, user_id, title, artist, album, genre, year, track_no, duration,
			 file_name, file_path, mime_type, file_size, has_cover, plays, created_at,
			 alt_path, alt_mime, alt_size)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,0,?,?,?,?)`,
			id, u.ID, "Title "+id, "Artist", "Album", "", 0, 0, 200.0,
			id+".mp3", "uploads/"+id+".mp3", "audio/mpeg", 1, 0, nowRFC3339(), "", "", 0); err != nil {
			t.Fatal(err)
		}
	}
	return s, u
}

func do(s *Server, u *User, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/playback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	switch method {
	case http.MethodGet:
		s.handleGetPlayback(rec, withUser(req, u))
	case http.MethodPut:
		s.handlePutPlayback(rec, withUser(req, u))
	case http.MethodDelete:
		s.handleDeletePlayback(rec, withUser(req, u))
	}
	return rec
}

func TestPlaybackRoundTrip(t *testing.T) {
	s, u := newTestServer(t)

	if rec := do(s, u, http.MethodGet, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("empty state: got %d", rec.Code)
	}

	// Save with a queue, then a position-only save must keep that queue.
	if rec := do(s, u, http.MethodPut, `{"trackId":"t2","position":42.5,"queue":["t1","t2","t3"]}`); rec.Code != 204 {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	if rec := do(s, u, http.MethodPut, `{"trackId":"t2","position":50}`); rec.Code != 204 {
		t.Fatalf("put position: %d %s", rec.Code, rec.Body)
	}
	var st PlaybackState
	rec := do(s, u, http.MethodGet, "")
	if rec.Code != 200 {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Track.ID != "t2" || st.Position != 50 || st.QueueIndex != 1 || len(st.Queue) != 3 {
		t.Fatalf("unexpected state: track=%s pos=%v idx=%d queue=%d", st.Track.ID, st.Position, st.QueueIndex, len(st.Queue))
	}

	// A queued track deleted since is dropped; the current track's index shifts.
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}
	rec = do(s, u, http.MethodGet, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if len(st.Queue) != 2 || st.QueueIndex != 0 || st.Queue[0].ID != "t2" {
		t.Fatalf("after deleting queued track: idx=%d queue=%v", st.QueueIndex, st.Queue)
	}

	// Played to the end → resumes from the start.
	do(s, u, http.MethodPut, `{"trackId":"t2","position":199.5}`)
	rec = do(s, u, http.MethodGet, "")
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Position != 0 {
		t.Fatalf("finished track should restart, got position %v", st.Position)
	}

	// Deleting the current track (FK cascade) leaves nothing to resume.
	if _, err := s.db.Exec(`DELETE FROM tracks WHERE id = 't2'`); err != nil {
		t.Fatal(err)
	}
	if rec := do(s, u, http.MethodGet, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("after deleting current track: got %d", rec.Code)
	}

	// Unknown track and explicit clear.
	if rec := do(s, u, http.MethodPut, `{"trackId":"nope","position":1}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown track: got %d", rec.Code)
	}
	do(s, u, http.MethodPut, `{"trackId":"t3","position":1,"queue":["t3"]}`)
	if rec := do(s, u, http.MethodDelete, ""); rec.Code != 204 {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := do(s, u, http.MethodGet, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("after clear: got %d", rec.Code)
	}
}

func TestTrimQueueWindowsAroundCurrent(t *testing.T) {
	ids := make([]string, 2000)
	for i := range ids {
		ids[i] = "id-" + strconv.Itoa(i)
	}
	out := trimQueue(ids, ids[1000])
	if len(out) != maxSavedQueue {
		t.Fatalf("len %d", len(out))
	}
	if out[0] != ids[950] || out[len(out)-1] != ids[1449] {
		t.Fatalf("window is %s..%s", out[0], out[len(out)-1])
	}
	if got := trimQueue(ids, "missing"); len(got) != maxSavedQueue || got[0] != ids[0] {
		t.Fatal("missing current should keep the head")
	}
	if got := trimQueue([]string{" a ", "", "b"}, "b"); len(got) != 2 || got[0] != "a" {
		t.Fatalf("blank entries should be dropped: %v", got)
	}
}
