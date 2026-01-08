package pdp

import (
	"context"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// NewDatasetSetWatch runes processing steps for data set creation and piece addtion
// These two are run in sequence to allow for combined create-and-add flow to first
// create the data set, then add the pieces to it.
func NewDataSetWatch(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(chainsched.HandlerEntry{Fn:func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetCreates(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending data set creates: %v", err)
		}

		err = processPendingDataSetPieceAdds(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending data set piece adds: %v", err)
		}
		return nil
	}, Priority: chainsched.PriorityNormal}); err != nil {
		panic(err)
	}
}
