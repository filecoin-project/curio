package cuhttp

import (
	"context"

	"github.com/go-chi/chi/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/market/denylist"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/market/retrieval"
	"github.com/filecoin-project/curio/market/retrieval/gate"
)

// MountRetrievalPublicRoutes mounts piece/IPFS retrieval with bad-bits denylist filtering.
// Skiff and full Curio both use this; tests can mount it without standing up IPNI.
func MountRetrievalPublicRoutes(ctx context.Context, r *chi.Mux, d *deps.Deps) *denylist.Filter {
	df := denylist.NewFilter(ctx, d.Cfg.HTTP.DenylistServers)
	rp := retrieval.NewRetrievalProvider(ctx, d.DB, d.IndexStore, d.CachedPieceReader, df)

	// Opt-in retrieval permissioning. The resolver only dials the eth node on a gated request, so a
	// disabled gate (the default) costs nothing.
	ethGet := func(context.Context) (ethchain.EthClient, error) {
		if d.EthClient == nil {
			return nil, xerrors.New("eth client not configured; gated retrieval requires it")
		}
		return d.EthClient.Val()
	}
	res := gate.NewResolver(d.DB, d.IndexStore, ethGet)
	pieceGate := gate.NewMiddleware(d.Cfg.HTTP.EnableGatedRetrieval, "/piece/", res)
	ipfsGate := gate.NewContentMiddleware(d.Cfg.HTTP.EnableGatedRetrieval, "/ipfs/", res)

	retrieval.Router(r, rp, df, pieceGate, ipfsGate)
	return df
}

// MountCommonPublicRoutes mounts public endpoints shared by curio and skiff:
// piece/IPFS retrieval with bad-bits denylist filtering, and IPNI provider routes.
// The returned IPNI provider is started for publishing and can be passed to PDP route mounting.
func MountCommonPublicRoutes(ctx context.Context, r *chi.Mux, d *deps.Deps) (*ipni_provider.Provider, error) {
	_ = MountRetrievalPublicRoutes(ctx, r, d)

	ipp, err := ipni_provider.NewProvider(d)
	if err != nil {
		return nil, xerrors.Errorf("failed to create new ipni provider: %w", err)
	}
	ipni_provider.Routes(r, ipp)
	go ipp.StartPublishing(ctx)

	return ipp, nil
}
