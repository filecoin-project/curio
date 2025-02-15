package main

import (
	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/curio/lib/proofsvc/common"
)

func main() {
	if err := cbg.WriteTupleEncodersToFile("lib/proofsvc/common/ledger_cbor_gen.go", "testing",
		common.Message{},
		common.Messages{},
	); err != nil {
		panic(err)
	}
}
