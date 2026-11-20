package server

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestAppUpdateOffersOnlyANewerSignedBuild(t *testing.T) {
	c, s := newTestClient(t)
	token, _ := c.register("admin", "admin-password")
	dir := s.apkDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ask := func(current, base string) (int, map[string]any) {
		q := url.Values{"platform": {"appimage"}, "current": {current}, "base": {base}}
		return c.do("GET", "/api/app/update?"+q.Encode(), token, nil)
	}
	const base = "https://music.example.org/sub"
	signature := base64.StdEncoding.EncodeToString([]byte("untrusted comment: signature\nabc\n"))

	put("Music_1.2.0_amd64.AppImage", "build")
	if code, _ := ask("1.0.0", base); code != http.StatusNoContent {
		t.Fatalf("a build without a signature was offered: %d", code)
	}
	put("Music_1.2.0_amd64.AppImage.sig", "not base64 !")
	if code, _ := ask("1.0.0", base); code != http.StatusNoContent {
		t.Fatalf("a build with a broken signature file was offered: %d", code)
	}

	put("Music_1.2.0_amd64.AppImage.sig", signature+"\n")
	code, out := ask("1.0.0", base)
	if code != http.StatusOK {
		t.Fatalf("newer signed build: %d %v", code, out)
	}
	if out["version"] != "1.2.0" || out["signature"] != signature ||
		out["url"] != base+"/api/app/releases/Music_1.2.0_amd64.AppImage" {
		t.Fatalf("offer: %v", out)
	}
	for _, current := range []string{"1.2.0", "1.3.0", ""} {
		if code, _ := ask(current, base); code != http.StatusNoContent {
			t.Fatalf("offered to an app on %q: %d", current, code)
		}
	}
	for _, bad := range []string{"", "music.example.org", "ftp://music.example.org", "https://a@music.example.org", "https://music.example.org?x=1"} {
		if code, _ := ask("1.0.0", bad); code != http.StatusBadRequest {
			t.Fatalf("base %q accepted: %d", bad, code)
		}
	}
	if code, list := c.do("GET", "/api/app/releases/latest?platform=appimage&current=1.0.0", token, nil); code != http.StatusOK || list["latest"].(map[string]any)["fileName"] != "Music_1.2.0_amd64.AppImage" {
		t.Fatalf("the signature file changed the release list: %d %v", code, list)
	}
}
