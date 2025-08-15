package proof

import (
	"os"
	"path/filepath"

	"golang.org/x/xerrors"
)

const PauxFile = "p_aux"

func ReadPAux(cache string) ([32]byte, [32]byte, error) {
	commCcommRLast, err := os.ReadFile(filepath.Join(cache, PauxFile))
	if err != nil {
		return [32]byte{}, [32]byte{}, err
	}

	if len(commCcommRLast) != 64 {
		return [32]byte{}, [32]byte{}, xerrors.Errorf("invalid commCcommRLast length %d", len(commCcommRLast))
	}

	var commC, commRLast [32]byte
	copy(commC[:], commCcommRLast[:32])
	copy(commRLast[:], commCcommRLast[32:])

	return commC, commRLast, nil
}

func WritePAux(cache string, commC, commRLast [32]byte) error {
	commCcommRLast := make([]byte, 64)
	copy(commCcommRLast[:32], commC[:])
	copy(commCcommRLast[32:], commRLast[:])

	return os.WriteFile(filepath.Join(cache, PauxFile), commCcommRLast, 0644)
}