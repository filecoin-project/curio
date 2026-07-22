package webrpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/filecoin-project/curio/lib/urlhelper"
	"github.com/filecoin-project/curio/pdp"

	"github.com/filecoin-project/lotus/chain/types"
)

const (
	pdpGuideMinStorageBytes         = 20 * 1024 * 1024 * 1024  // 20 GiB
	pdpGuideRecommendedStorageBytes = 100 * 1024 * 1024 * 1024 // 100 GiB
	pdpGuideReachabilityTimeout     = 8 * time.Second
)

// PDPGuideStatus is the server-verified checklist for PDP readiness.
type PDPGuideStatus struct {
	Wallet   PDPGuideWalletStatus   `json:"wallet"`
	Storage  PDPGuideStorageStatus  `json:"storage"`
	DNS      PDPGuideDNSStatus      `json:"dns"`
	Registry PDPGuideRegistryStatus `json:"registry"`
}

type PDPGuideWalletStatus struct {
	OK           bool   `json:"ok"`
	Configured   bool   `json:"configured"`
	Address      string `json:"address,omitempty"`
	FilAddress   string `json:"filAddress,omitempty"`
	Balance      string `json:"balance,omitempty"`
	BalanceKnown bool   `json:"balanceKnown"`
	Funded       bool   `json:"funded"`
	ActorExists  bool   `json:"actorExists"`
	Detail       string `json:"detail,omitempty"`
}

type PDPGuideStorageStatus struct {
	OK               bool   `json:"ok"`
	PathCount        int    `json:"pathCount"`
	AvailableBytes   int64  `json:"availableBytes"`
	CapacityBytes    int64  `json:"capacityBytes"`
	AvailableHuman   string `json:"availableHuman"`
	CapacityHuman    string `json:"capacityHuman"`
	MeetsMinimum     bool   `json:"meetsMinimum"`
	MeetsRecommended bool   `json:"meetsRecommended"`
	Detail           string `json:"detail,omitempty"`
}

type PDPGuideDNSStatus struct {
	OK               bool   `json:"ok"`
	DomainConfigured bool   `json:"domainConfigured"`
	DomainName       string `json:"domainName,omitempty"`
	ServiceURL       string `json:"serviceURL,omitempty"`
	HTTPEnabled      bool   `json:"httpEnabled"`
	Reachable        bool   `json:"reachable"`
	ReachableDetail  string `json:"reachableDetail,omitempty"`
	SuggestTunnel    bool   `json:"suggestTunnel"`
	Detail           string `json:"detail,omitempty"`
}

