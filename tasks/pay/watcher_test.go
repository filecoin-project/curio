package pay

import (
	"context"
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
	blockCalls int
}

func (c *settleWatcherClient) BlockNumber(context.Context) (uint64, error) {
	c.blockCalls++
	return 1000, nil
}

func (c *settleWatcherClient) CallContract(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	c.t.Helper()
	response, ok := c.responses[string(msg.Data)]
	require.True(c.t, ok, "unexpected contract call: %x", msg.Data)
	return response, nil
}

func newSettleWatcherClient(t *testing.T) *settleWatcherClient {
	t.Helper()
	t.Setenv("CURIO_DEVNET_PAYMENTS_ADDRESS", "0x0000000000000000000000000000000000000001")
	t.Setenv("CURIO_DEVNET_PDP_VERIFIER_ADDRESS", "0x0000000000000000000000000000000000000002")
	t.Setenv("CURIO_DEVNET_FWSS_ADDRESS", "0x0000000000000000000000000000000000000003")

	c := &settleWatcherClient{t: t, responses: make(map[string][]byte)}
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
		LockupPeriod:      big.NewInt(30 * builtin.EpochsInDay),
		LockupFixed:       big.NewInt(0),
		SettledUpTo:       big.NewInt(900),
		EndEpoch:          big.NewInt(0),
		CommissionRateBps: big.NewInt(0),
	})
	addResponse(FWSS.FilecoinWarmStorageServiceStateViewMetaData, "railToDataSet", []any{big.NewInt(1)}, big.NewInt(1))
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
