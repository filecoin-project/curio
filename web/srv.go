// Package web defines the HTTP web server for static files and endpoints.
package web

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api"

	"github.com/filecoin-project/lotus/metrics"
)

var log = logging.Logger("web")

//go:embed static
var static embed.FS

var basePath = "/static/"

// A dev mode hack for no-restart changes to static and templates.
// You still need to recompile the binary for changes to go code.
var webDev = os.Getenv("CURIO_WEB_DEV") == "1"

func GetSrv(ctx context.Context, deps *deps.Deps, devMode bool) (*http.Server, error) {
	mx := mux.NewRouter()

	// Single CORS middleware that handles all CORS logic
	// Wrap the entire router to ensure middleware runs for all requests including unmatched routes
	corsHandler := func(next http.Handler) http.Handler {
		for _, ao := range deps.Cfg.HTTP.CORSOrigins {
			if ao == "*" {
				log.Infof("This CORS configuration allows any website to call irreversable APIs on the Curio node: %s", ao)
			}
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle OPTIONS preflight requests - always return 204, even if CORS not configured
			if r.Method == http.MethodOptions {
				if len(deps.Cfg.HTTP.CORSOrigins) > 0 {
					origin := r.Header.Get("Origin")
					var allowedOrigin string
					allowed := false

					// Check if origin is allowed
					for _, ao := range deps.Cfg.HTTP.CORSOrigins {
						if ao == "*" || ao == origin {
							allowedOrigin = ao
							allowed = true
							break
						}
					}

					if allowed {
						if allowedOrigin == "*" {

							w.Header().Set("Access-Control-Allow-Origin", "*")
						} else {
							w.Header().Set("Access-Control-Allow-Origin", origin)
						}
					}
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
					requestHeaders := r.Header.Get("Access-Control-Request-Headers")
					if requestHeaders != "" {
						w.Header().Set("Access-Control-Allow-Headers", requestHeaders)
					} else {
						w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Accept-Encoding")
					}
					w.Header().Set("Access-Control-Max-Age", "86400")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Set CORS headers for non-OPTIONS requests if CORS is configured
			if len(deps.Cfg.HTTP.CORSOrigins) > 0 {
				origin := r.Header.Get("Origin")
				if origin != "" {
					for _, ao := range deps.Cfg.HTTP.CORSOrigins {
						if ao == "*" || ao == origin {
							if ao == "*" {
								w.Header().Set("Access-Control-Allow-Origin", "*")
							} else {
								w.Header().Set("Access-Control-Allow-Origin", origin)
							}
							break
						}
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}

	if !devMode {
		api.Routes(mx.PathPrefix("/api").Subrouter(), deps, webDev)
	} else {
		if err := setupDevModeProxy(mx); err != nil {
			return nil, fmt.Errorf("failed to setup dev mode proxy: %v", err)
		}
	}

	var static fs.FS = static
	if webDev || devMode {
		basePath = ""
		static = os.DirFS("web/static")
		mx.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				log.Debugf("Rcv request: %s %s", r.Method, r.URL.Path)
				x := &interceptResponseWriter{ResponseWriter: w}
				next.ServeHTTP(x, r)
				log.Debugf("HTTP %s %s returned %d bytes with status code %d", r.Method, r.URL.Path, x.Length(), x.StatusCode())
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
		Handler: corsHandler(mx),
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

func setupDevModeProxy(mx *mux.Router) error {
	log.Debugf("Setting up dev mode proxy")
	apiSrv := os.Getenv("CURIO_API_SRV")
	if apiSrv == "" {
		return fmt.Errorf("CURIO_API_SRV environment variable is not set")
	}

	apiURL, err := url.Parse(apiSrv)
	if err != nil {
		return fmt.Errorf("invalid CURIO_API_SRV URL: %v", err)
	}
	log.Debugf("Parsed API URL: %s", apiURL.String())

	proxy := createReverseProxy(apiURL)

	mx.PathPrefix("/api").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debugf("Received request: %s %s", r.Method, r.URL.Path)
		if websocket.IsWebSocketUpgrade(r) {
			websocketProxy(apiURL, w, r)
		} else {
			proxy.ServeHTTP(w, r)
		}
	})

	log.Infof("Dev mode proxy setup complete")
	return nil
}

func createReverseProxy(target *url.URL) *httputil.ReverseProxy {
	log.Debugf("Creating reverse proxy")
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		log.Debugf("Directing request: %s %s", req.Method, req.URL.Path)
		originalDirector(req)
		req.URL.Path = path.Join(target.Path, req.URL.Path)

		if !strings.HasPrefix(req.URL.Path, "/") {
			req.URL.Path = "/" + req.URL.Path
		}

		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		log.Debugf("Modifying response: %d %s", resp.StatusCode, resp.Status)
		resp.Header.Del("Connection")
		resp.Header.Del("Upgrade")
		return nil
	}

	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	log.Infof("Reverse proxy created")
	return proxy
}

func websocketProxy(target *url.URL, w http.ResponseWriter, r *http.Request) {
	log.Debugf("Starting WebSocket proxy")
	d := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}

	// Preserve the original path and query
	wsTarget := *target
	wsTarget.Scheme = "ws"
	wsTarget.Path = path.Join(wsTarget.Path, r.URL.Path)

	if !strings.HasPrefix(wsTarget.Path, "/") {
		wsTarget.Path = "/" + wsTarget.Path
	}

	wsTarget.RawQuery = r.URL.RawQuery
	backendConn, resp, err := d.Dial(wsTarget.String(), nil)
	if err != nil {
		log.Errorf("Failed to connect to backend: %v", err)
		if resp != nil {
			log.Debugf("Backend response: %d %s", resp.StatusCode, resp.Status)
		}
		http.Error(w, "Failed to connect to backend", http.StatusServiceUnavailable)
		return
	}
	defer func() {
		_ = backendConn.Close()
	}()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Implement a more secure check in production
		},
	}
	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("Failed to upgrade connection: %v", err)
		http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = clientConn.Close()
	}()

	errc := make(chan error, 2)
	go proxyCopy(clientConn, backendConn, errc, "client -> backend")
	go proxyCopy(backendConn, clientConn, errc, "backend -> client")

	err = <-errc
	log.Debugf("WebSocket proxy ended: %v", err)
}

func proxyCopy(dst, src *websocket.Conn, errc chan<- error, direction string) {
	for {
		messageType, p, err := src.ReadMessage()
		if err != nil {
			log.Errorf("Error reading message (%s): %v", direction, err)
			errc <- err
			return
		}
		log.Debugf("Proxying message (%s): type %d, size %d bytes", direction, messageType, len(p))
		if err := dst.WriteMessage(messageType, p); err != nil {
			log.Errorf("Error writing message (%s): %v", direction, err)
			errc <- err
			return
		}
	}
}
