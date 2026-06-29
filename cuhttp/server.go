package cuhttp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	logging "github.com/ipfs/go-log/v2"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/cuhttp/servicedeps"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/market/denylist"
	mhttp "github.com/filecoin-project/curio/market/http"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/market/libp2p"
	"github.com/filecoin-project/curio/market/retrieval"
	"github.com/filecoin-project/curio/pdp"
	storage_market "github.com/filecoin-project/curio/tasks/storage-market"
)

var log = logging.Logger("cu-http")

// RouterMap is the map that allows the library user to pass in their own routes
type RouterMap map[string]http.HandlerFunc

type startTime string

// libp2pConnMiddleware intercepts WebSocket upgrade requests to "/" and rewrites the path to "/libp2p"
func libp2pConnMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			if isWebSocketUpgrade(r) {
				r.URL.Path = "/libp2p"
				r.RequestURI = "/libp2p"
			}
		}
		next.ServeHTTP(w, r)
	})
}

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
	servicedeps.Deps
	DealMarket *storage_market.CurioStorageDealMarket
}

// StartHTTPServer starts the public-facing server for market calls.
func StartHTTPServer(ctx context.Context, d *deps.Deps, sd *ServiceDeps) error {
	cfg := d.Cfg.HTTP

	chiRouter := NewRouter(RouterConfig{CSP: cfg.CSP, DomainName: cfg.DomainName})

	compressionMw, err := Compression(&cfg.CompressionLevels)
	if err != nil {
		log.Fatalf("Failed to initialize compression middleware: %s", err)
	}

	chiRouter.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, "Requested resource not found")
	})

	chiRouter.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "Hello, World!\n -Curio\n")
	})

	MountStandardRoutes(chiRouter)

	chiRouter, err = attachRouters(ctx, chiRouter, d, sd)
	if err != nil {
		return xerrors.Errorf("failed to attach routers: %w", err)
	}

	handler := libp2pConnMiddleware(LoggingMiddleware(compressionMw(chiRouter)))
	return StartServer(ctx, &cfg, d.DB, handler, "HTTPS server")
}

func attachRouters(ctx context.Context, r *chi.Mux, d *deps.Deps, sd *ServiceDeps) (*chi.Mux, error) {
	df := denylist.NewFilter(ctx, d.Cfg.HTTP.DenylistServers)

	rp := retrieval.NewRetrievalProvider(ctx, d.DB, d.IndexStore, d.CachedPieceReader, df)
	retrieval.Router(r, rp, df)

	ipp, err := ipni_provider.NewProvider(d)
	if err != nil {
		return nil, xerrors.Errorf("failed to create new ipni provider: %w", err)
	}
	ipni_provider.Routes(r, ipp)

	go ipp.StartPublishing(ctx)

	rd := libp2p.NewRedirector(d.DB)
	libp2p.Router(r, rd)

	if sd.EthSender != nil {
		if err := pdp.MountRoutes(ctx, r, pdp.MountDeps{
			DB:         d.DB,
			LocalStore: d.LocalStore,
			EthClient:  must.One(d.EthClient.Get()),
			Chain:      d.Chain,
			EthSender:  sd.EthSender,
			AlertTask:  sd.AlertTask,
		}, ipp); err != nil {
			return nil, err
		}
	}

	dh, err := mhttp.NewMarketHandler(d.DB, d.Cfg, sd.DealMarket, must.One(d.EthClient.Get()), d.Chain, sd.EthSender, d.LocalStore)
	if err != nil {
		return nil, xerrors.Errorf("failed to create new market handler: %w", err)
	}
	mhttp.Router(r, dh)

	return r, nil
}
