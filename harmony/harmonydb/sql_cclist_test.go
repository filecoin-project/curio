package harmonydb_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	"github.com/filecoin-project/go-state-types/abi"
)

func TestCCList(t *testing.T) {
	// this tests outputs the cc cid list defined in 20240611-snap-pipeline.sql

	numProofs := abi.RegisteredSealProof_StackedDrg64GiBV1_1_Feat_SyntheticPoRep + 1

	pstrs := make([]string, numProofs)

	for spt, si := range abi.SealProofInfos {
		ccCid := zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(si.SectorSize).Unpadded())
		pstrs[spt] = fmt.Sprintf("(%d, '%s'),\n", spt, ccCid.String())
	}

	fmt.Println(strings.Join(pstrs, ""))
}
