package cuhttp

import (
	"context"

	"github.com/go-chi/chi/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/market/denylist"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/market/retrieval"
)

// MountCommonPublicRoutes mounts public endpoints shared by curio and skiff:
// piece/IPFS retrieval with bad-bits denylist filtering, and IPNI provider routes.
// The returned IPNI provider is started for publishing and can be passed to PDP route mounting.
func MountCommonPublicRoutes(ctx context.Context, r *chi.Mux, d *deps.Deps) (*ipni_provider.Provider, error) {
	df := denylist.NewFilter(ctx, d.Cfg.HTTP.DenylistServers)
	rp := retrieval.NewRetrievalProvider(ctx, d.DB, d.IndexStore, d.CachedPieceReader, df)
	retrieval.Router(r, rp, df)

	ipp, err := ipni_provider.NewProvider(d)
	if err != nil {
		return nil, xerrors.Errorf("failed to create new ipni provider: %w", err)
	}
	ipni_provider.Routes(r, ipp)
	go ipp.StartPublishing(ctx)

	return ipp, nil
}
