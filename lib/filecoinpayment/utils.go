package filecoinpayment

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

var log = logging.Logger("filecoin-pay")

// SettleTargetResolver returns the highest epoch a rail should settle to.
// ok=false means the rail is valid but should not be settled in this pass.
// Implementation must ensure that PaymentsRailView is not mutated
type SettleTargetResolver func(ctx context.Context, railID *big.Int, rail PaymentsRailView, currentEpoch *big.Int) (target *big.Int, ok bool, err error)

// gasOverestimation is applied on top of the eth_estimateGas result when
// sending settleRail transactions, since it has been observed to fall short
// at execution time and burn the whole gas limit.
const gasOverestimation = 1.1

func SettleLockupPeriod(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, sender *message.SenderETH, from common.Address, payees []common.Address, resolvers map[common.Address]SettleTargetResolver, al curioalerting.AlertingInterface, system, subsystem string) error {
	paymentContractAddr, err := PaymentContractAddress()
	if err != nil {
		return fmt.Errorf("failed to get payment contract address: %w", err)
	}

	payment, err := NewPayments(paymentContractAddr, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to create payments: %w", err)
	}

	tokenAddress, err := contract.USDFCAddress()
	if err != nil {
		return xerrors.Errorf("failed to get USDFC address: %w", err)
	}

	var railIds []*big.Int

	current, err := ethClient.BlockNumber(ctx)
	if err != nil {
		return xerrors.Errorf("failed to get current block number: %w", err)
	}

	for _, payee := range payees {
		rails, err := payment.GetRailsForPayeeAndToken(&bind.CallOpts{Context: ctx}, payee, tokenAddress, big.NewInt(0), big.NewInt(0))
		if err != nil {
			return xerrors.Errorf("failed to get Rails for: %w", err)
		}

		for _, r := range rails.Results {
			if shouldInspectRailForSettlement(r) {
				railIds = append(railIds, r.RailId)
			}
		}

	}

	type toSettleRail struct {
		railId      *big.Int
		settledUpTo *big.Int
		target      *big.Int
	}

	var toSettle []toSettleRail
	currentEpoch := new(big.Int).SetUint64(current)
	for _, rail := range railIds {
		railID := new(big.Int).Set(rail)
		view, err := payment.GetRail(&bind.CallOpts{Context: ctx}, rail)
		if err != nil {
			return xerrors.Errorf("failed to get rail: %w", err)
		}

		resolver, ok := resolvers[view.Operator]
		if !ok {
			continue
		}

		if !railNeedsSettlement(view, current) {
			continue
		}

		target, ok, err := resolver(ctx, railID, view, new(big.Int).Set(currentEpoch))
		if err != nil {
			log.Errorw("failed to resolve settle target", "railID", railID.String(), "operator", view.Operator, "error", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    system,
				Subsystem: subsystem,
				Message:   fmt.Sprintf("failed to resolve settle target for rail %s: %s", railID.String(), err.Error()),
			})
			continue
		}
		if !ok || target == nil {
			continue
		}
		if target.Cmp(view.SettledUpTo) <= 0 && !IsTerminatedRailFinalizationTarget(view, target) {
			continue
		}

		toSettle = append(toSettle, toSettleRail{
			railId:      railID,
			settledUpTo: new(big.Int).Set(view.SettledUpTo),
			target:      new(big.Int).Set(target),
		})
	}

	log.Debugw("Total number of rails to be settled", "total", len(railIds), "rails", railIds)

	pabi, err := PaymentsMetaData.GetAbi()
	if err != nil {
		return xerrors.Errorf("failed to get Payments ABI: %w", err)
	}

	type settleRailTx struct {
		rail int64
		upTo int64
	}

	transactionsToSend := make(map[*types.Transaction]settleRailTx)
	for _, detail := range toSettle {
		settleUpTo, err := calculateSettleUpTo(ctx, pabi, &paymentContractAddr, ethClient, detail.railId, from, detail.settledUpTo, detail.target)
		if err != nil {
			if isSkippableSettleRailError(err) {
				log.Debugw("skipping settlement after non-actionable Pay settleRail revert", "railID", detail.railId.String(), "target", detail.target.String(), "error", err)
				continue
			}
			log.Errorf("failed to gas estimate settle transaction for rail %s: %s", detail.railId.String(), err.Error())
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    system,
				Subsystem: subsystem,
				Message:   fmt.Sprintf("failed to gas estimate settle transaction for rail %s: %s", detail.railId.String(), err.Error()),
			})
			continue
		}

		data, err := pabi.Pack("settleRail", detail.railId, big.NewInt(settleUpTo))
		if err != nil {
			return xerrors.Errorf("failed to pack data: %w", err)
		}

		// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
		txEth := types.NewTx(&types.LegacyTx{
			Nonce:    0,
			To:       &paymentContractAddr,
			Value:    big.NewInt(0),
			Gas:      0,
			GasPrice: nil,
			Data:     data,
		})

		transactionsToSend[txEth] = settleRailTx{
			rail: detail.railId.Int64(),
			upTo: settleUpTo,
		}
	}

	for txToSend, details := range transactionsToSend {
		txHash, err := sender.SendWithGasOverestimate(ctx, from, txToSend, "settleRail", gasOverestimation)
		if err != nil {
			if isSkippableSettleRailError(err) {
				log.Debugw("skipping settlement send after non-actionable Pay settleRail revert", "railID", details.rail, "settleUpTo", details.upTo, "error", err)
				continue
			}
			log.Errorw("failed to send settle transaction", "railIDs", details.rail, "settleUpTo", details.upTo, "error", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    system,
				Subsystem: subsystem,
				Message:   fmt.Sprintf("failed to send settle transaction for railID %d: %s", details.rail, err.Error()),
			})
			continue
		}

		txHashHex := strings.ToLower(txHash.Hex())
		log.Infow("sent settle transaction", "txHash", txHashHex, "railIDs", details.rail, "settleUpTo", details.upTo)

		// Insert into message_waits_eth and filecoin_payment_transactions atomically
		committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// Insert into message_waits_eth for confirmation tracking
			n, err := tx.Exec(`INSERT INTO message_waits_eth (signed_tx_hash, tx_status) VALUES ($1, 'pending')`, txHashHex)
			if err != nil {
				return false, xerrors.Errorf("failed to insert into message_waits_eth: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected to insert 1 row into message_waits_eth, inserted %d", n)
			}

			// Insert into filecoin_payment_transactions
			n, err = tx.Exec(`INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids) VALUES ($1, $2)`, txHashHex, []int64{details.rail})
			if err != nil {
				return false, xerrors.Errorf("failed to insert into filecoin_payment_transactions: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected to insert 1 row into filecoin_payment_transactions, inserted %d", n)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("failed to record settlement transaction: %w", err)
		}
		if !committed {
			return xerrors.Errorf("failed to commit settlement transaction record")
		}
	}

	return nil
}

