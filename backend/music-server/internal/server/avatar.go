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

        // Registered for their decoders only; everything is re-encoded as JPEG.
        _ "image/gif"
        _ "image/png"
)

// Avatars are small and displayed at ~20px next to every track, so the upload
// is capped tightly and re-encoded to a square JPEG. Re-encoding also strips
// EXIF (which can carry GPS) and guarantees the stored bytes are really an
// image rather than something disguised as one.
const (
        maxAvatarUploadBytes = 8 << 20 // 8 MB accepted from the client
        avatarSize           = 256     // stored square edge, in pixels
        // Upper bound on decoded pixels (~50 megapixels, roughly 200 MB as
        // RGBA). Generous for a real photo, fatal for a decompression bomb.
        maxAvatarPixels = 50_000_000
)

func (s *Server) avatarPath(userID string) string {
        return filepath.Join(s.avaDir, userID+".jpg")
}

// handleUploadAvatar accepts a multipart "file" field and stores a normalized
// square JPEG for the signed-in user.
func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)

        r.Body = http.MaxBytesReader(w, r.Body, maxAvatarUploadBytes)
        if err := r.ParseMultipartForm(maxAvatarUploadBytes); err != nil {
                writeErr(w, http.StatusBadRequest, "image is too large (8 MB maximum)")
                return
        }
        defer func() {
                if r.MultipartForm != nil {
                        _ = r.MultipartForm.RemoveAll()
                }
        }()

        file, _, err := r.FormFile("file")
        if err != nil {
                writeErr(w, http.StatusBadRequest, "no image supplied")
                return
        }
        defer file.Close()

        raw, err := io.ReadAll(io.LimitReader(file, maxAvatarUploadBytes))
        if err != nil {
                s.internalErr(w, "read avatar", err)
                return
        }

        square, err := decodeSquareJPEG(raw, parseCropRect(r))
        if err != nil {
                writeErr(w, http.StatusBadRequest, "that file isn't a readable image (JPEG, PNG or GIF)")
                return
        }

        if err := os.MkdirAll(s.avaDir, 0o755); err != nil {
                s.internalErr(w, "create avatar dir", err)
                return
        }
        // Write-then-rename so a failed write can't leave a truncated avatar.
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

func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
        u := userFrom(r)
        os.Remove(s.avatarPath(u.ID))
        if _, err := s.db.Exec(`UPDATE users SET has_avatar = 0 WHERE id = ?`, u.ID); err != nil {
                s.internalErr(w, "clear avatar", err)
                return
        }
        s.writeCurrentUser(w, u)
}

// handleUserAvatar serves another user's avatar. Any signed-in user may fetch
// any avatar — they're shown next to every track in the shared library.
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

// writeCurrentUser re-reads and returns the signed-in user so the client can
// refresh its cached profile after an avatar change.
func (s *Server) writeCurrentUser(w http.ResponseWriter, u *User) {
        var created string
        var hasAvatar int
        err := s.db.QueryRow(`SELECT created_at, has_avatar FROM users WHERE id = ?`, u.ID).Scan(&created, &hasAvatar)
        if err != nil {
                s.internalErr(w, "lookup user", err)
                return
        }
        writeJSON(w, http.StatusOK, publicUser{
                ID: u.ID, Username: u.Username, CreatedAt: created, HasAvatar: hasAvatar == 1,
        })
}

// decodeSquareJPEG centre-crops to a square, scales to avatarSize and encodes
// as JPEG. Scaling is a simple nearest-neighbour sample: at 256px for a 20px
// display that's indistinguishable from a filtered resize and avoids pulling
// in an image-processing dependency.
// cropRect is the region of the source image to keep, in fractions of the
// image's own width and height so it survives any resizing the client did for
// display. A zero value means "centre crop", which is what happens when a
// client doesn't send one.
type cropRect struct {
        X, Y, Size float64 // size is a fraction of the shorter edge
}

func (c cropRect) isSet() bool { return c.Size > 0 }

// parseCropRect reads the optional crop fields from the upload form. Anything
// missing, unparseable or out of range falls back to a centre crop rather than
// failing the upload — a bad crop is not worth rejecting a picture over.
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

func decodeSquareJPEG(raw []byte, crop cropRect) ([]byte, error) {
        // Check the declared dimensions before decoding: a ~1 MB PNG can declare
        // 30000x30000, which image.Decode would happily expand into gigabytes of
        // RGBA. DecodeConfig reads only the header.
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

        // Pick the square to keep. The client sends one when the user framed
        // the picture themselves; otherwise fall back to the centre.
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
                // Clamp so a rounding error or a hostile request can never read
                // outside the image.
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

        out := image.NewRGBA(image.Rect(0, 0, avatarSize, avatarSize))
        // Fill first: source images with transparency would otherwise land on
        // black, which looks wrong against the app's dark-but-not-black surfaces.
        draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)

        for y := 0; y < avatarSize; y++ {
                srcY := offY + y*edge/avatarSize
                for x := 0; x < avatarSize; x++ {
                        srcX := offX + x*edge/avatarSize
                        out.Set(x, y, src.At(srcX, srcY))
                }
        }

        var buf bytes.Buffer
        if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 82}); err != nil {
                return nil, err
        }
        return buf.Bytes(), nil
}
