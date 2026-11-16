package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/pdp/contract"
)

const providerLookupTimeout = 3 * time.Second

// Identity is the minimal public fingerprint shown on setup and login:
// which product and which miner/provider this GUI is for.
type Identity struct {
	Product    string   `json:"product"`
	Miners     []string `json:"miners,omitempty"`
	ProviderID *int64   `json:"providerId,omitempty"`
}

// buildIdentity returns the cached identity, computing it once on first call.
func (a *Auth) buildIdentity() Identity {
	a.identityOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), providerLookupTimeout)
		defer cancel()
		a.identity = computeIdentity(ctx, a.deps)
	})
	return a.identity
}

// computeIdentity returns product plus miner actor IDs / provider ID only.
func computeIdentity(ctx context.Context, d *deps.Deps) Identity {
	id := Identity{Product: productName()}
	if d == nil || d.Cfg == nil {
		return id
	}

	if d.Cfg.Addresses != nil {
		seen := map[string]struct{}{}
		for _, entry := range d.Cfg.Addresses.Get() {
			for _, m := range entry.MinerAddresses {
				if m == "" {
					continue
				}
				if _, ok := seen[m]; ok {
					continue
				}
				seen[m] = struct{}{}
				id.Miners = append(id.Miners, m)
			}
		}
	}

	if d.DB != nil {
		var addr string
		err := d.DB.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addr)
		if err == nil && addr != "" {
			if pid, ok := lookupProviderID(ctx, d, addr); ok {
				id.ProviderID = &pid
			}
		}
	}
	return id
}

// lookupProviderID returns the on-chain provider id when registered.
func lookupProviderID(ctx context.Context, d *deps.Deps, addr string) (int64, bool) {
	if d.EthClient == nil {
		return 0, false
	}

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
	pid, err := registry.GetProviderIdByAddress(&bind.CallOpts{Context: ctx}, common.HexToAddress(addr))
	if err != nil || pid == nil {
		return 0, false
	}
	return pid.Int64(), true
}

func (a *Auth) handleIdentity(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(a.buildIdentity())
}
