package cuhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/handlers"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/crypto/acme/autocert"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var log = logging.Logger("cu-http")

type startTime string

// RouterConfig configures the shared public HTTP router middleware stack.
type RouterConfig struct {
	// CSP enables secure response headers when non-empty (market server only).
	CSP string
	// DomainName is used to derive the default allowed CORS origin (https://{DomainName}).
	DomainName string
}

// NewRouter builds a chi router with the standard public-server middleware.
func NewRouter(cfg RouterConfig) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(handlers.ProxyHeaders)
	if cfg.CSP != "" {
		r.Use(SecureHeaders(cfg.CSP))
	}
	r.Use(CORS(AllowedCORSOrigins(cfg.DomainName)))
	return r
}

// MountStandardRoutes registers health and version endpoints shared by public servers.
func MountStandardRoutes(r chi.Router) {
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "Service is up and running")
	})
	r.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "%s", build.UserVersion())
	})
}

// StartServer runs cfg's public HTTPS (or delegated HTTP) listener until ctx is cancelled.
func StartServer(ctx context.Context, cfg *config.HTTPConfig, db *harmonydb.DB, handler http.Handler, label string) error {
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      time.Hour * 2,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	if !cfg.DelegateTLS {
		certManager := autocert.Manager{
			Cache:      AutocertCache{DB: db},
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.DomainName),
		}
		server.TLSConfig = certManager.TLSConfig()
	}

	go func() {
		log.Infof("Starting %s for https://%s on %s", label, cfg.DomainName, cfg.ListenAddress)
		var serr error
		if !cfg.DelegateTLS {
			serr = server.ListenAndServeTLS("", "")
		} else {
			serr = server.ListenAndServe()
		}
		if serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			log.Errorf("Failed to start %s: %s", label, serr)
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
