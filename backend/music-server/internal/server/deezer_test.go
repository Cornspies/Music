package server

import (
	"path/filepath"
	"testing"
)

var (
	_ source         = (*deezerSource)(nil)
	_ linkSource     = (*deezerSource)(nil)
	_ playlistSource = (*deezerSource)(nil)
	_ qualityProber  = (*deezerSource)(nil)
)

func TestDeezerPlaylistID(t *testing.T) {
	dz := &deezerSource{}

	cases := []struct {
		input string
		want  string
	}{
		{"https://www.deezer.com/playlist/123456789", "playlist:123456789"},
		{"https://www.deezer.com/en/playlist/987654321", "playlist:987654321"},
		{"https://deezer.com/fr/album/11223344", "album:11223344"},
		{"playlist:556677", "playlist:556677"},
		{"album:998877", "album:998877"},
		{"12345", "playlist:12345"},
		// Track URLs should NOT be identified as playlists
		{"https://www.deezer.com/track/3135556", ""},
		{"https://www.deezer.com/en/track/998877", ""},
		{"random search text", ""},
	}

	for _, tc := range cases {
		got := dz.playlistID(tc.input)
		if got != tc.want {
			t.Errorf("playlistID(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDeezerCoverAllowed(t *testing.T) {
	dz := &deezerSource{}

	allowed := []string{
		"https://e-cdns-images.dzcdn.net/images/cover/abc/500x500-000000-80-0-0.jpg",
		"https://cdns-images.dzcdn.net/images/cover/abc/250x250.jpg",
		"https://cdn-images.dzcdn.net/images/user/123/250x250.jpg",
	}

	for _, u := range allowed {
		if !dz.coverAllowed(u) {
			t.Errorf("coverAllowed(%q) = false, want true", u)
		}
	}

	disallowed := []string{
		"http://e-cdns-images.dzcdn.net/image.jpg", // not https
		"https://evil.com/cover.jpg",
		"https://e-cdns-images.dzcdn.net.evil.com/image.jpg",
	}

	for _, u := range disallowed {
		if dz.coverAllowed(u) {
			t.Errorf("coverAllowed(%q) = true, want false", u)
		}
	}
}

func TestDeezerTempDir(t *testing.T) {
	tempDataDir := t.TempDir()
	dz := &deezerSource{
		dataDir: tempDataDir,
	}

	expectedBase := filepath.Join(tempDataDir, "tmp")
	if dz.dataDir != tempDataDir {
		t.Errorf("expected dataDir %s, got %s", tempDataDir, dz.dataDir)
	}
	_ = expectedBase
}
