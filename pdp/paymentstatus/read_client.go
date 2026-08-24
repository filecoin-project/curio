package paymentstatus

import (
	"context"
	"sync"

	"github.com/ethereum/go-ethereum/ethclient"
	erpc "github.com/ethereum/go-ethereum/rpc"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/lib/ethchain"
)

var log = logging.Logger("paymentstatus")

func glifRPCURL() (string, bool) {
	switch build.BuildType {
	case build.BuildMainnet:
		return "https://api.node.glif.io/rpc/v1", true
	case build.BuildCalibnet:
		return "https://api.calibration.node.glif.io/rpc/v1", true
	default:
		return "", false
	}
}

func dialGlifEthClient(ctx context.Context) (ethchain.EthClient, error) {
	url, ok := glifRPCURL()
	if !ok {
		return nil, xerrors.Errorf("no public read RPC for network %s", build.BuildTypeString())
	}

	rpcClient, err := erpc.DialContext(ctx, url)
	if err != nil {
		return nil, xerrors.Errorf("dial glif: %w", err)
	}
	client := &ethchain.ChainErrorWrap{EthClient: ethclient.NewClient(rpcClient)}
	if _, err := client.BlockNumber(ctx); err != nil {
		client.Close()
		return nil, xerrors.Errorf("glif block number: %w", err)
	}
	return client, nil
}

type readClientCache struct {
	mu     sync.Mutex
	client ethchain.EthClient
}

func (c *readClientCache) get(ctx context.Context) (ethchain.EthClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		return c.client, nil
	}

	client, err := dialGlifEthClient(ctx)
	if err != nil {
		return nil, err
	}

	log.Infow("payment status using Glif read RPC")
	c.client = client
	return client, nil
}

func (c *readClientCache) markUnhealthy() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
}

var glifReadClient readClientCache

// preferReadEthClient returns Glif when available. The second return value is
// false when Glif could not be used and the caller should stick with fallback.
func preferReadEthClient(ctx context.Context) (ethchain.EthClient, bool) {
	client, err := glifReadClient.get(ctx)
	if err != nil {
		log.Debugw("Glif read RPC unavailable, will use local eth client", "error", err)
		return nil, false
	}
	return client, true
}

func markGlifUnhealthy() {
	glifReadClient.markUnhealthy()
}
