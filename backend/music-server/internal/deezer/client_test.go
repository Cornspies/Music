package deezer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeResp(r *http.Request, status int, body string, headers ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(headers); i += 2 {
		h.Set(headers[i], headers[i+1])
	}
	return &http.Response{
		StatusCode:    status,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       r,
	}
}

func newTestClient(arl string, rt rtFunc) *Client {
	c := &Client{http: newHTTPClient(nil), arl: arl}
	c.http.Transport = rt
	return c
}

func TestParseURLShortLinks(t *testing.T) {
	var evilHits atomic.Int32
	c := newTestClient("", func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "deezer.page.link":
			switch r.URL.Path {
			case "/abc":
				return fakeResp(r, 302, "", "Location", "https://link.deezer.com/s/xyz"), nil
			case "/evil":
				return fakeResp(r, 302, "", "Location", "https://evil.example/?u=deezer.com/track/9"), nil
			case "/loop":
				return fakeResp(r, 302, "", "Location", "https://deezer.page.link/loop"), nil
			}
		case "link.deezer.com":
			return fakeResp(r, 302, "", "Location", "/en/track/3135556"), nil
		case "evil.example":
			evilHits.Add(1)
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		return fakeResp(r, 404, ""), nil
	})

	for _, in := range []string{
		"https://deezer.page.link/abc",
		"http://deezer.page.link/abc",
		"deezer.page.link/abc",
		"  https://deezer.page.link/abc  ",
	} {
		kind, id, err := c.ParseURL(context.Background(), in)
		if err != nil || kind != "track" || id != "3135556" {
			t.Errorf("ParseURL(%q) = %q, %q, %v; want track 3135556", in, kind, id, err)
		}
	}

	for _, in := range []string{"https://deezer.page.link/evil", "https://deezer.page.link/loop"} {
		if kind, id, err := c.ParseURL(context.Background(), in); err == nil {
			t.Errorf("ParseURL(%q) = %q, %q, want error", in, kind, id)
		}
	}
	if n := evilHits.Load(); n != 0 {
		t.Errorf("followed a redirect to a foreign host %d times", n)
	}

	if kind, id, err := c.ParseURL(context.Background(), "https://www.deezer.com/fr/album/42"); err != nil || kind != "album" || id != "42" {
		t.Errorf("album URL = %q, %q, %v", kind, id, err)
	}
}

func TestClientRefusesForeignRedirects(t *testing.T) {
	var evilHits atomic.Int32
	c := newTestClient("", func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://www.deezer.com/to-evil":
			return fakeResp(r, 302, "", "Location", "https://evil.example/x"), nil
		case "https://www.deezer.com/to-http":
			return fakeResp(r, 302, "", "Location", "http://www.deezer.com/x"), nil
		case "https://www.deezer.com/to-cdn":
			return fakeResp(r, 302, "", "Location", "https://e-cdns-proxy-a.dzcdn.net/ok"), nil
		case "https://e-cdns-proxy-a.dzcdn.net/ok":
			return fakeResp(r, 200, "fine"), nil
		}
		if r.URL.Host == "evil.example" || r.URL.Scheme == "http" {
			evilHits.Add(1)
		}
		return fakeResp(r, 404, ""), nil
	})

	for _, path := range []string{"to-evil", "to-http"} {
		req, _ := c.newRequest(context.Background(), http.MethodGet, "https://www.deezer.com/"+path, nil)
		if resp, err := c.http.Do(req); err == nil {
			resp.Body.Close()
			t.Errorf("redirect via %s was followed", path)
		}
	}
	if n := evilHits.Load(); n != 0 {
		t.Errorf("transport reached a disallowed target %d times", n)
	}

	req, _ := c.newRequest(context.Background(), http.MethodGet, "https://www.deezer.com/to-cdn", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("redirect to dzcdn.net failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRefreshLicense(t *testing.T) {
	var requests atomic.Int32
	license := ""
	rt := func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.Method == http.MethodPost {
			return fakeResp(r, 200, `{"results":{"checkForm":"csrf","USER":{"USER_ID":0,"OPTIONS":{"license_token":"`+license+`"}}}}`), nil
		}
		return fakeResp(r, 200, "{}"), nil
	}
	ctx := context.Background()

	c := newTestClient("expired-arl", rt)
	if got := c.refreshLicense(ctx, ""); got != "" {
		t.Fatalf("refreshLicense = %q, want empty", got)
	}
	if n := requests.Load(); n != 2 {
		t.Fatalf("first refresh made %d requests, want 2 (one login)", n)
	}
	for i := 0; i < 3; i++ {
		c.refreshLicense(ctx, "")
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("refreshes within the cooldown made %d extra requests", n-2)
	}

	c.mu.Lock()
	c.licenseToken = "fresh"
	c.mu.Unlock()
	if got := c.refreshLicense(ctx, "old"); got != "fresh" {
		t.Errorf("refreshLicense(old) = %q, want fresh", got)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("returning an already-refreshed token made %d requests", n-2)
	}

	license = "newer"
	c.lastLogin = time.Now().Add(-2 * relogInterval)
	if got := c.refreshLicense(ctx, "fresh"); got != "newer" {
		t.Errorf("refreshLicense after cooldown = %q, want newer", got)
	}
	if n := requests.Load(); n != 4 {
		t.Errorf("requests = %d, want 4", n)
	}

	requests.Store(0)
	anon := newTestClient("", rt)
	anon.refreshLicense(ctx, "")
	if n := requests.Load(); n != 0 {
		t.Errorf("anonymous refresh made %d requests", n)
	}
}

