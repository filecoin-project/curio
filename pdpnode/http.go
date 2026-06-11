package pdpnode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/CAFxX/httpcompression"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/handlers"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/xerrors"

	curiobuild "github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/web"
)

type autocertCache struct {
	db *harmonydb.DB
}

func (c autocertCache) Get(ctx context.Context, key string) ([]byte, error) {
	var ret []byte
	err := c.db.QueryRow(ctx, `SELECT v FROM autocert_cache WHERE k = $1`, key).Scan(&ret)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, autocert.ErrCacheMiss
		}
		return nil, xerrors.Errorf("autocert cache get: %w", err)
	}
	return ret, nil
}

func (c autocertCache) Put(ctx context.Context, key string, data []byte) error {
	_, err := c.db.Exec(ctx, `INSERT INTO autocert_cache (k, v) VALUES ($1, $2)
						ON CONFLICT (k) DO UPDATE SET v = EXCLUDED.v`, key, data)
	return err
}

func (c autocertCache) Delete(ctx context.Context, key string) error {
	_, err := c.db.Exec(ctx, `DELETE FROM autocert_cache WHERE k = $1`, key)
	return err
}

// StartPublic serves the customer-facing PDP API.
func StartPublic(ctx context.Context, d *Deps, sd *TaskResult) error {
	cfg := d.Cfg.HTTP
	if !cfg.Enable {
		return nil
	}

	chiRouter := chi.NewRouter()
	chiRouter.Use(middleware.RequestID)
	chiRouter.Use(middleware.RealIP)
	chiRouter.Use(middleware.Recoverer)
	chiRouter.Use(handlers.ProxyHeaders)
	chiRouter.Use(corsHeaders)

	corsOrigin := "https://" + cfg.DomainName
	if devURL := os.Getenv("DEV_CURIO_EXTERNAL_URL"); devURL != "" {
		corsOrigin = devURL
	}
	chiRouter.Use(handlers.CORS(handlers.AllowedOrigins([]string{corsOrigin})))

	compressionMw, err := compressionMiddleware(&cfg.CompressionLevels)
	if err != nil {
		return err
	}

	chiRouter.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "Service is up and running")
	})
	chiRouter.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "%s", curiobuild.UserVersion())
	})

	if err := MountPublicRoutes(ctx, chiRouter, d, &sd.ServiceDeps); err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           compressionMw(chiRouter),
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      time.Hour * 2,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	if !cfg.DelegateTLS {
		certManager := autocert.Manager{
			Cache:      autocertCache{db: d.DB},
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.DomainName),
		}
		server.TLSConfig = certManager.TLSConfig()
	}

	go func() {
		log.Infof("Starting public PDP HTTPS server for https://%s on %s", cfg.DomainName, cfg.ListenAddress)
		var serr error
		if !cfg.DelegateTLS {
			serr = server.ListenAndServeTLS("", "")
		} else {
			serr = server.ListenAndServe()
		}
		if serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			log.Errorf("public PDP server failed: %s", serr)
			panic(serr)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	return nil
}

// StartAdmin serves the private operator GUI (webrpc + config).
func StartAdmin(ctx context.Context, d *Deps) (*http.Server, error) {
	if !d.Cfg.Subsystems.EnableWebGui {
		return nil, nil
	}

	srv, err := web.GetSrv(ctx, d.CurioDeps(), false)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()

	log.Infof("Admin GUI: http://%s", d.Cfg.Subsystems.GuiAddress)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("admin GUI server failed: %s", err)
		}
	}()

	return srv, nil
}

func corsHeaders(next http.Handler) http.Handler {
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

func compressionMiddleware(cfg *config.CompressionConfig) (func(http.Handler) http.Handler, error) {
	return httpcompression.DefaultAdapter(
		httpcompression.GzipCompressionLevel(cfg.GzipLevel),
		httpcompression.BrotliCompressionLevel(cfg.BrotliLevel),
		httpcompression.DeflateCompressionLevel(cfg.DeflateLevel),
	)
}
