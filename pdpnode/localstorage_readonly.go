package pdpnode

import (
	"github.com/filecoin-project/curio/lib/paths"
)

func newReadonlyLocalStorage() paths.LocalStorage {
	return paths.NewReadonlyLocalStorage()
}
