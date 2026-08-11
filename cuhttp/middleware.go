package cuhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/CAFxX/httpcompression"

	"github.com/filecoin-project/curio/deps/config"
)

// CORS allows any origin — PDP endpoints should accept requests from any source.
func CORS() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Accept-Encoding")
			w.Header().Set("Access-Control-Expose-Headers", "Location")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecureHeaders adds standard browser security headers.
func SecureHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			switch csp {
			case "off":
			case "self":
				w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:")
			case "inline":
				fallthrough
			default:
				w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval'; img-src 'self' data: blob:")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Compression wraps handlers with gzip/brotli/deflate based on config levels.
func Compression(cfg *config.CompressionConfig) (func(http.Handler) http.Handler, error) {
	return httpcompression.DefaultAdapter(
		httpcompression.GzipCompressionLevel(cfg.GzipLevel),
		httpcompression.BrotliCompressionLevel(cfg.BrotliLevel),
		httpcompression.DeflateCompressionLevel(cfg.DeflateLevel),
	)
}

// LoggingMiddleware attaches request timing for debug logs.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := context.WithValue(r.Context(), startTime("startTime"), start)
		next.ServeHTTP(w, r.WithContext(ctx))

		log.Debugf("%s %s %s %dms", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start).Milliseconds())
	})
}
