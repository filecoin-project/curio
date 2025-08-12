package types

import (
	"github.com/ipfs/go-cid"
)

//go:generate cbor-gen-for --map-encoding PieceInfo

// PieceInfo is used to generate the context CIDs for PDP IPNI ads
type PieceInfo struct {
	// PieceCID is piece CID V2
	PieceCID cid.Cid

	// Payload determines if the IPNI ad is TransportFilecoinPieceHttp or TransportIpfsGatewayHttp
	Payload bool
}
