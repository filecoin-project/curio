package pdp

import (
	"context"

	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// NewProofSetWatch runes processing steps for proofset creation and piece addtion
// These two are run in sequence to allow for combined create-and-add flow to first
// create the proofset, then add the pieces to it.
func NewProofSetWatch(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetCreates(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending data set creates: %v", err)
		}

		err = processPendingDataSetPieceAdds(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending data set piece adds: %v", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}
