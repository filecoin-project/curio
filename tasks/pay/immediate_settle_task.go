package pay

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

/*
# PDPv0 Client Termination Settlement Races

Immediate settlement is only an accelerator after client-requested termination.
Periodic pay settlement remains the correctness path.

## No Attempt Counters

`client terminate succeeds`
-> `pdp_delete_data_set.service_termination_epoch` is set
-> `ClientSettle` may try to settle the PDP rail
-> if it fails or gets stuck, leave the row as-is
-> periodic pay later settles the rail
-> pay watcher sets `deletion_allowed = TRUE`
-> delete pipeline continues

Failed immediate-settle transactions must not reset immediate-settle fields for
retry. Otherwise the immediate path can requeue forever.

## Races

### Periodic Settles Before Immediate Scheduling

`terminate watcher sets service_termination_epoch`
-> `periodic pay sends settle`
-> `pay watcher sees finalized rail`
-> `deletion_allowed = TRUE`
-> immediate scheduler no longer matches the row

Handled.

### Immediate Scheduled, Periodic Settles Before Do Runs

`immediate_settle_task_id` is set
-> `periodic pay settles/finalizes`
-> `ClientSettle.Do` reads the rail
-> rail is already finalized or settled to `endEpoch`
-> `ClientSettle.Do` marks `deletion_allowed = TRUE` or exits cleanly

Handled.

### Immediate And Periodic Both Send Settle

`ClientSettle.Do` reads unsettled rail
-> periodic pay also reads unsettled rail
-> both send settle txs
-> first tx lands and settles/finalizes the rail
-> second tx may fail, revert, or make no progress
-> watcher for the winning tx moves the pipeline
-> watcher for the losing tx removes its payment tracking row

Handled, with possible log noise.

### Periodic Sends Older Settle Before Termination

`periodic pay sends settle(until old current)`
-> client termination succeeds
-> periodic tx lands but does not finalize to termination `endEpoch`
-> terminate watcher sets `service_termination_epoch`
-> immediate settle or a later periodic settle reaches `endEpoch`
-> pay watcher sets `deletion_allowed = TRUE`

Handled.

### Immediate Tx Fails On Chain

`ClientSettle` sends tx
-> tx fails
-> pay watcher sees failure
-> pay watcher does not clear immediate-settle fields
-> row stays terminated but not deletion-allowed
-> periodic pay later settles
-> pay watcher moves the pipeline

Handled.

### Immediate Task Fails Before Sending Tx

`immediate_settle_task_id` is set
-> `ClientSettle.Do` fails before tx submission
-> Harmony retries with task backoff
-> if retries exhaust, Harmony deletes the task
-> row keeps the stale `immediate_settle_task_id`
-> immediate settlement does not reschedule
-> periodic pay later settles
-> pay watcher moves the pipeline

Handled.
*/

type ImmediateSettleTask struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	sender    *message.SenderETH
}

func NewImmediateSettleTask(db *harmonydb.DB, ethClient ethchain.EthClient, sender *message.SenderETH) *ImmediateSettleTask {
	return &ImmediateSettleTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
	}
}

