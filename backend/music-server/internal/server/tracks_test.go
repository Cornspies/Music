package server

import (
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRejectedUploadLeavesNoFile(t *testing.T) {
	c, s := newTestClient(t)
	token, _ := c.register("admin", "admin-password")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, name := range []string{"one.mp3", "two.mp3"} {
		part, _ := mw.CreateFormFile("file", name)
		part.Write([]byte("not really audio"))
	}
	mw.Close()
	code, out := c.do("POST", "/api/tracks", token, multipartBody{data: buf.Bytes(), contentType: mw.FormDataContentType()})
	if code != http.StatusBadRequest {
		t.Fatalf("two files in one upload: %d %v", code, out)
	}
	left, err := os.ReadDir(filepath.Join(s.dataDir, "uploads"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("rejected upload left %d file(s) behind: %s", len(left), left[0].Name())
	}
}

func TestOversizedCoverBytesAreRefused(t *testing.T) {
	encode := func(w, h int, noise bool) []byte {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if noise {
			rand.New(rand.NewSource(1)).Read(img.Pix)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	small := encode(100, 100, false)
	if !safeCoverBytes(small) {
		t.Fatal("a small picture was refused")
	}
	padded := append(append([]byte{}, small...), make([]byte, maxArtworkBytes)...)
	if safeCoverBytes(padded) {
		t.Fatalf("a 100x100 picture with %d bytes of padding was accepted", maxArtworkBytes)
	}
	large := encode(2200, 2200, true)
	if len(large) <= maxArtworkBytes {
		t.Fatalf("test picture is only %d bytes", len(large))
	}
	if !safeCoverBytes(large) {
		t.Fatal("a large picture that shrinks to a normal cover was refused")
	}
	if got := len(shrinkCover(large)); got > maxArtworkBytes {
		t.Fatalf("shrunk cover is %d bytes", got)
	}
}
