package webrpc

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// ProofShareMeta holds the data from the proofshare_meta table.
type ProofShareMeta struct {
	Enabled       bool    `db:"enabled" json:"enabled"`
	Wallet        *string `db:"wallet" json:"wallet"`
	RequestTaskID *int64  `db:"request_task_id" json:"request_task_id"`
	Price         string  `db:"pprice"         json:"price"`
}

// ProofShareQueueItem represents each row in proofshare_queue.
type ProofShareQueueItem struct {
	ServiceID     string    `db:"service_id"     json:"service_id"`
	ObtainedAt    time.Time `db:"obtained_at"    json:"obtained_at"`
	ComputeTaskID *int64    `db:"compute_task_id" json:"compute_task_id"`
	ComputeDone   bool      `db:"compute_done"   json:"compute_done"`
	SubmitTaskID  *int64    `db:"submit_task_id" json:"submit_task_id"`
	SubmitDone    bool      `db:"submit_done"    json:"submit_done"`
	WasPoW        bool      `db:"was_pow"        json:"was_pow"`
	PaymentAmount string    `json:"payment_amount"`
}

// PSGetMeta returns the current meta row from proofshare_meta (always a single row).
func (a *WebRPC) PSGetMeta(ctx context.Context) (ProofShareMeta, error) {
	var meta ProofShareMeta

	err := a.deps.DB.QueryRow(ctx, `
        SELECT enabled, wallet, request_task_id, pprice
        FROM proofshare_meta
        WHERE singleton = TRUE
    `).Scan(&meta.Enabled, &meta.Wallet, &meta.RequestTaskID, &meta.Price)
	if err != nil {
		return meta, xerrors.Errorf("PSGetMeta: failed to query proofshare_meta: %w", err)
	}

	pta, err := types.BigFromString(meta.Price)
	if err != nil {
		return meta, xerrors.Errorf("PSGetMeta: invalid price: %w", err)
	}
	meta.Price = types.FIL(pta).Unitless()

	return meta, nil
}

