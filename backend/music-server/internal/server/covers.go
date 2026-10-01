package server

import (
	"bytes"
	"image"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var coverThumbSizes = map[int]bool{128: true, 256: true, 512: true}

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
	small := resizeCover(raw, size)
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

func (s *Server) shrinkStoredCovers() {
	defer logPanic("cover shrink")
	paths, err := filepath.Glob(filepath.Join(s.covDir, "*.img"))
	if err != nil {
		return
	}
	shrunk := 0
	for _, p := range paths {
		if strings.Count(filepath.Base(p), ".") != 1 {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width <= maxCoverEdge && cfg.Height <= maxCoverEdge {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if small := shrinkCover(raw); !bytes.Equal(small, raw) && writeFileAtomic(p, small) == nil {
			shrunk++
		}
	}
	if shrunk > 0 {
		log.Printf("covers: scaled %d oversized covers down to %dpx", shrunk, maxCoverEdge)
	}
}
