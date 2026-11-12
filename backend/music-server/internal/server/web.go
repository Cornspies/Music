package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed web-headers.json
var webHeadersJSON []byte

type webHeaderSpec struct {
	ContentSecurityPolicy [][]string  `json:"contentSecurityPolicy"`
	Headers               [][2]string `json:"headers"`
}

var webHeaders = loadWebHeaders()

func loadWebHeaders() webHeaderSpec {
	var spec webHeaderSpec
	if err := json.Unmarshal(webHeadersJSON, &spec); err != nil {
		panic("web-headers.json: " + err.Error())
	}
	return spec
}

func (s webHeaderSpec) policy(scriptHashes []string) string {
	directives := make([]string, 0, len(s.ContentSecurityPolicy))
	for _, d := range s.ContentSecurityPolicy {
		if len(d) > 0 && d[0] == "script-src" {
			d = append(append([]string{}, d...), scriptHashes...)
		}
		directives = append(directives, strings.Join(d, " "))
	}
	return strings.Join(directives, "; ")
}

var basePolicy = webHeaders.policy(nil)

var (
	scriptTagRe = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script\s*>`)
	scriptSrcRe = regexp.MustCompile(`(?i)(^|\s)src\s*=`)
)

func inlineScriptHashes(page []byte) []string {
	var hashes []string
	seen := map[string]bool{}
	for _, m := range scriptTagRe.FindAllSubmatch(page, -1) {
		if scriptSrcRe.Match(m[1]) || len(m[2]) == 0 {
			continue
		}
		sum := sha256.Sum256(m[2])
		hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		if !seen[hash] {
			seen[hash] = true
			hashes = append(hashes, hash)
		}
	}
	return hashes
}

var webTypes = map[string]string{
	".webmanifest": "application/manifest+json",
	".txt":         "text/plain; charset=utf-8",
}

const gzCacheBytes = 32 << 20

var compressible = map[string]bool{
	".html": true, ".js": true, ".css": true, ".json": true, ".txt": true,
	".svg": true, ".webmanifest": true, ".xml": true,
}

type webApp struct {
	root     string
	mu       sync.Mutex
	gz       map[string]gzEntry
	policies map[string]policyEntry
}

type policyEntry struct {
	mod    time.Time
	size   int64
	policy string
}

type gzEntry struct {
	mod  time.Time
	size int64
	data []byte
	used time.Time
}

func newWebApp(root string) *webApp {
	return &webApp{root: root, gz: map[string]gzEntry{}, policies: map[string]policyEntry{}}
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
	ext := strings.ToLower(filepath.Ext(full))
	if ext == ".html" {
		h.Set("Content-Security-Policy", a.pagePolicy(full, info))
	} else {
		h.Set("Content-Security-Policy", basePolicy)
	}
	for _, kv := range webHeaders.Headers {
		h.Set(kv[0], kv[1])
	}
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

func (a *webApp) pagePolicy(full string, info os.FileInfo) string {
	a.mu.Lock()
	cached, ok := a.policies[full]
	a.mu.Unlock()
	if ok && cached.size == info.Size() && cached.mod.Equal(info.ModTime()) {
		return cached.policy
	}
	page, err := os.ReadFile(full)
	if err != nil {
		return basePolicy
	}
	policy := webHeaders.policy(inlineScriptHashes(page))
	a.mu.Lock()
	a.policies[full] = policyEntry{mod: info.ModTime(), size: info.Size(), policy: policy}
	a.mu.Unlock()
	return policy
}

func (a *webApp) gzipped(full string, info os.FileInfo) ([]byte, bool) {
	a.mu.Lock()
	cached, ok := a.gz[full]
	fresh := ok && cached.size == info.Size() && cached.mod.Equal(info.ModTime())
	if fresh {
		cached.used = time.Now()
		a.gz[full] = cached
	}
	a.mu.Unlock()
	if fresh {
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
	a.gz[full] = gzEntry{mod: info.ModTime(), size: info.Size(), data: buf.Bytes(), used: time.Now()}
	a.prune()
	a.mu.Unlock()
	return buf.Bytes(), true
}

func (a *webApp) prune() {
	total := 0
	for name, e := range a.gz {
		info, err := os.Stat(name)
		if err != nil || info.Size() != e.size || !info.ModTime().Equal(e.mod) {
			delete(a.gz, name)
			continue
		}
		total += len(e.data)
	}
	for total > gzCacheBytes {
		oldest := ""
		for name, e := range a.gz {
			if oldest == "" || e.used.Before(a.gz[oldest].used) {
				oldest = name
			}
		}
		total -= len(a.gz[oldest].data)
		delete(a.gz, oldest)
	}
}
