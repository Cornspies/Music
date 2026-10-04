package server

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const webCSP = "default-src 'self'; script-src 'self' 'unsafe-inline' blob:; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com data:; " +
	"img-src 'self' blob: data:; media-src 'self' blob:; connect-src 'self' blob:; worker-src 'self' blob:; " +
	"manifest-src 'self'; object-src 'none'; frame-src 'self' com.cornspies.music:; base-uri 'self'; " +
	"form-action 'self'; frame-ancestors 'none'"

var webHeaders = [][2]string{
	{"Content-Security-Policy", webCSP},
	{"Referrer-Policy", "same-origin"},
	{"X-Frame-Options", "DENY"},
	{"Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()"},
	{"Cross-Origin-Opener-Policy", "same-origin"},
	{"Strict-Transport-Security", "max-age=31536000"},
}

var webTypes = map[string]string{
	".webmanifest": "application/manifest+json",
	".txt":         "text/plain; charset=utf-8",
}

var compressible = map[string]bool{
	".html": true, ".js": true, ".css": true, ".json": true, ".txt": true,
	".svg": true, ".webmanifest": true, ".xml": true,
}

type webApp struct {
	root string
	mu   sync.Mutex
	gz   map[string]gzEntry
}

type gzEntry struct {
	mod  time.Time
	size int64
	data []byte
}

func newWebApp(root string) *webApp {
	return &webApp{root: root, gz: map[string]gzEntry{}}
}

func (a *webApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean("/" + r.URL.Path)
	if clean == "/api" || strings.HasPrefix(clean, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	full, info, ok := a.resolve(clean, strings.Contains(r.Header.Get("Accept"), "text/html"))
	if !ok {
		http.NotFound(w, r)
		return
	}

	h := w.Header()
	for _, kv := range webHeaders {
		h.Set(kv[0], kv[1])
	}
	ext := strings.ToLower(filepath.Ext(full))
	switch {
	case webTypes[ext] != "":
		h.Set("Content-Type", webTypes[ext])
	case ext == "" && strings.HasPrefix(clean, "/.well-known/"):
		h.Set("Content-Type", "application/json")
	case mime.TypeByExtension(ext) != "":
		h.Set("Content-Type", mime.TypeByExtension(ext))
	}
	if strings.HasPrefix(clean, "/_next/static/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}

	if compressible[ext] && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		if data, ok := a.gzipped(full, info); ok {
			h.Set("Content-Encoding", "gzip")
			h.Add("Vary", "Accept-Encoding")
			http.ServeContent(w, r, "", info.ModTime(), bytes.NewReader(data))
			return
		}
	}
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func (a *webApp) resolve(clean string, page bool) (string, os.FileInfo, bool) {
	if clean != "/" {
		if full, info, ok := a.file(clean); ok {
			return full, info, true
		}
		if !page || strings.HasPrefix(clean, "/_next/") {
			return "", nil, false
		}
		for _, alt := range []string{clean + ".html", clean + "/index.html"} {
			if full, info, ok := a.file(alt); ok {
				return full, info, true
			}
		}
	}
	return a.file("/index.html")
}

func (a *webApp) file(name string) (string, os.FileInfo, bool) {
	full := filepath.Join(a.root, filepath.FromSlash(name))
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, false
	}
	return full, info, true
}

func (a *webApp) gzipped(full string, info os.FileInfo) ([]byte, bool) {
	a.mu.Lock()
	cached, ok := a.gz[full]
	a.mu.Unlock()
	if ok && cached.size == info.Size() && cached.mod.Equal(info.ModTime()) {
		return cached.data, true
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, false
	}
	if _, err := zw.Write(raw); err != nil || zw.Close() != nil {
		return nil, false
	}
	a.mu.Lock()
	a.gz[full] = gzEntry{mod: info.ModTime(), size: info.Size(), data: buf.Bytes()}
	a.mu.Unlock()
	return buf.Bytes(), true
}
