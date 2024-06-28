package proof

import (
	"bytes"
	"encoding/json"
	"github.com/filecoin-project/filecoin-ffi/cgo"
	"os"
	"testing"
)

func TestDecode(t *testing.T) {
	binFile := "../../extern/supra_seal/demos/c2-test/resources/test/commit-phase1-output"

	rawData, err := os.ReadFile(binFile)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := DecodeCommit1OutRaw(bytes.NewReader(rawData))
	if err != nil {
		t.Fatal(err)
	}

	p1o, err := json.Marshal(dec)
	if err != nil {
		t.Fatal(err)
	}

	var proverID = [32]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	pba := cgo.AsByteArray32(proverID[:])

	_, err = cgo.SealCommitPhase2(cgo.AsSliceRefUint8(p1o), uint64(0), &pba)
	if err != nil {
		t.Fatal(err)
	}
}
