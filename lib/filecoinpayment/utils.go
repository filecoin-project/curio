package filecoinpayment

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/multicall"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
)

var log = logging.Logger("filecoin-pay")

func SettleLockupPeriod(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH, from common.Address, payees []common.Address, operators []common.Address) error {
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

			// If rail terminated in the last 30 days, we should consider it for settlement
			if uint64(r.EndEpoch.Int64()) > current-(builtin.EpochsInDay*30) {
				railIds = append(railIds, r.RailId)
			}
		}

	}

	var toSettle []*big.Int
	bufferPeriod := big.NewInt(2 * builtin.EpochsInDay)
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
			//If rail is not terminated, settle if SettledUpTo+LockupPeriod-2days > current block
			threshold := big.NewInt(0).Add(view.SettledUpTo, view.LockupPeriod)
			thresholdWithGrace := big.NewInt(0).Sub(threshold, bufferPeriod)

			if thresholdWithGrace.Uint64() < current {
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

	for txToSend, railIDs := range transactionsToSend {
		tx, err := sender.Send(ctx, from, txToSend, "settleRail")
		if err != nil {
			return xerrors.Errorf("failed to send transaction: %w", err)
		}
		log.Infow("sent the settle transaction with hash %s", tx.Hex())
		n, err := db.Exec(ctx, `INSERT INTO filecoin_payment_transactions (tx_hash, rail_ids, settled_at) VALUES ($1, $2, $3)`, tx.Hex(), railIDs, current)
		if err != nil {
			return xerrors.Errorf("failed to insert into filecoin_payment_transactions: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("expected to insert 1 row, inserted %d", n)
		}
	}

	return nil
}
