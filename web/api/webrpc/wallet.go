package webrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
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
		AddedAt    time.Time `db:"added_at"`
	}

	err := a.deps.DB.Select(ctx, &messages, `SELECT signed_message_cid, added_at FROM message_waits WHERE executed_tsk_cid IS NULL ORDER BY added_at DESC`)
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
