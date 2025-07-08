package contract

import "math/big"

// RootData matches the Solidity RootData struct
type RootData struct {
	Root    struct{ Data []byte }
	RawSize *big.Int
}
