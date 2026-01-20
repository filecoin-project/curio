package proofshare

import (
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/proofsvc/cuhelper"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var AutosettleInterval = 2 * time.Minute

const SettleGas = 50_000_000
const WithdrawableFeeMult = 1 / 0.002 // max 0.2% network gas fee

func NewTaskAutosettle(db *harmonydb.DB, chain api.FullNode, sender *message.Sender) *TaskAutosettle {
	return &TaskAutosettle{db: db, chain: chain, sender: sender}
}

type TaskAutosettle struct {
	db     *harmonydb.DB
	chain  api.FullNode
	sender *message.Sender
}

func (t *TaskAutosettle) Adder(harmonytask.AddTaskFunc) {
}

func (t *TaskAutosettle) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *TaskAutosettle) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// 1. check that autosettle is enabled
	var enabled bool
	err = t.db.QueryRow(ctx, `SELECT autosettle FROM proofshare_meta LIMIT 1`).Scan(&enabled)
	if err != nil {
		return false, xerrors.Errorf("failed to check autosettle: %w", err)
	}

	if !enabled {
		return true, nil
	}

	// 2. calculate settle gas fee and minimum withdrawable balance
	head, err := t.chain.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	basefee := head.MinTicketBlock().ParentBaseFee
	networkFee := big.Mul(basefee, big.NewInt(SettleGas))
	// overestimate by 1.2x
	networkFee = big.Mul(networkFee, big.NewInt(120))
	networkFee = big.Div(networkFee, big.NewInt(100))

	minWithdrawableBalance := big.Mul(networkFee, big.NewInt(WithdrawableFeeMult))

	// 3. Get unsettled fil for all providers
	type ProviderUnsettled struct {
		ProviderID      int64
		UnsettledAmount big.Int
		LatestNonce     int64
	}

	var providersToSettle []ProviderUnsettled

	// Query inspired by PSProviderLastPaymentsSummary
	rows, err := t.db.Query(ctx, `
WITH MaxNonces AS (
    SELECT
        provider_id,
        MAX(payment_nonce) AS max_payment_nonce
    FROM proofshare_provider_payments
    GROUP BY provider_id
),
LatestPayments AS (
    SELECT
        p.provider_id,
        p.payment_nonce AS last_payment_nonce,
        p.payment_cumulative_amount AS latest_payment_value
    FROM proofshare_provider_payments p
    INNER JOIN MaxNonces mn ON p.provider_id = mn.provider_id AND p.payment_nonce = mn.max_payment_nonce
),
MaxSettledNonces AS (
    SELECT
        provider_id,
        MAX(payment_nonce) AS max_settled_nonce
    FROM proofshare_provider_payments_settlement
    GROUP BY provider_id
),
LatestSettlements AS (
    SELECT
        s.provider_id,
        s.payment_nonce AS last_settled_nonce,
        p.payment_cumulative_amount AS last_settled_payment_value
    FROM proofshare_provider_payments_settlement s
    INNER JOIN MaxSettledNonces msn ON s.provider_id = msn.provider_id AND s.payment_nonce = msn.max_settled_nonce
    INNER JOIN proofshare_provider_payments p ON s.provider_id = p.provider_id AND p.payment_nonce = msn.max_settled_nonce
)
SELECT
    lp.provider_id,
    lp.last_payment_nonce,
    lp.latest_payment_value,
    COALESCE(ls.last_settled_payment_value, '0') AS last_settled_payment_value
FROM LatestPayments lp
LEFT JOIN LatestSettlements ls ON lp.provider_id = ls.provider_id
WHERE lp.latest_payment_value != COALESCE(ls.last_settled_payment_value, '0')`)
	if err != nil {
		return false, xerrors.Errorf("failed to query unsettled providers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var providerID, latestNonce int64
		var latestPaymentStr, lastSettledPaymentStr string

		err := rows.Scan(&providerID, &latestNonce, &latestPaymentStr, &lastSettledPaymentStr)
		if err != nil {
			return false, xerrors.Errorf("failed to scan row: %w", err)
		}

		latestPaymentBig, err := types.BigFromString(latestPaymentStr)
		if err != nil {
			return false, xerrors.Errorf("failed to parse latest payment amount: %w", err)
		}

		lastSettledBig, err := types.BigFromString(lastSettledPaymentStr)
		if err != nil {
			return false, xerrors.Errorf("failed to parse last settled payment amount: %w", err)
		}

		unsettledAmount := big.Sub(latestPaymentBig, lastSettledBig)

		// Only consider positive unsettled amounts
		if unsettledAmount.GreaterThan(big.Zero()) {
			providersToSettle = append(providersToSettle, ProviderUnsettled{
				ProviderID:      providerID,
				UnsettledAmount: unsettledAmount,
				LatestNonce:     latestNonce,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return false, xerrors.Errorf("failed to iterate rows: %w", err)
	}

	// 4. Filter providers with unsettled balance > minWithdrawableBalance
	var providersToSettleFiltered []ProviderUnsettled
	for _, provider := range providersToSettle {
		if provider.UnsettledAmount.GreaterThan(minWithdrawableBalance) {
			providersToSettleFiltered = append(providersToSettleFiltered, provider)
			log.Infow("provider eligible for auto-settlement",
				"provider_id", provider.ProviderID,
				"unsettled_amount", types.FIL(provider.UnsettledAmount).Short(),
				"min_withdrawable", types.FIL(minWithdrawableBalance).Short())
		}
	}

	if len(providersToSettleFiltered) == 0 {
		log.Debugw("no providers eligible for auto-settlement",
			"total_unsettled_providers", len(providersToSettle),
			"min_withdrawable_balance", types.FIL(minWithdrawableBalance).Short())
		return true, nil
	}

	// 5. Trigger settlements for eligible providers
	settledCount := 0
	for _, provider := range providersToSettleFiltered {
		if !stillOwned() {
			log.Warnw("task no longer owned, stopping settlements",
				"settled_so_far", settledCount,
				"remaining", len(providersToSettleFiltered)-settledCount)
			return false, nil
		}

		log.Infow("settling provider",
			"provider_id", provider.ProviderID,
			"unsettled_amount", types.FIL(provider.UnsettledAmount).Short())

		// Use the common settlement function
		settleCid, err := cuhelper.SettleProvider(ctx, t.db, t.chain,
			func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
				return t.sender.Send(ctx, msg, mss, "ps-provider-settle")
			},
			provider.ProviderID)

		if err != nil {
			log.Errorw("failed to settle provider",
				"provider_id", provider.ProviderID,
				"error", err)
			// Continue with other providers
			continue
		}

		// Track the settlement
		err = t.trackSettlement(ctx, provider.ProviderID, provider.LatestNonce, settleCid)
		if err != nil {
			log.Errorw("failed to track settlement",
				"provider_id", provider.ProviderID,
				"settlement_cid", settleCid.String(),
				"error", err)
			// Settlement was sent but tracking failed - this is not fatal
		}

		settledCount++
		log.Infow("provider settled",
			"provider_id", provider.ProviderID,
			"settlement_cid", settleCid.String(),
			"settled_count", settledCount,
			"total_to_settle", len(providersToSettleFiltered))
	}

	log.Infow("auto-settlement complete",
		"settled_count", settledCount,
		"total_eligible", len(providersToSettleFiltered))

	return true, nil
}

func (t *TaskAutosettle) trackSettlement(ctx context.Context, providerID int64, paymentNonce int64, settleCid cid.Cid) error {
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Insert into message_waits for tracking
		_, err := tx.Exec(`
			INSERT INTO message_waits (signed_message_cid)
			VALUES ($1)
		`, settleCid)
		if err != nil {
			return false, xerrors.Errorf("failed to insert message_waits: %w", err)
		}

		// Insert into provider messages tracking
		_, err = tx.Exec(`
			INSERT INTO proofshare_provider_messages (signed_cid, wallet, action)
			VALUES ($1, $2, $3)
		`, settleCid, providerID, "autosettle")
		if err != nil {
			return false, xerrors.Errorf("failed to insert proofshare_provider_messages: %w", err)
		}

		// Record the settlement
		err = cuhelper.RecordSettlement(ctx, tx, providerID, paymentNonce, settleCid)
		if err != nil {
			return false, xerrors.Errorf("failed to record settlement: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	return err
}

// TypeDetails implements harmonytask.TaskInterface.
func (t *TaskAutosettle) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "PSAutoSettle",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(AutosettleInterval, t),
	}
}

var _ = harmonytask.Reg(&TaskAutosettle{})
