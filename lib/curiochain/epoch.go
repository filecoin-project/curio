package curiochain

import (
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/types"
)

func EpochTime(curr *types.TipSet, e abi.ChainEpoch) time.Time {
	diff := int64(buildconstants.BlockDelaySecs) * int64(e-curr.Height())
	curTs := curr.MinTimestamp() // unix seconds

	return time.Unix(int64(curTs)+diff, 0)
}
