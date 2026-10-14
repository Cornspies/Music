package deezer

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseURL(t *testing.T) {
	c := &Client{}
	ctx := context.Background()

	cases := []struct {
		url      string
		wantKind string
		wantID   string
	}{
		{"https://www.deezer.com/track/3135556", "track", "3135556"},
		{"https://www.deezer.com/en/track/12345678", "track", "12345678"},
		{"https://deezer.com/fr/album/456789", "album", "456789"},
		{"https://www.deezer.com/playlist/987654321", "playlist", "987654321"},
		{"https://www.deezer.com/artist/112233", "artist", "112233"},
	}

	for _, tc := range cases {
		kind, id, err := c.ParseURL(ctx, tc.url)
		if err != nil {
			t.Errorf("ParseURL(%q) unexpected error: %v", tc.url, err)
			continue
		}
		if kind != tc.wantKind || id != tc.wantID {
			t.Errorf("ParseURL(%q) = (%q, %q), want (%q, %q)", tc.url, kind, id, tc.wantKind, tc.wantID)
		}
	}
}

func TestQualities(t *testing.T) {
	if QualityCode(QualityFLAC) != 9 {
		t.Errorf("expected FLAC code 9, got %d", QualityCode(QualityFLAC))
	}
	if QualityCode(QualityMP3_320) != 3 {
		t.Errorf("expected MP3_320 code 3, got %d", QualityCode(QualityMP3_320))
	}
	if QualityCode(QualityMP3_128) != 1 {
		t.Errorf("expected MP3_128 code 1, got %d", QualityCode(QualityMP3_128))
	}

	if QualityExt(QualityFLAC) != ".flac" {
		t.Errorf("expected .flac, got %s", QualityExt(QualityFLAC))
	}
	if QualityExt(QualityMP3_320) != ".mp3" {
		t.Errorf("expected .mp3, got %s", QualityExt(QualityMP3_320))
	}

	fbFLAC := FallbackQualities(QualityFLAC)
	if len(fbFLAC) != 2 || fbFLAC[0] != QualityMP3_320 || fbFLAC[1] != QualityMP3_128 {
		t.Errorf("unexpected FLAC fallbacks: %v", fbFLAC)
	}

	fb320 := FallbackQualities(QualityMP3_320)
	if len(fb320) != 1 || fb320[0] != QualityMP3_128 {
		t.Errorf("unexpected MP3_320 fallbacks: %v", fb320)
	}

	fb256 := FallbackQualities(QualityMP3_256)
	if len(fb256) != 1 || fb256[0] != QualityMP3_128 {
		t.Errorf("unexpected MP3_256 fallbacks: %v", fb256)
	}
}

func TestIsAllowedDeezerURL(t *testing.T) {
	allowed := []string{
		"https://www.deezer.com/ajax/gw-light.php",
		"https://api.deezer.com/track/123",
		"https://media.deezer.com/v1/get_url",
		"https://e-cdns-proxy-a.dzcdn.net/mobile/1/abc",
		"https://e-cdns-images.dzcdn.net/images/cover/123/250x250.jpg",
		"https://deezer.com/track/456",
		"https://dzcdn.net/stream",
	}
	for _, u := range allowed {
		if !IsAllowedDeezerURL(u) {
			t.Errorf("expected allowed for %s, got false", u)
		}
	}

	disallowed := []string{
		"http://www.deezer.com/ajax/gw-light.php",
		"https://deezer.com.attacker.com/steal",
		"https://dzcdn.net.attacker.com/steal",
		"https://127.0.0.1/internal",
		"https://169.254.169.254/latest/meta-data",
		"https://google.com/",
		"javascript:alert(1)",
		"",
	}
	for _, u := range disallowed {
		if IsAllowedDeezerURL(u) {
			t.Errorf("expected disallowed for %s, got true", u)
		}
	}
}

