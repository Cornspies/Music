package server

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"path/filepath"
)

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
				continue
			}
			r, g, bl := float64(cr>>8), float64(cg>>8), float64(cb>>8)
			hi, lo := max(r, g, bl), min(r, g, bl)
			lum := (hi + lo) / 2 / 255
			sat := 0.0
			if hi > 0 {
				sat = (hi - lo) / hi
			}
			w := 0.15 + sat
			w *= min(1, lum*4) * min(1, (1-lum)*4)
			if w <= 0 {
				continue
			}
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

func (s *Server) updateCoverColor(trackID string) {
	defer logPanic("cover colour of track " + trackID)
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
