package cuhelper

import (
	"context"
	"database/sql"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("cuhelper/settle")

// ProviderPaymentInfo holds the payment information for a provider
type ProviderPaymentInfo struct {
	ProviderID       int64
	LatestNonce      int64
	CumulativeAmount string
	Signature        []byte
}

// GetProviderPaymentInfo fetches the latest payment info for a provider
func GetProviderPaymentInfo(ctx context.Context, db *harmonydb.DB, providerID int64) (*ProviderPaymentInfo, error) {
	var info ProviderPaymentInfo
	info.ProviderID = providerID

	err := db.QueryRow(ctx, `
		SELECT payment_nonce, payment_cumulative_amount, payment_signature
		FROM proofshare_provider_payments
		WHERE provider_id = $1
		ORDER BY payment_nonce DESC
		LIMIT 1
	`, providerID).Scan(&info.LatestNonce, &info.CumulativeAmount, &info.Signature)

	if err != nil {
		if xerrors.Is(err, sql.ErrNoRows) {
			return nil, xerrors.Errorf("no payment records found for provider ID %d", providerID)
		}
		return nil, xerrors.Errorf("failed to query latest payment for provider ID %d: %w", providerID, err)
	}

	return &info, nil
}

// CheckIfAlreadySettled checks if the latest payment is already settled
func CheckIfAlreadySettled(ctx context.Context, db *harmonydb.DB, providerID int64, latestNonce int64) (bool, error) {
	var lastSettledNonce sql.NullInt64
	err := db.QueryRow(ctx, `
		SELECT MAX(payment_nonce)
		FROM proofshare_provider_payments_settlement
		WHERE provider_id = $1
	`, providerID).Scan(&lastSettledNonce)

	if err != nil && !xerrors.Is(err, sql.ErrNoRows) {
		return false, xerrors.Errorf("failed to query last settlement nonce for provider ID %d: %w", providerID, err)
	}

	if lastSettledNonce.Valid && latestNonce <= lastSettledNonce.Int64 {
		return true, nil
	}

	return false, nil
}

// SettleProvider performs the settlement for a provider
func SettleProvider(
	ctx context.Context,
	db *harmonydb.DB,
	chain api.FullNode,
	sendFunc func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error),
	providerID int64,
) (cid.Cid, error) {
	// 1. Create provider address
	providerAddr, err := address.NewIDAddress(uint64(providerID))
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create address from provider ID %d: %w", providerID, err)
	}

	// 2. Get payment info
	paymentInfo, err := GetProviderPaymentInfo(ctx, db, providerID)
	if err != nil {
		return cid.Undef, err
	}

	// 3. Check if already settled
	alreadySettled, err := CheckIfAlreadySettled(ctx, db, providerID, paymentInfo.LatestNonce)
	if err != nil {
		return cid.Undef, err
	}
	if alreadySettled {
		return cid.Undef, xerrors.Errorf("latest payment (nonce %d) for provider ID %d is already settled", paymentInfo.LatestNonce, providerID)
	}

	// 4. Parse cumulative amount
	cumulativeAmountBig, err := types.BigFromString(paymentInfo.CumulativeAmount)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to parse cumulative amount '%s' for provider ID %d, nonce %d: %w",
			paymentInfo.CumulativeAmount, providerID, paymentInfo.LatestNonce, err)
	}

	// 5. Call the service to redeem the voucher
	svc := common.NewServiceCustomSend(chain, sendFunc)

	log.Infow("calling ServiceRedeemProviderVoucher",
		"provider_id", providerID,
		"provider_address", providerAddr.String(),
		"cumulative_amount", cumulativeAmountBig.String(),
		"nonce", paymentInfo.LatestNonce)

	settleCid, err := svc.ServiceRedeemProviderVoucher(
		ctx,
		providerAddr,
		uint64(providerID),
		cumulativeAmountBig,
		uint64(paymentInfo.LatestNonce),
		paymentInfo.Signature,
	)
	if err != nil {
		return cid.Undef, xerrors.Errorf("ServiceRedeemProviderVoucher call for provider %s (ID %d), nonce %d failed: %w",
			providerAddr.String(), providerID, paymentInfo.LatestNonce, err)
	}

	log.Infow("Successfully initiated provider settlement",
		"provider_id", providerID,
		"provider_address", providerAddr.String(),
		"settled_nonce", paymentInfo.LatestNonce,
		"settled_cumulative_amount", cumulativeAmountBig.String(),
		"settlement_cid", settleCid.String())

	return settleCid, nil
}

// RecordSettlement records the settlement in the database
func RecordSettlement(ctx context.Context, tx *harmonydb.Tx, providerID int64, paymentNonce int64, settleCid cid.Cid) error {
	_, err := tx.Exec(`
		INSERT INTO proofshare_provider_payments_settlement (provider_id, payment_nonce, settled_at, settle_message_cid)
		VALUES ($1, $2, CURRENT_TIMESTAMP, $3)
	`, providerID, paymentNonce, settleCid.String())
	return err
}
