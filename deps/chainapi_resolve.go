package deps

import (
	"os"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
)

func resolveChainAPIInfo(cctx *cli.Context, apis config.ApisConfig) (*config.Dynamic[[]string], func(), error) {
	_ = cctx
	if v := os.Getenv("FULLNODE_API_INFO"); v != "" {
		return config.NewDynamic([]string{v}), func() {}, nil
	}
	if apis.ChainApiInfo != nil && len(apis.ChainApiInfo.Get()) > 0 {
		return apis.ChainApiInfo, func() {}, nil
	}
	return nil, nil, xerrors.Errorf("chain API not configured: set [APIs].ChainApiInfo or FULLNODE_API_INFO")
}
