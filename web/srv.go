// Package web defines the HTTP web server for static files and endpoints.
package web

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api"

	"github.com/filecoin-project/lotus/metrics"
)

//go:embed static
var static embed.FS

var basePath = "/static/"

// An dev mode hack for no-restart changes to static and templates.
// You still need to recomplie the binary for changes to go code.
var webDev = os.Getenv("CURIO_WEB_DEV") == "1"

func GetSrv(ctx context.Context, deps *deps.Deps) (*http.Server, error) {
	mx := mux.NewRouter()
	api.Routes(mx.PathPrefix("/api").Subrouter(), deps, webDev)

	var static fs.FS = static
	if webDev {
		basePath = ""
		static = os.DirFS("web/static")
		mx.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Log the request
				log.Printf("Rcv request: %s %s", r.Method, r.URL.Path)

				x := &interceptResponseWriter{ResponseWriter: w}

				// Call the next handler
				next.ServeHTTP(x, r)

				// Log the response
				log.Printf("HTTP %s %s returned %d bytes with status code %d", r.Method, r.URL.Path, x.Length(), x.StatusCode())
			})
		})
	}

	mx.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If the request is for a directory, redirect to the index file.
		if strings.HasSuffix(r.URL.Path, "/") {
			r.URL.Path += "index.html"
		}

		file, err := static.Open(path.Join(basePath, r.URL.Path)[1:])
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("404 Not Found"))
			return
		}
		defer func() { _ = file.Close() }()

		fileInfo, err := file.Stat()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("500 Internal Server Error"))
			return
		}

		http.ServeContent(w, r, fileInfo.Name(), fileInfo.ModTime(), file.(io.ReadSeeker))
	})

	return &http.Server{
		Handler: http.HandlerFunc(mx.ServeHTTP),
		BaseContext: func(listener net.Listener) context.Context {
			ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "curio"))
			return ctx
		},
		Addr:              deps.Cfg.Subsystems.GuiAddress,
		ReadTimeout:       time.Minute * 3,
		ReadHeaderTimeout: time.Minute * 3, // lint
	}, nil
}

type interceptResponseWriter struct {
	http.ResponseWriter
	length   int
	status   int
	hijacker http.Hijacker
}

func (w *interceptResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.hijacker != nil {
		return w.hijacker.Hijack()
	}
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("ResponseWriter does not support Hijacker interface")
	}
	w.hijacker = hijacker
	return hijacker.Hijack()
}

func (w *interceptResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *interceptResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.length += n
	return n, err
}

func (w *interceptResponseWriter) Length() int {
	return w.length
}

func (w *interceptResponseWriter) StatusCode() int {
	return w.status
}
