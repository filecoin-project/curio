package cuhttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/handlers"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var log = logging.Logger("cu-http")

// RouterMap is the map that allows the library user to pass in their own routes
type RouterMap map[string]http.HandlerFunc

type startTime string

// Custom middleware to add secure HTTP headers
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// Logging middleware, attaches logger to the request context for easier debugging
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := context.WithValue(r.Context(), startTime("startTime"), start)
		next.ServeHTTP(w, r.WithContext(ctx))

		log.Debugf("%s %s %s %dms", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start).Milliseconds())
	})
}

// Compression middleware from https://github.com/CAFxX/httpcompression
// Uses the compression levels defined in the config
func compressionMiddleware(config *config.CompressionConfig) (func(http.Handler) http.Handler, error) {
	adapter, err := httpcompression.DefaultAdapter(
		httpcompression.GzipCompressionLevel(config.GzipLevel),
		httpcompression.BrotliCompressionLevel(config.BrotliLevel),
		httpcompression.DeflateCompressionLevel(config.DeflateLevel),
	)
	if err != nil {
		return nil, err
	}
	return adapter, nil
}

func NewHTTPServer(ctx context.Context, deps *deps.Deps, config *config.HTTPConfig) error {
	ch := cache{db: deps.DB}

	// Set up the autocert manager for Let's Encrypt
	certManager := autocert.Manager{
		Cache:      ch,
		Prompt:     autocert.AcceptTOS, // Automatically accept the Terms of Service
		HostPolicy: autocert.HostWhitelist(config.DomainName),
	}

	// Setup the Chi router for more complex routing (if needed in the future)
	chiRouter := chi.NewRouter()

	// Chi-specific middlewares
	chiRouter.Use(middleware.RequestID)
	chiRouter.Use(middleware.RealIP)
	chiRouter.Use(middleware.Recoverer)
	chiRouter.Use(handlers.ProxyHeaders) // Handle reverse proxy headers like X-Forwarded-For
	chiRouter.Use(secureHeaders)

	if config.EnableCORS {
		chiRouter.Use(handlers.CORS(handlers.AllowedOrigins([]string{"https://" + config.DomainName})))
	}

	// Set up the compression middleware with custom compression levels
	compressionMw, err := compressionMiddleware(&config.CompressionLevels)
	if err != nil {
		log.Fatalf("Failed to initialize compression middleware: %s", err)
	}

	// Use http.ServeMux as a fallback for routes not handled by chi
	chiRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Requested resource not found")
	})

	// Root path handler (simpler routes handled by http.ServeMux)
	chiRouter.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Hello, World!\n -Curio")
	})

	// Status endpoint to check the health of the service
	chiRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Service is up and running")
	})

	// TODO: Attach other subrouters here

	// Set up the HTTP server with proper timeouts
	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           loggingMiddleware(compressionMw(chiRouter)), // Attach middlewares
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
	}

	// We don't need to run an HTTP server. Any HTTP request should simply be handled as HTTPS.

	// Start the server with TLS
	eg := errgroup.Group{}
	eg.Go(func() error {
		log.Infof("Starting HTTPS server on https://%s", config.DomainName)
		serr := server.ListenAndServeTLS("", "")
		if serr != nil {
			return xerrors.Errorf("failed to start listening: %w", serr)
		}
		return nil
	})

	go func() {
		<-ctx.Done()
		log.Warn("Shutting down HTTP Server...")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Errorf("shutting down web server failed: %s", err)
		}
		log.Warn("HTTP Server graceful shutdown successful")
	}()

	return eg.Wait()
}

type cache struct {
	db *harmonydb.DB
}

func (c cache) Get(ctx context.Context, key string) ([]byte, error) {
	var ret []byte
	err := c.db.QueryRow(ctx, `SELECT v FROM autocert_cache WHERE k = $1`, key).Scan(&ret)
	if err != nil {
		return nil, xerrors.Errorf("failed to get the value from DB for key %s: %w", key, err)
	}
	return ret, nil
}

func (c cache) Put(ctx context.Context, key string, data []byte) error {
	_, err := c.db.Exec(ctx, `INSERT INTO autocert_cache (k, v) VALUES ($1, $2) 
						ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v`, key, data)
	if err != nil {
		return xerrors.Errorf("failed to inset key value pair in DB: %w", err)
	}
	return nil
}

func (c cache) Delete(ctx context.Context, key string) error {
	_, err := c.db.Exec(ctx, `DELETE FROM autocert_cache WHERE k = $1`, key)
	if err != nil {
		return xerrors.Errorf("failed to delete key value pair from DB: %w", err)
	}
	return nil
}

var _ autocert.Cache = cache{}