func TestParseURLRecognition(t *testing.T) {
	c := &Client{http: newHTTPClient(nil)}
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request %s", r.URL)
		return fakeResp(r, 500, ""), nil
	})

	match := map[string][2]string{
		"https://www.deezer.com/track/3135556":                      {"track", "3135556"},
		"https://www.deezer.com/en/track/3135556?utm_source=x":      {"track", "3135556"},
		"https://deezer.com/pt-br/album/11223344":                   {"album", "11223344"},
		"HTTPS://WWW.DEEZER.COM/FR/PLAYLIST/99":                     {"playlist", "99"},
		"deezer.com/artist/27":                                      {"artist", "27"},
		"listen to this: https://www.deezer.com/album/5 it's great": {"album", "5"},
		"(https://api.deezer.com/track/8)":                          {"track", "8"},
	}
	for in, want := range match {
		kind, id, err := c.ParseURL(context.Background(), in)
		if err != nil || kind != want[0] || id != want[1] {
			t.Errorf("ParseURL(%q) = %q, %q, %v; want %v", in, kind, id, err, want)
		}
	}

	for _, in := range []string{
		"https://evildeezer.com/track/1",
		"https://deezer.com.attacker.com/track/1",
		"https://example.com/?u=deezer.com/track/9",
		"https://www.deezer.com/en/user/5",
		"https://www.deezer.com/track/abc",
		"just some words",
	} {
		if kind, id, err := c.ParseURL(context.Background(), in); err == nil {
			t.Errorf("ParseURL(%q) = %q, %q, want an error", in, kind, id)
		}
	}
}

func TestCatalogRejectsNonNumericIDs(t *testing.T) {
	c := &Client{http: newHTTPClient(nil)}
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("request made for an invalid id: %s", r.URL)
		return fakeResp(r, 500, ""), nil
	})
	ctx := context.Background()
	for _, id := range []string{"", "12/../34", "1 2", "abc", "1?x=y"} {
		if _, err := c.GetTrack(ctx, id); err == nil {
			t.Errorf("GetTrack(%q) succeeded", id)
		}
		if _, err := c.GetAlbum(ctx, id); err == nil {
			t.Errorf("GetAlbum(%q) succeeded", id)
		}
		if _, err := c.GetPlaylist(ctx, id); err == nil {
			t.Errorf("GetPlaylist(%q) succeeded", id)
		}
	}
}

func pagedAPI(t *testing.T, failPage2 *atomic.Int32) (*Client, *sync.Map) {
	t.Helper()
	hits := &sync.Map{}
	c := &Client{http: newHTTPClient(nil)}
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		key := r.URL.RequestURI()
		n, _ := hits.LoadOrStore(key, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)
		switch key {
		case "/playlist/9":
			return fakeResp(r, 200, `{"id":9,"title":"P","nb_tracks":5,"tracks":{"data":[{"id":1,"title":"a"},{"id":2,"title":"b","readable":false}],"next":"http://api.deezer.com/playlist/9/tracks?index=2"}}`), nil
		case "/playlist/9/tracks?index=2":
			if failPage2.Add(-1) >= 0 {
				return fakeResp(r, 500, ""), nil
			}
			return fakeResp(r, 200, `{"data":[{"id":3,"title":"c"},{"id":4,"title":"d"}],"next":"https://api.deezer.com/playlist/9/tracks?index=4"}`), nil
		case "/playlist/9/tracks?index=4":
			return fakeResp(r, 200, `{"data":[{"id":5,"title":"e"}]}`), nil
		}
		t.Errorf("unexpected request %s", r.URL)
		return fakeResp(r, 404, ""), nil
	})
	return c, hits
}

