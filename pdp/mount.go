package pdp

import (
	"context"

	"github.com/go-chi/chi/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/paths"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
	"github.com/filecoin-project/curio/tasks/message"
)

// MountDeps holds dependencies for mounting PDP HTTP routes.
type MountDeps struct {
	DB         *harmonydb.DB
	LocalStore paths.StashStore
	EthClient  ethchain.EthClient
	Chain      api.Chain
	EthSender  *message.SenderETH
	AlertTask  *alertmanager.AlertTask
}

// MountRoutes registers PDP HTTP routes on an existing router.
func MountRoutes(ctx context.Context, r chi.Router, d MountDeps, ipp *ipni_provider.Provider) error {
	if d.EthSender == nil {
		return xerrors.Errorf("eth sender required for PDP routes")
	}

	pdsvc := NewPDPService(ctx, d.DB, d.LocalStore, d.EthClient, d.Chain, d.EthSender, d.AlertTask, ipp)
	Routes(r, pdsvc)
	return nil
}