func shouldInspectRailForSettlement(rail PaymentsRailInfo) bool {
	if !rail.IsTerminated {
		return true
	}

	// Terminated rails must stay visible until Filecoin Pay finalizes them.
	// The final settlement to endEpoch is what collects any remaining rate payment
	// from lockup and releases the rail's fixed lockup accounting.
	return rail.EndEpoch != nil && rail.EndEpoch.Sign() > 0
}

func railNeedsSettlement(rail PaymentsRailView, current uint64) bool {
	// settledUpTo is non-null on any valid Pay rail view; nil only protects
	// against malformed test data or a failed/manual construction path.
	if rail.SettledUpTo == nil {
		return false
	}

	if rail.EndEpoch != nil && rail.EndEpoch.Sign() > 0 {
		// The equality case can still need one final settleRail call to zero the
		// rail. Once finalized, getRail stops returning the rail.
		return rail.SettledUpTo.Cmp(rail.EndEpoch) <= 0
	}

	return activeRailSettlementDue(rail, current)
}

// IsTerminatedRailFinalizationTarget reports whether settling to target can be
// useful even without advancing settledUpTo. Filecoin Pay finalizes terminated
// rails in a final settleRail call; until GetRail returns RailInactiveOrSettled,
// a rail at settledUpTo == endEpoch may still need that call.
func IsTerminatedRailFinalizationTarget(rail PaymentsRailView, target *big.Int) bool {
	if rail.SettledUpTo == nil || rail.EndEpoch == nil || target == nil {
		return false
	}
	if rail.EndEpoch.Sign() <= 0 {
		return false
	}

	return rail.SettledUpTo.Cmp(rail.EndEpoch) == 0 && target.Cmp(rail.EndEpoch) == 0
}

