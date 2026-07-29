package pdpv0

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

const cleanupPiecesBatchSize = 10_000

type CleanupPiecesTask struct {
	db        *harmonydb.DB
	ethClient api.EthClientInterface
	sender    *message.SenderETH
}

func NewCleanupPiecesTask(db *harmonydb.DB, ethClient api.EthClientInterface, sender *message.SenderETH) *CleanupPiecesTask {
	return &CleanupPiecesTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
	}
}

func (t *CleanupPiecesTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var dataSetID int64
	err = t.db.QueryRow(ctx, `SELECT id FROM pdp_delete_data_set WHERE cleanup_pieces_task_id = $1`, taskID).Scan(&dataSetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, xerrors.Errorf("failed to select cleanup data set: %w", err)
	}

	sender, err := getPDPOwner(ctx, t.db)
	if err != nil {
		return false, xerrors.Errorf("failed to get pdp owner: %w", err)
	}

	state, err := readDataSetCleanupState(ctx, t.ethClient, dataSetID)
	if err != nil {
		return false, xerrors.Errorf("failed to read PDP cleanup state for data set %d: %w", dataSetID, err)
	}

	// Contract state is authoritative after retries or externally-submitted txs.
	if state.Finalized() {
		if err := cleanupFinalizedDataSet(ctx, t.db, dataSetID); err != nil {
			return false, err
		}
		return true, nil
	}

	// A live set needs deleteDataSet again before cleanupPieces can safely run.
	if state.Live {
		if err := resetCleanupToDelete(ctx, t.db, dataSetID, taskID); err != nil {
			return false, err
		}
		log.Warnw("PDP data set is live during cleanup; reset to deleteDataSet stage", "dataSetId", dataSetID)
		return true, nil
	}

	if !state.CleanupMode {
		return false, xerrors.Errorf("data set %d is not in PDP cleanup mode", dataSetID)
	}

	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier
	txHash, batchSize, err := t.sendCleanupPieces(ctx, sender, pdpAddress, pdpABI, dataSetID, state.NextPieceID)
	if err != nil {
		return false, xerrors.Errorf("failed to send cleanupPieces transaction: %w", err)
	}

	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`UPDATE pdp_delete_data_set
			SET cleanup_pieces_tx_hash = $2,
			    cleanup_pieces_task_id = NULL
			WHERE cleanup_pieces_task_id = $1`, taskID, txHash.Hex())
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_delete_data_set cleanup tx hash: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 row but got %d", n)
		}

		_, err = tx.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, $2)`, txHash.Hex(), "pending")
		if err != nil {
			return false, xerrors.Errorf("failed to insert cleanupPieces message wait: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("failed to commit cleanupPieces transaction state: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit cleanupPieces transaction state")
	}

	log.Infow("submitted PDP cleanupPieces transaction",
		"dataSetId", dataSetID,
		"txHash", txHash.Hex(),
		"batchSize", batchSize,
		"remainingPieceSlots", state.NextPieceID,
		"refundRecipient", sender.Hex())

	return true, nil
}

// sendCleanupPieces submits one cleanupPieces batch. PDPVerifier consumes piece
// slots from nextPieceId downward, so Curio does not keep a local cursor.
func (t *CleanupPiecesTask) sendCleanupPieces(ctx context.Context, sender common.Address, pdpAddress common.Address, pdpABI *abi.ABI, dataSetID int64, remainingPieces *big.Int) (common.Hash, uint64, error) {
	batchSize := uint64(cleanupPiecesBatchSize)
	if remainingPieces.IsUint64() && remainingPieces.Uint64() < batchSize {
		batchSize = remainingPieces.Uint64()
	}
	if batchSize == 0 {
		return common.Hash{}, 0, xerrors.Errorf("cleanupPieces batch size is zero for data set %d", dataSetID)
	}

	for {
		data, err := pdpABI.Pack("cleanupPieces", big.NewInt(dataSetID), new(big.Int).SetUint64(batchSize))
		if err != nil {
			return common.Hash{}, 0, xerrors.Errorf("failed to pack PDP cleanup data set: %w", err)
		}

		// cleanupPieces refunds the prepaid cleanup deposit on finalization.
		txEth := types.NewTransaction(
			0,
			pdpAddress,
			big.NewInt(0),
			0,
			nil,
			data,
		)

		txHash, err := t.sender.Send(ctx, sender, txEth, "pdp-cleanup-pieces")
		if err == nil {
			return txHash, batchSize, nil
		}

		if !isCleanupPiecesGasEstimateOutOfGas(err) {
			return common.Hash{}, 0, err
		}

		// SenderETH estimates gas before submission. This error means the batch was not sent,
		// so retrying with a smaller batch cannot duplicate cleanup work.
		if batchSize == 1 {
			return common.Hash{}, 0, xerrors.Errorf("cleanupPieces gas estimate failed with batch size 1: %w", err)
		}

		nextBatchSize := batchSize / 2
		if nextBatchSize == 0 {
			nextBatchSize = 1
		}
		log.Warnw("cleanupPieces gas estimate failed; retrying with smaller batch",
			"dataSetId", dataSetID,
			"batchSize", batchSize,
			"nextBatchSize", nextBatchSize,
			"err", err)
		batchSize = nextBatchSize
	}
}

func (t *CleanupPiecesTask) schedule(ctx context.Context, addTaskFunc harmonytask.AddTaskFunc) error {
	var stop bool

	for !stop {
		addTaskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true

			n, err := tx.Exec(`WITH pending AS (
					SELECT id
					FROM pdp_delete_data_set
					WHERE cleanup_pieces_task_id IS NULL
					  AND cleanup_pieces_tx_hash IS NULL
					  AND after_delete_data_set = TRUE
					  AND delete_tx_hash IS NULL
					  AND service_termination_epoch IS NOT NULL
					  AND terminated = FALSE
					ORDER BY id
					LIMIT 1
				)
				UPDATE pdp_delete_data_set p
				SET cleanup_pieces_task_id = $1
				FROM pending
				WHERE p.id = pending.id
				  AND p.cleanup_pieces_task_id IS NULL
				  AND p.cleanup_pieces_tx_hash IS NULL
				  AND p.after_delete_data_set = TRUE
				  AND p.delete_tx_hash IS NULL
				  AND p.service_termination_epoch IS NOT NULL
				  AND p.terminated = FALSE`, taskID)
			if err != nil {
				return false, xerrors.Errorf("failed to assign PDP cleanup task: %w", err)
			}
			if n == 0 {
				log.Debugw("no pending PDP data sets for piece cleanup")
				return false, nil
			}
			if n != 1 {
				return false, xerrors.Errorf("updated %d rows assigning PDP cleanup task", n)
			}

			log.Debugw("scheduled PDP cleanupPieces task")
			stop = false
			return true, nil
		})
	}
	return nil
}

func (t *CleanupPiecesTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *CleanupPiecesTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(15),
		Name: tasknames.PDPv0_Cleanup,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Minute*10, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *CleanupPiecesTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

// resetCleanupToDelete repairs unexpected live state by moving back to
// deleteDataSet; cleanupPieces is only valid after PDPVerifier cleanup mode.
func resetCleanupToDelete(ctx context.Context, db *harmonydb.DB, dataSetID int64, taskID harmonytask.TaskID) error {
	n, err := db.Exec(ctx, `UPDATE pdp_delete_data_set
		SET cleanup_pieces_task_id = NULL,
		    cleanup_pieces_tx_hash = NULL,
		    after_delete_data_set = FALSE,
		    delete_tx_hash = NULL,
		    delete_data_set_task_id = NULL
		WHERE id = $1
		  AND cleanup_pieces_task_id = $2`, dataSetID, taskID)
	if err != nil {
		return xerrors.Errorf("failed to reset cleanup state for data set %d: %w", dataSetID, err)
	}
	if n > 1 {
		return xerrors.Errorf("expected to update 0 or 1 rows for data set %d, updated %d", dataSetID, n)
	}
	return nil
}

var _ = harmonytask.Reg(&CleanupPiecesTask{})
var _ harmonytask.TaskInterface = &CleanupPiecesTask{}

var cleanupModeSentinel = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)) // PDPVerifier CLEANUP_MODE_SENTINEL type(uint256).max

type dataSetCleanupState struct {
	Live        bool
	CleanupMode bool
	NextPieceID *big.Int
}

func (s dataSetCleanupState) Finalized() bool {
	return !s.Live && !s.CleanupMode && s.NextPieceID.Sign() == 0
}

func finalizedDataSetCleanupState() dataSetCleanupState {
	return dataSetCleanupState{
		Live:        false,
		CleanupMode: false,
		NextPieceID: big.NewInt(0),
	}
}

func isCleanupPiecesGasEstimateOutOfGas(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "call ran out of gas") ||
		strings.Contains(errStr, "out of gas (7)")
}

// readDataSetCleanupState reduces PDPVerifier state to the deletion states
// Curio cares about: live, cleanup mode, and finalized cleanup.
func readDataSetCleanupState(ctx context.Context, ethClient api.EthClientInterface, dataSetID int64) (dataSetCleanupState, error) {
	verifier, err := contract.NewPDPVerifier(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return dataSetCleanupState{}, xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	setID := big.NewInt(dataSetID)
	live, err := verifier.DataSetLive(contract.EthCallOpts(ctx), setID)
	if err != nil {
		if IsPDPVerifierDataSetNotFound(err) {
			return finalizedDataSetCleanupState(), nil
		}
		return dataSetCleanupState{}, xerrors.Errorf("failed to check if data set %d is live: %w", dataSetID, err)
	}

	nextPieceID, err := verifier.GetNextPieceId(contract.EthCallOpts(ctx), setID)
	if err != nil {
		if IsPDPVerifierDataSetNotFound(err) {
			return finalizedDataSetCleanupState(), nil
		}
		return dataSetCleanupState{}, xerrors.Errorf("failed to get next piece id for data set %d: %w", dataSetID, err)
	}

	cleanupMode := false
	if !live && nextPieceID.Sign() > 0 {
		nextChallengeEpoch, err := verifier.GetNextChallengeEpoch(contract.EthCallOpts(ctx), setID)
		if err != nil {
			if IsPDPVerifierDataSetNotFound(err) {
				return finalizedDataSetCleanupState(), nil
			}
			return dataSetCleanupState{}, xerrors.Errorf("failed to get next challenge epoch for data set %d: %w", dataSetID, err)
		}
		cleanupMode = nextChallengeEpoch.Cmp(cleanupModeSentinel) == 0
	}

	return dataSetCleanupState{
		Live:        live,
		CleanupMode: cleanupMode,
		NextPieceID: new(big.Int).Set(nextPieceID),
	}, nil
}
