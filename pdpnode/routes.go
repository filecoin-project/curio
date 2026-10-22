package pdpnode

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/cuhttp/servicedeps"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/pdp"
)

// MountPDPRoutes attaches PDP HTTP routes using an existing IPNI provider.
func MountPDPRoutes(ctx context.Context, r chi.Router, d *Deps, sd *servicedeps.Deps, ipp *ipni_provider.Provider) error {
	return pdp.MountRoutes(ctx, r, pdp.MountDeps{
		DB:               d.DB,
		PieceIO:          d.PieceIO,
		EthClient:        must.One(d.EthClient.Val()),
		Chain:            d.Chain,
		EthSender:        sd.EthSender,
		AlertTask:        sd.AlertTask,
		AuthorizerConfig: d.Cfg.Subsystems.PDPAuthorizers,
	}, ipp)
}

// MountPublicRoutes attaches shared public HTTP routes (retrieval + denylist + IPNI) and PDP.
func MountPublicRoutes(ctx context.Context, r *chi.Mux, d *Deps, sd *servicedeps.Deps) error {
	ipp, err := cuhttp.MountCommonPublicRoutes(ctx, r, d.CurioDeps())
	if err != nil {
		return xerrors.Errorf("common public routes: %w", err)
	}

	return MountPDPRoutes(ctx, r, d, sd, ipp)
}
