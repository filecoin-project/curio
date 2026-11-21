package pay

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

type settleWatcherClient struct {
	ethchain.EthClient
	t          *testing.T
	responses  map[string][]byte
	block      uint64
	blockCalls int
}

func (c *settleWatcherClient) BlockNumber(context.Context) (uint64, error) {
	c.blockCalls++
	return c.block, nil
}

func (c *settleWatcherClient) CallContract(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	c.t.Helper()
	response, ok := c.responses[string(msg.Data)]
	require.True(c.t, ok, "unexpected contract call: %x", msg.Data)
	return response, nil
}

func newSettleWatcherClient(t *testing.T) *settleWatcherClient {
	t.Helper()
	return newSettleWatcherClientWith(t, settleWatcherRail{block: 1000, lockupPeriod: 30 * builtin.EpochsInDay, dataSetID: 1})
}

type settleWatcherRail struct {
	block        uint64
	lockupPeriod int64
	endEpoch     int64
	dataSetID    int64
}

func newSettleWatcherClientWith(t *testing.T, rail settleWatcherRail) *settleWatcherClient {
	t.Helper()
	t.Setenv("CURIO_DEVNET_PAYMENTS_ADDRESS", "0x0000000000000000000000000000000000000001")
	t.Setenv("CURIO_DEVNET_PDP_VERIFIER_ADDRESS", "0x0000000000000000000000000000000000000002")
	t.Setenv("CURIO_DEVNET_FWSS_ADDRESS", "0x0000000000000000000000000000000000000003")

	c := &settleWatcherClient{t: t, responses: make(map[string][]byte), block: rail.block}
	addResponse := func(metadata *bind.MetaData, name string, args []any, value any) {
		parsed, err := metadata.GetAbi()
		require.NoError(t, err)
		data, err := parsed.Pack(name, args...)
		require.NoError(t, err)
		response, err := parsed.Methods[name].Outputs.Pack(value)
		require.NoError(t, err)
		c.responses[string(data)] = response
	}

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	addResponse(filecoinpayment.PaymentsMetaData, "getRail", []any{big.NewInt(1)}, filecoinpayment.PaymentsRailView{
		Operator:          serviceAddr,
		Validator:         serviceAddr,
		PaymentRate:       big.NewInt(1),
		LockupPeriod:      big.NewInt(rail.lockupPeriod),
		LockupFixed:       big.NewInt(0),
		SettledUpTo:       big.NewInt(900),
		EndEpoch:          big.NewInt(rail.endEpoch),
		CommissionRateBps: big.NewInt(0),
	})
	addResponse(FWSS.FilecoinWarmStorageServiceStateViewMetaData, "railToDataSet", []any{big.NewInt(1)}, big.NewInt(rail.dataSetID))
	addResponse(contract.ContractWithViewMetaData, "viewContractAddress", nil, common.HexToAddress("0x0000000000000000000000000000000000000004"))
	return c
}

type settleWatcherAlerts struct {
	curioalerting.AlertingInterface
	events []curioalerting.AlertEvent
}

func (a *settleWatcherAlerts) EmitEvent(_ context.Context, event curioalerting.AlertEvent) error {
	a.events = append(a.events, event)
	return nil
}

func insertSettleWatcherState(t *testing.T, db *harmonydb.DB, retry, runNow bool) settled {
	t.Helper()
	s := settled{
		Hash:  common.HexToHash("0x01").Hex(),
		Rails: []int64{1},
		Retry: retry,
	}
	_, err := db.Exec(t.Context(), `
		INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids, retry)
		VALUES ($1, $2, $3)
	`, s.Hash, s.Rails, s.Retry)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `
		INSERT INTO harmony_task_singletons (task_name, run_now_request)
		VALUES ($1, $2), ('UnrelatedTask', FALSE)
	`, tasknames.Settle, runNow)
	require.NoError(t, err)
	return s
}

