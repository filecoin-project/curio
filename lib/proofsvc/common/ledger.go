package common

import (
	"bytes"

	block "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/lib/genadt"

	"github.com/filecoin-project/lotus/chain/types"
)

//go:generate cbor-gen-for --map-encoding BlockHeader

type OpType uint64

const (
	OpTypeUnknown OpType = iota
	OpTypeProofOrder
	OpTypeMatch
	OpTypeProofReward
	OpTypeDeassign
)

type BlockHeader struct {
	Version uint64

	Parent BlockLink
	Height uint64
	L1Base types.TipSetKey

	OpType OpType

	PaymentCumulative abi.TokenAmount
	PaymentNonce      uint64
	PaymentSignature  []byte

	Provider *address.Address
	Client   *address.Address

	Metadata string

	Validator address.Address
	Signature *crypto.Signature
}

func (blk *BlockHeader) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := blk.MarshalCBOR(buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (blk *BlockHeader) ToStorageBlock() (block.Block, error) {
	data, err := blk.Serialize()
	if err != nil {
		return nil, err
	}

	c, err := abi.CidBuilder.Sum(data)
	if err != nil {
		return nil, err
	}

	return block.NewBlockWithCid(data, c)
}

func (blk *BlockHeader) SigningBytes() ([]byte, error) {
	blkcopy := *blk
	blkcopy.Signature = nil

	return blkcopy.Serialize()
}

func (blk *BlockHeader) Cid() cid.Cid {
	b, err := blk.ToStorageBlock()
	if err != nil {
		panic(err)
	}
	return b.Cid()
}

type BlockLink = *genadt.CborLink[*BlockHeader]
