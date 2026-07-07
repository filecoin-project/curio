package pdpv0

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/paths/alertinginterface"

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
	if err := w.AddWatcher(func(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, al alertinginterface.AlertingInterface, revert, apply *chainTypes.TipSet) {
		cat := al.AddAlertType(alertNameCreateDataSet, alertType)
		err := processPendingDataSetCreates(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process pending data set creates: %v", err)
			al.Raise(cat, map[string]interface{}{
				"error": err.Error(),
			})
		}

		adat := al.AddAlertType(alertNameAddPiece, alertType)
		err = processPendingDataSetPieceAdds(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process pending data set piece adds: %v", err)
			al.Raise(adat, map[string]interface{}{
				"error": err.Error(),
			})
		}
	}, WatcherOrderCreateAndAdd); err != nil {
		panic(err)
	}
}
