package server

import (
	"bytes"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "image/gif"
	_ "image/png"
)

const (
	maxAvatarUploadBytes = 8 << 20
	avatarSize           = 256
	maxAvatarPixels      = 50_000_000
)

func (s *Server) avatarPath(userID string) string {
	return filepath.Join(s.avaDir, userID+".jpg")
}

func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)

	raw, ok := s.readUploadedImage(w, r)
	if !ok {
		return
	}

	square, err := decodeSquareJPEG(raw, parseCropRect(r), avatarSize)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that file isn't a readable image (JPEG, PNG or GIF)")
		return
	}

	if err := os.MkdirAll(s.avaDir, 0o755); err != nil {
		s.internalErr(w, "create avatar dir", err)
		return
	}
	target := s.avatarPath(u.ID)
	tmp := target + ".part"
	if err := os.WriteFile(tmp, square, 0o644); err != nil {
		s.internalErr(w, "save avatar", err)
		return
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		s.internalErr(w, "save avatar", err)
		return
	}

	if _, err := s.db.Exec(`UPDATE users SET has_avatar = 1 WHERE id = ?`, u.ID); err != nil {
		s.internalErr(w, "flag avatar", err)
		return
	}
	s.writeCurrentUser(w, u)
}

func (s *Server) readUploadedImage(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarUploadBytes)
	if err := r.ParseMultipartForm(maxAvatarUploadBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "image is too large (8 MB maximum)")
		return nil, false
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "no image supplied")
		return nil, false
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxAvatarUploadBytes))
	if err != nil {
		s.internalErr(w, "read image", err)
		return nil, false
	}
	return raw, true
}

func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	os.Remove(s.avatarPath(u.ID))
	if _, err := s.db.Exec(`UPDATE users SET has_avatar = 0 WHERE id = ?`, u.ID); err != nil {
		s.internalErr(w, "clear avatar", err)
		return
	}
	s.writeCurrentUser(w, u)
}

func (s *Server) handleUserAvatar(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var hasAvatar int
	err := s.db.QueryRow(`SELECT has_avatar FROM users WHERE id = ?`, id).Scan(&hasAvatar)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && hasAvatar == 0) {
		writeErr(w, http.StatusNotFound, "no avatar")
		return
	} else if err != nil {
		s.internalErr(w, "lookup avatar", err)
		return
	}

	b, err := os.ReadFile(s.avatarPath(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, "no avatar")
		return
	}

	serveJPEG(w, r, b)
}

func serveJPEG(w http.ResponseWriter, r *http.Request, b []byte) {
	sum := sha1.Sum(b)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

func (s *Server) writeCurrentUser(w http.ResponseWriter, u *User) {
	user, err := s.publicUserByID(u.ID)
	if err != nil {
		s.internalErr(w, "lookup user", err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

type cropRect struct {
	X, Y, Size float64
}

func (c cropRect) isSet() bool { return c.Size > 0 }

func parseCropRect(r *http.Request) cropRect {
	num := func(name string) (float64, bool) {
		v := strings.TrimSpace(r.FormValue(name))
		if v == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	x, okX := num("cropX")
	y, okY := num("cropY")
	size, okS := num("cropSize")
	if !okX || !okY || !okS {
		return cropRect{}
	}
	if size <= 0 || size > 1 || x < 0 || y < 0 || x > 1 || y > 1 {
		return cropRect{}
	}
	return cropRect{X: x, Y: y, Size: size}
}

func decodeSquareJPEG(raw []byte, crop cropRect, size int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("empty image")
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxAvatarPixels {
		return nil, fmt.Errorf("image is too large (%dx%d)", cfg.Width, cfg.Height)
	}

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("empty image")
	}

	shorter := b.Dx()
	if b.Dy() < shorter {
		shorter = b.Dy()
	}
	edge := shorter
	offX := b.Min.X + (b.Dx()-edge)/2
	offY := b.Min.Y + (b.Dy()-edge)/2

	if crop.isSet() {
		edge = int(crop.Size * float64(shorter))
		if edge < 1 {
			edge = 1
		}
		offX = b.Min.X + int(crop.X*float64(b.Dx()))
		offY = b.Min.Y + int(crop.Y*float64(b.Dy()))
		if offX+edge > b.Max.X {
			offX = b.Max.X - edge
		}
		if offY+edge > b.Max.Y {
			offY = b.Max.Y - edge
		}
		if offX < b.Min.X {
			offX = b.Min.X
		}
		if offY < b.Min.Y {
			offY = b.Min.Y
		}
	}

	region := croppedImage{src, image.Rect(offX, offY, offX+edge, offY+edge)}
	var out *image.RGBA
	if edge > size {
		out = downscale(region, size)
	} else {
		out = image.NewRGBA(image.Rect(0, 0, size, size))
		draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
		for y := 0; y < size; y++ {
			srcY := offY + y*edge/size
			for x := 0; x < size; x++ {
				srcX := offX + x*edge/size
				out.Set(x, y, src.At(srcX, srcY))
			}
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type croppedImage struct {
	image.Image
	rect image.Rectangle
}

func (c croppedImage) Bounds() image.Rectangle { return c.rect }
