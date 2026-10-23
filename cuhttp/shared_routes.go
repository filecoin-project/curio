package cuhttp

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/market/denylist"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/market/retrieval"
)

// MountRetrievalPublicRoutes mounts piece/IPFS retrieval with bad-bits denylist filtering.
// Skiff and full Curio both use this; tests can mount it without standing up IPNI.
func MountRetrievalPublicRoutes(ctx context.Context, r *chi.Mux, d *deps.Deps) *denylist.Filter {
	df := denylist.NewFilter(ctx, d.Cfg.HTTP.DenylistServers)
	rp := retrieval.NewRetrievalProvider(ctx, d.DB, d.IndexStore, d.CachedPieceReader, df)
	setBlockChain(ctx, rp, d)
	retrieval.Router(r, rp, df)
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

// setBlockChain loads the blockchain FOC grants must name.
// A failure leaves it unset, and ACL downloads are refused until it is known.
func setBlockChain(ctx context.Context, rp *retrieval.Provider, d *deps.Deps) {
	if d == nil || d.EthClient == nil {
		log.Warn("FOC grant checks have no eth client; ACL downloads will be refused")
		return
	}
	eth, err := d.EthClient.Get()
	if err != nil {
		log.Warnw("FOC grant checks could not open the eth client", "error", err)
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	id, err := eth.ChainID(cctx)
	if err != nil {
		log.Warnw("FOC grant checks could not read the blockchain", "error", err)
		return
	}
	rp.SetBlockChain(id)
}
