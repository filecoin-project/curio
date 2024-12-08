package webrpc

import (
	"context"
	"fmt"
	"time"

	"github.com/hako/durafmt"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/build/buildconstants"
)

func (a *WebRPC) EpochPretty(ctx context.Context, e abi.ChainEpoch) (string, error) {
	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return "", err
	}

	curr := head.Height()

	switch {
	case curr > e:
		return fmt.Sprintf("%d (%s ago)", e, durafmt.Parse(time.Second*time.Duration(int64(buildconstants.BlockDelaySecs)*int64(curr-e))).LimitFirstN(2)), nil
	case curr == e:
		return fmt.Sprintf("%d (now)", e), nil
	case curr < e:
		return fmt.Sprintf("%d (in %s)", e, durafmt.Parse(time.Second*time.Duration(int64(buildconstants.BlockDelaySecs)*int64(e-curr))).LimitFirstN(2)), nil
	}

	return "", xerrors.Errorf("this is awkward")
}
