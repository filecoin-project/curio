package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/pdp/contract"

	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

const providerLookupTimeout = 3 * time.Second

// Identity is the public node fingerprint shown on setup and login.
type Identity struct {
	Product    string         `json:"product"`
	Miners     []MinerIdentity `json:"miners,omitempty"`
	PDPWallet  *PDPWalletInfo `json:"pdpWallet,omitempty"`
	ProviderID *int64         `json:"providerId,omitempty"`
}

// MinerIdentity lists a miner actor and its control wallets from config.
type MinerIdentity struct {
	MinerAddresses       []string `json:"minerAddresses,omitempty"`
	PreCommitControl     []string `json:"preCommitControl,omitempty"`
	CommitControl        []string `json:"commitControl,omitempty"`
	DealPublishControl   []string `json:"dealPublishControl,omitempty"`
	TerminateControl     []string `json:"terminateControl,omitempty"`
}

// PDPWalletInfo is the configured PDP eth key address (never the private key).
type PDPWalletInfo struct {
	Address    string `json:"address"`
	FilAddress string `json:"filAddress,omitempty"`
}

type providerCache struct {
	mu        sync.Mutex
	addr      string
	id        int64
	hasID     bool
	fetchedAt time.Time
}

var sharedProviderCache = &providerCache{}

const providerCacheTTL = 5 * time.Minute

// buildIdentity collects public identifiers from deps. Eth/registry failures leave ProviderID empty.
func buildIdentity(ctx context.Context, d *deps.Deps) Identity {
	id := Identity{Product: productName()}
	if d == nil || d.Cfg == nil {
		return id
	}

	if d.Cfg.Addresses != nil {
		for _, entry := range d.Cfg.Addresses.Get() {
			m := MinerIdentity{
				MinerAddresses:     append([]string(nil), entry.MinerAddresses...),
				PreCommitControl:   append([]string(nil), entry.PreCommitControl...),
				CommitControl:      append([]string(nil), entry.CommitControl...),
				DealPublishControl: append([]string(nil), entry.DealPublishControl...),
				TerminateControl:   append([]string(nil), entry.TerminateControl...),
			}
			if len(m.MinerAddresses) == 0 && len(m.PreCommitControl) == 0 &&
				len(m.CommitControl) == 0 && len(m.DealPublishControl) == 0 &&
				len(m.TerminateControl) == 0 {
				continue
			}
			id.Miners = append(id.Miners, m)
		}
	}

	if d.DB != nil {
		var addr string
		err := d.DB.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addr)
		if err == nil && addr != "" {
			w := &PDPWalletInfo{Address: addr}
			if fil, ferr := ethToFilAddress(addr); ferr == nil {
				w.FilAddress = fil
			}
			id.PDPWallet = w
			if pid, ok := lookupProviderID(ctx, d, addr); ok {
				id.ProviderID = &pid
			}
		} else if err != nil && err != pgx.ErrNoRows {
			// leave wallet empty; identity still returns product/miners
		}
	}
	return id
}

// ethToFilAddress converts a 0x address to a Filecoin delegated address string.
func ethToFilAddress(ethAddr string) (string, error) {
	ea, err := ethtypes.ParseEthAddress(ethAddr)
	if err != nil {
		return "", err
	}
	fa, err := ea.ToFilecoinAddress()
	if err != nil {
		return "", err
	}
	return fa.String(), nil
}

// lookupProviderID returns the on-chain provider id when registered. Uses a short cache.
func lookupProviderID(ctx context.Context, d *deps.Deps, addr string) (int64, bool) {
	if d.EthClient == nil {
		return 0, false
	}

	sharedProviderCache.mu.Lock()
	if sharedProviderCache.addr == addr && time.Since(sharedProviderCache.fetchedAt) < providerCacheTTL {
		id, ok := sharedProviderCache.id, sharedProviderCache.hasID
		sharedProviderCache.mu.Unlock()
		return id, ok
	}
	sharedProviderCache.mu.Unlock()

	cctx, cancel := context.WithTimeout(ctx, providerLookupTimeout)
	defer cancel()

	eclient, err := d.EthClient.Val()
	if err != nil {
		return 0, false
	}
	registryAddr, err := contract.ServiceRegistryAddress()
	if err != nil {
		return 0, false
	}
	registry, err := contract.NewServiceProviderRegistry(registryAddr, eclient)
	if err != nil {
		return 0, false
	}
	pid, err := registry.GetProviderIdByAddress(&bind.CallOpts{Context: cctx}, common.HexToAddress(addr))
	if err != nil || pid == nil {
		sharedProviderCache.mu.Lock()
		sharedProviderCache.addr = addr
		sharedProviderCache.hasID = false
		sharedProviderCache.fetchedAt = time.Now()
		sharedProviderCache.mu.Unlock()
		return 0, false
	}
	id := pid.Int64()
	sharedProviderCache.mu.Lock()
	sharedProviderCache.addr = addr
	sharedProviderCache.id = id
	sharedProviderCache.hasID = true
	sharedProviderCache.fetchedAt = time.Now()
	sharedProviderCache.mu.Unlock()
	return id, true
}

func (a *Auth) handleIdentity(w http.ResponseWriter, r *http.Request) {
	id := buildIdentity(r.Context(), a.deps)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(id)
}
