package pdpnode

import (
	"context"

	"github.com/filecoin-project/curio/cuhttp/servicedeps"
	curiodeps "github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/piecestore"
	pdpwallet "github.com/filecoin-project/curio/pdp/wallet"
)

// Attach registers PDP tasks on a curio node.
func Attach(
	ctx context.Context,
	cd *curiodeps.Deps,
	activeTasks *[]harmonytask.TaskInterface,
	sdeps *servicedeps.Deps,
	chainSched *chainsched.CurioChainSched,
) error {
	if cd.Cfg.Subsystems.EnablePDP {
		hasKey, err := pdpwallet.HasPDPKey(ctx, cd.DB)
		if err != nil {
			log.Warnf("checking PDP wallet: %s", err)
		} else if !hasKey && cd.Alert != nil {
			log.Warn("PDP signing key not configured")
			cd.Alert.AddAlert("PDP wallet not configured. Create or assign a key on the PDP page.")
		}
	}

	d := FromCurio(cd)
	sd, err := AppendTasks(ctx, d, chainSched, activeTasks)
	if err != nil {
		return err
	}
	if sd.EthSender != nil {
		sdeps.EthSender = sd.EthSender
	}
	if sd.AlertTask != nil {
		sdeps.AlertTask = sd.AlertTask
	}
	return nil
}

// FromCurio maps curio runtime deps into pdpnode deps.
func FromCurio(cd *curiodeps.Deps) *Deps {
	return &Deps{
		Cfg:               cd.Cfg,
		DB:                cd.DB,
		Chain:             cd.Chain,
		Bstore:            cd.Bstore,
		Stor:              cd.Stor,
		LocalStore:        cd.LocalStore,
		LocalPaths:        cd.LocalPaths,
		Si:                cd.Si,
		PieceIO:           piecestore.New(cd.Stor, cd.LocalStore, cd.Si),
		IndexStore:        cd.IndexStore,
		SectorReader:      cd.SectorReader,
		CachedPieceReader: cd.CachedPieceReader,
		ServeChunker:      cd.ServeChunker,
		EthClient:         cd.EthClient,
		Sender:            cd.Sender,
		Al:                cd.Al,
		Alert:             cd.Alert,
		MachineHost:       cd.ListenAddr,
		Name:              cd.Name,
	}
}