func requireSettleWatcherState(t *testing.T, db *harmonydb.DB, s settled, tracked, runNow bool) {
	t.Helper()
	var exists bool
	err := db.QueryRow(t.Context(), `SELECT EXISTS (
		SELECT 1 FROM filecoin_payment_transactions WHERE tx_hash = $1
	)`, s.Hash).Scan(&exists)
	require.NoError(t, err)
	require.Equal(t, tracked, exists, "settlement tracking")

	var requested bool
	err = db.QueryRow(t.Context(), `
		SELECT run_now_request FROM harmony_task_singletons WHERE task_name = $1
	`, tasknames.Settle).Scan(&requested)
	require.NoError(t, err)
	require.Equal(t, runNow, requested, "settlement rerun request")
	err = db.QueryRow(t.Context(), `
		SELECT run_now_request FROM harmony_task_singletons WHERE task_name = 'UnrelatedTask'
	`).Scan(&requested)
	require.NoError(t, err)
	require.False(t, requested, "unrelated singleton must remain unchanged")
}

func TestIntegration_ProcessPendingTransactions_Retry(t *testing.T) {
	success, failure := true, false
	tests := []struct {
		name         string
		status       string
		success      *bool
		retry        bool
		runNow       bool
		wantTracked  bool
		wantRunNow   bool
		wantVerified int
		wantAlerts   int
	}{
		{name: "confirmed partial settlement requests rerun", status: "confirmed", success: &success, retry: true, wantRunNow: true, wantVerified: 1},
		{name: "confirmed complete settlement needs no rerun", status: "confirmed", success: &success, wantVerified: 1},
		{name: "partial settlement preserves existing request", status: "confirmed", success: &success, retry: true, runNow: true, wantRunNow: true, wantVerified: 1},
		{name: "complete settlement preserves existing request", status: "confirmed", success: &success, runNow: true, wantRunNow: true, wantVerified: 1},
		{name: "pending settlement waits for confirmation", status: "pending", retry: true, wantTracked: true},
		{name: "failed settlement is cleaned up without rerun", status: "failed", success: &failure, retry: true, wantAlerts: 1},
		{name: "reverted settlement is cleaned up without rerun", status: "confirmed", success: &failure, retry: true, wantAlerts: 1},
		{name: "confirmed settlement without success is rejected", status: "confirmed", retry: true, wantAlerts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newSettleWatcherClient(t)
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			s := insertSettleWatcherState(t, db, tt.retry, tt.runNow)
			_, err = db.Exec(t.Context(), `
				INSERT INTO message_waits_eth (signed_tx_hash, tx_status, tx_success)
				VALUES ($1, $2, $3)
			`, s.Hash, tt.status, tt.success)
			require.NoError(t, err)

			alerts := &settleWatcherAlerts{}
			require.NoError(t, processPendingTransactions(t.Context(), db, client, alerts))
			requireSettleWatcherState(t, db, s, tt.wantTracked, tt.wantRunNow)
			require.Equal(t, tt.wantVerified, client.blockCalls)
			require.Len(t, alerts.events, tt.wantAlerts)
			if tt.wantAlerts > 0 {
				require.Contains(t, alerts.events[0].Message, s.Hash)
			}

			if !tt.wantTracked {
				_, err = db.Exec(t.Context(), `UPDATE harmony_task_singletons SET run_now_request = FALSE WHERE task_name = $1`, tasknames.Settle)
				require.NoError(t, err)
				require.NoError(t, processPendingTransactions(t.Context(), db, client, alerts))
				requireSettleWatcherState(t, db, s, false, false)
				require.Equal(t, tt.wantVerified, client.blockCalls, "completed tracking must not be verified again")
				require.Len(t, alerts.events, tt.wantAlerts)
			}
		})
	}
}

