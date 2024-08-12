package proof

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
)

var tauxName = "t_aux"

// EnsureTauxForType ensures that the t_aux file exists in the specified path for the specified seal proof type.
// If the file does not exist, it will be created with the default values for the specified seal proof type.
func EnsureTauxForType(spt abi.RegisteredSealProof, path string) error {
	// check if the file exists
	auxPath := filepath.Join(path, tauxName)
	if _, err := os.Stat(auxPath); !os.IsNotExist(err) {
		return nil // file exists, nothing to do
	}

	// no t_aux, create one that will satisfy rust-fil-proofs

	if spt != abi.RegisteredSealProof_StackedDrg32GiBV1_1 && spt != abi.RegisteredSealProof_StackedDrg32GiBV1_1_Feat_SyntheticPoRep {
		return xerrors.Errorf("unsupported seal proof type: %v", spt)
	}

	treeRRowsToDiscard := uint64(2)
	if envset := os.Getenv("FIL_PROOFS_ROWS_TO_DISCARD"); envset != "" {
		_, err := fmt.Sscan(envset, &treeRRowsToDiscard)
		if err != nil {
			return xerrors.Errorf("failed to parse FIL_PROOFS_ROWS_TO_DISCARD: %w", err)
		}
	}

	taux := TemporaryAux{
		Labels: TAuxLabels{},
		TreeDConfig: StoreConfig{
			Path:          path,
			ID:            "tree-d",
			Size:          iptr(2147483647),
			RowsToDiscard: 0,
		},
		TreeRConfig: StoreConfig{
			Path:          path,
			ID:            "tree-r-last",
			Size:          iptr(153391689),
			RowsToDiscard: treeRRowsToDiscard,
		},
		TreeCConfig: StoreConfig{
			Path:          path,
			ID:            "tree-c",
			Size:          iptr(153391689),
			RowsToDiscard: 0,
		},
	}

	for i := 0; i < 11; i++ {
		taux.Labels.Labels = append(taux.Labels.Labels, StoreConfig{
			Path:          path,
			ID:            fmt.Sprintf("layer-%d", i+1),
			Size:          iptr(1073741824),
			RowsToDiscard: 0,
		})
	}

	var buf bytes.Buffer
	if err := EncodeTAux(&buf, taux); err != nil {
		return xerrors.Errorf("failed to encode taux: %w", err)
	}

	if err := os.WriteFile(auxPath, buf.Bytes(), 0644); err != nil {
		return xerrors.Errorf("failed to write taux file: %w", err)
	}

	return nil
}

func iptr(i uint64) *uint64 {
	return &i
}
