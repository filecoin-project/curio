//go:build !skiff

package deps

import cliutil "github.com/filecoin-project/lotus/cli/util"

func isChainAPIVeryVerbose() bool {
	return cliutil.IsVeryVerbose
}