func TestGetTrackDownloadURLFallback(t *testing.T) {
	const md5Origin = "0123456789abcdef0123456789abcdef"
	var apiHits atomic.Int32
	c := newTestClient("arl", func(r *http.Request) (*http.Response, error) {
		switch r.URL.Hostname() {
		case "media.deezer.com":
			return fakeResp(r, 200, `{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"https://cdns-proxy-a.dzcdn.net/stream"}]}]}]}`), nil
		case "e-cdns-proxy-0.dzcdn.net":
			return fakeResp(r, 206, "ab"), nil
		}
		apiHits.Add(1)
		t.Errorf("unexpected request %s", r.URL)
		return fakeResp(r, 500, ""), nil
	})
	c.licenseToken = "lic"
	ctx := context.Background()

	t.Run("mobile CDN fallback track", func(t *testing.T) {
		data := map[string]any{
			"SNG_ID": "1",
			"FALLBACK": map[string]any{
				"SNG_ID": "2", "MD5_ORIGIN": md5Origin, "MEDIA_VERSION": "3",
			},
		}
		u, q, err := c.GetTrackDownloadURL(ctx, data, QualityMP3_128, false)
		if err != nil || u == "" || q != QualityMP3_128 {
			t.Fatalf("GetTrackDownloadURL = %q, %q, %v", u, q, err)
		}
		if data["SNG_ID"] != "2" || data["MD5_ORIGIN"] != md5Origin || data["MEDIA_VERSION"] != "3" {
			t.Errorf("trackData not switched to the fallback track: %v", data)
		}
	})

	t.Run("media API fallback track", func(t *testing.T) {
		data := map[string]any{
			"SNG_ID":   "1",
			"FALLBACK": map[string]any{"SNG_ID": "2", "TRACK_TOKEN": "tok"},
		}
		u, _, err := c.GetTrackDownloadURL(ctx, data, QualityMP3_128, false)
		if err != nil || u != "https://cdns-proxy-a.dzcdn.net/stream" {
			t.Fatalf("GetTrackDownloadURL = %q, %v", u, err)
		}
		if data["SNG_ID"] != "2" {
			t.Errorf("SNG_ID = %v, want 2", data["SNG_ID"])
		}
	})

	t.Run("ignores fallback without SNG_ID", func(t *testing.T) {
		data := map[string]any{
			"SNG_ID":   "1",
			"FALLBACK": map[string]any{"TRACK_TOKEN": "tok"},
		}
		if u, _, err := c.GetTrackDownloadURL(ctx, data, QualityMP3_128, false); err == nil {
			t.Fatalf("resolved %q through a fallback with no SNG_ID", u)
		}
		if data["SNG_ID"] != "1" {
			t.Errorf("SNG_ID changed to %v", data["SNG_ID"])
		}
	})

	if n := apiHits.Load(); n != 0 {
		t.Errorf("%d unexpected API requests", n)
	}
}

