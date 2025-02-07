package common

import (
	"context"
	"github.com/filecoin-project/curio/lib/genadt"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
)

//go:generate cbor-gen-for --map-encoding Message Messages VoucherTable BlockHeader

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

type Voucher = []byte

type VoucherTable struct {
	Vouchers []Voucher
}

type BlockHeader struct {
	Parent   cid.Cid
	Height   uint64
	L1Base   *types.TipSetKey
	Messages *genadt.CborLink[*Messages]
	Inputs   *genadt.CborLink[*VoucherTable]
	Outputs  *genadt.CborLink[*VoucherTable]

	Validator address.Address
	Signature *crypto.Signature
}

type BlockLink = *genadt.CborLink[*BlockHeader]

type ProofL2RPC interface {
	ChainHead(ctx context.Context) (BlockLink, error)
	ChainBlockAtHeight(ctx context.Context, height uint64) (BlockLink, error)
	ChainGetBlock(ctx context.Context, c cid.Cid) ([]byte, error)
	ChainHasBlock(ctx context.Context, c cid.Cid) (bool, error)
}
