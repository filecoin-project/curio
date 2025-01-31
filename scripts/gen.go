package main

import (
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	if err := cbg.WriteTupleEncodersToFile("lib/proofsvc/common/ledger_cbor_gen.go", "testing",
		common.Message{},
		common.Messages{},
	); err != nil {
		panic(err)
	}
}
