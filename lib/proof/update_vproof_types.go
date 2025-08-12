package proof

import "github.com/filecoin-project/go-state-types/abi"

type Snap struct {
	ProofType abi.RegisteredUpdateProof

	OldR Commitment `json:"old_r"`
	NewR Commitment `json:"new_r"`
	NewD Commitment `json:"new_d"`

	// rust-fil-proofs bincoded repr; Todo is implementing types in Go
	Proofs [][]byte
}
