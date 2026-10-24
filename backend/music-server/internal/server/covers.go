package server

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

const backdropSize = 32

var coverThumbSizes = map[int]bool{backdropSize: true, 128: true, 256: true, 512: true}

func (s *Server) coverPath(id string) string { return filepath.Join(s.covDir, id+".img") }

func (s *Server) thumbPath(id string, size int) string {
	return filepath.Join(s.covDir, id+"."+strconv.Itoa(size)+".img")
}

func (s *Server) coverBytes(id string, size int) ([]byte, error) {
	if size == 0 {
		return os.ReadFile(s.coverPath(id))
	}
	orig, err := os.Stat(s.coverPath(id))
	if err != nil {
		return nil, err
	}
	thumb := s.thumbPath(id, size)
	if st, err := os.Stat(thumb); err == nil && !orig.ModTime().After(st.ModTime()) {
		if b, err := os.ReadFile(thumb); err == nil {
			return b, nil
		}
	}
	raw, err := os.ReadFile(s.coverPath(id))
	if err != nil {
		return nil, err
	}
	var small []byte
	if size == backdropSize {
		small = backdropCover(raw)
	} else {
		small = resizeCover(raw, size)
	}
	if err := writeFileAtomic(thumb, small); err != nil {
		logErr("cache cover thumbnail", err)
	}
	return small, nil
}

func (s *Server) removeCoverFiles(id string) {
	os.Remove(s.coverPath(id))
	s.removeThumbnails(id)
}

func (s *Server) removeThumbnails(id string) {
	for size := range coverThumbSizes {
		os.Remove(s.thumbPath(id, size))
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		werr = os.Chmod(tmp.Name(), 0o644)
	}
	if werr == nil && cerr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	return nil
}

func backdropCover(raw []byte) []byte {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || int64(cfg.Width)*int64(cfg.Height) > maxAvatarPixels {
		return raw
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, blur(downscale(src, backdropSize), 1.2), &jpeg.Options{Quality: 88}); err != nil {
		return raw
	}
	return buf.Bytes()
}

func blur(src *image.RGBA, sigma float64) *image.RGBA {
	radius := int(math.Ceil(sigma * 3))
	kernel := make([]float64, 2*radius+1)
	var sum float64
	for i := range kernel {
		d := float64(i - radius)
		kernel[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	b := src.Bounds()
	pass := func(in *image.RGBA, dx, dy int) *image.RGBA {
		out := image.NewRGBA(b)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				var r, g, bl float64
				for i, w := range kernel {
					k := i - radius
					c := in.RGBAAt(min(max(x+k*dx, b.Min.X), b.Max.X-1), min(max(y+k*dy, b.Min.Y), b.Max.Y-1))
					r, g, bl = r+w*float64(c.R), g+w*float64(c.G), bl+w*float64(c.B)
				}
				out.SetRGBA(x, y, color.RGBA{uint8(r + 0.5), uint8(g + 0.5), uint8(bl + 0.5), 255})
			}
		}
		return out
	}
	return pass(pass(src, 1, 0), 0, 1)
}
