// Package compression provides a minimal HTTP compression middleware using stdlib only.
// Replaces github.com/CAFxX/httpcompression to avoid ~2.4MB from brotli/deflate deps.
package compression

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// Config holds compression level (0=off, 1-9 for gzip).
// Brotli and Deflate are not supported in this minimal impl; their levels are ignored.
type Config struct {
	GzipLevel    int
	BrotliLevel  int // ignored
	DeflateLevel int // ignored
}

// Middleware returns an http.Handler that gzip-compresses responses when the client
// accepts gzip. Uses GzipLevel from config (1-9, default 6).
func Middleware(cfg Config) func(http.Handler) http.Handler {
	level := cfg.GzipLevel
	if level < 1 {
		level = 1
	}
	if level > 9 {
		level = 9
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !acceptsGzip(r) {
				next.ServeHTTP(w, r)
				return
			}
			gzw, err := gzip.NewWriterLevel(w, level)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			defer gzw.Close()
			w.Header().Set("Content-Encoding", "gzip")
			gzipW := &gzipResponseWriter{ResponseWriter: w, gzw: gzw}
			next.ServeHTTP(gzipW, r)
		})
	}
}

func acceptsGzip(r *http.Request) bool {
	ae := r.Header.Get("Accept-Encoding")
	for _, enc := range strings.Split(ae, ",") {
		enc = strings.TrimSpace(strings.Split(enc, ";")[0])
		if strings.EqualFold(enc, "gzip") {
			return true
		}
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gzw *gzip.Writer
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gzw.Write(b)
}

// Level returns a gzip level from 1-9, clamped.
func Level(n int) int {
	if n < 1 {
		return 1
	}
	if n > 9 {
		return 9
	}
	return n
}
