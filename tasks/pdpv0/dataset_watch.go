package pdpv0

import (
	"context"
	"fmt"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

const (
	alertType              = "PDPV0"
	alertNameCreateDataSet = "CreateDataSet"
	alertNameAddPiece      = "AddPiece"
)

// NewDataSetWatch runes processing steps for data set creation and piece addtion
// These two are run in sequence to allow for combined create-and-add flow to first
// create the data set, then add the pieces to it.
func NewDataSetWatch(w *Watcher) {
	if err := w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al curioalerting.AlertingInterface, revert, apply *chainTypes.TipSet) {
		err := processPendingDataSetCreates(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process pending data set creates: %v", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameCreateDataSet,
				Message:   fmt.Sprintf("failed to process pending data set creates: %s", err),
			})
		}

		err = processPendingDataSetPieceAdds(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process pending data set piece adds: %v", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    alertType,
				Subsystem: alertNameAddPiece,
				Message:   fmt.Sprintf("failed to process pending data set piece adds: %s", err),
			})
		}
	}, WatcherOrderCreateAndAdd); err != nil {
		panic(err)
	}
}