func TestIntegration_ProcessPendingTransactions_RailDefaultAlert(t *testing.T) {
	// The rail is settled up to epoch 900 with a 50-epoch lockup, so its lockup ends at epoch 950.
	tests := []struct {
		name        string
		block       uint64
		endEpoch    int64
		wantMessage string
	}{
		{
			name:        "within grace",
			block:       1000,
			wantMessage: "Rail 1 is soon to default (dataSet 2)",
		},
		{
			name:        "past grace",
			block:       950 + temporaryDefaultGraceInEpochs + 1,
			wantMessage: "Rail 1 has been unpaid for more than 30 days past its lockup period; Curio will not terminate dataSet 2 on its own",
		},
		{
			name:        "terminated rail",
			block:       1000,
			endEpoch:    2000,
			wantMessage: "Rail 1 is soon to default (dataSet 2)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newSettleWatcherClientWith(t, settleWatcherRail{block: tc.block, lockupPeriod: 50, endEpoch: tc.endEpoch, dataSetID: 2})
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			s := insertSettleWatcherState(t, db, false, false)
			_, err = db.Exec(t.Context(), `
				INSERT INTO message_waits_eth (signed_tx_hash, tx_status, tx_success)
				VALUES ($1, 'confirmed', TRUE)
			`, s.Hash)
			require.NoError(t, err)

			require.NoError(t, processPendingTransactions(t.Context(), db, client, curioalerting.NewAlertingSystem(db)))
			requireSettleWatcherState(t, db, s, false, false)

			var events []struct {
				Message string `db:"message"`
				Context []byte `db:"context"`
			}
			require.NoError(t, db.Select(t.Context(), &events, `SELECT message, context FROM alert_history WHERE kind = 'event'`))
			if tc.wantMessage == "" {
				require.Empty(t, events)
			} else {
				require.Len(t, events, 1)
				require.Equal(t, tc.wantMessage, events[0].Message)
				require.JSONEq(t, `{"dataset": "2", "rail": "1"}`, string(events[0].Context))
			}

			var deletionQueued bool
			require.NoError(t, db.QueryRow(t.Context(), `
				SELECT EXISTS (SELECT 1 FROM pdp_delete_data_set WHERE id = 2)
			`).Scan(&deletionQueued))
			require.False(t, deletionQueued, "the watcher must not queue a data set for deletion")
		})
	}
}

// insertSettleWatcherDataSet adds the local data set that rail 1 pays for.
func insertSettleWatcherDataSet(t *testing.T, db *harmonydb.DB) {
	t.Helper()
	_, err := db.Exec(t.Context(), `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, []byte("settle-watcher"), "settle-watcher")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
		VALUES (2, $1, 'settle-watcher', 100, 10, FALSE)
	`, fmt.Sprintf("0x%064x", 2))
	require.NoError(t, err)
}

func insertConfirmedSettle(t *testing.T, db *harmonydb.DB, hash string) {
	t.Helper()
	_, err := db.Exec(t.Context(), `
		INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids, retry)
		VALUES ($1, $2, FALSE)
	`, hash, []int64{1})
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `
		INSERT INTO message_waits_eth (signed_tx_hash, tx_status, tx_success)
		VALUES ($1, 'confirmed', TRUE)
	`, hash)
	require.NoError(t, err)
}

func TestIntegration_ProcessPendingTransactions_TerminatedRail(t *testing.T) {
	// Block 940 is inside the rail's lockup (settled up to 900, lockup 50), so no default alert fires.
	// An end epoch of 900 equals settledUpTo: the rail is finalized.
	tests := []struct {
		name                string
		endEpoch            int64
		clientRequested     bool
		wantDeletionAllowed bool
	}{
		{
			name:     "terminated rail",
			endEpoch: 2000,
		},
		{
			name:     "finalized rail without a deletion request",
			endEpoch: 900,
		},
		{
			name:                "finalized rail unblocks a requested deletion",
			endEpoch:            900,
			clientRequested:     true,
			wantDeletionAllowed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newSettleWatcherClientWith(t, settleWatcherRail{block: 940, lockupPeriod: 50, endEpoch: tc.endEpoch, dataSetID: 2})
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			insertSettleWatcherDataSet(t, db)
			if tc.clientRequested {
				_, err = db.Exec(t.Context(), `
					INSERT INTO pdp_delete_data_set (id, client_requested_termination, termination_extra_data)
					VALUES (2, TRUE, '\x01')
				`)
				require.NoError(t, err)
			}

			alerts := &settleWatcherAlerts{}
			insertConfirmedSettle(t, db, common.HexToHash("0x01").Hex())
			require.NoError(t, processPendingTransactions(t.Context(), db, client, alerts))

			require.Empty(t, alerts.events, "the watcher does not report a terminated service")
			requireSettleWatcherOutcome(t, db, tc.clientRequested, tc.wantDeletionAllowed)
		})
	}
}

