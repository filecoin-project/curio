package cuhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/handlers"
	logging "github.com/ipfs/go-log/v2"
	"github.com/snadrus/must"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	mhttp "github.com/filecoin-project/curio/market/http"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/market/libp2p"
	"github.com/filecoin-project/curio/market/retrieval"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/tasks/message"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

var log = logging.Logger("cu-http")

// RouterMap is the map that allows the library user to pass in their own routes
type RouterMap map[string]http.HandlerFunc

type startTime string

// Custom middleware to add secure HTTP headers
func secureHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Set CSP based on configuration
			switch csp {
			case "off":
				// Do nothing
			case "self":
				w.Header().Set("Content-Security-Policy", "default-src 'self'")
			case "inline":
				fallthrough
			default:
				w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval'")
			}

			next.ServeHTTP(w, r)
		})
	}
}

func corsHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Accept-Encoding")
		// Expose Location header for PDP Piece upload
		w.Header().Set("Access-Control-Expose-Headers", "Location")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
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

// libp2pConnMiddleware intercepts WebSocket upgrade requests to "/" and rewrites the path to "/libp2p"
func libp2pConnMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the request path is "/"
		if r.URL.Path == "/" || r.URL.Path == "" {
			// Check if the request is a WebSocket upgrade request
			if isWebSocketUpgrade(r) {
				// Rewrite the path to "/libp2p"
				r.URL.Path = "/libp2p"
				// Update RequestURI in case downstream handlers use it
				r.RequestURI = "/libp2p"
			}
		}
		// Call the next handler in the chain
		next.ServeHTTP(w, r)
	})
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade request
func isWebSocketUpgrade(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
		return false
	}
	if strings.ToLower(r.Header.Get("Connection")) != "upgrade" {
		return false
	}
	return true
}

type ServiceDeps struct {
	EthSender *message.SenderETH
}

func StartHTTPServer(ctx context.Context, d *deps.Deps, sd *ServiceDeps, dm *storage_market.CurioStorageDealMarket) error {
	cfg := d.Cfg.HTTP

	// Setup the Chi router for more complex routing (if needed in the future)
	chiRouter := chi.NewRouter()

	// Chi-specific middlewares
	chiRouter.Use(middleware.RequestID)
	chiRouter.Use(middleware.RealIP)
	chiRouter.Use(middleware.Recoverer)
	chiRouter.Use(handlers.ProxyHeaders) // Handle reverse proxy headers like X-Forwarded-For
	chiRouter.Use(secureHeaders(cfg.CSP))
	chiRouter.Use(corsHeaders)

	if cfg.EnableCORS {
		chiRouter.Use(handlers.CORS(handlers.AllowedOrigins([]string{"https://" + cfg.DomainName})))
	}

	// Set up the compression middleware with custom compression levels
	compressionMw, err := compressionMiddleware(&cfg.CompressionLevels)
	if err != nil {
		log.Fatalf("Failed to initialize compression middleware: %s", err)
	}

	// Use http.ServeMux as a fallback for routes not handled by chi
	chiRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, "Requested resource not found")
	})

	// Root path handler (simpler routes handled by http.ServeMux)
	chiRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "Hello, World!\n -Curio\n")
	})

	// Status endpoint to check the health of the service
	chiRouter.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "Service is up and running")
	})

	// Status endpoint to check the health of the service
	chiRouter.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "%s", build.UserVersion())
	})

	chiRouter, err = attachRouters(ctx, chiRouter, d, sd, dm)
	if err != nil {
		return xerrors.Errorf("failed to attach routers: %w", err)
	}

	// Set up the HTTP server with proper timeouts
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           libp2pConnMiddleware(loggingMiddleware(compressionMw(chiRouter))), // Attach middlewares
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      time.Hour * 2,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	if !cfg.DelegateTLS {
		// Set up the autocert manager for Let's Encrypt
		certManager := autocert.Manager{
			Cache:      cache{db: d.DB},
			Prompt:     autocert.AcceptTOS, // Automatically accept the Terms of Service
			HostPolicy: autocert.HostWhitelist(cfg.DomainName),
		}

		server.TLSConfig = certManager.TLSConfig()
	}

	// We don't need to run an HTTP server. Any HTTP request should simply be handled as HTTPS.

	// Start the server with TLS
	go func() {
		log.Infof("Starting HTTPS server for https://%s on %s", cfg.DomainName, cfg.ListenAddress)
		var serr error
		if !cfg.DelegateTLS {
			serr = server.ListenAndServeTLS("", "")
		} else {
			serr = server.ListenAndServe()
		}
		if serr != nil {
			log.Errorf("Failed to start HTTPS server: %s", serr)
			panic(serr)
		}
	}()

	go func() {
		<-ctx.Done()
		log.Warn("Shutting down HTTP Server...")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Errorf("shutting down web server failed: %s", err)
		}
		log.Warn("HTTP Server graceful shutdown successful")
	}()

	return nil
}

type cache struct {
	db *harmonydb.DB
}

func (c cache) Get(ctx context.Context, key string) ([]byte, error) {
	var ret []byte
	err := c.db.QueryRow(ctx, `SELECT v FROM autocert_cache WHERE k = $1`, key).Scan(&ret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, autocert.ErrCacheMiss
		}

		log.Warnf("failed to get the value from DB for key %s: %s", key, err)
		return nil, xerrors.Errorf("failed to get the value from DB for key %s: %w", key, err)
	}
	return ret, nil
}

func (c cache) Put(ctx context.Context, key string, data []byte) error {
	_, err := c.db.Exec(ctx, `INSERT INTO autocert_cache (k, v) VALUES ($1, $2)
						ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v`, key, data)
	if err != nil {
		log.Warnf("failed to inset key value pair in DB: %s", err)
		return xerrors.Errorf("failed to inset key value pair in DB: %w", err)
	}
	return nil
}

func (c cache) Delete(ctx context.Context, key string) error {
	_, err := c.db.Exec(ctx, `DELETE FROM autocert_cache WHERE k = $1`, key)
	if err != nil {
		log.Warnf("failed to delete key value pair from DB: %s", err)
		return xerrors.Errorf("failed to delete key value pair from DB: %w", err)
	}
	return nil
}

var _ autocert.Cache = cache{}

func attachRouters(ctx context.Context, r *chi.Mux, d *deps.Deps, sd *ServiceDeps, dm *storage_market.CurioStorageDealMarket) (*chi.Mux, error) {
	// Attach retrievals
	rp := retrieval.NewRetrievalProvider(ctx, d.DB, d.IndexStore, d.CachedPieceReader)
	retrieval.Router(r, rp)

	// Attach IPNI
	ipp, err := ipni_provider.NewProvider(d)
	if err != nil {
		return nil, xerrors.Errorf("failed to create new ipni provider: %w", err)
	}
	ipni_provider.Routes(r, ipp)

	go ipp.StartPublishing(ctx)

	// Attach LibP2P redirector
	rd := libp2p.NewRedirector(d.DB)
	libp2p.Router(r, rd)

	if sd.EthSender != nil {
		pdsvc := pdp.NewPDPService(ctx, d.DB, d.LocalStore, must.One(d.EthClient.Get()), d.Chain, sd.EthSender)
		pdp.Routes(r, pdsvc)
	}

	// Attach the market handler
	dh, err := mhttp.NewMarketHandler(d.DB, d.Cfg, dm)
	if err != nil {
		return nil, xerrors.Errorf("failed to create new market handler: %w", err)
	}
	mhttp.Router(r, dh)

	return r, nil
}