func TestGetMediaURLRefreshesExpiredSession(t *testing.T) {
	var logins, mediaCalls atomic.Int32
	loginLicense := "new"
	c := newTestClient("arl", func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "media.deezer.com" {
			mediaCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"license_token":"new"`) {
				return fakeResp(r, 200, `{"data":[{"media":[{"format":"MP3_128","sources":[{"url":"https://cdns-proxy-a.dzcdn.net/s"}]}]}]}`), nil
			}
			return fakeResp(r, 403, `{"errors":[{"code":2002}]}`), nil
		}
		if r.Method == http.MethodPost {
			logins.Add(1)
			return fakeResp(r, 200, `{"results":{"checkForm":"csrf","USER":{"USER_ID":0,"OPTIONS":{"license_token":"`+loginLicense+`"}}}}`), nil
		}
		return fakeResp(r, 200, "{}"), nil
	})
	c.licenseToken = "old"
	ctx := context.Background()

	u, q, ok := c.getMediaURL(ctx, "tok", QualityMP3_128, false)
	if !ok || u != "https://cdns-proxy-a.dzcdn.net/s" || q != QualityMP3_128 {
		t.Fatalf("getMediaURL = %q, %q, %v", u, q, ok)
	}
	if logins.Load() != 1 || mediaCalls.Load() != 2 {
		t.Errorf("logins = %d, media calls = %d; want 1 and 2", logins.Load(), mediaCalls.Load())
	}

	c.mu.Lock()
	c.licenseToken = "other"
	c.mu.Unlock()
	loginLicense = "other"
	c.lastLogin = time.Time{}
	logins.Store(0)
	mediaCalls.Store(0)
	if _, _, ok := c.getMediaURL(ctx, "tok", QualityMP3_128, false); ok {
		t.Fatal("getMediaURL succeeded with a rejected license")
	}
	if logins.Load() != 1 || mediaCalls.Load() != 1 {
		t.Errorf("logins = %d, media calls = %d; want 1 and 1 (no retry with an unchanged token)", logins.Load(), mediaCalls.Load())
	}

	for i := 0; i < 3; i++ {
		c.getMediaURL(ctx, "tok", QualityMP3_128, false)
	}
	if logins.Load() != 1 {
		t.Errorf("logins during cooldown = %d, want 1", logins.Load())
	}
}

type testBody struct {
	ctx   context.Context
	data  []byte
	chunk int
	delay time.Duration
	end   error
	stall bool
}

func (b *testBody) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		switch {
		case b.stall:
			<-b.ctx.Done()
			return 0, b.ctx.Err()
		case b.end != nil:
			return 0, b.end
		}
		return 0, io.EOF
	}
	if b.delay > 0 {
		select {
		case <-time.After(b.delay):
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		}
	}
	n := min(len(p), len(b.data))
	if b.chunk > 0 {
		n = min(n, b.chunk)
	}
	copy(p, b.data[:n])
	b.data = b.data[n:]
	return n, nil
}

func (b *testBody) Close() error { return nil }

func downloadFixture(t *testing.T, serve func(r *http.Request) *http.Response) *Client {
	t.Helper()
	const md5Origin = "0123456789abcdef0123456789abcdef"
	c := newTestClient("", func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Hostname() == "www.deezer.com":
			return fakeResp(r, 200, `{"results":{"SNG_ID":"1","SNG_TITLE":"Song","ART_NAME":"Artist","FALLBACK":{"SNG_ID":"2","MD5_ORIGIN":"`+md5Origin+`","MEDIA_VERSION":"3"}}}`), nil
		case strings.HasSuffix(r.URL.Hostname(), "dzcdn.net") && r.Header.Get("Range") != "":
			return fakeResp(r, 206, "ab"), nil
		case strings.HasSuffix(r.URL.Hostname(), "dzcdn.net"):
			resp := serve(r)
			if resp == nil {
				return nil, r.Context().Err()
			}
			return resp, nil
		}
		t.Errorf("unexpected request %s", r.URL)
		return fakeResp(r, 500, ""), nil
	})
	c.token = "t"
	c.preferredQuality = QualityMP3_128
	return c
}

func bodyResp(r *http.Request, contentLength int64, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body, ContentLength: contentLength, Request: r}
}

func TestDownloadTrack(t *testing.T) {
	payload := make([]byte, 5000)
	for i := range payload {
		payload[i] = byte(i)
	}
	ctx := context.Background()

	t.Run("complete download keeps the requested track's metadata", func(t *testing.T) {
		c := downloadFixture(t, func(r *http.Request) *http.Response {
			return bodyResp(r, int64(len(payload)), &testBody{ctx: r.Context(), data: payload})
		})
		dst := filepath.Join(t.TempDir(), "out")
		q, ext, track, err := c.DownloadTrack(ctx, "1", dst, 0)
		if err != nil || q != QualityMP3_128 || ext != ".mp3" {
			t.Fatalf("DownloadTrack = %q, %q, %v", q, ext, err)
		}
		if track.ID != 1 || !strings.HasSuffix(track.Link, "/track/1") || track.Title != "Song" {
			t.Errorf("track = %+v, want ID 1 and link to /track/1", track)
		}
		if fi, err := os.Stat(dst); err != nil || fi.Size() != int64(len(payload)) {
			t.Errorf("output file: %v, %v", fi, err)
		}
	})

	t.Run("short body with a clean EOF is rejected", func(t *testing.T) {
		c := downloadFixture(t, func(r *http.Request) *http.Response {
			return bodyResp(r, 8000, &testBody{ctx: r.Context(), data: payload})
		})
		dst := filepath.Join(t.TempDir(), "out")
		_, _, _, err := c.DownloadTrack(ctx, "1", dst, 0)
		if err == nil || !strings.Contains(err.Error(), "received 5000 of 8000") {
			t.Fatalf("err = %v, want a short-download error", err)
		}
		if _, serr := os.Stat(dst); serr == nil {
			t.Error("partial file was left behind")
		}
	})

	t.Run("connection cut mid-body is rejected", func(t *testing.T) {
		c := downloadFixture(t, func(r *http.Request) *http.Response {
			return bodyResp(r, 8000, &testBody{ctx: r.Context(), data: payload, end: io.ErrUnexpectedEOF})
		})
		dst := filepath.Join(t.TempDir(), "out")
		_, _, _, err := c.DownloadTrack(ctx, "1", dst, 0)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
		}
		if _, serr := os.Stat(dst); serr == nil {
			t.Error("partial file was left behind")
		}
	})

	t.Run("transfer longer than the API timeout succeeds while data keeps flowing", func(t *testing.T) {
		c := downloadFixture(t, func(r *http.Request) *http.Response {
			return bodyResp(r, int64(len(payload)), &testBody{ctx: r.Context(), data: payload, chunk: 500, delay: 40 * time.Millisecond})
		})
		c.http.Timeout = 150 * time.Millisecond
		dst := filepath.Join(t.TempDir(), "out")
		if _, _, _, err := c.DownloadTrack(ctx, "1", dst, 0); err != nil {
			t.Fatalf("slow but steady download failed: %v", err)
		}
	})

	t.Run("stalled transfer is cut off", func(t *testing.T) {
		old := downloadIdleTimeout
		downloadIdleTimeout = 100 * time.Millisecond
		t.Cleanup(func() { downloadIdleTimeout = old })

		c := downloadFixture(t, func(r *http.Request) *http.Response {
			return bodyResp(r, 8000, &testBody{ctx: r.Context(), data: payload[:2048], stall: true})
		})
		start := time.Now()
		_, _, _, err := c.DownloadTrack(ctx, "1", filepath.Join(t.TempDir(), "out"), 0)
		if err == nil || !strings.Contains(err.Error(), "no data received") {
			t.Fatalf("err = %v, want a stall error", err)
		}
		if time.Since(start) > 5*time.Second {
			t.Errorf("took %s to give up", time.Since(start))
		}
	})

	t.Run("server that never answers is cut off", func(t *testing.T) {
		old := downloadIdleTimeout
		downloadIdleTimeout = 100 * time.Millisecond
		t.Cleanup(func() { downloadIdleTimeout = old })

		c := downloadFixture(t, func(r *http.Request) *http.Response {
			<-r.Context().Done()
			return nil
		})
		_, _, _, err := c.DownloadTrack(ctx, "1", filepath.Join(t.TempDir(), "out"), 0)
		if err == nil || !strings.Contains(err.Error(), "no data received") {
			t.Fatalf("err = %v, want a stall error", err)
		}
	})

	t.Run("caller cancellation is not reported as a stall", func(t *testing.T) {
		c := downloadFixture(t, func(r *http.Request) *http.Response {
			return bodyResp(r, 8000, &testBody{ctx: r.Context(), stall: true})
		})
		cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, _, _, err := c.DownloadTrack(cctx, "1", filepath.Join(t.TempDir(), "out"), 0)
		if err == nil || strings.Contains(err.Error(), "no data received") {
			t.Fatalf("err = %v, want a plain cancellation error", err)
		}
	})
}

func TestLoginClearsUserWhenARLExpires(t *testing.T) {
	uid := "5"
	c := newTestClient("arl", func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return fakeResp(r, 200, `{"results":{"checkForm":"c","USER":{"USER_ID":`+uid+`,"BLOG_NAME":"bob","OPTIONS":{"license_token":"L"}}}}`), nil
		}
		return fakeResp(r, 200, "{}"), nil
	})
	if err := c.login(context.Background()); err != nil || c.User() == nil || c.User().Name != "bob" {
		t.Fatalf("first login: user = %+v, err = %v", c.User(), err)
	}
	uid = "0"
	if err := c.login(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u := c.User(); u != nil {
		t.Errorf("User() = %+v after the ARL stopped being accepted, want nil", u)
	}
}

func TestNormalizeQuality(t *testing.T) {
	cases := []struct {
		in     string
		hasARL bool
		want   string
	}{
		{"flac", true, QualityFLAC},
		{" mp3_256 ", false, QualityMP3_256},
		{"", true, QualityMP3_320},
		{"", false, QualityMP3_128},
		{"HIFI", true, QualityMP3_320},
		{"flacc", false, QualityMP3_128},
	}
	for _, tc := range cases {
		if got := normalizeQuality(tc.in, tc.hasARL); got != tc.want {
			t.Errorf("normalizeQuality(%q, %v) = %q, want %q", tc.in, tc.hasARL, got, tc.want)
		}
	}
}