func (t *ImmediateSettleTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var dataSetID int64
	err = t.db.QueryRow(ctx, `
		SELECT id
		FROM pdp_delete_data_set
		WHERE immediate_settle_task_id = $1
		  AND client_requested_termination = TRUE
	`, taskID).Scan(&dataSetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, nil
		}
		return false, xerrors.Errorf("failed to select data set for immediate settlement: %w", err)
	}

	if !stillOwned() {
		return false, nil
	}

	from, err := pdpSettlementSender(ctx, t.db)
	if err != nil {
		return false, err
	}

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, t.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to get FWSS view address: %w", err)
	}

	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, t.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate FWSS service state view: %w", err)
	}

	ds, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get data set %d from FWSS view: %w", dataSetID, err)
	}
	if ds.PdpRailId == nil || ds.PdpRailId.Sign() <= 0 {
		return false, xerrors.Errorf("data set %d has no PDP rail", dataSetID)
	}

	paymentContractAddr, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return false, fmt.Errorf("failed to get payment contract address: %w", err)
	}

	payment, err := filecoinpayment.NewPayments(paymentContractAddr, t.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to create payments contract: %w", err)
	}

	rail, err := payment.GetRail(contract.EthCallOpts(ctx), ds.PdpRailId)
	if err != nil {
		if filecoinpayment.IsRailInactiveOrSettledError(err) {
			if err := t.completeImmediateSettle(ctx, dataSetID, taskID); err != nil {
				return false, err
			}
			log.Infow("PDP client termination rail already finalized", "dataSetId", dataSetID, "railId", ds.PdpRailId)
			return true, nil
		}
		return false, xerrors.Errorf("failed to get payment rail %s for data set %d: %w", ds.PdpRailId, dataSetID, err)
	}

	if rail.EndEpoch == nil || rail.EndEpoch.Sign() == 0 {
		return false, xerrors.Errorf("PDP rail %s for data set %d is not terminated", ds.PdpRailId, dataSetID)
	}
	if rail.SettledUpTo != nil && rail.SettledUpTo.Cmp(rail.EndEpoch) >= 0 {
		if err := t.completeImmediateSettle(ctx, dataSetID, taskID); err != nil {
			return false, err
		}
		log.Infow("PDP client termination rail already settled to end epoch", "dataSetId", dataSetID, "railId", ds.PdpRailId, "endEpoch", rail.EndEpoch, "settledUpTo", rail.SettledUpTo)
		return true, nil
	}

	current, err := t.ethClient.BlockNumber(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get current block number: %w", err)
	}
	if rail.EndEpoch.Uint64() > current {
		if err := t.clearImmediateSettleTask(ctx, dataSetID, taskID); err != nil {
			return false, err
		}
		log.Warnw("PDP client termination rail end epoch has not elapsed; skipping immediate settlement for now", "dataSetId", dataSetID, "railId", ds.PdpRailId, "endEpoch", rail.EndEpoch, "current", current)
		return true, nil
	}

	pabi, err := filecoinpayment.PaymentsMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get Payments ABI: %w", err)
	}

	data, err := pabi.Pack("settleRail", ds.PdpRailId, big.NewInt(int64(current)))
	if err != nil {
		return false, xerrors.Errorf("failed to pack settleRail call: %w", err)
	}

	txEth := types.NewTx(&types.LegacyTx{
		Nonce:    0,
		To:       &paymentContractAddr,
		Value:    big.NewInt(0),
		Gas:      0,
		GasPrice: nil,
		Data:     data,
	})

	if !stillOwned() {
		return false, nil
	}

	txHash, err := t.sender.Send(ctx, from, txEth, "pdp-client-termination-settle")
	if err != nil {
		return false, xerrors.Errorf("failed to send immediate settlement transaction: %w", err)
	}

	txHashHex := strings.ToLower(txHash.Hex())
	railIDs := []int64{ds.PdpRailId.Int64()}

	committed, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE pdp_delete_data_set
			SET immediate_settle_tx_hash = $2,
			    after_immediate_settle = TRUE,
			    immediate_settle_task_id = NULL
			WHERE immediate_settle_task_id = $1
			  AND id = $3
			  AND client_requested_termination = TRUE
		`, taskID, txHashHex, dataSetID)
		if err != nil {
			return false, xerrors.Errorf("failed to update immediate settlement state: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 immediate settlement row, updated %d", n)
		}

		n, err = tx.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')`, txHashHex)
		if err != nil {
			return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to insert 1 row into message_waits_eth, inserted %d", n)
		}

		n, err = tx.Exec(`
			INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids, settle_reason)
			VALUES ($1, $2, $3)
		`, txHashHex, railIDs, SettleReasonPDPv0ClientTermination)
		if err != nil {
			return false, xerrors.Errorf("failed to insert into filecoin_payment_transactions: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to insert 1 row into filecoin_payment_transactions, inserted %d", n)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, err
	}
	if !committed {
		return false, xerrors.Errorf("failed to commit immediate settlement transaction record")
	}

	log.Infow("sent immediate PDP client termination settlement transaction", "dataSetId", dataSetID, "railId", ds.PdpRailId, "txHash", txHashHex)
	return true, nil
}

