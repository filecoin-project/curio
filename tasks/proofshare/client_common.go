package proofshare

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
)

// getClientRequest retrieves or creates a client request record
func getClientRequest(ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, sector abi.SectorID, requestPartitionCost int64) (*ClientRequest, error) {
	var clientRequest ClientRequest
	err := db.QueryRow(ctx, `
		SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done, request_partition_cost
		FROM proofshare_client_requests
		WHERE task_id = $1
	`, taskID).Scan(
		&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
		&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
		&clientRequest.RequestPartitionCost,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get client request: %w", err)
	}

	// If we don't have a client request yet, create one
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = db.Exec(ctx, `
			INSERT INTO proofshare_client_requests (task_id, sp_id, sector_num, request_partition_cost, created_at)
			VALUES ($1, $2, $3, $4, NOW())
		`, taskID, sector.Miner, sector.Number, requestPartitionCost)
		if err != nil {
			return nil, xerrors.Errorf("failed to create client request: %w", err)
		}

		// Reload the client request
		err = db.QueryRow(ctx, `
			SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done, request_partition_cost
			FROM proofshare_client_requests
			WHERE task_id = $1
		`, taskID).Scan(
			&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
			&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
			&clientRequest.RequestPartitionCost,
		)
		if err != nil {
			return nil, xerrors.Errorf("failed to reload client request: %w", err)
		}
	}

	return &clientRequest, nil
}


