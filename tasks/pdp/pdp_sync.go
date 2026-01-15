package pdp

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/pdp/contract"
)

type PDPSyncTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
}

func NewPDPSyncTask(db *harmonydb.DB, ethClient *ethclient.Client) *PDPSyncTask {
	return &PDPSyncTask{
		db:        db,
		ethClient: ethClient,
	}
}

func (P *PDPSyncTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Fetch all proving pieces from DB
	var provingPieces []struct {
		ID     int64   `db:"id"`
		Pieces []int64 `db:"pieces"`
	}

	err = P.db.Select(ctx, &provingPieces, `SELECT
											  d.id AS data_set_id,
											  array_agg(p.piece ORDER BY p.piece) AS pieces
											FROM pdp_data_set d
											JOIN pdp_dataset_piece p
											  ON p.data_set_id = d.id
											WHERE COALESCE(d.removed, FALSE) = FALSE
											  AND COALESCE(p.removed, FALSE) = FALSE
											GROUP BY d.id;`)
	if err != nil {
		return false, xerrors.Errorf("failed to get proving pieces: %w", err)
	}

	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, P.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	var removedDataSetIDs []int64
	var provingDataSetIDs []int

	// Check if the data set is live
	for i, pp := range provingPieces {
		did := big.NewInt(pp.ID)
		live, err := verifier.DataSetLive(&bind.CallOpts{Context: ctx}, did)
		if err != nil {
			return false, xerrors.Errorf("failed to check if data set %d is live: %w", pp.ID, err)
		}
		if !live {
			removedDataSetIDs = append(removedDataSetIDs, pp.ID)
		} else {
			provingDataSetIDs = append(provingDataSetIDs, i)
		}
	}

	// Check if the pieces are live in the data set which are still live
	removedPieces := make(map[int64][]int64)

	for _, pp := range provingDataSetIDs {
		for _, piece := range provingPieces[pp].Pieces {
			did := big.NewInt(piece)
			live, err := verifier.PieceLive(&bind.CallOpts{Context: ctx}, did, big.NewInt(piece))
			if err != nil {
				return false, xerrors.Errorf("failed to check if piece %d is live in data set %d: %w", piece, provingPieces[pp].ID, err)
			}
			if !live {
				removedPieces[provingPieces[pp].ID] = append(removedPieces[provingPieces[pp].ID], piece)
			}
		}
	}

	// Update in DB
	comm, err := P.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Mark the data set as removed
		if len(removedPieces) > 0 {
			_, err = tx.Exec(`UPDATE pdp_data_set SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE id = ANY($3)`, "Terminated On Chain", "Terminated On Chain", removedDataSetIDs) // TODO: Figure out how can we reliably get txHash for this from events
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_data_set: %w", err)
			}

			// Start piece cleanup tasks
			_, err = tx.Exec(`INSERT INTO piece_cleanup (id, piece_cid_v2, pdp, sp_id, sector_number, piece_ref)
								SELECT p.add_deal_id, p.piece_cid_v2, TRUE, -1, -1, p.piece_ref
								FROM pdp_dataset_piece AS p
								WHERE p.data_set_id = ANY($1)
									AND p.removed = FALSE
								ON CONFLICT (id, pdp) DO NOTHING;`, removedDataSetIDs)
			if err != nil {
				return false, xerrors.Errorf("failed to insert into piece_cleanup: %w", err)
			}

			_, err = tx.Exec(`UPDATE pdp_dataset_piece SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE data_set_id = ANY($3)`, "Terminated On Chain", "Terminated On Chain", removedDataSetIDs) // TODO: Figure out how can we reliably get txHash for this from events
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_dataset_piece: %w", err)
			}
		}

		// Mark the pieces as removed
		for did, pp := range removedPieces {
			if len(pp) == 0 {
				continue
			}

			_, err = tx.Exec(`UPDATE pdp_dataset_piece SET 
                             removed = TRUE, 
                             remove_deal_id = $1, 
                             remove_message_hash = $2  WHERE data_set_id = $3 AND piece = ANY($4)`, "Terminated On Chain", "Terminated On Chain", did, pp) // TODO: Figure out how can we reliably get txHash for this from events
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_dataset_piece: %w", err)
			}

			_, err = tx.Exec(`INSERT INTO piece_cleanup (id, piece_cid_v2, pdp, sp_id, sector_number, piece_ref)
								SELECT p.add_deal_id, p.piece_cid_v2, TRUE, -1, -1, p.piece_ref
								FROM pdp_dataset_piece AS p
								WHERE p.data_set_id = $1
								  AND p.piece = ANY($2)
								ON CONFLICT (id, pdp) DO NOTHING;`, did, pp)
			if err != nil {
				return false, xerrors.Errorf("failed to insert into piece_cleanup: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, xerrors.Errorf("failed to commit transaction: %w", err)
	}

	if !comm {
		return false, xerrors.Errorf("failed to commit transaction")
	}

	return true, nil
}

func (P *PDPSyncTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (P *PDPSyncTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "PDPSync",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		MaxFailures: 3,
		IAmBored:    harmonytask.SingletonTaskAdder(time.Hour*6, P),
	}
}

func (P *PDPSyncTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &PDPSyncTask{}
var _ = harmonytask.Reg(&PDPSyncTask{})
