package pay

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
)

type pendingSettleClient struct {
	*settleWatcherClient
	railMethods map[string]int64
	allowed     map[int64]bool
	railCalls   []int64
}

func (c *pendingSettleClient) CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	c.t.Helper()
	if railID, ok := c.railMethods[string(msg.Data)]; ok {
		require.True(c.t, c.allowed[railID], "tracked rail %d must be skipped before GetRail", railID)
		c.railCalls = append(c.railCalls, railID)
	}
	return c.settleWatcherClient.CallContract(ctx, msg, block)
}

func (c *pendingSettleClient) EstimateGas(context.Context, ethereum.CallMsg) (uint64, error) {
	c.t.Fatal("settlement must not estimate gas for tracked or resolver-deferred rails")
	return 0, nil
}

func TestIntegration_SettleLockupPeriod_SkipsOutstandingRailsUntilCleanup(t *testing.T) {
	for _, tt := range []struct {
		name  string
		retry bool
	}{
		{name: "full target pending"},
		{name: "partial target pending", retry: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &pendingSettleClient{
				settleWatcherClient: newSettleWatcherClient(t),
				railMethods:         make(map[string]int64),
				allowed:             map[int64]bool{3: true},
			}
			t.Setenv("CURIO_DEVNET_USDFC_ADDRESS", "0x0000000000000000000000000000000000000005")
			token, err := contract.USDFCAddress()
			require.NoError(t, err)
			payee := common.HexToAddress("0x06")
			operator := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
			paymentABI, err := filecoinpayment.PaymentsMetaData.GetAbi()
			require.NoError(t, err)
			listCall, err := paymentABI.Pack("getRailsForPayeeAndToken", payee, token, big.NewInt(0), big.NewInt(0))
			require.NoError(t, err)
			listResponse, err := paymentABI.Methods["getRailsForPayeeAndToken"].Outputs.Pack([]filecoinpayment.PaymentsRailInfo{
				{RailId: big.NewInt(1), EndEpoch: big.NewInt(0)},
				{RailId: big.NewInt(2), IsTerminated: true, EndEpoch: big.NewInt(900)},
				{RailId: big.NewInt(3), EndEpoch: big.NewInt(0)},
			}, big.NewInt(0), big.NewInt(3))
			require.NoError(t, err)
			client.responses[string(listCall)] = listResponse
			for _, railID := range []int64{1, 2, 3} {
				view := filecoinpayment.PaymentsRailView{
					Operator: operator, Validator: operator,
					PaymentRate: big.NewInt(1), LockupPeriod: big.NewInt(builtin.EpochsInDay), LockupFixed: big.NewInt(0),
					SettledUpTo: big.NewInt(800), EndEpoch: big.NewInt(0), CommissionRateBps: big.NewInt(0),
				}
				if railID == 2 {
					view.EndEpoch = big.NewInt(900)
				}
				call, err := paymentABI.Pack("getRail", big.NewInt(railID))
				require.NoError(t, err)
				response, err := paymentABI.Methods["getRail"].Outputs.Pack(view)
				require.NoError(t, err)
				client.railMethods[string(call)] = railID
				client.responses[string(call)] = response
			}

			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			hash := common.HexToHash("0x01").Hex()
			_, err = db.Exec(t.Context(), `
				INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids, retry)
				VALUES ($1, $2, $3)
			`, hash, []int64{1, 2}, tt.retry)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')`, hash)
			require.NoError(t, err)

			var resolved []int64
			resolvers := map[common.Address]filecoinpayment.SettleTargetResolver{
				operator: func(_ context.Context, railID *big.Int, _ filecoinpayment.PaymentsRailView, _ *big.Int) (*big.Int, bool, error) {
					resolved = append(resolved, railID.Int64())
					return nil, false, nil
				},
			}
			alerts := &settleWatcherAlerts{}
			require.NoError(t, filecoinpayment.SettleLockupPeriod(t.Context(), db, client, nil, operator, []common.Address{payee}, resolvers, alerts, alertType, alertName))
			require.Equal(t, []int64{3}, client.railCalls)
			require.Equal(t, []int64{3}, resolved)

			var trackedRails []int64
			var retry bool
			err = db.QueryRow(t.Context(), `SELECT rail_ids, retry FROM filecoin_payment_transactions WHERE tx_hash = $1`, hash).Scan(&trackedRails, &retry)
			require.NoError(t, err)
			require.Equal(t, []int64{1, 2}, trackedRails)
			require.Equal(t, tt.retry, retry)

			_, err = db.Exec(t.Context(), `UPDATE message_waits_eth SET tx_status = 'confirmed', tx_success = TRUE WHERE signed_tx_hash = $1`, hash)
			require.NoError(t, err)
			client.railCalls, resolved = nil, nil
			require.NoError(t, filecoinpayment.SettleLockupPeriod(t.Context(), db, client, nil, operator, []common.Address{payee}, resolvers, alerts, alertType, alertName))
			require.Equal(t, []int64{3}, client.railCalls, "confirmation alone must not bypass watcher cleanup")
			require.Equal(t, []int64{3}, resolved)

			_, err = db.Exec(t.Context(), `DELETE FROM filecoin_payment_transactions WHERE tx_hash = $1`, hash)
			require.NoError(t, err)
			client.allowed[1], client.allowed[2] = true, true
			client.railCalls, resolved = nil, nil
			require.NoError(t, filecoinpayment.SettleLockupPeriod(t.Context(), db, client, nil, operator, []common.Address{payee}, resolvers, alerts, alertType, alertName))
			require.Equal(t, []int64{1, 2, 3}, client.railCalls)
			require.Equal(t, []int64{1, 2, 3}, resolved)
			require.Empty(t, alerts.events)
		})
	}
}
