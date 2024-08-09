package legacytypes

import (
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
)

//go:generate cbor-gen-for --map-encoding SignedStorageAsk StorageAsk Balance AskRequest AskResponse

// AskProtocolID is the ID for the libp2p protocol for querying miners for their current StorageAsk.
const AskProtocolID = "/fil/storage/ask/1.1.0"

// StorageAsk defines the parameters by which a miner will choose to accept or
// reject a deal. Note: making a storage deal proposal which matches the miner's
// ask is a precondition, but not sufficient to ensure the deal is accepted (the
// storage provider may run its own decision logic).
type StorageAsk struct {
	// Price per GiB / Epoch
	Price         abi.TokenAmount
	VerifiedPrice abi.TokenAmount

	MinPieceSize abi.PaddedPieceSize
	MaxPieceSize abi.PaddedPieceSize
	Miner        address.Address
	Timestamp    abi.ChainEpoch
	Expiry       abi.ChainEpoch
	SeqNo        uint64
}

// SignedStorageAsk is an ask signed by the miner's private key
type SignedStorageAsk struct {
	Ask       *StorageAsk
	Signature *crypto.Signature
}

// StorageAskOption allows custom configuration of a storage ask
type StorageAskOption func(*StorageAsk)

// MinPieceSize configures a minimum piece size of a StorageAsk
func MinPieceSize(minPieceSize abi.PaddedPieceSize) StorageAskOption {
	return func(sa *StorageAsk) {
		sa.MinPieceSize = minPieceSize
	}
}

// MaxPieceSize configures maxiumum piece size of a StorageAsk
func MaxPieceSize(maxPieceSize abi.PaddedPieceSize) StorageAskOption {
	return func(sa *StorageAsk) {
		sa.MaxPieceSize = maxPieceSize
	}
}

// Balance represents a current balance of funds in the StorageMarketActor.
type Balance struct {
	Locked    abi.TokenAmount
	Available abi.TokenAmount
}

// AskRequest is a request for current ask parameters for a given miner
type AskRequest struct {
	Miner address.Address
}

// AskResponse is the response sent over the network in response
// to an ask request
type AskResponse struct {
	Ask *SignedStorageAsk
}
