//go:build !cunative

package cunative

import (
	"io"

	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/abi"
)

func DecodeSnap(spt abi.RegisteredSealProof, commD, commK cid.Cid, key, replica io.Reader, out io.Writer) error {
	panic("DecodeSnap: cunative build tag not enabled")
}

func Decode(replica, key io.Reader, out io.Writer) error {
	panic("Decode: cunative build tag not enabled")
}
