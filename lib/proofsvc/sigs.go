package proofsvc

import "time"

const NonceExpiry = 60 * time.Second

//go:generate cbor-gen-for --map-encoding SignedMsg

type SignedMsg struct {
	Data []byte

	NonceTime uint64 // unix timestamp
	NonceID   uint64

	Signer string
	Sig    []byte
}
