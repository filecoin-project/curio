package common

import (
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
	OpTypeProofMatch
	OpTypeProofResult
	OpTypeProofReward
)

type BlockHeader struct {
	Version uint64

	Parent BlockLink
	Height uint64
	L1Base *types.TipSetKey

	OpType OpType

	PaymentCumulative abi.TokenAmount
	PaymentNonce      abi.ChainEpoch

	Provider *address.Address
	Client   *address.Address

	Validator address.Address
	Signature *crypto.Signature
}

type BlockLink = *genadt.CborLink[*BlockHeader]