// settleWatcherFinalizedClient answers getRail(1) with the RailInactiveOrSettled
// revert that FilecoinPay returns once the final settlement has zeroed the rail.
type settleWatcherFinalizedClient struct {
	*settleWatcherClient
	getRail string
}

func (c *settleWatcherFinalizedClient) CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	if string(msg.Data) == c.getRail {
		return nil, errors.New("execution reverted: 0x" + filecoinpayment.ErrSelectorRailInactiveOrSettled)
	}
	return c.settleWatcherClient.CallContract(ctx, msg, block)
}

func newSettleWatcherFinalizedClient(t *testing.T, rail settleWatcherRail) *settleWatcherFinalizedClient {
	t.Helper()
	parsed, err := filecoinpayment.PaymentsMetaData.GetAbi()
	require.NoError(t, err)
	data, err := parsed.Pack("getRail", big.NewInt(1))
	require.NoError(t, err)
	return &settleWatcherFinalizedClient{settleWatcherClient: newSettleWatcherClientWith(t, rail), getRail: string(data)}
}

func TestIntegration_ProcessPendingTransactions_FinalizedRailRevert(t *testing.T) {
	tests := []struct {
		name      string
		requested bool
	}{
		{name: "nobody requested deletion"},
		{name: "requested deletion is unblocked", requested: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := newSettleWatcherFinalizedClient(t, settleWatcherRail{block: 940, lockupPeriod: 50, endEpoch: 900, dataSetID: 2})
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			insertSettleWatcherDataSet(t, db)
			if tc.requested {
				_, err = db.Exec(t.Context(), `
					INSERT INTO pdp_delete_data_set (id, operator_requested_deletion, after_terminate_service, service_termination_epoch)
					VALUES (2, TRUE, TRUE, 900)
				`)
				require.NoError(t, err)
			}

			alerts := &settleWatcherAlerts{}
			insertConfirmedSettle(t, db, common.HexToHash("0x01").Hex())
			require.NoError(t, processPendingTransactions(t.Context(), db, client, alerts))

			require.Empty(t, alerts.events)
			requireSettleWatcherOutcome(t, db, tc.requested, tc.requested)
		})
	}
}

