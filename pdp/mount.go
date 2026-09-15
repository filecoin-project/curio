package pdp

import (
	"context"

	"github.com/go-chi/chi/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/alertmanager"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/piecestore"
	ipni_provider "github.com/filecoin-project/curio/market/ipni/ipni-provider"
)

// MountDeps holds dependencies for mounting PDP HTTP routes.
type MountDeps struct {
	DB         *harmonydb.DB
	PieceIO    piecestore.PieceIO
	EthClient  ethchain.EthClient
	Chain      api.Chain
	EthSender  ETHTxSender
	AlertTask  *alertmanager.AlertTask
	// AuthorizerConfig is the SP-side authorizer allowlist policy (Subsystems.PDPAuthorizers). The
	// zero value opts out (relays for any authorizer); real callers pass Cfg.Subsystems.PDPAuthorizers.
	AuthorizerConfig config.PDPAuthorizerConfig
}

// MountRoutes registers PDP HTTP routes on an existing router.
func MountRoutes(ctx context.Context, r chi.Router, d MountDeps, ipp *ipni_provider.Provider) error {
	if d.EthSender == nil {
		return xerrors.Errorf("eth sender required for PDP routes")
	}
	if d.PieceIO == nil {
		return xerrors.Errorf("piece IO required for PDP routes")
	}

	authPolicy, err := NewAuthorizerAllowlist(d.AuthorizerConfig)
	if err != nil {
		return xerrors.Errorf("building PDP authorizer allowlist: %w", err)
	}

	pdsvc := NewPDPService(ctx, d.DB, d.PieceIO, d.EthClient, d.Chain, d.EthSender, d.AlertTask, ipp)

	Routes(r, pdsvc)
	return nil
}
