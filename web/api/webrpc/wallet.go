package webrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
)

var walletOnce sync.Once
var walletFriendlyNames = map[string]string{}
var walletFriendlyNamesLock sync.Mutex

func (a *WebRPC) WalletName(ctx context.Context, id string) (string, error) {
	walletOnce.Do(func() {
		populateWalletFriendlyNames(a.deps.DB)
	})
	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	name, ok := walletFriendlyNames[id]
	if ok {
		return name, nil
	}
	return id, nil
}

func (a *WebRPC) WalletNameChange(ctx context.Context, wallet, newName string) error {
	if len(newName) == 0 {
		return errors.New("name cannot be empty")
	}
	_, err := a.deps.DB.Exec(ctx, `UPDATE wallet_names SET name = $1 WHERE wallet = $2`, newName, wallet)
	if err != nil {
		log.Errorf("failed to set wallet name for %s: %s", wallet, err)
		return err
	}
	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	walletFriendlyNames[wallet] = newName
	return nil
}

func populateWalletFriendlyNames(db *harmonydb.DB) {
	// Get all wallet from DB
	var idNames []struct {
		Wallet string `db:"wallet"`
		Name   string `db:"name"`
	}

	err := db.Select(context.Background(), &idNames, `SELECT wallet, name FROM wallet_names`)
	if err != nil {
		log.Errorf("failed to get wallet names: %s", err)
		return
	}

	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	for _, idName := range idNames {
		walletFriendlyNames[idName.Wallet] = idName.Name
	}
}

func (a *WebRPC) WalletNames(ctx context.Context) (map[string]string, error) {
	walletOnce.Do(func() {
		populateWalletFriendlyNames(a.deps.DB)
	})
	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	return walletFriendlyNames, nil
}

func (a *WebRPC) WalletAdd(ctx context.Context, wallet, name string) error {
	if len(name) == 0 {
		return errors.New("name cannot be empty")
	}
	if len(wallet) == 0 {
		return errors.New("wallet cannot be empty")
	}
	_, err := a.deps.DB.Exec(ctx, `INSERT INTO wallet_names (wallet, name) VALUES ($1, $2)`, wallet, name)
	if err != nil {
		log.Errorf("failed to add wallet name for %s: %s", wallet, err)
		return err
	}

	walletFriendlyNamesLock.Lock()
	walletFriendlyNames[wallet] = name
	defer walletFriendlyNamesLock.Unlock()
	return nil
}

func (a *WebRPC) WalletRemove(ctx context.Context, wallet string) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM wallet_names WHERE wallet = $1`, wallet)
	if err != nil {
		log.Errorf("failed to remove wallet name for %s: %s", wallet, err)
		return err
	}
	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	delete(walletFriendlyNames, wallet)
	return nil
}

type PendingMessages struct {
	Messages []PendingMessage `json:"messages"`
	Total    int              `json:"total"`
}

type PendingMessage struct {
	Message string    `json:"message"`
	AddedAt time.Time `json:"added_at"`
}

func (a *WebRPC) PendingMessages(ctx context.Context) (PendingMessages, error) {

	var messages []struct {
		MessageCid string    `db:"signed_message_cid"`
		AddedAt    time.Time `db:"created_at"`
	}

	err := a.deps.DB.Select(ctx, &messages, `SELECT signed_message_cid, created_at FROM message_waits WHERE executed_tsk_cid IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return PendingMessages{}, xerrors.Errorf("failed to get pending messages: %w", err)
	}

	var msgs []PendingMessage

	for i := range messages {
		msg := PendingMessage{
			Message: messages[i].MessageCid,
			AddedAt: messages[i].AddedAt,
		}
		msgs = append(msgs, msg)
	}

	ret := PendingMessages{
		Messages: msgs,
		Total:    len(msgs),
	}

	return ret, nil
}

type WalletInfoShort struct {
	IDAddress       string `json:"id_address"`
	KeyAddress      string `json:"key_address"`
	Balance         string `json:"balance"`
	PendingMessages int    `json:"pending_messages"`
}

func (a *WebRPC) WalletInfoShort(ctx context.Context, walletID string) (WalletInfoShort, error) {
	waddr, err := address.NewFromString(walletID)
	if err != nil {
		return WalletInfoShort{}, xerrors.Errorf("failed to parse wallet ID: %w", err)
	}

	// balance from chain
	balance, err := a.deps.Chain.WalletBalance(ctx, waddr)
	if err != nil {
		return WalletInfoShort{}, xerrors.Errorf("failed to get balance: %w", err)
	}

	kaddr, err := a.deps.Chain.StateAccountKey(ctx, waddr, types.EmptyTSK)
	if err != nil {
		return WalletInfoShort{}, xerrors.Errorf("failed to get key address: %w", err)
	}

	iaddr, err := a.deps.Chain.StateLookupID(ctx, kaddr, types.EmptyTSK)
	if err != nil {
		return WalletInfoShort{}, xerrors.Errorf("failed to get ID address: %w", err)
	}

	var pendingMessages int
	err = a.deps.DB.QueryRow(ctx, `
		SELECT COUNT(1)
		FROM message_waits
		WHERE executed_tsk_cid IS NULL AND executed_msg_data->>'From' = $1
	`, kaddr.String()).Scan(&pendingMessages)
	if err != nil {
		// If there's an error (e.g., no rows), we can assume 0 pending messages,
		// but log the error for debugging.
		// Depending on strictness, one might want to return an error here.
		// For now, let's assume 0 if there's an issue, as the original code for WalletBalance
		// and StateAccountKey/StateLookupID would have already errored out for critical issues.
		log.Warnf("failed to get pending messages count for wallet %s (key: %s): %v", walletID, kaddr.String(), err)
		pendingMessages = 0 // Default to 0 on error
	}

	return WalletInfoShort{
		IDAddress:       iaddr.String(),
		KeyAddress:      kaddr.String(),
		Balance:         types.FIL(balance).Short(),
		PendingMessages: pendingMessages,
	}, nil
}
