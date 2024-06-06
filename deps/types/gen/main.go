package main

import (
	"fmt"
	"os"

	gen "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/curio/deps/types"
)

func main() {
	err := gen.WriteTupleEncodersToFile("./deps/types/cbor_gen.go", "types",
		types.BlockHeader{},
		types.Ticket{},
		types.ElectionProof{},
		types.Message{},
		types.SignedMessage{},
		types.MsgMeta{},
		types.ActorV4{},
		types.ActorV5{},
		// types.MessageReceipt{}, // Custom serde to deal with versioning.
		types.BlockMsg{},
		types.ExpTipSet{},
		types.BeaconEntry{},
		types.StateRoot{},
		types.StateInfo0{},
		types.Event{},
		types.EventEntry{},
		// Tracing
		types.GasTrace{},
		types.ActorTrace{},
		types.MessageTrace{},
		types.ReturnTrace{},
		types.ExecutionTrace{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	/* obsolete from lotus?
	err = gen.WriteMapEncodersToFile("./paychmgr/cbor_gen.go", "paychmgr",
		paychmgr.VoucherInfo{},
		paychmgr.ChannelInfo{},
		paychmgr.MsgInfo{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteMapEncodersToFile("./api/cbor_gen.go", "api",
		api.PaymentInfo{},
		api.SealedRef{},
		api.SealedRefs{},
		api.SealTicket{},
		api.SealSeed{},
		api.SectorPiece{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteTupleEncodersToFile("./node/hello/cbor_gen.go", "hello",
		hello.HelloMessage{},
		hello.LatencyMessage{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteTupleEncodersToFile("./chain/market/cbor_gen.go", "market",
		market.FundedAddressState{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteTupleEncodersToFile("./chain/exchange/cbor_gen.go", "exchange",
		exchange.Request{},
		exchange.Response{},
		exchange.CompactedMessagesCBOR{},
		exchange.BSTipSet{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteMapEncodersToFile("./storage/sealer/storiface/cbor_gen.go", "storiface",
		storiface.CallID{},
		storiface.SecDataHttpHeader{},
		storiface.SectorLocation{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteMapEncodersToFile("./storage/pipeline/piece/cbor_gen.go", "piece",
		piece.PieceDealInfo{},
		piece.DealSchedule{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = gen.WriteMapEncodersToFile("./storage/sealer/cbor_gen.go", "sealer",
		sectorstorage.Call{},
		sectorstorage.WorkState{},
		sectorstorage.WorkID{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = gen.WriteMapEncodersToFile("./cmd/lotus-shed/shedgen/cbor_gen.go", "shedgen",
		shedgen.CarbNode{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	err = gen.WriteTupleEncodersToFile("./blockstore/cbor_gen.go", "blockstore",
		blockstore.NetRpcReq{},
		blockstore.NetRpcResp{},
		blockstore.NetRpcErr{},
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	*/
}
