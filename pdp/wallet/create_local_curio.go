//go:build !maxboom

package wallet

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func createPDPKeyLocal(_ context.Context, _ *harmonydb.DB) (*CreatedKey, error) {
	return nil, nil
}
