package server

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"music-server/internal/deezer"
)

type stubTransport func(*http.Request) (*http.Response, error)

func (f stubTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubResponse(r *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode:    200,
		Header:        http.Header{},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       r,
	}
}

func newFallbackTestSource(t *testing.T, entries string) (*deezerSource, func() (int, int)) {
	t.Helper()

	orig := http.DefaultTransport
	http.DefaultTransport = stubTransport(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "api.deezer.com" && r.URL.Path == "/track/1":
			return stubResponse(r, `{"id":1,"title":"Song","duration":200,"artist":{"id":2,"name":"Artist"},"album":{"id":3,"title":"Album"}}`), nil
		case r.URL.Host == "api.deezer.com" && r.URL.Path == "/album/5":
			return stubResponse(r, `{"id":5,"title":"Album","nb_tracks":1,"artist":{"name":"Artist"},"tracks":{"data":[{"id":1,"title":"Song","duration":200,"artist":{"name":"Artist"}}]}}`), nil
		case r.URL.Host == "api.deezer.com" && r.URL.Path == "/search/track":
			return stubResponse(r, `{"data":[],"total":0}`), nil
		case r.URL.Host == "www.deezer.com" && r.Method == http.MethodPost:
			return stubResponse(r, `{"results":{"checkForm":"csrf","USER":{"USER_ID":0,"OPTIONS":{}}}}`), nil
		case r.URL.Host == "www.deezer.com":
			return stubResponse(r, "{}"), nil
		}
		t.Errorf("unexpected request %s", r.URL)
		return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = orig })

	client, err := deezer.NewClient("", "")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	entriesPath := filepath.Join(dir, "entries.json")
	logPath := filepath.Join(dir, "calls.log")
	if err := os.WriteFile(entriesPath, []byte(entries), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$*\" >> '" + logPath + "'\n" +
		"case \"$*\" in\n  *ytsearch*) cat '" + entriesPath + "' ;;\n  *) echo 'download refused' >&2; exit 1 ;;\nesac\n"
	bin := filepath.Join(dir, "yt-dlp")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	d := &deezerSource{client: client, yt: &ytdlpSource{bin: bin}}
	counts := func() (searches, downloads int) {
		raw, _ := os.ReadFile(logPath)
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			switch {
			case strings.Contains(line, "ytsearch"):
				searches++
			case line != "":
				downloads++
			}
		}
		return
	}
	return d, counts
}

func TestFetchYTOldFallbackNoMatchSearchesEachQueryOnce(t *testing.T) {
	d, counts := newFallbackTestSource(t,
		`{"entries":[{"id":"aaaaaaaaaaa","title":"Completely Different Tune","channel":"Someone","duration":200}]}`)

	_, err := d.fetchYTOldFallback(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "no matching recording") {
		t.Fatalf("err = %v, want a no-match error", err)
	}
	if searches, downloads := counts(); searches != 3 || downloads != 0 {
		t.Errorf("searches = %d, downloads = %d; want 3 and 0", searches, downloads)
	}
}

func TestFetchYTOldFallbackRelaxedMatchReusesSearchResults(t *testing.T) {
	d, counts := newFallbackTestSource(t,
		`{"entries":[{"id":"aaaaaaaaaaa","title":"Artist - Song","channel":"Artist","duration":207}]}`)

	_, err := d.fetchYTOldFallback(context.Background(), "1")
	if err == nil || strings.Contains(err.Error(), "no matching recording") {
		t.Fatalf("err = %v, want the download attempt's error (a match was found)", err)
	}
	if searches, downloads := counts(); searches != 3 || downloads != 1 {
		t.Errorf("searches = %d, downloads = %d; want 3 and 1", searches, downloads)
	}
}

func TestFetchYTOldFallbackRejectsLengthsBeyondSlack(t *testing.T) {
	d, counts := newFallbackTestSource(t,
		`{"entries":[{"id":"aaaaaaaaaaa","title":"Artist - Song","channel":"Artist","duration":212}]}`)

	_, err := d.fetchYTOldFallback(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), "no matching recording") {
		t.Fatalf("err = %v, want a no-match error", err)
	}
	if _, downloads := counts(); downloads != 0 {
		t.Errorf("downloaded a %ds-off upload", 12)
	}
}

func TestFetchYTOldFallbackStrictMatchStopsSearching(t *testing.T) {
	d, counts := newFallbackTestSource(t,
		`{"entries":[{"id":"aaaaaaaaaaa","title":"Artist - Song","channel":"Artist","duration":201}]}`)

	if _, err := d.fetchYTOldFallback(context.Background(), "1"); err == nil {
		t.Fatal("expected the stub download to fail")
	}
	if searches, downloads := counts(); searches != 1 || downloads != 1 {
		t.Errorf("searches = %d, downloads = %d; want 1 and 1", searches, downloads)
	}
}

func TestDeezerSearchTrackLinks(t *testing.T) {
	d, _ := newFallbackTestSource(t, `{"entries":[]}`)
	ctx := context.Background()

	for _, q := range []string{
		"https://www.deezer.com/track/1",
		"https://www.deezer.com/fr/track/1?utm_source=x",
		"look at https://www.deezer.com/track/1 !",
	} {
		results, _, err := d.search(ctx, q, 1)
		if err != nil || len(results) != 1 || results[0].ID != "1" || results[0].Title != "Song" {
			t.Errorf("search(%q) = %+v, %v; want track 1", q, results, err)
		}
	}

	results, _, err := d.search(ctx, "https://www.deezer.com/en/album/5", 1)
	if err != nil || len(results) != 1 || results[0].ID != "1" || results[0].Album != "Album" {
		t.Errorf("search(album link) = %+v, %v; want the album's one track", results, err)
	}

	for _, q := range []string{"https://evildeezer.com/track/1", "https://example.com/?u=deezer.com/track/1"} {
		results, _, err := d.search(ctx, q, 1)
		if err != nil || len(results) != 0 {
			t.Errorf("search(%q) = %+v, %v; want a text search with no results", q, results, err)
		}
	}
}
