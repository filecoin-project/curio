package filecoinpayment

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/multicall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

var log = logging.Logger("filecoin-pay")

func SettleLockupPeriod(ctx context.Context, db *harmonydb.DB, ethClient ethchain.EthClient, sender *message.SenderETH, from common.Address, payees []common.Address, operators []common.Address) error {
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

	var toSettle []*big.Int
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
				toSettle = append(toSettle, rail)
			}
		} else {
			// could be a constant, or a config variable this is the important tunable
			settleInterval := big.NewInt(builtin.EpochsInDay * 7)

			gracedLockup := new(big.Int).Sub(view.LockupPeriod, big.NewInt(builtin.EpochsInDay))
			if gracedLockup.Sign() <= 0 {
				// special case, lockup period is <= 1 day so settle every run
				// alternatively adjust the grace down from a day to something smaller
				toSettle = append(toSettle, rail)
				continue
			} else if gracedLockup.Cmp(settleInterval) < 0 {
				settleInterval = gracedLockup
			}

			nextSettle := new(big.Int).Add(view.SettledUpTo, settleInterval)
			if nextSettle.Uint64() < current {
				toSettle = append(toSettle, rail)
			}
		}
	}

	log.Debugw("Total number of rails to be settled", "total", len(railIds), "rails", railIds)

	pabi, err := PaymentsMetaData.GetAbi()
	if err != nil {
		return xerrors.Errorf("failed to get Payments ABI: %w", err)
	}

	// Batch in the 10s to settle rail
	calls := make([]multicall.Multicall3Call, 0, 10)
	rails := make([]int64, 0, 10)
	transactionsToSend := make(map[*types.Transaction][]int64)

	for _, id := range toSettle {
		data, err := pabi.Pack("settleRail", id, big.NewInt(int64(current)))
		if err != nil {
			return xerrors.Errorf("failed to pack data: %w", err)
		}
		calls = append(calls, multicall.Multicall3Call{
			Target:   paymentContractAddr,
			CallData: data,
		})
		rails = append(rails, id.Int64())
		if len(calls) >= 10 {
			tx, err := multicall.BatchCallGenerate(calls)
			if err != nil {
				return xerrors.Errorf("failed to generate batch call: %w", err)
			}
			transactionsToSend[tx] = rails
			calls = make([]multicall.Multicall3Call, 0, 10)
			rails = make([]int64, 0, 10)
		}
	}
	// Handle any remaining calls that didn't make a full batch
	if len(calls) > 0 {
		tx, err := multicall.BatchCallGenerate(calls)
		if err != nil {
			return xerrors.Errorf("failed to generate batch call: %w", err)
		}
		transactionsToSend[tx] = rails
	}

	for txToSend, railIDs := range transactionsToSend {
		txHash, err := sender.Send(ctx, from, txToSend, "settleRail")
		if err != nil {
			log.Errorw("failed to send settle transaction", "error", err)
			continue
		}

		txHashHex := strings.ToLower(txHash.Hex())
		log.Infow("sent settle transaction", "txHash", txHashHex, "railIDs", railIDs)

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
			n, err = tx.Exec(`INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids) VALUES ($1, $2)`, txHashHex, railIDs)
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