// PSSetMeta updates proofshare_meta with new "enabled" flag and "wallet" address.
// If you want to allow a NULL wallet, you could accept a pointer or do conditional logic.
func (a *WebRPC) PSSetMeta(ctx context.Context, enabled bool, wallet string, price string) error {
	ta, err := types.ParseFIL(price)
	if err != nil {
		return xerrors.Errorf("PSSetMeta: invalid price: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `
        UPDATE proofshare_meta
        SET enabled = $1, wallet = $2, pprice = $3
        WHERE singleton = TRUE
    `, enabled, wallet, abi.TokenAmount(ta).String())
	if err != nil {
		return xerrors.Errorf("PSSetMeta: failed to update proofshare_meta: %w", err)
	}
	return nil
}

func (a *WebRPC) PSListAsks(ctx context.Context) ([]common.WorkAsk, error) {
	var meta ProofShareMeta

	err := a.deps.DB.QueryRow(ctx, `
        SELECT wallet
        FROM proofshare_meta
        WHERE singleton = TRUE
    `).Scan(&meta.Wallet)
	if err != nil {
		return nil, xerrors.Errorf("PSListAsks: failed to query proofshare_meta: %w", err)
	}

	if meta.Wallet == nil {
		return nil, nil
	}

	work, err := proofsvc.PollWork(*meta.Wallet)
	if err != nil {
		return nil, xerrors.Errorf("failed to poll work: %w", err)
	}

	var out []common.WorkAsk
	for _, ask := range work.ActiveAsks {
		out = append(out, common.WorkAsk{
			ID:           ask.ID,
			CreatedAt:    ask.CreatedAt,
			MinPriceNfil: ask.MinPriceNfil,
			MinPriceFil:  types.FIL(proofsvc.TokenAmountFromNfil(ask.MinPriceNfil)).Short(),
		})
	}

	// sort, oldest first
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	return out, nil
}

func (a *WebRPC) PSAskWithdraw(ctx context.Context, askID int64) error {
	err := proofsvc.WithdrawAsk(askID)
	if err != nil {
		return xerrors.Errorf("PSAskWithdraw: failed to withdraw ask: %w", err)
	}

	return nil
}

// PSListQueue returns all records from the proofshare_queue table, ordered by the newest first.
func (a *WebRPC) PSListQueue(ctx context.Context) ([]*ProofShareQueueItem, error) {
	items := []*ProofShareQueueItem{}

	err := a.deps.DB.Select(ctx, &items, `
        SELECT request_cid as service_id,
               obtained_at,
               compute_task_id,
               compute_done,
               submit_task_id,
               submit_done,
			   was_pow
        FROM proofshare_queue
        ORDER BY obtained_at DESC
        LIMIT 15
    `)
	if err != nil {
		return nil, xerrors.Errorf("PSListQueue: failed to query proofshare_queue: %w", err)
	}

	for i := range items {
		if !items[i].SubmitDone || items[i].WasPoW {
			continue
		}

		var paymentAmt string
		var providerID, paymentNonce int64
		err := a.deps.DB.QueryRow(ctx, `
			SELECT payment_cumulative_amount, provider_id, payment_nonce
			FROM proofshare_provider_payments
			WHERE request_cid = $1
		`, items[i].ServiceID).Scan(&paymentAmt, &providerID, &paymentNonce)
		if err != nil {
			return nil, xerrors.Errorf("PSListQueue: failed to query proofshare_provider_payments: %w", err)
		}

		var prevPaymentAmt string = "0"
		if paymentNonce > 0 {
			err = a.deps.DB.QueryRow(ctx, `
				SELECT payment_cumulative_amount
				FROM proofshare_provider_payments
				WHERE provider_id = $1 AND payment_nonce = $2
			`, providerID, paymentNonce-1).Scan(&prevPaymentAmt)
		}

		cumAmt, err := types.BigFromString(paymentAmt)
		if err != nil {
			return nil, xerrors.Errorf("PSListQueue: failed to parse payment amount: %w", err)
		}
		prevAmt, err := types.BigFromString(prevPaymentAmt)
		if err != nil {
			return nil, xerrors.Errorf("PSListQueue: failed to parse previous payment amount: %w", err)
		}
		items[i].PaymentAmount = types.FIL(big.Sub(cumAmt, prevAmt)).Short()
	}

	return items, nil
}

func (a *WebRPC) PSProviderSettle(ctx context.Context, providerID int64) (cid.Cid, error) {
	providerAddr, err := address.NewIDAddress(uint64(providerID))
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSProviderSettle: failed to create address from provider ID %d: %w", providerID, err)
	}

	// 2. Fetch the latest payment record for the provider
	var latestPayment struct {
		Nonce            int64  `db:"payment_nonce"`
		CumulativeAmount string `db:"payment_cumulative_amount"`
		Signature        []byte `db:"payment_signature"`
	}
	err = a.deps.DB.QueryRow(ctx, `
		SELECT payment_nonce, payment_cumulative_amount, payment_signature
		FROM proofshare_provider_payments
		WHERE provider_id = $1
		ORDER BY payment_nonce DESC
		LIMIT 1
	`, providerID).Scan(&latestPayment.Nonce, &latestPayment.CumulativeAmount, &latestPayment.Signature)

	if err != nil {
		if xerrors.Is(err, sql.ErrNoRows) {
			return cid.Undef, xerrors.Errorf("PSProviderSettle: no payment records found for provider ID %d, nothing to settle", providerID)
		}
		return cid.Undef, xerrors.Errorf("PSProviderSettle: failed to query latest payment for provider ID %d: %w", providerID, err)
	}

	// 3. Fetch the latest settlement nonce for the provider
	var lastSettledNonce sql.NullInt64
	err = a.deps.DB.QueryRow(ctx, `
		SELECT MAX(payment_nonce)
		FROM proofshare_provider_payments_settlement
		WHERE provider_id = $1
	`, providerID).Scan(&lastSettledNonce)
	if err != nil && !xerrors.Is(err, sql.ErrNoRows) { // sql.ErrNoRows is fine, means no settlements yet
		return cid.Undef, xerrors.Errorf("PSProviderSettle: failed to query last settlement nonce for provider ID %d: %w", providerID, err)
	}

	if lastSettledNonce.Valid && latestPayment.Nonce <= lastSettledNonce.Int64 {
		return cid.Undef, xerrors.Errorf("PSProviderSettle: latest payment (nonce %d) for provider ID %d is already settled or not newer than last settlement (nonce %d)", latestPayment.Nonce, providerID, lastSettledNonce.Int64)
	}

	// 4. Prepare data for service call
	cumulativeAmountBig, err := types.BigFromString(latestPayment.CumulativeAmount)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSProviderSettle: failed to parse cumulative amount '%s' for provider ID %d, nonce %d: %w", latestPayment.CumulativeAmount, providerID, latestPayment.Nonce, err)
	}

	// Use a custom sender for the service context
	svc := common.NewServiceCustomSend(a.deps.Chain, func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		return a.deps.Sender.Send(ctx, msg, mss, "ps-provider-settle")
	})

	// 5. Call ServiceRedeemProviderVoucher
	log.Infow("PSProviderSettle: calling ServiceRedeemProviderVoucher",
		"provider_id", providerID,
		"provider_address", providerAddr.String(),
		"cumulative_amount", cumulativeAmountBig.String(),
		"nonce", latestPayment.Nonce)

	settleCid, err := svc.ServiceRedeemProviderVoucher(
		ctx,
		providerAddr,                // The address sending the transaction
		uint64(providerID),          // The ID of the provider to settle with
		cumulativeAmountBig,         // The cumulative amount to settle
		uint64(latestPayment.Nonce), // The nonce of the payment
		latestPayment.Signature,     // The signature of the payment voucher
	)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSProviderSettle: ServiceRedeemProviderVoucher call for provider %s (ID %d), nonce %d failed: %w", providerAddr.String(), providerID, latestPayment.Nonce, err)
	}

	// 6. Track the sent message for UI and internal state updates
	err = a.addMessageTrackingProvider(ctx, settleCid, providerID, "settle", func(tx *harmonydb.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO proofshare_provider_payments_settlement (provider_id, payment_nonce, settle_message_cid)
			VALUES ($1, $2, $3)
		`, providerID, latestPayment.Nonce, settleCid.String())
		return err
	})
	if err != nil {
		// If tracking fails, it's usually critical as the system might lose track of an on-chain action.
		// Log the error but consider if the settlement CID should still be returned or if this failure is terminal.
		// For now, returning the error as it might indicate a deeper issue with message tracking.
		log.Errorw("PSProviderSettle: failed to track settlement message, but settlement was sent",
			"provider_id", providerID,
			"settlement_cid", settleCid.String(),
			"tracking_error", err)
		return cid.Undef, xerrors.Errorf("PSProviderSettle: failed to track settlement message %s for provider %s (ID %d): %w", settleCid.String(), providerAddr.String(), providerID, err)
	}

	log.Infow("Successfully initiated provider settlement",
		"provider_id", providerID,
		"provider_address", providerAddr.String(),
		"settled_nonce", latestPayment.Nonce,
		"settled_cumulative_amount", cumulativeAmountBig.String(),
		"settlement_cid", settleCid.String())

	return settleCid, nil
}