func TestGetPlaylistPagination(t *testing.T) {
	old := pageRetryDelay
	pageRetryDelay = time.Millisecond
	t.Cleanup(func() { pageRetryDelay = old })
	ctx := context.Background()

	t.Run("collects every page, keeping unreadable tracks for the fallback download", func(t *testing.T) {
		c, _ := pagedAPI(t, new(atomic.Int32))
		pl, err := c.GetPlaylist(ctx, "9")
		if err != nil || len(pl.Tracks) != 5 {
			t.Fatalf("GetPlaylist = %d tracks, %v; want 5", len(pl.Tracks), err)
		}
	})

	t.Run("a page that fails once is retried", func(t *testing.T) {
		failures := new(atomic.Int32)
		failures.Store(1)
		c, hits := pagedAPI(t, failures)
		pl, err := c.GetPlaylist(ctx, "9")
		if err != nil || len(pl.Tracks) != 5 {
			t.Fatalf("GetPlaylist = %d tracks, %v; want 5", len(pl.Tracks), err)
		}
		if n, _ := hits.Load("/playlist/9/tracks?index=2"); n.(*atomic.Int32).Load() != 2 {
			t.Errorf("page 2 requested %d times, want 2", n.(*atomic.Int32).Load())
		}
	})

	t.Run("a page that keeps failing leaves the pages fetched so far", func(t *testing.T) {
		failures := new(atomic.Int32)
		failures.Store(100)
		c, _ := pagedAPI(t, failures)
		pl, err := c.GetPlaylist(ctx, "9")
		if err != nil || len(pl.Tracks) != 2 {
			t.Fatalf("GetPlaylist = %v tracks, %v; want the 2 from page 1 and no error", pl, err)
		}
		if pl.NbTracks != 5 {
			t.Errorf("NbTracks = %d, want the real count (5) so callers can see the list is short", pl.NbTracks)
		}

		got, err := c.collectPages(ctx, []json.RawMessage{json.RawMessage(`{"id":1}`)}, "https://api.deezer.com/playlist/9/tracks?index=2")
		if err == nil || len(got) != 1 {
			t.Errorf("collectPages = %d entries, %v; want the 1 initial entry and an error", len(got), err)
		}
	})

	t.Run("an album keeps the pages fetched so far too", func(t *testing.T) {
		c := &Client{http: newHTTPClient(nil)}
		c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/album/7" {
				return fakeResp(r, 200, `{"id":7,"title":"Album","nb_tracks":3,"tracks":{"data":[{"id":1,"title":"a"},{"id":2,"title":"b"}],"next":"https://api.deezer.com/album/7/tracks?index=2"}}`), nil
			}
			return fakeResp(r, 500, ""), nil
		})
		album, err := c.GetAlbum(ctx, "7")
		if err != nil || len(album.Tracks) != 2 || album.NbTracks != 3 {
			t.Fatalf("GetAlbum = %+v, %v; want 2 of 3 tracks and no error", album, err)
		}
	})

	t.Run("a next link to another host is not followed", func(t *testing.T) {
		c := &Client{http: newHTTPClient(nil)}
		c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
			t.Errorf("unexpected request %s", r.URL)
			return fakeResp(r, 500, ""), nil
		})
		for _, next := range []string{"https://evildeezer.com/x", "https://deezer.com.attacker.com/x", "https://127.0.0.1/x"} {
			got, err := c.collectPages(ctx, []json.RawMessage{json.RawMessage(`{}`)}, next)
			if err != nil || len(got) != 1 {
				t.Errorf("collectPages(%q) = %d entries, %v", next, len(got), err)
			}
		}
	})
}

func TestGetAlbumInheritsAlbumMetadata(t *testing.T) {
	c := &Client{http: newHTTPClient(nil)}
	c.http.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/album/7" {
			t.Errorf("unexpected request %s", r.URL)
		}
		return fakeResp(r, 200, `{"id":7,"title":"Album","release_date":"2001-03-12","genres":{"data":[{"name":"Electro"}]},
			"artist":{"name":"Band"},"tracks":{"data":[{"id":1,"title":"a"},{"id":2,"title":"b","release_date":"1999-01-01"}]}}`), nil
	})
	album, err := c.GetAlbum(context.Background(), "7")
	if err != nil || len(album.Tracks) != 2 {
		t.Fatalf("GetAlbum = %+v, %v", album, err)
	}
	if got := album.Tracks[0]; got.ReleaseDate != "2001-03-12" || got.Genre != "Electro" || got.Album != "Album" || got.AlbumID != 7 {
		t.Errorf("track 1 = %+v, want the album's date, genre and title", got)
	}
	if got := album.Tracks[1].ReleaseDate; got != "1999-01-01" {
		t.Errorf("track 2 release date = %q, want its own", got)
	}
}