type PDPGuideRegistryStatus struct {
	OK         bool   `json:"ok"`
	Registered bool   `json:"registered"`
	ProviderID int64  `json:"providerId,omitempty"`
	Name       string `json:"name,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

func (a *WebRPC) PDPGuideStatus(ctx context.Context) (*PDPGuideStatus, error) {
	return &PDPGuideStatus{
		Wallet:   a.pdpGuideWallet(ctx),
		Storage:  a.pdpGuideStorage(ctx),
		DNS:      a.pdpGuideDNS(ctx),
		Registry: a.pdpGuideRegistry(ctx),
	}, nil
}

func (a *WebRPC) pdpGuideWallet(ctx context.Context) PDPGuideWalletStatus {
	status, err := a.PDPKeyStatus(ctx)
	if err != nil {
		return PDPGuideWalletStatus{Detail: err.Error()}
	}
	out := PDPGuideWalletStatus{
		Configured:   status.Configured,
		Address:      status.Address,
		FilAddress:   status.FilAddress,
		Balance:      status.Balance,
		BalanceKnown: status.BalanceKnown,
		Funded:       status.Funded,
		ActorExists:  status.ActorExists,
	}
	if status.Configured && !status.BalanceKnown {
		out.Balance = "Err"
	}
	switch {
	case !status.Configured:
		out.Detail = "No PDP wallet configured. Create a key or import a hex key / lotus wallet export."
	case !status.BalanceKnown:
		out.Detail = "Wallet configured but balance could not be fetched from the chain."
	case !status.Funded:
		out.Detail = "Wallet configured but unfunded. Send FIL/tFIL to the address before registering."
	default:
		out.OK = true
		out.Detail = "Wallet configured and funded."
	}
	return out
}

func (a *WebRPC) pdpGuideStorage(ctx context.Context) PDPGuideStorageStatus {
	out := PDPGuideStorageStatus{}

	type row struct {
		Available int64 `db:"available"`
		Capacity  int64 `db:"capacity"`
	}
	var rows []row
	err := a.Deps.DB.Select(ctx, &rows, `
		SELECT COALESCE(available, 0) AS available, COALESCE(capacity, 0) AS capacity
		FROM storage_path
		WHERE can_store = true`)
	if err != nil {
		out.Detail = fmt.Sprintf("Failed to query storage paths: %v", err)
		return out
	}

	out.PathCount = len(rows)
	for _, r := range rows {
		out.AvailableBytes += r.Available
		out.CapacityBytes += r.Capacity
	}
	out.AvailableHuman = types.SizeStr(types.NewInt(uint64(max64(out.AvailableBytes, 0))))
	out.CapacityHuman = types.SizeStr(types.NewInt(uint64(max64(out.CapacityBytes, 0))))
	out.MeetsMinimum = out.AvailableBytes >= pdpGuideMinStorageBytes
	out.MeetsRecommended = out.AvailableBytes >= pdpGuideRecommendedStorageBytes
	out.OK = out.MeetsMinimum

	switch {
	case out.PathCount == 0:
		out.Detail = "No store-capable storage paths found. Mount writable disks under /data (Curio-PDP) or add storage paths."
	case !out.MeetsMinimum:
		out.Detail = fmt.Sprintf("Only %s available (need at least 20 GiB).", out.AvailableHuman)
	case !out.MeetsRecommended:
		out.Detail = fmt.Sprintf("%s available — meets the 20 GiB minimum, but under the 100 GiB recommended capacity.", out.AvailableHuman)
	default:
		out.Detail = fmt.Sprintf("%s available across %d path(s).", out.AvailableHuman, out.PathCount)
	}
	return out
}

func (a *WebRPC) pdpGuideDNS(ctx context.Context) PDPGuideDNSStatus {
	domain := strings.TrimSpace(a.Deps.Cfg.HTTP.DomainName)
	out := PDPGuideDNSStatus{
		DomainName:       domain,
		DomainConfigured: domain != "",
		HTTPEnabled:      a.Deps.Cfg.HTTP.Enable,
	}

	if !out.DomainConfigured {
		out.SuggestTunnel = true
		out.Detail = "HTTP.DomainName is not set. Configure a public DNS name. A tunnel (for example Cloudflare Tunnel) is one possible design if inbound ports are blocked."
		return out
	}

	serviceURL, err := urlhelper.GetExternalURL(&a.Deps.Cfg.HTTP)
	if err != nil {
		out.Detail = fmt.Sprintf("Domain is set but service URL could not be built: %v", err)
		out.SuggestTunnel = true
		return out
	}
	out.ServiceURL = serviceURL.String()

	reachable, detail := probePDPReachability(ctx, serviceURL.String())
	out.Reachable = reachable
	out.ReachableDetail = detail
	out.OK = reachable
	out.SuggestTunnel = !reachable
	if reachable {
		out.Detail = "Public endpoint responds to /pdp/ping."
	} else {
		out.Detail = "Domain is configured but not reachable from this node. A tunnel (for example Cloudflare Tunnel) is one possible design to expose the PDP API without opening inbound ports."
	}
	return out
}

func (a *WebRPC) pdpGuideRegistry(ctx context.Context) PDPGuideRegistryStatus {
	status, err := a.FSRegistryStatus(ctx)
	if err != nil {
		// Missing wallet is expected before setup.
		if strings.Contains(err.Error(), "no PDP key") {
			return PDPGuideRegistryStatus{Detail: "Register after configuring a funded PDP wallet and public DNS."}
		}
		return PDPGuideRegistryStatus{Detail: err.Error()}
	}
	if status == nil {
		return PDPGuideRegistryStatus{Detail: "Not registered with the Filecoin Onchain Cloud service provider registry."}
	}
	detail := fmt.Sprintf("Registered as %q (provider id %d).", status.Name, status.ID)
	if !status.Active {
		detail += " Provider is inactive."
	}
	return PDPGuideRegistryStatus{
		OK:         true,
		Registered: true,
		ProviderID: status.ID,
		Name:       status.Name,
		Detail:     detail,
	}
}

func probePDPReachability(ctx context.Context, serviceURL string) (bool, string) {
	pingURL := strings.TrimRight(serviceURL, "/") + "/pdp/ping"
	reqCtx, cancel := context.WithTimeout(ctx, pdpGuideReachabilityTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, pingURL, nil)
	if err != nil {
		return false, err.Error()
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return false, fmt.Sprintf("failed to read %s: %v", pingURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, pingURL)
	}
	if string(body) != pdp.PingOKBody {
		return false, fmt.Sprintf("unexpected body from %s: %q (want %q)", pingURL, string(body), pdp.PingOKBody)
	}
	return true, fmt.Sprintf("HTTP 200 with expected body from %s", pingURL)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
