//go:build !skiff

package deps

import (
	"github.com/filecoin-project/curio/lib/apiconn"

	cliutil "github.com/filecoin-project/lotus/cli/util"
)

type apiInfo = apiconn.Info

func parseAPIInfo(s string) apiInfo {
	legacy := cliutil.ParseApiInfo(s)
	return apiconn.Info{Addr: legacy.Addr, Token: legacy.Token}
}