// requireSettleWatcherOutcome checks that the settlement was processed and that
// data set 2 has a pipeline row only when its deletion was requested.
func requireSettleWatcherOutcome(t *testing.T, db *harmonydb.DB, requested, wantDeletionAllowed bool) {
	t.Helper()

	var tracked int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT COUNT(*) FROM filecoin_payment_transactions`).Scan(&tracked))
	require.Zero(t, tracked, "the settlement must be processed")

	var rows []struct {
		DeletionAllowed bool `db:"deletion_allowed"`
	}
	require.NoError(t, db.Select(t.Context(), &rows, `SELECT deletion_allowed FROM pdp_delete_data_set WHERE id = 2`))
	if !requested {
		require.Empty(t, rows, "the watcher must not queue a data set for deletion")
		return
	}
	require.Len(t, rows, 1)
	require.Equal(t, wantDeletionAllowed, rows[0].DeletionAllowed)
}

func TestIntegration_PipelineFinalizationElapsed(t *testing.T) {
	const current = 940
	tests := []struct {
		name          string
		row           bool
		requested     bool
		terminated    bool
		endEpoch      int64
		wantFinalized bool
		wantRequested bool
	}{
		{name: "no pipeline row"},
		{name: "requested, service not terminated yet", row: true, requested: true, wantRequested: true},
		{name: "requested, service end ahead", row: true, requested: true, terminated: true, endEpoch: current + 1, wantRequested: true},
		{name: "requested, service ended", row: true, requested: true, terminated: true, endEpoch: current, wantFinalized: true, wantRequested: true},
		{name: "unrequested row, service ended", row: true, terminated: true, endEpoch: current, wantFinalized: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			if tc.row {
				var endEpoch *int64
				if tc.terminated {
					endEpoch = &tc.endEpoch
				}
				_, err = db.Exec(t.Context(), `
					INSERT INTO pdp_delete_data_set (id, operator_requested_deletion, after_terminate_service, service_termination_epoch)
					VALUES (2, $1, $2, $3)
				`, tc.requested, tc.terminated, endEpoch)
				require.NoError(t, err)
			}

			var finalized, requested bool
			_, err = db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
				var err error
				finalized, requested, err = pipelineFinalizationElapsed(tx, 2, current)
				return false, err
			})
			require.NoError(t, err)
			require.Equal(t, tc.wantFinalized, finalized, "finalized")
			require.Equal(t, tc.wantRequested, requested, "requested")
		})
	}
}

func TestIntegration_VerifySettle_AlreadyProcessedSettleRaisesNoAlert(t *testing.T) {
	client := newSettleWatcherClientWith(t, settleWatcherRail{block: 1000, lockupPeriod: 50, dataSetID: 2})
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	s := insertSettleWatcherState(t, db, false, false)

	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(t.Context(), serviceAddr, client)
	require.NoError(t, err)
	view, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, client)
	require.NoError(t, err)

	// The second run finds the settle already removed, as another node would.
	alerts := &settleWatcherAlerts{}
	require.NoError(t, verifySettle(t.Context(), db, client, view, serviceAddr, s, alerts))
	require.NoError(t, verifySettle(t.Context(), db, client, view, serviceAddr, s, alerts))
	require.Len(t, alerts.events, 1)
}

func TestIntegration_VerifySettle_RetryRollsBackWithCleanup(t *testing.T) {
	for _, failCommit := range []bool{false, true} {
		name := "rerun request fails"
		if failCommit {
			name = "commit fails after rerun request"
		}
		t.Run(name, func(t *testing.T) {
			client := newSettleWatcherClient(t)
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			s := insertSettleWatcherState(t, db, true, false)
			_, err = db.Exec(t.Context(), `
				CREATE FUNCTION reject_settle_retry_test() RETURNS TRIGGER AS $$
				BEGIN
					RAISE EXCEPTION 'injected settlement write failure';
				END;
				$$ LANGUAGE plpgsql
			`)
			require.NoError(t, err)
			if failCommit {
				// Delay the failure until COMMIT so an update outside the transaction escapes rollback.
				_, err = db.Exec(t.Context(), `
					CREATE CONSTRAINT TRIGGER reject_settle_retry_test
					AFTER DELETE ON filecoin_payment_transactions
					DEFERRABLE INITIALLY DEFERRED
					FOR EACH ROW EXECUTE FUNCTION reject_settle_retry_test()
				`)
			} else {
				_, err = db.Exec(t.Context(), `
					CREATE TRIGGER reject_settle_retry_test
					BEFORE UPDATE ON harmony_task_singletons
					FOR EACH ROW EXECUTE FUNCTION reject_settle_retry_test()
				`)
			}
			require.NoError(t, err)

			serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
			viewAddr, err := contract.ResolveViewAddress(t.Context(), serviceAddr, client)
			require.NoError(t, err)
			view, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, client)
			require.NoError(t, err)
			alerts := &settleWatcherAlerts{}
			err = verifySettle(t.Context(), db, client, view, serviceAddr, s, alerts)
			require.ErrorContains(t, err, "injected settlement write failure")
			requireSettleWatcherState(t, db, s, true, false)

			if failCommit {
				_, err = db.Exec(t.Context(), `DROP TRIGGER reject_settle_retry_test ON filecoin_payment_transactions`)
			} else {
				_, err = db.Exec(t.Context(), `DROP TRIGGER reject_settle_retry_test ON harmony_task_singletons`)
			}
			require.NoError(t, err)
			require.NoError(t, verifySettle(t.Context(), db, client, view, serviceAddr, s, alerts))
			requireSettleWatcherState(t, db, s, false, true)
		})
	}
}
