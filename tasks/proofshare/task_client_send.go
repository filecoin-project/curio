package proofshare

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
)

var (
	psBuckets  = []float64{0.05, 0.2, 0.5, 1, 5, 15, 45} // seconds
	psDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "curio_psvc_proofshare_duration_seconds",
		Help:    "Duration of proofshare client_common operations",
		Buckets: psBuckets,
	}, []string{"call"})

	retryBuckets  = []float64{0, 1, 3, 8, 20, 50, 200}
	psRetryCounts = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "curio_psvc_proofshare_retry_count",
		Help:    "Retry count per call in proofshare client_common operations",
		Buckets: retryBuckets,
	}, []string{"call"})
)

func init() {
	_ = prometheus.Register(psDuration)
	_ = prometheus.Register(psRetryCounts)
}

func recordPSDuration(call string, start time.Time) {
	psDuration.WithLabelValues(call).Observe(time.Since(start).Seconds())
}

func recordPSRetries(call string, retries int) {
	psRetryCounts.WithLabelValues(call).Observe(float64(retries))
}

type TaskClientSend struct {
	db     *harmonydb.DB
	api    ClientServiceAPI
	router *common.Service
}

func NewTaskClientSend(db *harmonydb.DB, api ClientServiceAPI, router *common.Service) *TaskClientSend {
	return &TaskClientSend{db: db, api: api, router: router}
}

// Adder implements harmonytask.TaskInterface.
func (t *TaskClientSend) Adder(atf harmonytask.AddTaskFunc) {
	// generally as background task TaskSendClient is pretty static, but we want nodes to stay on top of new wallets
	// being brought into the system
	go func() {
		for range time.NewTicker(3 * time.Minute).C {
			atf(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// If there is a sender entry with null task_id, set it to the current task id
				n, err := tx.Exec("UPDATE proofshare_client_sender SET task_id = $1, updated_at = current_timestamp WHERE task_id IS NULL", id)
				if err != nil {
					return false, err
				}

				return n > 0, nil
			})
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface.
func (t *TaskClientSend) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

type Payment struct {
	Wallet           int64  `db:"wallet"`
	Nonce            int64  `db:"nonce"`
	Consumed         bool   `db:"consumed"`
	CumulativeAmount string `db:"cumulative_amount"`
	Signature        []byte `db:"signature"`
}

// Do implements harmonytask.TaskInterface.
func (t *TaskClientSend) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// get our wallet id
	var walletID int64
	err = t.db.QueryRow(ctx, `
		SELECT wallet_id
		FROM proofshare_client_sender
		WHERE task_id = $1
	`, taskID).Scan(&walletID)
	if err != nil {
		return false, xerrors.Errorf("failed to get wallet id: %w", err)
	}

	// background task
	for {
		if !stillOwned() {
			return false, nil
		}

		// 1. Pick a task
		// 1.1. See if there was one in progress

		// Check if there's an unconsumed payment
		var lastPayment Payment

		err = t.db.QueryRow(ctx, `
			SELECT wallet, nonce, consumed, cumulative_amount, signature
			FROM proofshare_client_payments
			WHERE wallet = $1
			ORDER BY nonce DESC
			LIMIT 1
		`, walletID).Scan(&lastPayment.Wallet, &lastPayment.Nonce, &lastPayment.Consumed, &lastPayment.CumulativeAmount, &lastPayment.Signature)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, xerrors.Errorf("failed to check for unconsumed payments: %w", err)
		}

		if lastPayment.Consumed {
			// it was, create a new payment
			// Note: Payment Consumed == it was sent and is now being processed in the market
			match, err := t.doCreatePayment(ctx, taskID, walletID)
			if err != nil {
				return false, xerrors.Errorf("failed to create payment: %w", err)
			}
			if !match {
				log.Infow("no applicable requests found", "taskID", taskID, "wallet", walletID)
				time.Sleep(time.Duration(100+rand.Intn(150)) * time.Second / 100)
				continue
			}

			// update the last payment
			err = t.db.QueryRow(ctx, `
				SELECT wallet, nonce, consumed, cumulative_amount, signature
				FROM proofshare_client_payments
				WHERE wallet = $1
				ORDER BY nonce DESC
				LIMIT 1
			`, walletID).Scan(&lastPayment.Wallet, &lastPayment.Nonce, &lastPayment.Consumed, &lastPayment.CumulativeAmount, &lastPayment.Signature)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return false, xerrors.Errorf("failed to check for unconsumed payments: %w", err)
			}
		}

		// else it wasn't consumed, but was not sent? Try to send again (service is idempotent)
		// in any case load the request again and send it
		// (also here if a new payment was created)

		match, err := t.doSendRequest(ctx, taskID, lastPayment)
		if err != nil {
			log.Errorw("failed to send request", "taskID", taskID, "wallet", walletID, "error", err)
			t.undoPayment(ctx, taskID, lastPayment)
			continue
		}
		if !match {
			log.Infow("no applicable requests found", "taskID", taskID, "wallet", walletID)
			time.Sleep(time.Duration(100+rand.Intn(150)) * time.Second / 100)
			continue
		}

		//time.Sleep(time.Duration(50+rand.Intn(150)) * time.Second / 100)
	}
}

