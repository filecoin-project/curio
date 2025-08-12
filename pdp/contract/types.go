package contract

import "math/big"

// PieceData matches the Solidity PieceData struct
type PieceData struct {
	Piece   struct{ Data []byte }
	RawSize *big.Int
}