func (a *WebRPC) addMessageTrackingProvider(ctx context.Context, messageCid cid.Cid, providerID int64, action string, txcb func(tx *harmonydb.Tx) error) error {
	addr, err := address.NewIDAddress(uint64(providerID))
	if err != nil {
		return xerrors.Errorf("addMessageTracking: invalid wallet address: %w", err)
	}

	idAddr, err := a.deps.Chain.StateLookupID(ctx, addr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("addMessageTracking: failed to lookup id: %w", err)
	}

	walletID, err := address.IDFromAddress(idAddr)
	if err != nil {
		return xerrors.Errorf("addMessageTracking: failed to get wallet id: %w", err)
	}

	_, err = a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(`
			INSERT INTO message_waits (signed_message_cid)
			VALUES ($1)
		`, messageCid)
		if err != nil {
			return false, xerrors.Errorf("addMessageTracking: failed to insert message_waits: %w", err)
		}

		_, err = tx.Exec(`
			INSERT INTO proofshare_provider_messages (signed_cid, wallet, action)
			VALUES ($1, $2, $3)
		`, messageCid, walletID, action)
		if err != nil {
			return false, xerrors.Errorf("addMessageTracking: failed to insert proofshare_provider_messages: %w", err)
		}

		err = txcb(tx)
		if err != nil {
			return false, xerrors.Errorf("addMessageTracking: transaction callback failed: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("addMessageTracking: transaction failed: %w", err)
	}

	return nil
}

///////
// CLIENT

// ProofShareClientSettings model
// Matches proofshare_client_settings table columns
type ProofShareClientSettings struct {
	SpID               int64   `db:"sp_id"                 json:"sp_id"`
	Enabled            bool    `db:"enabled"               json:"enabled"`
	Wallet             *string `db:"wallet"                json:"wallet"`
	MinimumPendingSecs int64   `db:"minimum_pending_seconds" json:"minimum_pending_seconds"`
	DoPoRep            bool    `db:"do_porep"              json:"do_porep"`
	DoSnap             bool    `db:"do_snap"               json:"do_snap"`
	Price              string  `db:"pprice"`

	Address string `db:"-" json:"address"`
	FilPerP string `db:"-" json:"price"`
}

// PSClientGet fetches all proofshare_client_settings rows.
func (a *WebRPC) PSClientGet(ctx context.Context) ([]ProofShareClientSettings, error) {
	var out []ProofShareClientSettings
	err := a.deps.DB.Select(ctx, &out, `
        SELECT sp_id, enabled, wallet, minimum_pending_seconds, do_porep, do_snap, pprice
        FROM proofshare_client_settings
        ORDER BY sp_id ASC
    `)
	if err != nil {
		return nil, xerrors.Errorf("PSClientGet: query error: %w", err)
	}

	for i := range out {
		out[i].Address = must.One(address.NewIDAddress(uint64(out[i].SpID))).String()

		pta, err := types.BigFromString(out[i].Price)
		if err != nil {
			return nil, xerrors.Errorf("PSClientGet: invalid price: %w", err)
		}
		out[i].FilPerP = types.FIL(pta).Unitless()
	}

	return out, nil
}

// PSClientSet updates or inserts a row in proofshare_client_settings.
// If a row for sp_id doesn't exist, do an INSERT; otherwise do an UPDATE.
func (a *WebRPC) PSClientSet(ctx context.Context, s ProofShareClientSettings) error {
	maddr, err := address.NewFromString(s.Address)
	if err != nil {
		return xerrors.Errorf("PSClientSet: invalid address: %w", err)
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return xerrors.Errorf("PSClientSet: invalid address: %w", err)
	}

	s.SpID = int64(mid)

	filamt, err := types.ParseFIL(s.FilPerP)
	if err != nil {
		return xerrors.Errorf("PSClientSet: invalid price: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `
        INSERT INTO proofshare_client_settings (sp_id, enabled, wallet, minimum_pending_seconds, do_porep, do_snap, pprice)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (sp_id) DO UPDATE
          SET enabled = EXCLUDED.enabled,
              wallet  = EXCLUDED.wallet,
              minimum_pending_seconds = EXCLUDED.minimum_pending_seconds,
              do_porep = EXCLUDED.do_porep,
              do_snap = EXCLUDED.do_snap,
              pprice = EXCLUDED.pprice
    `,
		s.SpID,
		s.Enabled,
		s.Wallet,
		s.MinimumPendingSecs,
		s.DoPoRep,
		s.DoSnap,
		abi.TokenAmount(filamt).String(),
	)
	if err != nil {
		return xerrors.Errorf("PSClientSet: upsert error: %w", err)
	}
	return nil
}

// ProofShareClientRequest model
type ProofShareClientRequest struct {
	TaskID          int64        `db:"task_id"           json:"task_id"`
	SpID            int64        `db:"sp_id"`
	SectorNum       int64        `db:"sector_num"        json:"sector_num"`
	RequestCID      *string      `db:"request_cid"       json:"request_cid,omitempty"`
	RequestUploaded bool         `db:"request_uploaded"  json:"request_uploaded"`
	PaymentWallet   *int64       `db:"payment_wallet"    json:"payment_wallet,omitempty"`
	PaymentNonce    *int64       `db:"payment_nonce"     json:"payment_nonce,omitempty"`
	RequestSent     bool         `db:"request_sent"      json:"request_sent"`
	ResponseData    []byte       `db:"response_data"     json:"response_data,omitempty"`
	Done            bool         `db:"done"              json:"done"`
	CreatedAt       time.Time    `db:"created_at"        json:"created_at"`
	DoneAt          sql.NullTime `db:"done_at"           json:"done_at,omitempty"`

	PaymentAmount *string `db:"-" json:"payment_amount"`
	SpIDStr       string  `db:"-" json:"sp_id"`
}

// PSClientRequests returns the list of proofshare_client_requests for a given sp_id
func (a *WebRPC) PSClientRequests(ctx context.Context, spId int64) ([]*ProofShareClientRequest, error) {
	var rows []*ProofShareClientRequest

	// If you want spId=0 to mean "all," you can do logic in WHERE
	// e.g.: WHERE (sp_id = $1 OR $1=0)
	err := a.deps.DB.Select(ctx, &rows, `
        SELECT task_id, sp_id, sector_num, request_cid, request_uploaded, 
               payment_wallet, payment_nonce, request_sent, response_data,
               done, created_at, done_at
        FROM proofshare_client_requests
        WHERE sp_id = $1
        ORDER BY created_at DESC
		LIMIT 10
    `, spId)
	if err != nil {
		return nil, xerrors.Errorf("PSClientRequests: query error: %w", err)
	}

	for _, r := range rows {
		if r.PaymentWallet == nil || r.PaymentNonce == nil {
			// No payment info, so no payment amount to calculate
			continue
		}

		walletID := *r.PaymentWallet
		currentNonce := *r.PaymentNonce

		var currentCumulativeAmountStr string
		err := a.deps.DB.QueryRow(ctx, `
			SELECT cumulative_amount FROM proofshare_client_payments
			WHERE wallet = $1 AND nonce = $2
		`, walletID, currentNonce).Scan(&currentCumulativeAmountStr)

		currentCumulativeAmount := big.NewInt(0)
		if err != nil {
			if xerrors.Is(err, sql.ErrNoRows) {
				// If current payment record not found, treat current cumulative amount as 0.
				// This means the calculated payment_amount will be 0 (or negative, then clamped to 0).
				log.Warnw("PSClientRequests: current payment record not found, assuming 0 amount", "wallet", walletID, "nonce", currentNonce)
				// currentCumulativeAmount remains 0
			} else {
				// For other errors, log and skip payment calculation for this item.
				log.Errorw("PSClientRequests: failed to query current cumulative amount", "wallet", walletID, "nonce", currentNonce, "error", err)
				continue // Skip to next request
			}
		} else {
			parsedAmount, parseErr := types.BigFromString(currentCumulativeAmountStr)
			if parseErr != nil {
				log.Errorw("PSClientRequests: failed to parse current cumulative amount", "wallet", walletID, "nonce", currentNonce, "value", currentCumulativeAmountStr, "error", parseErr)
				// Treat as 0 if unparseable, or could skip. Let's treat as 0.
				// currentCumulativeAmount remains 0
			} else {
				currentCumulativeAmount = parsedAmount
			}
		}

		previousCumulativeAmount := big.NewInt(0)
		if currentNonce > 0 { // Assuming nonces are non-negative. If nonce can be 0, previous is -1.
			previousNonce := currentNonce - 1
			var previousCumulativeAmountStr string
			errDb := a.deps.DB.QueryRow(ctx, `
				SELECT cumulative_amount FROM proofshare_client_payments
				WHERE wallet = $1 AND nonce = $2
			`, walletID, previousNonce).Scan(&previousCumulativeAmountStr)

			if errDb != nil {
				if !xerrors.Is(errDb, sql.ErrNoRows) {
					// Log error if it's not ErrNoRows (ErrNoRows is expected for the first payment or if nonces are not contiguous)
					log.Errorw("PSClientRequests: failed to query previous cumulative amount", "wallet", walletID, "nonce", previousNonce, "error", errDb)
					// previousCumulativeAmount remains 0
				}
				// If ErrNoRows, previousCumulativeAmount correctly remains 0.
			} else {
				parsedPrevAmount, parseErr := types.BigFromString(previousCumulativeAmountStr)
				if parseErr != nil {
					log.Errorw("PSClientRequests: failed to parse previous cumulative amount", "wallet", walletID, "nonce", previousNonce, "value", previousCumulativeAmountStr, "error", parseErr)
					// previousCumulativeAmount remains 0
				} else {
					previousCumulativeAmount = parsedPrevAmount
				}
			}
		}

		paymentAmountBig := big.Sub(currentCumulativeAmount, previousCumulativeAmount)
		if paymentAmountBig.Sign() < 0 {
			// This could happen if data is inconsistent or amounts decrease.
			log.Warnw("PSClientRequests: calculated payment amount is negative, clamping to 0",
				"wallet", walletID, "currentNonce", currentNonce,
				"currentAmount", types.FIL(currentCumulativeAmount).Short(),
				"previousAmount", types.FIL(previousCumulativeAmount).Short(),
				"calculatedNegative", types.FIL(paymentAmountBig).Short())
			paymentAmountBig = big.NewInt(0)
		}

		paymentAmountStr := types.FIL(paymentAmountBig).Short()
		r.PaymentAmount = &paymentAmountStr

		r.SpIDStr = must.One(address.NewIDAddress(uint64(r.SpID))).String()
	}

	return rows, nil
}

// PSClientRemove removes a row from proofshare_client_settings if sp_id != 0.
func (a *WebRPC) PSClientRemove(ctx context.Context, spId int64) error {
	if spId == 0 {
		return xerrors.Errorf("cannot remove default sp_id=0 row")
	}
	_, err := a.deps.DB.Exec(ctx, `
        DELETE FROM proofshare_client_settings
        WHERE sp_id = $1
    `, spId)
	if err != nil {
		return xerrors.Errorf("PSClientRemove: delete error: %w", err)
	}
	return nil
}

///////
// CLIENT PAYMENTS

type ProofShareClientWallet struct {
	Wallet int64 `db:"wallet" json:"wallet"`

	// db ignored
	Address address.Address `db:"-" json:"address"`

	ChainBalance string `db:"-" json:"chain_balance"`

	// balance which appears as "available" in the router on-chain
	RouterAvailBalance string `db:"-" json:"router_avail_balance"`

	// balance "to settle" in the router on-chain (service has vouches for those payments, but didn't settle on-chain yet)
	RouterUnsettledBalance string `db:"-" json:"router_unsettled_balance"`

	// balance "unlocked" (withdrawable)
	RouterUnlockedBalance string `db:"-" json:"router_unlocked_balance"`

	// Actually available balance, i.e. RouterAvailBalance - RouterUnsettledBalance
	AvailableBalance string `db:"-" json:"available_balance"`

	WithdrawTimestamp *time.Time `db:"-" json:"withdraw_timestamp"`
}

func (a *WebRPC) PSClientWallets(ctx context.Context) ([]*ProofShareClientWallet, error) {
	out := []*ProofShareClientWallet{}
	err := a.deps.DB.Select(ctx, &out, `
		SELECT wallet
		FROM proofshare_client_wallets
		ORDER BY wallet ASC
	`)
	if err != nil {
		return nil, xerrors.Errorf("PSClientWallets: query error: %w", err)
	}

	for _, w := range out {
		w.Address, err = address.NewIDAddress(uint64(w.Wallet))
		if err != nil {
			return nil, xerrors.Errorf("PSClientWallets: invalid address: %w", err)
		}

		wb, err := a.deps.Chain.WalletBalance(ctx, w.Address)
		if err != nil {
			return nil, xerrors.Errorf("PSClientWallets: failed to get chain balance: %w", err)
		}
		w.ChainBalance = types.FIL(wb).Short()

		svc := common.NewService(a.deps.Chain)
		clientState, err := svc.GetClientState(ctx, uint64(w.Wallet))
		if err != nil {
			return nil, xerrors.Errorf("PSClientWallets: failed to get client state: %w", err)
		}

		if !clientState.WithdrawTimestamp.IsZero() {
			// unix seconds
			wtime := time.Unix(clientState.WithdrawTimestamp.Int64(), 0).Add(common.WithdrawWindow)
			w.WithdrawTimestamp = &wtime
		}

		w.RouterAvailBalance = types.FIL(clientState.Balance).Short()

		w.AvailableBalance = types.FIL(clientState.Balance).Short()
		w.RouterUnlockedBalance = "0"

		log.Infow("PSClientWallets", "clientState", clientState, "wtime", w.WithdrawTimestamp)

		if w.WithdrawTimestamp != nil {
			if time.Now().After(*w.WithdrawTimestamp) {
				w.RouterUnlockedBalance = types.FIL(clientState.WithdrawAmount).Short()
				w.AvailableBalance = types.FIL(types.BigSub(types.BigInt(clientState.Balance), types.BigInt(clientState.WithdrawAmount))).Short()
			} else {
				w.RouterUnlockedBalance = fmt.Sprintf("(%s in %s)", types.FIL(clientState.WithdrawAmount).Short(), time.Until(*w.WithdrawTimestamp).Round(time.Second).String())
			}
		}

	}

	return out, nil
}

func (a *WebRPC) PSClientAddWallet(ctx context.Context, wallet string) error {
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return xerrors.Errorf("PSClientAddWallet: invalid address: %w", err)
	}

	ida, err := a.deps.Chain.StateLookupID(ctx, addr, types.EmptyTSK)
	if err != nil {
		return err
	}

	id, err := address.IDFromAddress(ida)
	if err != nil {
		return err
	}

	_, err = a.deps.DB.Exec(ctx, `
		INSERT INTO proofshare_client_wallets (wallet)
		VALUES ($1)
	`, id)
	return err
}

// Define a struct to represent a client message.
type ClientMessage struct {
	StartedAt   time.Time  `json:"started_at"`
	SignedCID   string     `json:"signed_cid"`
	Wallet      int64      `json:"wallet"`
	Action      string     `json:"action"`
	Success     *bool      `json:"success,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	Address address.Address `json:"address" db:"-"`
}

// PSClientListMessages queries and returns the 10 most recently started client messages.
func (a *WebRPC) PSClientListMessages(ctx context.Context) ([]ClientMessage, error) {
	const query = `
		SELECT started_at, signed_cid, wallet, action, success, completed_at
		FROM proofshare_client_messages
		ORDER BY started_at DESC
		LIMIT 10
	`
	rows, err := a.deps.DB.Query(ctx, query)
	if err != nil {
		return nil, xerrors.Errorf("PSClientListMessages: failed to execute query: %w", err)
	}
	defer rows.Close()

	messages := []ClientMessage{}
	for rows.Next() {
		var msg ClientMessage
		var success sql.NullBool
		var completedAt sql.NullTime

		if err := rows.Scan(&msg.StartedAt, &msg.SignedCID, &msg.Wallet, &msg.Action, &success, &completedAt); err != nil {
			return nil, xerrors.Errorf("PSClientListMessages: failed to scan row: %w", err)
		}
		if success.Valid {
			msg.Success = &success.Bool
		}
		if completedAt.Valid {
			msg.CompletedAt = &completedAt.Time
		}

		msg.Address, err = address.NewIDAddress(uint64(msg.Wallet))
		if err != nil {
			return nil, xerrors.Errorf("PSClientListMessages: failed to get address: %w", err)
		}

		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, xerrors.Errorf("PSClientListMessages: row iteration error: %w", err)
	}

	return messages, nil
}

func (a *WebRPC) addMessageTrackingClient(ctx context.Context, messageCid cid.Cid, wallet string, action string) error {
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return xerrors.Errorf("addMessageTracking: invalid wallet address: %w", err)
	}

	idAddr, err := a.deps.Chain.StateLookupID(ctx, addr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("addMessageTracking: failed to lookup id: %w", err)
	}

	walletID, err := address.IDFromAddress(idAddr)
	if err != nil {
		return xerrors.Errorf("addMessageTracking: failed to get wallet id: %w", err)
	}

	_, err = a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		_, err := tx.Exec(`
			INSERT INTO message_waits (signed_message_cid)
			VALUES ($1)
		`, messageCid)
		if err != nil {
			return false, xerrors.Errorf("addMessageTracking: failed to insert message_waits: %w", err)
		}

		_, err = tx.Exec(`
			INSERT INTO proofshare_client_messages (signed_cid, wallet, action)
			VALUES ($1, $2, $3)
		`, messageCid, walletID, action)
		if err != nil {
			return false, xerrors.Errorf("addMessageTracking: failed to insert proofshare_client_messages: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("addMessageTracking: transaction failed: %w", err)
	}

	return nil
}

func (a *WebRPC) PSClientRouterAddBalance(ctx context.Context, wallet string, amountStr string) (cid.Cid, error) {
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterAddBalance: invalid address: %w", err)
	}

	amountFIL, err := types.ParseFIL(amountStr)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterAddBalance: invalid amount: %w", err)
	}

	availBalance, err := a.deps.Chain.WalletBalance(ctx, addr)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterAddBalance: failed to get chain balance: %w", err)
	}

	amount := abi.TokenAmount(amountFIL)

	if availBalance.LessThan(amount) {
		return cid.Undef, xerrors.Errorf("PSClientRouterAddBalance: not enough balance: %s < %s",
			types.FIL(availBalance).Short(), amountFIL.Short())
	}

	svc := common.NewServiceCustomSend(a.deps.Chain, func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		return a.deps.Sender.Send(ctx, msg, mss, "ps-client-deposit")
	})

	depositCid, err := svc.ClientDeposit(ctx, addr, amount)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterAddBalance: failed to deposit: %w", err)
	}

	log.Infow("PSClientRouterAddBalance", "deposit_cid", depositCid)

	if err := a.addMessageTrackingClient(ctx, depositCid, wallet, "deposit"); err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterAddBalance: failed to track message: %w", err)
	}

	return depositCid, nil
}

