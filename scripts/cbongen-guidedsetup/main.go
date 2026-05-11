// Generates CBOR marshal/unmarshal code for guidedsetup.SectorInfo.
// Run from repo root: go run ./scripts/cbongen-guidedsetup
package main

import (
	"fmt"
	"os"
	"path/filepath"

	cborgen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/curio/cmd/curio/guidedsetup"
)

func main() {
	// Output to shared_cbor_gen.go in current directory (go generate runs from cmd/curio/guidedsetup)
	genName := filepath.Join(".", "shared_cbor_gen.go")

	fmt.Print("Generating Cbor Marshal/Unmarshal for guidedsetup.SectorInfo...")
	if err := cborgen.WriteMapEncodersToFile(genName, "guidedsetup", guidedsetup.SectorInfo{}); err != nil {
		fmt.Println("Failed:")
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("Done.")
}
