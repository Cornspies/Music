package server

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
)

// Cover colour.
//
// Each cover gets one representative colour, stored on the track as
// "#rrggbb" and sent to the clients as coverColor. The full-screen lyrics
// view paints its background with it (the way Spotify does), and doing this
// once on the server means every client agrees and none of them has to
// decode the image for it.

// dominantColor picks the colour a person would call "the" colour of the
// picture: the most common hue after collapsing near-identical shades,
// weighted towards saturated, mid-brightness colours so a black border or a
// white background doesn't win. Returns "" when the image can't be read.
func dominantColor(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxAvatarPixels {
		return ""
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	b := img.Bounds()
	// ~10k samples are plenty; step so that a 1000² image is sampled every
	// 10th pixel in each direction.
	step := max(1, int(float64(max(b.Dx(), b.Dy()))/100))

	type bucket struct {
		n        float64
		r, g, bl float64
	}
	buckets := map[uint16]*bucket{}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			cr, cg, cb, ca := img.At(x, y).RGBA()
			if ca < 0x8000 {
				continue // transparent
			}
			r, g, bl := float64(cr>>8), float64(cg>>8), float64(cb>>8)
			hi, lo := max(r, g, bl), min(r, g, bl)
			lum := (hi + lo) / 2 / 255
			sat := 0.0
			if hi > 0 {
				sat = (hi - lo) / hi
			}
			// Weight: colourful beats grey, and anything close to black or
			// white is nearly ignored.
			w := 0.15 + sat
			w *= min(1, lum*4) * min(1, (1-lum)*4)
			if w <= 0 {
				continue
			}
			// 4 bits per channel: shades that differ by less than 16 merge.
			key := uint16(cr>>12)<<8 | uint16(cg>>12)<<4 | uint16(cb>>12)
			bk := buckets[key]
			if bk == nil {
				bk = &bucket{}
				buckets[key] = bk
			}
			bk.n += w
			bk.r += r * w
			bk.g += g * w
			bk.bl += bl * w
		}
	}
	var best *bucket
	for _, bk := range buckets {
		if best == nil || bk.n > best.n {
			best = bk
		}
	}
	if best == nil || best.n == 0 {
		return ""
	}
	return fmt.Sprintf("#%02x%02x%02x", int(best.r/best.n), int(best.g/best.n), int(best.bl/best.n))
}

// updateCoverColor computes and stores the colour for a track's cover file.
// Run in the background: decoding a 1000² JPEG takes tens of milliseconds
// and nothing waits on the result.
func (s *Server) updateCoverColor(trackID string) {
	raw, err := os.ReadFile(filepath.Join(s.covDir, trackID+".img"))
	if err != nil {
		return
	}
	c := dominantColor(raw)
	if c == "" {
		return
	}
	if _, err := s.db.Exec(`UPDATE tracks SET cover_color = ? WHERE id = ?`, c, trackID); err != nil {
		logErr("cover color", err)
	}
}

// backfillCoverColors fills in colours for covers stored before this
// existed, one at a time, at start-up.
func (s *Server) backfillCoverColors() {
	rows, err := s.db.Query(`SELECT id FROM tracks WHERE has_cover = 1 AND COALESCE(cover_color, '') = ''`)
	if err != nil {
		logErr("cover color backfill", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.updateCoverColor(id)
	}
}