func activeRailSettlementDue(rail PaymentsRailView, current uint64) bool {
	if rail.LockupPeriod == nil || rail.SettledUpTo == nil {
		return false
	}

	// Active rails are settled before the lockup period becomes the only
	// remaining guarantee. This protects the SP from a client withdrawing funds
	// after Filecoin Pay can no longer keep enough account lockup reserved.
	settleInterval := big.NewInt(builtin.EpochsInDay * 7)

	// Keep one day of lockup as a safety buffer. Once settlement is this close to
	// the lockup horizon, every pass should try to settle so the SP does not rely
	// on funds the client may soon be able to withdraw.
	gracedLockup := new(big.Int).Sub(rail.LockupPeriod, big.NewInt(builtin.EpochsInDay))
	if gracedLockup.Sign() <= 0 {
		return true
	}

	// Do not wait longer than the usable lockup window. The regular interval is a
	// batching optimization; the lockup period is the actual payment guarantee.
	if gracedLockup.Cmp(settleInterval) < 0 {
		settleInterval = gracedLockup
	}

	// settledUpTo is the last epoch Pay has accounted for on this rail. If the next
	// planned settlement point is behind the chain head, this rail needs a new
	// settle attempt.
	nextSettle := new(big.Int).Add(rail.SettledUpTo, settleInterval)
	return nextSettle.Cmp(new(big.Int).SetUint64(current)) < 0
}

// calculateSettleUpTo starts from the resolver-provided semantic target and
// only lowers it when gas estimation shows the settlement would run out of gas.
func calculateSettleUpTo(ctx context.Context, pabi *abi.ABI, paymentContractAddr *common.Address, ethClient ethchain.EthClient, id *big.Int, from common.Address, settledUpTo, targetEpoch *big.Int) (int64, error) {
	next := new(big.Int).Set(targetEpoch)
	for {
		data, err := pabi.Pack("settleRail", id, next)
		if err != nil {
			return 0, xerrors.Errorf("failed to pack data: %w", err)
		}

		_, err = ethClient.EstimateGas(ctx, ethereum.CallMsg{
			From:  from,
			To:    paymentContractAddr,
			Value: big.NewInt(0),
			Data:  data,
		})

		if err != nil {
			if isGasEstimateOutOfGas(err) {
				delta := big.NewInt(0).Sub(next, settledUpTo)
				halfDelta := big.NewInt(0).Div(delta, big.NewInt(2))
				next = big.NewInt(0).Add(settledUpTo, halfDelta)
				if next.Cmp(settledUpTo) <= 0 {
					return 0, xerrors.Errorf("failed to estimate gas: %w", err)
				}
				continue
			}
			return 0, xerrors.Errorf("failed to estimate gas: %w", err)
		}

		return next.Int64(), nil
	}
}

func isGasEstimateOutOfGas(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "call ran out of gas") ||
		strings.Contains(errStr, "out of gas (7)")
}
