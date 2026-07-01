package pdpnode

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/cuhttp/servicedeps"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/pdp"
)

// MountPDPRoutes attaches PDP HTTP routes using an existing IPNI provider.
func MountPDPRoutes(ctx context.Context, r chi.Router, d *Deps, sd *servicedeps.Deps, ipp *ipni_provider.Provider) error {
	return pdp.MountRoutes(ctx, r, pdp.MountDeps{
		DB:                   d.DB,
		LocalStore:           d.LocalStore,
		EthClient:            must.One(d.EthClient.Val()),
		Chain:                d.Chain,
		EthSender:            sd.EthSender,
		AlertTask:            sd.AlertTask,
		PDPUploadRequireAuth: d.Cfg.Subsystems.PDPUploadRequireAuth,
	}, ipp)
}

// MountPublicRoutes attaches PDP-only public HTTP routes (IPNI + PDP).
func MountPublicRoutes(ctx context.Context, r chi.Router, d *Deps, sd *servicedeps.Deps) error {
	ipp, err := ipni_provider.NewProvider(d.CurioDeps())
	if err != nil {
		return xerrors.Errorf("ipni provider: %w", err)
	}

	go ipp.StartPublishing(ctx)

	return MountPDPRoutes(ctx, r, d, sd, ipp)
}