func (a *WebRPC) PSClientRouterRequestWithdrawal(ctx context.Context, wallet string, amountStr string) (cid.Cid, error) {
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterRequestWithdrawal: invalid address: %w", err)
	}

	svc := common.NewServiceCustomSend(a.deps.Chain, func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		return a.deps.Sender.Send(ctx, msg, mss, "ps-client-withdraw")
	})

	amountFIL, err := types.ParseFIL(amountStr)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterRequestWithdrawal: invalid amount: %w", err)
	}

	amount := abi.TokenAmount(amountFIL)

	withdrawCid, err := svc.ClientInitiateWithdrawal(ctx, addr, amount)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterRequestWithdrawal: failed to withdraw: %w", err)
	}

	if err := a.addMessageTrackingClient(ctx, withdrawCid, wallet, "withdraw-request"); err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterRequestWithdrawal: failed to track message: %w", err)
	}

	return withdrawCid, nil
}

func (a *WebRPC) PSClientRouterCancelWithdrawal(ctx context.Context, wallet string) (cid.Cid, error) {
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCancelWithdrawal: invalid address: %w", err)
	}

	svc := common.NewServiceCustomSend(a.deps.Chain, func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		return a.deps.Sender.Send(ctx, msg, mss, "ps-client-cancel-withdrawal")
	})

	cancelCid, err := svc.ClientCancelWithdrawal(ctx, addr)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCancelWithdrawal: failed to cancel withdrawal: %w", err)
	}

	if err := a.addMessageTrackingClient(ctx, cancelCid, wallet, "withdraw-cancel"); err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCancelWithdrawal: failed to track message: %w", err)
	}

	return cancelCid, nil
}