func (t *TaskClientSend) undoPayment(ctx context.Context, taskID harmonytask.TaskID, payment Payment) {
	// Get the payment status
	status, err := proofsvc.GetClientPaymentStatus(abi.ActorID(payment.Wallet))
	if err != nil {
		log.Errorw("failed to get payment status", "taskID", taskID, "paymentWallet", payment.Wallet, "paymentNonce", payment.Nonce, "error", err)
		return
	}

	log.Infow("considering undoPayment", "taskID", taskID, "paymentWallet", payment.Wallet, "paymentNonce", payment.Nonce, "statusNonce", status.Nonce)

	// If the payment is not consumed, unlock it
	// Payment is consumed if the backend nonce is less than the client nonce
	if status.Nonce < payment.Nonce || !status.Found {
		log.Warnw("undoing payment", "taskID", taskID, "paymentWallet", payment.Wallet, "paymentNonce", payment.Nonce)

		// Unlock the payment
		_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			// delete the payment
			_, err = tx.Exec(`
				DELETE FROM proofshare_client_payments
				WHERE wallet = $1 AND nonce = $2 AND consumed = FALSE
			`, payment.Wallet, payment.Nonce)
			if err != nil {
				return false, xerrors.Errorf("failed to delete payment: %w", err)
			}

			// update request payment fields to NULL
			_, err = tx.Exec(`
				UPDATE proofshare_client_requests
				SET payment_wallet = NULL, payment_nonce = NULL
				WHERE payment_wallet = $1 AND payment_nonce = $2
			`, payment.Wallet, payment.Nonce)
			if err != nil {
				return false, xerrors.Errorf("failed to update request payment fields: %w", err)
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			log.Errorw("failed to unlock payment", "taskID", taskID, "paymentWallet", payment.Wallet, "paymentNonce", payment.Nonce, "error", err)
			return
		}

		log.Infow("payment undone", "taskID", taskID, "paymentWallet", payment.Wallet, "paymentNonce", payment.Nonce)
	}
}

