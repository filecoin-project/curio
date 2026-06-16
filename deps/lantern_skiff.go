//go:build skiff

package deps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	lanternd "github.com/Reiers/lantern/pkg/daemon"
	lanternwallet "github.com/Reiers/lantern/wallet"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/pdp/contract"
)

var lanternLog = logging.Logger("skiff/lantern")

type embeddedLantern struct {
	daemon  *lanternd.Daemon
	cancel  context.CancelFunc
	apiInfo string
}

var (
	embedOnce sync.Once
	embedInst *embeddedLantern
	embedErr  error
)

func resolveChainAPIInfo(cctx *cli.Context, configured []string) ([]string, func(), error) {
	if v := os.Getenv("FULLNODE_API_INFO"); v != "" {
		return []string{v}, func() {}, nil
	}
	if len(configured) > 0 {
		return configured, func() {}, nil
	}

	embedOnce.Do(func() {
		embedInst, embedErr = startEmbeddedLantern(cctx)
	})
	if embedErr != nil {
		return nil, nil, embedErr
	}
	return []string{embedInst.apiInfo}, embedInst.stop, nil
}

func startEmbeddedLantern(cctx *cli.Context) (*embeddedLantern, error) {
	network := lanternNetwork()
	if network == "" {
		return nil, xerrors.Errorf("embedded Lantern is not available for %s builds; set [APIs].ChainApiInfo or FULLNODE_API_INFO", build.BuildTypeString())
	}

	repoPath := cctx.String(FlagRepoPath)
	dataDir := filepath.Join(repoPath, "lantern")

	w, err := lanternwallet.New(cctx.Context, dataDir, "")
	if err != nil {
		return nil, xerrors.Errorf("lantern wallet: %w", err)
	}

	cfg := lanternd.Config{
		DataDir:           dataDir,
		Wallet:            w,
		RPCListen:         "127.0.0.1:0",
		EmbeddedMode:      true,
		Network:           network,
		FEVMPrefetchAddrs: fevmPrefetchAddrs(),
	}

	d, err := lanternd.New(cfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := d.Start(ctx); err != nil && err != context.Canceled {
			lanternLog.Errorw("embedded Lantern stopped", "error", err)
		}
	}()

	for i := 0; i < 3000; i++ {
		if d.Started() && d.RPCAddr() != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !d.Started() || d.RPCAddr() == "" {
		cancel()
		return nil, xerrors.Errorf("embedded Lantern RPC did not become ready")
	}

	apiInfo, err := d.FullNodeAPIInfo()
	if err != nil {
		cancel()
		return nil, xerrors.Errorf("lantern api info: %w", err)
	}

	lanternLog.Infow("embedded Lantern ready", "rpc", d.RPCAddr(), "network", network)
	return &embeddedLantern{
		daemon:  d,
		cancel:  cancel,
		apiInfo: apiInfo,
	}, nil
}

func (e *embeddedLantern) stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

func lanternNetwork() string {
	switch build.BuildType {
	case build.BuildMainnet:
		return "mainnet"
	case build.BuildCalibnet:
		return "calibration"
	default:
		return ""
	}
}

func fevmPrefetchAddrs() []string {
	c := contract.ContractAddresses()
	var addrs []string
	if c.PDPVerifier != (common.Address{}) {
		addrs = append(addrs, c.PDPVerifier.Hex())
	}
	if c.AllowedPublicRecordKeepers.FWSService != (common.Address{}) {
		addrs = append(addrs, c.AllowedPublicRecordKeepers.FWSService.Hex())
	}
	if c.AllowedPublicRecordKeepers.Simple != (common.Address{}) {
		addrs = append(addrs, c.AllowedPublicRecordKeepers.Simple.Hex())
	}
	return addrs
}

// SkiffChainAPIInfoHint documents how skiff resolves its chain backend.
func SkiffChainAPIInfoHint() string {
	return fmt.Sprintf("Skiff embeds Lantern on %s when ChainApiInfo is unset", build.BuildTypeString())
}
