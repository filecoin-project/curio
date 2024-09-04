package cuhttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/handlers"
	logging "github.com/ipfs/go-log/v2"
	"github.com/vulcand/oxy/forward"
	"github.com/vulcand/oxy/roundrobin"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
)

var log = logging.Logger("cu-http")

// Config structure to hold all configurations, including compression levels
type Config struct {
	DomainName        string
	CertCacheDir      string
	ListenAddr        string
	HTTPRedirectAddr  string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	EnableCORS        bool
	Backends          []string
	LoadBalancer      bool
	CompressionLevels CompressionConfig
}

// CompressionConfig holds the compression levels for supported types
type CompressionConfig struct {
	GzipLevel    int
	BrotliLevel  int
	DeflateLevel int
}

// RouterMap is the map that allows the library user to pass in their own routes
type RouterMap map[string]http.HandlerFunc

type startTime string

func StartHTTPServer(ctx context.Context, cfg *config.HTTPConfig, deps *deps.Deps) error {
	err := NewHTTPServer(ctx, cfg)
	if err != nil {
		return err
	}
	if cfg.EnableLoadBalancer {
		return NewLoadBalancerServer(ctx, cfg)
	}
	return nil
}

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

func NewHTTPServer(ctx context.Context, config *config.HTTPConfig) error {
	// Set up the autocert manager for Let's Encrypt
	certManager := autocert.Manager{
		Cache:      autocert.DirCache(config.CertCacheDir), // Directory for storing certificates
		Prompt:     autocert.AcceptTOS,                     // Automatically accept the Terms of Service
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

// NewLoadBalancerServer sets up a load balancer server to distribute traffic to the backends
func NewLoadBalancerServer(ctx context.Context, cfg *config.HTTPConfig) error {
	certManager := autocert.Manager{
		Cache:      autocert.DirCache(cfg.CertCacheDir), // Directory for storing certificates
		Prompt:     autocert.AcceptTOS,                  // Automatically accept the Terms of Service
		HostPolicy: autocert.HostWhitelist(cfg.DomainName),
	}

	lb, err := NewLoadBalancer(cfg)
	if err != nil {
		return xerrors.Errorf("failed to create load balancer: %w", err)
	}

	go healthCheck(ctx, lb, time.Duration(cfg.LoadBalanceHealthCheckInterval))

	// Set up the HTTP load balancer server with proper timeouts
	server := &http.Server{
		Addr:              cfg.LoadBalancerListenAddr,
		Handler:           http.HandlerFunc(lb.ServeHTTP), // Attach the load balancer to handle requests
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		TLSConfig: &tls.Config{
			GetCertificate: certManager.GetCertificate,
		},
	}

	eg := errgroup.Group{}
	eg.Go(func() error {
		log.Infof("Starting load balancer server on https://%s", cfg.DomainName)
		serr := server.ListenAndServeTLS("", "")
		if serr != nil {
			return xerrors.Errorf("failed to start listening: %w", serr)
		}
		return nil
	})

	go func() {
		<-ctx.Done()
		log.Warn("Shutting down load balancer Server...")
		if err := server.Shutdown(context.Background()); err != nil {
			log.Errorf("shutting down web server failed: %s", err)
		}
		log.Warn("LoadBalancer Server graceful shutdown successful")
	}()

	return eg.Wait()
}

// NewLoadBalancer creates a load balancer with external backends
func NewLoadBalancer(config *config.HTTPConfig) (*roundrobin.RoundRobin, error) {
	fwd, _ := forward.New()
	lb, _ := roundrobin.New(fwd)

	// Add the backend servers to the load balancer
	for _, backend := range config.LoadBalancerBackends {
		burl, err := url.Parse("https://" + backend)
		if err != nil {
			return nil, xerrors.Errorf("Failed to parse backend URL %s: %w", backend, err)
		}
		err = lb.UpsertServer(burl)
		if err != nil {
			return nil, xerrors.Errorf("Failed to add proxy backend: %w", err)
		}
	}

	return lb, nil
}

// Health check function
func healthCheck(ctx context.Context, lb *roundrobin.RoundRobin, interval time.Duration) {
	var failedURLs []*url.URL

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Infof("Shutting fown the load balancer health check")
			return
		case <-ticker.C:
			var cf []*url.URL
			// Drop failed URLs from current set
			for _, u := range lb.Servers() {
				// Perform a health check by sending a GET request to /health endpoint
				resp, err := http.Get(u.String() + "/health") // Assuming /health is the health check endpoint
				if err != nil || resp.StatusCode != http.StatusOK {
					// If the server is down or the status is not OK, remove it from the load balancer
					log.Infof("Backend %s is down. Removing from load balancer.", u)
					_ = lb.RemoveServer(u)
					cf = append(cf, u)
					continue
				}
			}

			// Add back good URLs to current set from previous failed URLs
			for _, u := range failedURLs {
				// Perform a health check by sending a GET request to /health endpoint
				resp, err := http.Get(u.String() + "/health") // Assuming /health is the health check endpoint
				if err != nil || resp.StatusCode != http.StatusOK {
					// If the server is down or the status is not OK, do not add it to the load balancer
					log.Infof("Backend %s is down. Not adding to load balancer.", u)
					cf = append(cf, u)
					continue
				}

				// If the server responds with OK, ensure it add it to the load balancer
				err = lb.UpsertServer(u)
				if err != nil {
					log.Errorf("Failed to re-add backend %s: %v", u, err)
				}
			}

			failedURLs = cf
		}
	}
}
