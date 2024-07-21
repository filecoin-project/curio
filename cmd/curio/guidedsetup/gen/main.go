package main

import (
	"fmt"
	"os"

	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/curio/cmd/curio/guidedsetup"
)

func main() {
	err := gen.WriteMapEncodersToFile("./cbor_gen.go", "guidedsetup",
		guidedsetup.SectorInfo{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
