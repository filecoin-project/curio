package webrpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/urlhelper"

	"github.com/filecoin-project/lotus/chain/types"
)

const (
	pdpGuideMinStorageBytes         = 20 * 1024 * 1024 * 1024  // 20 GiB
	pdpGuideRecommendedStorageBytes = 100 * 1024 * 1024 * 1024 // 100 GiB
	pdpGuideStorageDocsURL          = "https://docs.curiostorage.org/curio-pdp#storage"
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
	OK          bool   `json:"ok"`
	Configured  bool   `json:"configured"`
	Address     string `json:"address,omitempty"`
	FilAddress  string `json:"filAddress,omitempty"`
	Balance     string `json:"balance,omitempty"`
	Funded      bool   `json:"funded"`
	ActorExists bool   `json:"actorExists"`
	Detail      string `json:"detail,omitempty"`
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
	DocsURL          string `json:"docsURL"`
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
	TunnelInstalled  bool   `json:"tunnelInstalled"`
	TunnelRunning    bool   `json:"tunnelRunning"`
	TunnelDetail     string `json:"tunnelDetail,omitempty"`
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
		Configured:  status.Configured,
		Address:     status.Address,
		FilAddress:  status.FilAddress,
		Balance:     status.Balance,
		Funded:      status.Funded,
		ActorExists: status.ActorExists,
	}
	switch {
	case !status.Configured:
		out.Detail = "No PDP wallet configured. Create a key or import a hex key / lotus wallet export."
	case !status.Funded:
		out.Detail = "Wallet configured but unfunded. Send FIL/tFIL to the address before registering."
	default:
		out.OK = true
		out.Detail = "Wallet configured and funded."
	}
	return out
}

func (a *WebRPC) pdpGuideStorage(ctx context.Context) PDPGuideStorageStatus {
	out := PDPGuideStorageStatus{
		DocsURL: pdpGuideStorageDocsURL,
	}

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

	tunnel := cloudflareTunnelStatus(a.tunnelWorkDir())
	out.TunnelInstalled = tunnel.Installed
	out.TunnelRunning = tunnel.Running
	out.TunnelDetail = tunnel.Detail

	if !out.DomainConfigured {
		out.SuggestTunnel = true
		out.Detail = "HTTP.DomainName is not set. Configure a public DNS name or use a Cloudflare Tunnel."
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
		out.Detail = "Domain is configured but not reachable from this node. A Cloudflare Tunnel can expose the PDP API without opening inbound ports."
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
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		// 2xx/3xx/4xx means something is listening on the public name.
		// Auth/method errors still prove reachability.
		return true, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, pingURL)
	}
	return false, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, pingURL)
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// --- Cloudflare Tunnel ---

type tunnelRuntimeStatus struct {
	Installed bool
	Running   bool
	Detail    string
}

var (
	tunnelMu      sync.Mutex
	tunnelCmd     *exec.Cmd
	tunnelLogPath string
)

func (a *WebRPC) tunnelWorkDir() string {
	if bls, ok := a.Deps.LocalPaths.(*paths.BasicLocalStorage); ok && bls.PathToJSON != "" {
		return filepath.Join(filepath.Dir(bls.PathToJSON), "cloudflared")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "curio-cloudflared")
	}
	return filepath.Join(home, ".curio", "cloudflared")
}

func cloudflaredBinaryPath(dir string) string {
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name = "cloudflared.exe"
	}
	return filepath.Join(dir, name)
}

func cloudflareTunnelStatus(dir string) tunnelRuntimeStatus {
	bin := cloudflaredBinaryPath(dir)
	st := tunnelRuntimeStatus{Installed: fileExists(bin)}

	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	if tunnelCmd != nil && tunnelCmd.Process != nil {
		// Process is still tracked; assume running unless Wait already finished.
		if tunnelCmd.ProcessState == nil || !tunnelCmd.ProcessState.Exited() {
			st.Running = true
			st.Detail = "cloudflared tunnel process is running."
			return st
		}
	}
	if st.Installed {
		st.Detail = "cloudflared is installed but not running."
	} else {
		st.Detail = "cloudflared is not installed."
	}
	return st
}

// PDPGuideConfigureCloudflareTunnel downloads cloudflared (if needed) and starts it with the given tunnel token.
func (a *WebRPC) PDPGuideConfigureCloudflareTunnel(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("cloudflare tunnel token is required")
	}

	dir := a.tunnelWorkDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating tunnel work dir: %w", err)
	}

	bin := cloudflaredBinaryPath(dir)
	if !fileExists(bin) {
		if err := downloadCloudflared(ctx, bin); err != nil {
			return "", err
		}
	}

	tokenPath := filepath.Join(dir, "tunnel.token")
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("writing tunnel token: %w", err)
	}

	if err := startCloudflaredTunnel(bin, token, dir); err != nil {
		return "", err
	}

	msg := "Cloudflare Tunnel started. Point the tunnel's public hostname at this node's HTTP listen address, and set HTTP.DomainName to that hostname."
	if domain := strings.TrimSpace(a.Deps.Cfg.HTTP.DomainName); domain != "" {
		msg = fmt.Sprintf("Cloudflare Tunnel started. Ensure the tunnel hostname matches HTTP.DomainName (%s) and forwards to this node's HTTP listen address.", domain)
	}
	return msg, nil
}

// PDPGuideStopCloudflareTunnel stops a tunnel process started by the guide.
func (a *WebRPC) PDPGuideStopCloudflareTunnel(ctx context.Context) error {
	_ = ctx
	tunnelMu.Lock()
	defer tunnelMu.Unlock()
	if tunnelCmd == nil || tunnelCmd.Process == nil {
		return fmt.Errorf("no cloudflared tunnel process is managed by this node")
	}
	if err := tunnelCmd.Process.Signal(os.Interrupt); err != nil {
		_ = tunnelCmd.Process.Kill()
	}
	tunnelCmd = nil
	return nil
}

func startCloudflaredTunnel(bin, token, dir string) error {
	tunnelMu.Lock()
	defer tunnelMu.Unlock()

	if tunnelCmd != nil && tunnelCmd.Process != nil && (tunnelCmd.ProcessState == nil || !tunnelCmd.ProcessState.Exited()) {
		return fmt.Errorf("cloudflared tunnel is already running")
	}

	logPath := filepath.Join(dir, "cloudflared.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening cloudflared log: %w", err)
	}

	cmd := exec.Command(bin, "tunnel", "--no-autoupdate", "run", "--token", token)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("starting cloudflared: %w", err)
	}

	tunnelCmd = cmd
	tunnelLogPath = logPath

	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		tunnelMu.Lock()
		if tunnelCmd == cmd {
			tunnelCmd = nil
		}
		tunnelMu.Unlock()
	}()

	return nil
}

func downloadCloudflared(ctx context.Context, dest string) error {
	url, err := cloudflaredDownloadURL()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading cloudflared: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading cloudflared: HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("writing cloudflared: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func cloudflaredDownloadURL() (string, error) {
	// Official release assets: https://github.com/cloudflare/cloudflared/releases
	// Bare binaries only (no archive extraction).
	const base = "https://github.com/cloudflare/cloudflared/releases/latest/download/"
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return base + "cloudflared-linux-amd64", nil
	case "linux/arm64":
		return base + "cloudflared-linux-arm64", nil
	default:
		return "", fmt.Errorf("automatic cloudflared download is not supported on %s/%s; install cloudflared into the node repo cloudflared/ directory manually", runtime.GOOS, runtime.GOARCH)
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
