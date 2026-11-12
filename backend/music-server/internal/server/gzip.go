package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

const (
	gzipMinBytes = 1024
	gzipLevel    = 4
)

var gzipWriters = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(io.Discard, gzipLevel)
	return zw
}}

func acceptsGzip(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
}

type gzipResponseWriter struct {
	http.ResponseWriter
	code    int
	held    bool
	decided bool
	zw      *gzip.Writer
}

func (g *gzipResponseWriter) compressible(code int) bool {
	if code < 200 || code >= 300 || code == http.StatusNoContent {
		return false
	}
	h := g.Header()
	return strings.HasPrefix(h.Get("Content-Type"), "application/json") &&
		h.Get("Content-Encoding") == "" && h.Get("Content-Range") == ""
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.decided || g.held {
		return
	}
	if g.compressible(code) {
		g.code, g.held = code, true
		return
	}
	g.decided = true
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if !g.held {
			g.WriteHeader(http.StatusOK)
		}
		if g.held {
			g.decided = true
			if len(b) >= gzipMinBytes {
				h := g.Header()
				h.Set("Content-Encoding", "gzip")
				h.Add("Vary", "Accept-Encoding")
				h.Del("Content-Length")
				g.zw = gzipWriters.Get().(*gzip.Writer)
				g.zw.Reset(g.ResponseWriter)
			}
			g.ResponseWriter.WriteHeader(g.code)
		}
	}
	if g.zw != nil {
		return g.zw.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.zw != nil {
		_ = g.zw.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipResponseWriter) finish() {
	if g.held && !g.decided {
		g.decided = true
		g.ResponseWriter.WriteHeader(g.code)
	}
	if g.zw != nil {
		_ = g.zw.Close()
		g.zw.Reset(io.Discard)
		gzipWriters.Put(g.zw)
		g.zw = nil
	}
}
