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
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

var log = logging.Logger("filecoin-pay")

// gasOverestimation is applied on top of the eth_estimateGas result when
// sending settleRail transactions, since it has been observed to fall short
// at execution time and burn the whole gas limit.
const gasOverestimation = 1.1

func SettleLockupPeriod(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, sender *message.SenderETH, from common.Address, payees []common.Address, operators []common.Address, al curioalerting.AlertingInterface, system, subsystem string) error {
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
			if !r.IsTerminated {
				railIds = append(railIds, r.RailId)
			}

			// If rail's settlement deadline (endEpoch) passed less than 7 days ago, keep trying to settle it
			if uint64(r.EndEpoch.Int64()) > current-(builtin.EpochsInDay*7) {
				railIds = append(railIds, r.RailId)
			}
		}

	}

	type toSettleRail struct {
		railId      *big.Int
		settledUpTo *big.Int
	}

	var toSettle []toSettleRail
	for _, rail := range railIds {
		view, err := payment.GetRail(&bind.CallOpts{Context: ctx}, rail)
		if err != nil {
			return xerrors.Errorf("failed to get rail: %w", err)
		}

		// Let's only settle rail if it's our operator i.e., PDP related operator
		if !lo.Contains(operators, view.Operator) {
			continue
		}

		// If rail is terminated but not settled yet, settle it
		if view.EndEpoch.Int64() > 0 {
			if view.SettledUpTo.Cmp(view.EndEpoch) == -1 {
				toSettle = append(toSettle, toSettleRail{
					railId:      rail,
					settledUpTo: view.SettledUpTo,
				})
			}
		} else {
			// could be a constant, or a config variable this is the important tunable
			settleInterval := big.NewInt(builtin.EpochsInDay * 7)

			gracedLockup := new(big.Int).Sub(view.LockupPeriod, big.NewInt(builtin.EpochsInDay))
			if gracedLockup.Sign() <= 0 {
				// special case, lockup period is <= 1 day so settle every run
				// alternatively adjust the grace down from a day to something smaller
				toSettle = append(toSettle, toSettleRail{
					railId:      rail,
					settledUpTo: view.SettledUpTo,
				})
				continue
			} else if gracedLockup.Cmp(settleInterval) < 0 {
				settleInterval = gracedLockup
			}

			nextSettle := new(big.Int).Add(view.SettledUpTo, settleInterval)
			if nextSettle.Uint64() < current {
				toSettle = append(toSettle, toSettleRail{
					settledUpTo: view.SettledUpTo,
					railId:      rail})
			}
		}
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
		settleUpTo, err := calculateSettleUpTo(ctx, pabi, &paymentContractAddr, ethClient, detail.railId, from, detail.settledUpTo, big.NewInt(int64(current)))
		if err != nil {
			log.Errorf("failed to gas estimate settle transaction: %s", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    system,
				Subsystem: subsystem,
				Message:   fmt.Sprintf("failed to gas estimate settle transaction: %s", err.Error()),
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
			log.Errorw("failed to send settle transaction", "railIDs", details.rail, "settleUpTo", details.upTo, "error", err)
			_ = al.EmitEvent(ctx, curioalerting.AlertEvent{
				System:    system,
				Subsystem: subsystem,
				Message:   fmt.Sprintf("failed to send settle transaction: %s", err.Error()),
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

func calculateSettleUpTo(ctx context.Context, pabi *abi.ABI, paymentContractAddr *common.Address, ethClient ethchain.EthClient, id *big.Int, from common.Address, settledUpTo, currentEpoch *big.Int) (int64, error) {
	next := currentEpoch
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