func (a *WebRPC) PSClientRouterCompleteWithdrawal(ctx context.Context, wallet string) (cid.Cid, error) {
	addr, err := address.NewFromString(wallet)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCompleteWithdrawal: invalid address: %w", err)
	}

	svc := common.NewServiceCustomSend(a.deps.Chain, func(ctx context.Context, msg *types.Message, mss *api.MessageSendSpec) (cid.Cid, error) {
		return a.deps.Sender.Send(ctx, msg, mss, "ps-client-complete-withdrawal")
	})

	completeCid, err := svc.ClientCompleteWithdrawal(ctx, addr)
	if err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCompleteWithdrawal: failed to complete withdrawal: %w", err)
	}

	if err := a.addMessageTrackingClient(ctx, completeCid, wallet, "withdraw-complete"); err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCompleteWithdrawal: failed to track message: %w", err)
	}

	return completeCid, nil
}

// ProviderLastPaymentSummary holds aggregated payment info for a provider.
type ProviderLastPaymentSummary struct {
	// Fields directly from SQL
	WalletID                   int64        `db:"wallet_id" json:"wallet_id"`
	LastPaymentNonce           int64        `db:"last_payment_nonce" json:"last_payment_nonce"`
	LatestPaymentValueRaw      *string      `db:"latest_payment_value" json:"-"`       // Raw from DB, not in final JSON
	LastSettledPaymentValueRaw *string      `db:"last_settled_payment_value" json:"-"` // Raw from DB, not in final JSON
	SQLLastSettledAt           sql.NullTime `db:"last_settled_at" json:"-"`            // Raw from DB, not in final JSON

	// Derived and formatted fields for JSON output
	Address                 string     `json:"address"`
	LastSettledAmountFIL    *string    `json:"last_settled_amount_fil,omitempty"`
	UnsettledAmountFIL      *string    `json:"unsettled_amount_fil,omitempty"`
	TimeSinceLastSettlement *string    `json:"time_since_last_settlement,omitempty"`
	LastSettledAt           *time.Time `json:"last_settled_at,omitempty"`

	ContractSettledFIL *string `json:"contract_settled_fil,omitempty"`
	ContractLastNonce  *uint64 `json:"contract_last_nonce,omitempty"`
}

