package proof

import (
	"bytes"
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

	t.Logf("Decoded: %+v", dec)
}
