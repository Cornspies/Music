package server

import (
	"image"
	_ "image/jpeg"
	"net/http"
	"os"
	"testing"
)

func TestPlaylistCover(t *testing.T) {
	c, s := newTestClient(t)
	ownerToken, _ := c.register("owner", "owner-password")
	otherToken, _ := c.register("other", "other-password")

	_, p := c.do("POST", "/api/playlists", ownerToken, map[string]string{"name": "Road trip"})
	id := p["id"].(string)
	if p["coverVersion"] != "" {
		t.Fatalf("new playlist has a cover: %v", p["coverVersion"])
	}

	if code, _ := c.do("POST", "/api/playlists/"+id+"/cover", otherToken, imageUpload(t, 40, 40)); code == http.StatusOK {
		t.Fatal("someone else set the cover")
	}
	code, p := c.do("POST", "/api/playlists/"+id+"/cover", ownerToken, imageUpload(t, 1200, 800))
	if code != http.StatusOK || p["coverVersion"] == "" {
		t.Fatalf("upload cover: %d %v", code, p)
	}

	req, _ := http.NewRequest("GET", c.srv.URL+"/api/playlists/"+id+"/cover", nil)
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(res.Body)
	res.Body.Close()
	if err != nil || format != "jpeg" || cfg.Width != playlistCoverSize || cfg.Height != playlistCoverSize {
		t.Fatalf("cover is %s %dx%d (%v)", format, cfg.Width, cfg.Height, err)
	}

	if code, _ := c.do("GET", "/api/playlists/"+id+"/cover", otherToken, nil); code != http.StatusNotFound {
		t.Fatalf("private playlist cover visible to others: %d", code)
	}

	code, p = c.do("DELETE", "/api/playlists/"+id+"/cover", ownerToken, nil)
	if code != http.StatusOK || p["coverVersion"] != "" {
		t.Fatalf("remove cover: %d %v", code, p)
	}
	if code, _ := c.do("GET", "/api/playlists/"+id+"/cover", ownerToken, nil); code != http.StatusNotFound {
		t.Fatalf("removed cover still served: %d", code)
	}

	c.do("POST", "/api/playlists/"+id+"/cover", ownerToken, imageUpload(t, 40, 40))
	c.do("DELETE", "/api/playlists/"+id, ownerToken, nil)
	if _, err := os.Stat(s.playlistCoverPath(id)); !os.IsNotExist(err) {
		t.Fatalf("cover file left behind after the playlist was deleted: %v", err)
	}
}