func (t *ImmediateSettleTask) completeImmediateSettle(ctx context.Context, dataSetID int64, taskID harmonytask.TaskID) error {
	n, err := t.db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET deletion_allowed = TRUE,
		    after_immediate_settle = TRUE,
		    immediate_settle_task_id = NULL
		WHERE id = $1
		  AND immediate_settle_task_id = $2
		  AND client_requested_termination = TRUE
	`, dataSetID, taskID)
	if err != nil {
		return xerrors.Errorf("failed to complete immediate settlement for data set %d: %w", dataSetID, err)
	}
	if n != 0 && n != 1 {
		return xerrors.Errorf("expected to update 0 or 1 immediate settlement rows for data set %d, updated %d", dataSetID, n)
	}
	return nil
}

func (t *ImmediateSettleTask) clearImmediateSettleTask(ctx context.Context, dataSetID int64, taskID harmonytask.TaskID) error {
	n, err := t.db.Exec(ctx, `
		UPDATE pdp_delete_data_set
		SET immediate_settle_task_id = NULL
		WHERE id = $1
		  AND immediate_settle_task_id = $2
		  AND client_requested_termination = TRUE
		  AND after_immediate_settle = FALSE
	`, dataSetID, taskID)
	if err != nil {
		return xerrors.Errorf("failed to clear immediate settlement task for data set %d: %w", dataSetID, err)
	}
	if n != 0 && n != 1 {
		return xerrors.Errorf("expected to update 0 or 1 immediate settlement task rows for data set %d, updated %d", dataSetID, n)
	}
	return nil
}

func (t *ImmediateSettleTask) schedule(ctx context.Context, addTaskFunc harmonytask.AddTaskFunc) error {
	current, err := t.ethClient.BlockNumber(ctx)
	if err != nil {
		return xerrors.Errorf("failed to get current block number: %w", err)
	}

	var stop bool
	for !stop {
		addTaskFunc(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true

			var pendings []int64
			err := tx.Select(&pendings, `
				SELECT id
				FROM pdp_delete_data_set
				WHERE client_requested_termination = TRUE
				  AND after_terminate_service = TRUE
				  AND service_termination_epoch IS NOT NULL
				  AND service_termination_epoch <= $1
				  AND deletion_allowed = FALSE
				  AND immediate_settle_task_id IS NULL
				  AND after_immediate_settle = FALSE
				ORDER BY service_termination_epoch, id
				LIMIT 1
			`, current)
			if err != nil {
				return false, xerrors.Errorf("failed to select client terminations for immediate settlement: %w", err)
			}

			if len(pendings) == 0 {
				log.Debugw("no client-requested PDP terminations to settle immediately")
				return false, nil
			}

			pending := pendings[0]
			n, err := tx.Exec(`
				UPDATE pdp_delete_data_set
				SET immediate_settle_task_id = $1,
				    immediate_settle_requested_at = COALESCE(immediate_settle_requested_at, NOW())
				WHERE id = $2
				  AND client_requested_termination = TRUE
				  AND after_terminate_service = TRUE
				  AND service_termination_epoch IS NOT NULL
				  AND service_termination_epoch <= $3
				  AND deletion_allowed = FALSE
				  AND immediate_settle_task_id IS NULL
				  AND after_immediate_settle = FALSE
			`, taskID, pending, current)
			if err != nil {
				return false, xerrors.Errorf("failed to update immediate settlement task id: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("updated %d immediate settlement rows", n)
			}

			log.Debugw("scheduled immediate PDP client termination settlement task", "dataSetId", pending)
			stop = false
			return true, nil
		})
	}

	return nil
}

func (t *ImmediateSettleTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *ImmediateSettleTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: tasknames.ClientSettle,
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Minute, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *ImmediateSettleTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &ImmediateSettleTask{}
var _ = harmonytask.Reg(&ImmediateSettleTask{})

func pdpSettlementSender(ctx context.Context, db *harmonydb.DB) (common.Address, error) {
	var sender string
	err := db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' ORDER BY address ASC`).Scan(&sender)
	if err != nil {
		return common.Address{}, fmt.Errorf("failed to retrieve PDP operator address: %w", err)
	}
	if sender == "" {
		return common.Address{}, fmt.Errorf("no PDP operator address found")
	}
	return common.HexToAddress(sender), nil
}
