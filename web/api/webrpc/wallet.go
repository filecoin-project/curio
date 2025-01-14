package webrpc

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/exp/maps"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var walletOnce sync.Once

func (a *WebRPC) WalletName(ctx context.Context, id string) (string, error) {
	walletOnce.Do(func() {
		populateWalletFriendlyNames(a.deps.Cfg.Addresses, a.deps.DB)
	})
	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	return walletFriendlyNames[id], nil
}

func (a *WebRPC) WalletNameChange(ctx context.Context, id string, newName string) error {
	_, err := a.deps.DB.Exec(ctx, `UDPATE wallet_names SET name = $1 WHERE id = $2`, newName, id)
	if err != nil {
		log.Errorf("failed to set wallet name for %s: %s", id, err)
		return err
	}
	walletFriendlyNamesLock.Lock()
	defer walletFriendlyNamesLock.Unlock()
	walletFriendlyNames[id] = newName
	return nil
}

var walletFriendlyNames = map[string]string{}
var walletFriendlyNamesLock sync.Mutex

func populateWalletFriendlyNames(addrGrps []config.CurioAddresses, db *harmonydb.DB) {
	type nx struct {
		purposes map[string]bool
		miner    map[string]bool
	}
	ex := map[string]*nx{}
	all_purposes := []string{"PC_Ctrl", "Cmt_Ctrl", "Term_Ctrl"}
	allMiners := map[string]bool{}
	for _, addrList := range addrGrps {
		n := map[string][]string{
			"PC_Ctrl":   addrList.PreCommitControl,
			"Cmt_Ctrl":  addrList.CommitControl,
			"Term_Ctrl": addrList.TerminateControl,
		}
		for name, addrs := range n {
			for i, addr := range addrs {
				a, err := address.NewFromString(addr)
				if err != nil {
					continue
				}
				allMiners[a.String()] = true

				if len(addrs) > 1 {
					name = fmt.Sprintf("%s%d", name, i)
				}
				if res, ok := ex[a.String()]; ok {
					res.purposes[name] = true
					for _, miner := range addrList.MinerAddresses {
						res.miner[miner] = true
					}
				} else {
					var t = map[string]bool{}
					for _, miner := range addrList.MinerAddresses {
						t[miner] = true
					}
					ex[a.String()] = &nx{purposes: map[string]bool{name: true}, miner: t}
				}
			}
		}
	}
	for addr, nx := range ex {
		purpose := ""
		miner := ""
		if len(nx.purposes) != len(all_purposes) { // impossible to be 0
			purpose = strings.Join(maps.Keys(nx.purposes), ",")
		}
		if len(nx.miner) != len(allMiners) { // impoossible to be 0
			miner = "_" + strings.Join(maps.Keys(nx.miner), ",")
		}
		if purpose == "" && miner == "" {
			purpose = "all"
		}
		walletFriendlyNames[addr] = purpose + miner + "Wallet"
	}

	var idNames []struct {
		ID   string
		Name string
	}
	err := db.Select(context.Background(), &idNames, `SELECT wallet_id as ID, name FROM wallet_names`)
	if err != nil {
		log.Errorf("failed to get wallet names: %s", err)
		return
	}
	for _, idName := range idNames {
		walletFriendlyNames[idName.ID] = idName.Name
	}
}