// createPayment creates a payment for the proof request
func createPayment(ctx context.Context, api ClientServiceAPI, db *harmonydb.DB, router *common.Service, taskID harmonytask.TaskID, sectorInfo abi.SectorID, requestPartitionCost int64) (bool, error) {
	log.Infow("createPayment start", "taskID", taskID, "spID", sectorInfo.Miner, "sectorNumber", sectorInfo.Number)
	// Get the wallet address from client settings for this SP ID
	var walletStr string
	err := db.QueryRow(ctx, `
		SELECT wallet FROM proofshare_client_settings 
		WHERE sp_id = $1 AND enabled = TRUE AND do_porep = TRUE
	`, sectorInfo.Miner).Scan(&walletStr)

	// If no specific settings for this SP ID, try the default (sp_id = 0)
	if errors.Is(err, pgx.ErrNoRows) {
		err = db.QueryRow(ctx, `
			SELECT wallet FROM proofshare_client_settings 
			WHERE sp_id = 0 AND enabled = TRUE AND do_porep = TRUE
		`).Scan(&walletStr)
	}

	if err != nil {
		return false, xerrors.Errorf("failed to get wallet from client settings: %w", err)
	}

	if walletStr == "" {
		return false, xerrors.Errorf("no wallet configured for SP ID %d", sectorInfo.Miner)
	}

	// Parse the wallet address
	wallet, err := address.NewFromString(walletStr)
	if err != nil {
		return false, xerrors.Errorf("failed to parse wallet address: %w", err)
	}

	// Get client ID from wallet address
	clientIDAddr, err := api.StateLookupID(ctx, wallet, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to lookup client ID: %w", err)
	}

	clientID, err := address.IDFromAddress(clientIDAddr)
	if err != nil {
		return false, xerrors.Errorf("failed to get client ID from address: %w", err)
	}

	// Check if the proof service is available
	// Exponential backoff if not available
	var available bool
	backoff := time.Second
	maxBackoff := 5 * time.Minute
	for {
		available, err = proofsvc.CheckAvailability()
		if err != nil {
			return false, xerrors.Errorf("failed to check proof service availability: %w", err)
		}
		if available {
			break
		}
		// Randomize backoff by ±30%
		var randVal [2]byte
		_, _ = rand.Read(randVal[:])
		// 0-65535 mapped to 0.0-1.0
		randFrac := float64(uint16(randVal[0])<<8|uint16(randVal[1])) / 65535.0
		mult := 0.7 + 0.6*randFrac // 0.7 to 1.3
		randomizedBackoff := time.Duration(float64(backoff) * mult)

		log.Infow("proof service not available, backing off", "backoff", randomizedBackoff)
		time.Sleep(randomizedBackoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	// Get current price for the proof
	price, err := proofsvc.GetCurrentPrice()
	if err != nil {
		return false, xerrors.Errorf("failed to get current price: %w", err)
	}

	price = big.Div(price, types.NanoFil)
	price = big.Div(big.Mul(price, big.NewInt(requestPartitionCost)), big.NewInt(10))
	price = big.Mul(price, types.NanoFil)

	// Create payment in a transaction
	var nextNonce int64
	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Check if there's an unconsumed payment
		var lastPayment struct {
			Wallet           int64  `db:"wallet"`
			Nonce            int64  `db:"nonce"`
			CumulativeAmount string `db:"cumulative_amount"`
			Consumed         bool   `db:"consumed"`
		}

		err = tx.QueryRow(`
			SELECT wallet, nonce, cumulative_amount, consumed
			FROM proofshare_client_payments
			WHERE wallet = $1
			ORDER BY nonce DESC
			LIMIT 1
		`, clientID).Scan(&lastPayment.Wallet, &lastPayment.Nonce, &lastPayment.CumulativeAmount, &lastPayment.Consumed)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, xerrors.Errorf("failed to check for unconsumed payments: %w", err)
		}

		// If there's an unconsumed payment, we need to wait for it to be consumed
		if err == nil && !lastPayment.Consumed {
			log.Infow("waiting for previous payment to be consumed",
				"wallet", lastPayment.Wallet,
				"nonce", lastPayment.Nonce)
			return false, nil
		}

		// Parse the cumulative amount
		cumulativeAmount := big.Zero()
		if err == nil {
			cumulativeAmount, err = types.BigFromString(lastPayment.CumulativeAmount)
			if err != nil {
				return false, xerrors.Errorf("failed to parse cumulative amount: %w", err)
			}
		}

		// Get the next nonce for this wallet
		err = tx.QueryRow(`
			SELECT COALESCE(MAX(nonce) + 1, 0)
			FROM proofshare_client_payments
			WHERE wallet = $1
		`, clientID).Scan(&nextNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to get next nonce: %w", err)
		}

		// calculate new cumulative amount
		cumulativeAmount = types.BigAdd(cumulativeAmount, price)

		// Create voucher
		voucher, err := router.CreateClientVoucher(ctx, uint64(clientID), cumulativeAmount.Int, uint64(nextNonce))
		if err != nil {
			return false, xerrors.Errorf("failed to create voucher: %w", err)
		}

		sig, err := api.WalletSign(ctx, wallet, voucher)
		if err != nil {
			return false, xerrors.Errorf("failed to sign voucher: %w", err)
		}

		// Insert the payment
		_, err = tx.Exec(`
			INSERT INTO proofshare_client_payments (wallet, nonce, cumulative_amount, signature, consumed)
			VALUES ($1, $2, $3, $4, FALSE)
		`, clientID, nextNonce, cumulativeAmount.String(), sig.Data)
		if err != nil {
			return false, xerrors.Errorf("failed to insert payment: %w", err)
		}

		// Update the client request with the payment info
		_, err = tx.Exec(`
			UPDATE proofshare_client_requests
			SET payment_wallet = $2, payment_nonce = $3
			WHERE task_id = $1
		`, taskID, clientID, nextNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to update client request with payment info: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, xerrors.Errorf("transaction failed: %w", err)
	}

	log.Infow("createPayment complete", "taskID", taskID, "wallet", clientID, "nonce", nextNonce, "price", price)
	return true, nil
}

// undoPayment unlocks the payment if the proof request failed
func undoPayment(ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, clientRequest *ClientRequest) error {
	// Get the payment status
	status, err := proofsvc.GetClientPaymentStatus(abi.ActorID(*clientRequest.PaymentWallet))
	if err != nil {
		return xerrors.Errorf("failed to get payment status: %w", err)
	}

	log.Infow("considering undoPayment", "taskID", taskID, "paymentWallet", clientRequest.PaymentWallet, "paymentNonce", clientRequest.PaymentNonce, "statusNonce", status.Nonce)

	// If the payment is not consumed, unlock it
	// Payment is consumed if the backend nonce is less than the client nonce
	if status.Nonce < *clientRequest.PaymentNonce || !status.Found {
		log.Warnw("undoing payment", "taskID", taskID, "paymentWallet", clientRequest.PaymentWallet, "paymentNonce", clientRequest.PaymentNonce)

		// Unlock the payment
		_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			// delete the payment
			_, err = tx.Exec(`
				DELETE FROM proofshare_client_payments
				WHERE wallet = $1 AND nonce = $2 AND consumed = FALSE
			`, clientRequest.PaymentWallet, *clientRequest.PaymentNonce)
			if err != nil {
				return false, xerrors.Errorf("failed to delete payment: %w", err)
			}

			// update request payment fields to NULL
			_, err = tx.Exec(`
				UPDATE proofshare_client_requests
				SET payment_wallet = NULL, payment_nonce = NULL
				WHERE task_id = $1
			`, taskID)
			if err != nil {
				return false, xerrors.Errorf("failed to update request payment fields: %w", err)
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("failed to unlock payment: %w", err)
		}
	}

	return nil
}


// sendRequest sends the proof request to the service
func sendRequest(ctx context.Context, api ClientServiceAPI, db *harmonydb.DB, taskID harmonytask.TaskID, clientRequest *ClientRequest) (bool, error) {
	log.Infow("sendRequest start", "taskID", taskID, "paymentWallet", clientRequest.PaymentWallet, "paymentNonce", clientRequest.PaymentNonce)
	// Get the payment details
	var payment struct {
		CumulativeAmount string `db:"cumulative_amount"`
		Signature        []byte `db:"signature"`
	}

	err := db.QueryRow(ctx, `
		SELECT cumulative_amount, signature
		FROM proofshare_client_payments
		WHERE wallet = $1 AND nonce = $2
	`, clientRequest.PaymentWallet, clientRequest.PaymentNonce).Scan(
		&payment.CumulativeAmount, &payment.Signature,
	)
	if err != nil {
		return false, xerrors.Errorf("failed to get payment details: %w", err)
	}

	// Parse the cumulative amount
	cumulativeAmount, err := types.BigFromString(payment.CumulativeAmount)
	if err != nil {
		return false, xerrors.Errorf("failed to parse cumulative amount: %w", err)
	}

	// Parse the request CID
	requestCid, err := cid.Parse(*clientRequest.RequestCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse request CID: %w", err)
	}

	// Get chain head for price epoch
	ts, err := api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	// Create the ProofRequest
	proofRequest := common.ProofRequest{
		Data: requestCid,

		PriceEpoch: int64(ts.Height()),

		PaymentClientID:         *clientRequest.PaymentWallet,
		PaymentNonce:            *clientRequest.PaymentNonce,
		PaymentCumulativeAmount: abi.NewTokenAmount(cumulativeAmount.Int64()),
		PaymentSignature:        payment.Signature,
	}

	// Submit the request with exponential backoff if service unavailable, capped at 1 minute
	var (
		maxRetries = 500
		baseDelay  = time.Second
		maxDelay   = time.Minute
	)
	for attempt := 0; attempt < maxRetries; attempt++ {
		backoff, err := proofsvc.RequestProof(proofRequest)
		if err == nil {
			break
		}
		// If backoff is true, service is unavailable, so retry with exponential backoff
		if backoff {
			delay := baseDelay * (1 << attempt)
			if delay > maxDelay {
				delay = maxDelay
			}
			log.Warnw("service unavailable, backing off", "attempt", attempt+1, "delay", delay, "taskID", taskID)
			time.Sleep(delay)
			continue
		}
		// Other errors: return immediately
		return false, xerrors.Errorf("failed to submit proof request: %w", err)
	}

	// Mark the payment as consumed
	_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		_, err = tx.Exec(`
			UPDATE proofshare_client_payments
			SET consumed = TRUE
			WHERE wallet = $1 AND nonce = $2
		`, clientRequest.PaymentWallet, clientRequest.PaymentNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to mark payment as consumed: %w", err)
		}

		// Mark the request as sent
		_, err = tx.Exec(`
			UPDATE proofshare_client_requests
			SET request_sent = TRUE
			WHERE task_id = $1
		`, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to mark request as sent: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("transaction failed: %w", err)
	}

	log.Infow("sendRequest complete", "taskID", taskID, "requestCID", clientRequest.RequestCID)
	return true, nil
}

// pollForProof polls for the proof status
func pollForProof(ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, clientRequest *ClientRequest, sectorInfo abi.SectorID) (bool, error) {
	log.Infow("pollForProof", "taskID", taskID, "requestCID", clientRequest.RequestCID)
	// Parse the request CID
	requestCid, err := cid.Parse(*clientRequest.RequestCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse request CID: %w", err)
	}

	// Get proof status by CID
	proofResp, err := proofsvc.GetProofStatus(requestCid)
	if err != nil || proofResp.Proof == nil {
		log.Infow("proof not ready", "taskID", taskID, "spID", sectorInfo.Miner, "sectorNumber", sectorInfo.Number)
		// Not ready yet, continue polling
		return false, nil
	}

	// We got a valid proof response, update the database
	_, err = db.Exec(ctx, `
		UPDATE proofshare_client_requests
		SET done = TRUE, response_data = $2, done_at = NOW()
		WHERE task_id = $1
	`, taskID, proofResp.Proof)
	if err != nil {
		return false, xerrors.Errorf("failed to update client request with proof: %w", err)
	}

	log.Infow("proof retrieved", "taskID", taskID, "spID", sectorInfo.Miner, "sectorNumber", sectorInfo.Number, "proofSize", len(proofResp.Proof))
	return true, nil
}
