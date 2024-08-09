package paths

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/filecoin-project/go-state-types/abi"
)

var parentCacheDir string

func init() {
	if os.Getenv("FIL_PROOFS_PARENT_CACHE") != "" {
		parentCacheDir = os.Getenv("FIL_PROOFS_PARENT_CACHE")
	} else {
		parentCacheDir = "/var/tmp/filecoin-parents"
	}
}

func ParentsForProof(spt abi.RegisteredSealProof) (string, error) {
	switch spt {
	case abi.RegisteredSealProof_StackedDrg2KiBV1_1:
		return filepath.Join(parentCacheDir, "v28-sdr-parent-494d91dc80f2df5272c4b9e129bc7ade9405225993af9fe34e6542a39a47554b.cache"), nil
	case abi.RegisteredSealProof_StackedDrg512MiBV1_1:
		return filepath.Join(parentCacheDir, "v28-sdr-parent-7ba215a1d2345774ab90b8cb1158d296e409d6068819d7b8c7baf0b25d63dc34.cache"), nil
	case abi.RegisteredSealProof_StackedDrg32GiBV1_1:
		return filepath.Join(parentCacheDir, "v28-sdr-parent-21981246c370f9d76c7a77ab273d94bde0ceb4e938292334960bce05585dc117.cache"), nil
	default:
		return "", errors.New("unsupported proof type")
	}
}
