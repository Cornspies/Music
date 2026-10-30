package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func fakeLRCLIB(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	old, oldTransport := lrclibBase, metadataLookupClient.Transport
	lrclibBase = srv.URL
	// The real transport refuses loopback addresses.
	metadataLookupClient.Transport = srv.Client().Transport
	resetBackoff := func() {
		lrclibBackoff.Lock()
		lrclibBackoff.until, lrclibBackoff.next = time.Time{}, 0
		lrclibBackoff.Unlock()
	}
	resetBackoff()
	t.Cleanup(func() {
		srv.Close()
		lrclibBase, metadataLookupClient.Transport = old, oldTransport
		resetBackoff()
	})
}

func TestLRCLIBDurationOutOfRange(t *testing.T) {
	var sent []string
	fakeLRCLIB(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get" {
			sent = append(sent, r.URL.Query().Get("duration"))
		}
		http.NotFound(w, r)
	})
	for _, d := range []float64{0.3, 184.4, 5400} {
		if _, err := lrclibLookup(context.Background(), &Track{Title: "x", Artist: "y", Duration: d}); err != nil {
			t.Fatalf("duration %v: %v", d, err)
		}
	}
	if want := []string{"", "184", ""}; !slices.Equal(sent, want) {
		t.Fatalf("durations sent = %q, want %q", sent, want)
	}
}

func TestLRCLIBBusyPausesAndBacksOff(t *testing.T) {
	var hits atomic.Int32
	fakeLRCLIB(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	track := &Track{Title: "x", Artist: "y"}
	for range 5 {
		if _, err := lrclibLookup(context.Background(), track); !errors.Is(err, errLRCLIBPaused) {
			t.Fatalf("err = %v, want errLRCLIBPaused", err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("lrclib asked %d times while paused, want 1", n)
	}
	lrclibBackoff.Lock()
	wait, next := time.Until(lrclibBackoff.until), lrclibBackoff.next
	lrclibBackoff.until = time.Time{}
	lrclibBackoff.Unlock()
	if wait < 55*time.Second || next != 2*lrclibMinBackoff {
		t.Fatalf("paused for %s with next %s; want about %s, then %s", wait, next, lrclibMinBackoff, 2*lrclibMinBackoff)
	}
	if d := lrclibPause(""); d != 2*lrclibMinBackoff {
		t.Fatalf("second pause = %s, want %s", d, 2*lrclibMinBackoff)
	}
	lrclibResume()
	if lrclibBackoff.next != 0 {
		t.Fatal("a good answer should reset the backoff")
	}
}

func TestLRCLIBAbandonedRequestDoesNotPause(t *testing.T) {
	fakeLRCLIB(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lrclibLookup(ctx, &Track{Title: "x", Artist: "y"}); err == nil {
		t.Fatal("want an error from a cancelled request")
	}
	if !lrclibAvailable() {
		t.Fatal("a request the listener abandoned should not pause lrclib")
	}
}

func TestStoredLyricsAreNotLookedUpAgain(t *testing.T) {
	var hits atomic.Int32
	fakeLRCLIB(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	})
	s, err := New(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Close() })
	now := nowRFC3339()
	if _, err := s.db.Exec(`INSERT INTO users (id, username, password_hash, created_at) VALUES ('u', 'u', 'x', ?)`, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"found", "typed", "missing"} {
		if _, err := s.db.Exec(`INSERT INTO tracks (id, user_id, title, artist, album, file_name, file_path, mime_type, file_size, created_at, updated_at)
			VALUES (?, 'u', 'x', 'y', '', 'a.mp3', 'a.mp3', 'audio/mpeg', 0, ?, ?)`, id, now, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.writeLyricsRow("found", lyricsRow{source: lyricsSourceLRCLIB, plain: "la la"})
	_ = s.writeLyricsRow("typed", lyricsRow{source: lyricsSourceManual, plain: "mine"})
	_ = s.writeLyricsRow("missing", lyricsRow{source: lyricsSourceNone})

	// A metadata change or a new file without lyrics keeps what a track has.
	noFile := t.TempDir() + "/none.mp3"
	for _, id := range []string{"found", "typed", "missing"} {
		s.forgetLyricsMiss(id)
		s.lyricsForNewAudio(id, noFile)
	}
	for id, want := range map[string]string{"found": "la la", "typed": "mine"} {
		row, err := s.resolveLyrics(context.Background(), &Track{ID: id, Title: "x", Artist: "y"}, noFile)
		if err != nil || row.plain != want {
			t.Fatalf("%s: row = %+v, err = %v; want %q", id, row, err, want)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("lrclib asked %d times for tracks that have lyrics", n)
	}
	// Only the track without lyrics is looked up again.
	if _, err := s.resolveLyrics(context.Background(), &Track{ID: "missing", Title: "x", Artist: "y"}, noFile); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n == 0 {
		t.Fatal("a track without lyrics should be looked up again after its metadata changed")
	}
}
