//go:build !maxboom

package pdpnode

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func ensureMaxBoomBaseLayer(_ context.Context, _ *harmonydb.DB) error {
	return nil
}
