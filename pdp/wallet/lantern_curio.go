//go:build !maxboom

package wallet

import (
	"context"
	"errors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var ErrLanternOnly = errors.New("CreatePDPKeyLantern is only available in the Curio-PDP (maxboom) build")

func CreatePDPKeyLantern(_ context.Context, _ *harmonydb.DB) (*CreatedKey, error) {
	return nil, ErrLanternOnly
}
