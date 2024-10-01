package chainstate

import (
	"context"

	"github.com/filecoin-project/lotus/api"
)

type versionAPI interface {
	StateGetNetworkParams(context.Context) (*api.NetworkParams, error)
}

func Version(ctx context.Context, api versionAPI) (apitypes.NetworkVersion, error) {
	v, err := api.StateGetNetworkParams(ctx)
	if err != nil {
		return "", err
	}
	return v.NetworkName, nil
}