// PSProviderLastPaymentsSummary returns a summary of the last payment and settlement status for each provider.
func (a *WebRPC) PSProviderLastPaymentsSummary(ctx context.Context) ([]ProviderLastPaymentSummary, error) {
	var summaries []ProviderLastPaymentSummary

	const query = `
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
        p.payment_nonce AS last_payment_nonce, -- This is max_payment_nonce
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
        s.payment_nonce AS last_settled_nonce, -- This is max_settled_nonce
        s.settled_at,
        p.payment_cumulative_amount AS last_settled_payment_value
    FROM proofshare_provider_payments_settlement s
    INNER JOIN MaxSettledNonces msn ON s.provider_id = msn.provider_id AND s.payment_nonce = msn.max_settled_nonce
    INNER JOIN proofshare_provider_payments p ON s.provider_id = p.provider_id AND p.payment_nonce = msn.max_settled_nonce -- amount for the settled nonce
)
SELECT
    lp.provider_id AS wallet_id,
    lp.last_payment_nonce,
    lp.latest_payment_value,
    ls.last_settled_payment_value, -- This can be NULL if no settlement for the provider
    ls.settled_at AS last_settled_at -- This can be NULL
FROM LatestPayments lp
LEFT JOIN LatestSettlements ls ON lp.provider_id = ls.provider_id
ORDER BY lp.provider_id;
    ` // End of query string

	err := a.deps.DB.Select(ctx, &summaries, query)
	if err != nil {
		return nil, xerrors.Errorf("PSProviderLastPaymentsSummary: failed to query payment summaries: %w", err)
	}

	svc := common.NewService(a.deps.Chain)

	for i := range summaries {
		item := &summaries[i] // Use pointer to modify in place

		// Resolve Address
		addr, addrErr := address.NewIDAddress(uint64(item.WalletID))
		if addrErr != nil {
			log.Warnw("PSProviderLastPaymentsSummary: failed to create ID address", "walletID", item.WalletID, "error", addrErr)
			item.Address = fmt.Sprintf("invalid_id_address_%d", item.WalletID)
		} else {
			keyAddr, keyAddrErr := a.deps.Chain.StateAccountKey(ctx, addr, types.EmptyTSK)
			if keyAddrErr != nil {
				log.Warnw("PSProviderLastPaymentsSummary: failed to get key address", "walletID", item.WalletID, "idAddress", addr.String(), "error", keyAddrErr)
				item.Address = addr.String() // Fallback to ID address string
			} else {
				item.Address = keyAddr.String()
			}
		}

		// Process amounts
		latestPaymentBigInt := types.NewInt(0)
		if item.LatestPaymentValueRaw != nil && *item.LatestPaymentValueRaw != "" {
			val, pErr := types.BigFromString(*item.LatestPaymentValueRaw)
			if pErr != nil {
				log.Warnw("PSProviderLastPaymentsSummary: failed to parse LatestPaymentValueRaw", "walletID", item.WalletID, "value", *item.LatestPaymentValueRaw, "error", pErr)
			} else {
				latestPaymentBigInt = val
			}
		}

		lastSettledPaymentBigInt := types.NewInt(0)
		if item.SQLLastSettledAt.Valid { // Indicates a settlement occurred
			item.LastSettledAt = &item.SQLLastSettledAt.Time
			tss := time.Since(item.SQLLastSettledAt.Time).Round(time.Second).String()
			item.TimeSinceLastSettlement = &tss

			if item.LastSettledPaymentValueRaw != nil && *item.LastSettledPaymentValueRaw != "" {
				val, pErr := types.BigFromString(*item.LastSettledPaymentValueRaw)
				if pErr != nil {
					log.Warnw("PSProviderLastPaymentsSummary: failed to parse LastSettledPaymentValueRaw", "walletID", item.WalletID, "value", *item.LastSettledPaymentValueRaw, "error", pErr)
					// lastSettledPaymentBigInt remains 0, LastSettledAmountFIL will be "0 FIL" or nil based on formatting choice
					// Forcing 0 FIL if parsing fails but settlement exists
					zeroFil := types.FIL(types.NewInt(0)).Short()
					item.LastSettledAmountFIL = &zeroFil
				} else {
					lastSettledPaymentBigInt = val
					formattedSettled := types.FIL(lastSettledPaymentBigInt).Short()
					item.LastSettledAmountFIL = &formattedSettled
				}
			} else {
				// LastSettledAt is valid, but LastSettledPaymentValueRaw is nil/empty. Treat as 0 FIL.
				zeroFil := types.FIL(types.NewInt(0)).Short()
				item.LastSettledAmountFIL = &zeroFil
			}
		} else {
			// No settlement: LastSettledAmountFIL, TimeSinceLastSettlement, LastSettledAt remain nil.
			// lastSettledPaymentBigInt is already 0.
		}

		// Calculate and format UnsettledAmountFIL
		// Unsettled = LatestPayment - LastSettledPayment (if no settlement, LastSettledPayment is 0)
		unsettledBigInt := types.BigSub(latestPaymentBigInt, lastSettledPaymentBigInt)

		if unsettledBigInt.Sign() < 0 {
			log.Warnw("PSProviderLastPaymentsSummary: Unsettled amount is negative, clamping to 0.",
				"walletID", item.WalletID,
				"latestFIL", types.FIL(latestPaymentBigInt).Short(),
				"settledFIL", types.FIL(lastSettledPaymentBigInt).Short(),
				"unsettledCalculated", types.FIL(unsettledBigInt).Short())
			unsettledBigInt = types.NewInt(0)
		}
		formattedUnsettled := types.FIL(unsettledBigInt).Short()
		item.UnsettledAmountFIL = &formattedUnsettled

		// Nil out raw fields after processing as they are not part of the final JSON
		item.LatestPaymentValueRaw = nil
		item.LastSettledPaymentValueRaw = nil
		// SQLLastSettledAt is already json:"-"

		// contract state
		voucherRedeemed, lastNonce, err := svc.GetProviderState(ctx, uint64(item.WalletID))
		if err != nil {
			log.Warnw("PSProviderLastPaymentsSummary: failed to get provider state", "walletID", item.WalletID, "error", err)
		} else {
			fil := types.FIL(voucherRedeemed).Short()
			item.ContractSettledFIL = &fil
			item.ContractLastNonce = &lastNonce
		}
	}

	return summaries, nil
}

