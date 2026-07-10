//go:build !skiff

package pdpnode

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func ensureSkiffBaseLayer(_ context.Context, _ *harmonydb.DB) error {
	return nil
}