func (t *TaskClientSend) doSendRequest(ctx context.Context, taskID harmonytask.TaskID, payment Payment) (match bool, err error) {
	start := time.Now()
	var retries int
	defer func() {
		recordPSDuration("sendRequest", start)
		recordPSRetries("sendRequest", retries)
	}()
	log.Infow("sendRequest start", "taskID", taskID, "paymentWallet", payment.Wallet, "paymentNonce", payment.Nonce)

	// Find requestCID
	var rcid string
	err = t.db.QueryRow(ctx, `
		SELECT request_cid
		FROM proofshare_client_requests
		WHERE payment_wallet = $1 AND payment_nonce = $2
	`, payment.Wallet, payment.Nonce).Scan(&rcid)
	if err != nil {
		return false, xerrors.Errorf("failed to get request CID: %w", err)
	}

	// Parse the request CID
	requestCid, err := cid.Parse(rcid)
	if err != nil {
		return false, xerrors.Errorf("failed to parse request CID: %w", err)
	}

	// Parse the cumulative amount
	cumulativeAmount, err := types.BigFromString(payment.CumulativeAmount)
	if err != nil {
		return false, xerrors.Errorf("failed to parse cumulative amount: %w", err)
	}

	// Create the ProofRequest
	proofRequest := common.ProofRequest{
		Data: requestCid,

		PaymentClientID:         payment.Wallet,
		PaymentNonce:            payment.Nonce,
		PaymentCumulativeAmount: cumulativeAmount,
		PaymentSignature:        payment.Signature,
	}

	// Submit the request with exponential backoff if service unavailable, capped at 1 minute
	var (
		maxRetries = 500
		baseDelay  = time.Second
		maxDelay   = 3 * time.Second
	)
	var attempt int
	for attempt = 0; attempt < maxRetries; attempt++ {
		backoff, err := proofsvc.RequestProof(proofRequest)
		if err == nil {
			retries = attempt
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
	// if loop exited due to max retries without success, count retries as maxRetries
	if retries == 0 && maxRetries > 0 && attempt == maxRetries {
		retries = maxRetries
	}

	// Mark the payment as consumed
	_, err = t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		_, err = tx.Exec(`
			UPDATE proofshare_client_payments
			SET consumed = TRUE
			WHERE wallet = $1 AND nonce = $2
		`, payment.Wallet, payment.Nonce)
		if err != nil {
			return false, xerrors.Errorf("failed to mark payment as consumed: %w", err)
		}

		// Mark the request as sent
		_, err = tx.Exec(`
			UPDATE proofshare_client_requests
			SET request_sent = TRUE
			WHERE payment_wallet = $1 AND payment_nonce = $2
		`, payment.Wallet, payment.Nonce)
		if err != nil {
			return false, xerrors.Errorf("failed to mark request as sent: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("transaction failed: %w", err)
	}

	log.Infow("sendRequest complete", "taskID", taskID, "requestCID", rcid)
	return true, nil
}

func (t *TaskClientSend) doCreatePayment(ctx context.Context, taskID harmonytask.TaskID, walletID int64) (match bool, err error) {
	log.Infow("creating new payment", "taskID", taskID, "wallet", walletID)

	clientID, err := address.NewIDAddress(uint64(walletID))
	if err != nil {
		return false, xerrors.Errorf("failed to get client ID from address: %w", err)
	}

	addr, err := t.api.StateAccountKey(ctx, clientID, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to get account key: %w", err)
	}

	spIDs, maxPerProofPrices, err := t.getApplicableSPIDs(ctx, addr)
	if err != nil {
		return false, xerrors.Errorf("failed to get applicable SPIDs: %w", err)
	}

	var request struct {
		SpID                 int64  `db:"sp_id"`
		SectorNumber         int64  `db:"sector_num"`
		RequestType          string `db:"request_type"`
		RequestCID           string `db:"request_cid"`
		RequestPartitionCost int64  `db:"request_partition_cost"`
	}
	// select proofshare_client_requests where sp_id in (spIDs) and request_sent = false and request_uploaded = true
	// Build Postgres array literal for sp_ids

	err = t.db.QueryRow(ctx, `
		SELECT sp_id, sector_num, request_type, request_cid, request_partition_cost
		FROM proofshare_client_requests
		WHERE sp_id = ANY($1)
		  AND request_sent = FALSE
		  AND request_uploaded = TRUE
		LIMIT 1
	`, spIDs).Scan(&request.SpID, &request.SectorNumber, &request.RequestType, &request.RequestCID, &request.RequestPartitionCost)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}

		return false, xerrors.Errorf("failed to get applicable requests: %w", err)
	}

	// Check if the proof service is available
	// Exponential backoff if not available
	var available bool
	backoff := time.Second
	maxBackoff := 3 * time.Second
	var availabilityRetries int
	for {
		availabilityRetries++
		available, err = proofsvc.CheckAvailability()
		if err != nil {
			return false, xerrors.Errorf("failed to check proof service availability: %w", err)
		}
		if available {
			break
		}
		// Randomize backoff by ±30%
		randomizedBackoff := time.Duration(float64(backoff) * (0.7 + 0.6*rand.Float64())) // 0.7 to 1.3

		log.Infow("proof service not available, backing off", "backoff", randomizedBackoff, "taskID", taskID, "spID", request.SpID, "sectorNumber", request.SectorNumber)
		time.Sleep(randomizedBackoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	// Get current price for the proof
	marketPrice, err := proofsvc.GetCurrentPrice()
	if err != nil {
		return false, xerrors.Errorf("failed to get current price: %w", err)
	}

	maxPerProofPrice, err := types.BigFromString(maxPerProofPrices[request.SpID])
	if err != nil {
		return false, xerrors.Errorf("failed to convert max per proof price to bigint: %w", err)
	}

	nfilBase, err := proofsvc.NfilFromTokenAmount(maxPerProofPrice)
	if err != nil {
		return false, xerrors.Errorf("failed to convert max per proof price to nfil: %w", err)
	}

	if marketPrice.PriceNfilBase+marketPrice.PriceNfilServiceFee > nfilBase {
		return false, xerrors.Errorf("max per proof price is too low, max per proof price: %d nFIL, market price: %d nFIL", nfilBase, marketPrice.PriceNfilBase+marketPrice.PriceNfilServiceFee)
	}

	basePrice := marketPrice.PriceNfilBase * proofsvc.NFilAmount(request.RequestPartitionCost) / 10
	serviceFee := marketPrice.PriceNfilServiceFee * proofsvc.NFilAmount(request.RequestPartitionCost) / 10

	price := proofsvc.TokenAmountFromNfil(basePrice + serviceFee)

	var nextNonce int64
	var waitConsumed bool
	// Create payment in a transaction

	_, err = t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		waitConsumed = false

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

		// If there's an unconsumed payment that probably means there is another version of this task running for the same wallet
		// Shouldn't normally happen, but we don't want to mess up the state when it does
		if err == nil && !lastPayment.Consumed {
			log.Infow("waiting for previous payment to be consumed",
				"taskID", taskID, "wallet", lastPayment.Wallet, "nonce", lastPayment.Nonce, "spID", request.SpID, "sectorNumber", request.SectorNumber)
			waitConsumed = true
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
		`, walletID).Scan(&nextNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to get next nonce: %w", err)
		}

		// calculate new cumulative amount
		prevCumulative := cumulativeAmount
		cumulativeAmount = types.BigAdd(cumulativeAmount, price)
		log.Infow("calculating cumulative amount", "prevCumulative", prevCumulative, "price", price, "cumulativeAmount", cumulativeAmount, "taskID", taskID, "spID", request.SpID, "sectorNumber", request.SectorNumber)

		// Create voucher
		voucher, err := t.router.CreateClientVoucher(ctx, uint64(walletID), cumulativeAmount.Int, uint64(nextNonce))
		if err != nil {
			return false, xerrors.Errorf("failed to create voucher: %w", err)
		}

		sig, err := t.api.WalletSign(ctx, addr, voucher)
		if err != nil {
			return false, xerrors.Errorf("failed to sign voucher: %w", err)
		}

		// Insert the payment
		_, err = tx.Exec(`
			INSERT INTO proofshare_client_payments (wallet, nonce, cumulative_amount, signature, consumed)
			VALUES ($1, $2, $3, $4, FALSE)
		`, walletID, nextNonce, cumulativeAmount.String(), sig.Data)
		if err != nil {
			return false, xerrors.Errorf("failed to insert payment: %w", err)
		}

		// Update the client request with the payment info
		_, err = tx.Exec(`
			UPDATE proofshare_client_requests
			SET payment_wallet = $4, payment_nonce = $5
			WHERE sp_id = $1 AND sector_num = $2 AND request_type = $3
		`, request.SpID, request.SectorNumber, request.RequestType, walletID, nextNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to update client request with payment info: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, xerrors.Errorf("transaction failed: %w", err)
	}

	if waitConsumed {
		return false, xerrors.Errorf("previous payment was not consumed, yielding")
	}

	log.Infow("createPayment complete", "taskID", taskID, "wallet", clientID, "nonce", nextNonce, "calcPrice", price, "price", marketPrice, "requestPartitionCost", request.RequestPartitionCost, "spID", request.SpID, "sectorNumber", request.SectorNumber)

	return true, nil
}

// TypeDetails implements harmonytask.TaskInterface.
func (t *TaskClientSend) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: taskhelp.BackgroundTask("PSClientSend"),
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 128 << 20,
			Gpu: 0,
		},
	}
}

func (t *TaskClientSend) getApplicableSPIDs(ctx context.Context, addr address.Address) ([]int64, map[int64]string, error) {
	// proofshare_client_settings where wallet (f1 text...) = walletID

	rows, err := t.db.Query(ctx, `
		SELECT sp_id, pprice
		FROM proofshare_client_settings
		WHERE wallet = $1 AND enabled = true
	`, addr.String())
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to get applicable SPIDs: %w", err)
	}

	prices := make(map[int64]string)
	var spIDs []int64
	for rows.Next() {
		var spID int64
		var pprice string
		err = rows.Scan(&spID, &pprice)
		if err != nil {
			return nil, nil, xerrors.Errorf("failed to scan SPID: %w", err)
		}
		spIDs = append(spIDs, spID)
		prices[spID] = pprice
	}
	rows.Close()

	if _, has0 := prices[0]; !has0 {
		// no spid 0, so we can return early
		return spIDs, prices, nil
	}

	// special case - if there is spid 0, get all SP IDs with pending requests that ARE NOT listed explicitly in the settings for any wallet
	rows, err = t.db.Query(ctx, `
		SELECT DISTINCT sp_id
		FROM proofshare_client_requests
		WHERE request_sent = false AND request_uploaded = true AND sp_id NOT IN (SELECT sp_id FROM proofshare_client_settings)
	`)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to get applicable SPIDs: %w", err)
	}
	for rows.Next() {
		var spID int64
		err = rows.Scan(&spID)
		if err != nil {
			return nil, nil, xerrors.Errorf("failed to scan SPID: %w", err)
		}
		spIDs = append(spIDs, spID)
		prices[spID] = prices[0]
	}
	rows.Close()

	return spIDs, prices, nil
}

var _ = harmonytask.Reg(&TaskClientSend{})