// ProofShareSettlementItem holds data for a single settlement event.
type ProofShareSettlementItem struct {
	ProviderID                  int64     `db:"provider_id" json:"provider_id"`
	PaymentNonce                int64     `db:"payment_nonce" json:"payment_nonce"`
	SettledAt                   time.Time `db:"settled_at" json:"settled_at"`
	SettleMessageCID            string    `db:"settle_message_cid" json:"settle_message_cid"`
	CurrentCumulativeAmountRaw  string    `db:"current_cumulative_amount" json:"-"`
	PreviousCumulativeAmountRaw *string   `db:"previous_cumulative_amount" json:"-"`

	// Derived
	Address                    string `json:"address"`
	AmountForThisSettlementFIL string `json:"amount_for_this_settlement_fil"`
}

// PSListSettlements returns the 8 most recent settlement records, including the amount for that specific transaction.
func (a *WebRPC) PSListSettlements(ctx context.Context) ([]ProofShareSettlementItem, error) {
	var items []ProofShareSettlementItem

	const query = `
WITH RankedSettlements AS (
    -- Get the 8 most recent settlements along with the cumulative amount for THAT settlement's nonce
    SELECT
        s.provider_id,
        s.payment_nonce, -- This is the nonce of the current settlement
        s.settled_at,
        s.settle_message_cid,
        p.payment_cumulative_amount AS current_cumulative_amount -- Cumulative amount FOR THIS nonce
    FROM proofshare_provider_payments_settlement s
             JOIN proofshare_provider_payments p
                  ON s.provider_id = p.provider_id AND s.payment_nonce = p.payment_nonce
    ORDER BY s.settled_at DESC
    LIMIT 4
)
SELECT
    rs.provider_id,
    rs.payment_nonce,
    rs.settled_at,
    rs.settle_message_cid,
    rs.current_cumulative_amount,
    (
        SELECT pp.payment_cumulative_amount
        FROM proofshare_provider_payments_settlement pp_prev
        LEFT JOIN proofshare_provider_payments pp on pp.provider_id = pp_prev.provider_id and pp.payment_nonce = pp_prev.payment_nonce
        WHERE pp_prev.provider_id = rs.provider_id
          AND pp_prev.payment_nonce < rs.payment_nonce -- Nonce strictly less than current settlement's nonce
        ORDER BY pp_prev.payment_nonce DESC -- Get the highest one among those
        LIMIT 1
    ) AS previous_cumulative_amount
FROM RankedSettlements rs
ORDER BY rs.settled_at DESC; -- Maintain final order
    `
	err := a.deps.DB.Select(ctx, &items, query)
	if err != nil {
		return nil, xerrors.Errorf("PSListSettlements: failed to query settlements: %w", err)
	}

	for i := range items {
		item := &items[i]

		// Resolve Address
		addr, addrErr := address.NewIDAddress(uint64(item.ProviderID))
		if addrErr != nil {
			log.Warnw("PSListSettlements: failed to create ID address", "providerID", item.ProviderID, "error", addrErr)
			item.Address = fmt.Sprintf("invalid_id_address_%d", item.ProviderID)
		} else {
			keyAddr, keyAddrErr := a.deps.Chain.StateAccountKey(ctx, addr, types.EmptyTSK)
			if keyAddrErr != nil {
				log.Warnw("PSListSettlements: failed to get key address", "providerID", item.ProviderID, "idAddress", addr.String(), "error", keyAddrErr)
				item.Address = addr.String() // Fallback to ID address
			} else {
				item.Address = keyAddr.String()
			}
		}

		// Calculate AmountForThisSettlementFIL
		currentAmountBig := types.NewInt(0)
		if item.CurrentCumulativeAmountRaw != "" {
			val, pErr := types.BigFromString(item.CurrentCumulativeAmountRaw)
			if pErr != nil {
				log.Warnw("PSListSettlements: failed to parse CurrentCumulativeAmountRaw", "providerID", item.ProviderID, "nonce", item.PaymentNonce, "amount", item.CurrentCumulativeAmountRaw, "error", pErr)
				item.AmountForThisSettlementFIL = "ErrorParsingCurrent"
				continue // Skip to next item if current amount is unparseable
			} else {
				currentAmountBig = val
			}
		} else {
			// This case should ideally not happen if current_cumulative_amount is NOT NULL and fetched correctly.
			log.Warnw("PSListSettlements: CurrentCumulativeAmountRaw is empty", "providerID", item.ProviderID, "nonce", item.PaymentNonce)
			item.AmountForThisSettlementFIL = "MissingCurrentAmount"
			continue
		}

		previousAmountBig := types.NewInt(0)
		if item.PreviousCumulativeAmountRaw != nil && *item.PreviousCumulativeAmountRaw != "" {
			val, pErr := types.BigFromString(*item.PreviousCumulativeAmountRaw)
			if pErr != nil {
				log.Warnw("PSListSettlements: failed to parse PreviousCumulativeAmountRaw", "providerID", item.ProviderID, "nonce", item.PaymentNonce-1, "amount", *item.PreviousCumulativeAmountRaw, "error", pErr)
				// If previous is unparseable, we can still show the current cumulative as the delta,
				// or an error. For now, let previousAmountBig remain 0.
			} else {
				previousAmountBig = val
			}
		}

		deltaAmountBig := types.BigSub(currentAmountBig, previousAmountBig)
		item.AmountForThisSettlementFIL = types.FIL(deltaAmountBig).Short()
	}

	return items, nil
}
