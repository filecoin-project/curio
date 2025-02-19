package webrpc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
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
	ServiceID     int64     `db:"service_id"     json:"service_id"`
	ObtainedAt    time.Time `db:"obtained_at"    json:"obtained_at"`
	ComputeTaskID *int64    `db:"compute_task_id" json:"compute_task_id"`
	ComputeDone   bool      `db:"compute_done"   json:"compute_done"`
	SubmitTaskID  *int64    `db:"submit_task_id" json:"submit_task_id"`
	SubmitDone    bool      `db:"submit_done"    json:"submit_done"`
	Price         string    `db:"pprice"         json:"price"`
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

// PSListQueue returns all records from the proofshare_queue table, ordered by the newest first.
func (a *WebRPC) PSListQueue(ctx context.Context) ([]ProofShareQueueItem, error) {
	items := []ProofShareQueueItem{}

	err := a.deps.DB.Select(ctx, &items, `
        SELECT service_id,
               obtained_at,
               compute_task_id,
               compute_done,
               submit_task_id,
               submit_done
        FROM proofshare_queue
        ORDER BY obtained_at DESC
    `)
	if err != nil {
		return nil, xerrors.Errorf("PSListQueue: failed to query proofshare_queue: %w", err)
	}

	return items, nil
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
	Price              string   `db:"pprice"`

	Address string `db:"-" json:"address"`
	FilPerP string  `db:"-" json:"price"`
}

// ProofShareClientRequest model
// Matches proofshare_client_requests table columns
type ProofShareClientRequest struct {
	TaskID    int64        `db:"task_id"      json:"task_id"`
	SpID      int64        `db:"sp_id"        json:"sp_id"`
	SectorNum int64        `db:"sector_num"   json:"sector_num"`
	ServiceID int64        `db:"service_id"   json:"service_id"`
	Done      bool         `db:"done"         json:"done"`
	CreatedAt time.Time    `db:"created_at"   json:"created_at"`
	DoneAt    sql.NullTime `db:"done_at"      json:"done_at,omitempty"`
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
// If a row for sp_id doesn’t exist, do an INSERT; otherwise do an UPDATE.
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

// PSClientRequests returns the list of proofshare_client_requests for a given sp_id
// (or all if sp_id=0 and that’s your convention).
func (a *WebRPC) PSClientRequests(ctx context.Context, spId int64) ([]ProofShareClientRequest, error) {
	var rows []ProofShareClientRequest

	// If you want spId=0 to mean "all," you can do logic in WHERE
	// e.g.: WHERE (sp_id = $1 OR $1=0)
	err := a.deps.DB.Select(ctx, &rows, `
        SELECT task_id, sp_id, sector_num, service_id, done, created_at, done_at
        FROM proofshare_client_requests
        WHERE (sp_id = $1 OR $1=0)
        ORDER BY created_at DESC
    `, spId)
	if err != nil {
		return nil, xerrors.Errorf("PSClientRequests: query error: %w", err)
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
func (a *WebRPC) addMessageTracking(ctx context.Context, messageCid cid.Cid, wallet string, action string) error {
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

	if err := a.addMessageTracking(ctx, depositCid, wallet, "deposit"); err != nil {
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

	if err := a.addMessageTracking(ctx, withdrawCid, wallet, "withdraw-request"); err != nil {
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

	if err := a.addMessageTracking(ctx, cancelCid, wallet, "withdraw-cancel"); err != nil {
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

	if err := a.addMessageTracking(ctx, completeCid, wallet, "withdraw-complete"); err != nil {
		return cid.Undef, xerrors.Errorf("PSClientRouterCompleteWithdrawal: failed to track message: %w", err)
	}

	return completeCid, nil
}

