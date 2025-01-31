package common

import (
	"github.com/filecoin-project/curio/lib/genadt"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

//go:generate cbor-gen-for --map-encoding Message Messages

type Message struct {
	Version uint64

	Actor address.Address
	Value abi.TokenAmount

	Method uint64
	Params *cid.Cid
}

type Messages struct {
	Messages []*genadt.CborLink[*Message]
}

/*type BlockHeader struct {
	Parent   cid.Cid
	Height   uint64
	L1Base   *types.TipSetKey
	Messages genadt.CborLink[Messages]
	Inputs   []Input
	Outputs  []Output
}
*/
