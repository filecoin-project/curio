package pdp

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/filecoin-project/curio/pdp/contract"
)

// MaxFilecoinMessageSize is lotus/chain/messagepool.MaxMessageSize (64 KiB).
// Copied instead of imported: that package pulls filecoin-ffi and breaks the
// PDP no-CGO build. itests/pdp_message_size_test.go pins this against Lotus.
const MaxFilecoinMessageSize = 64 << 10

// filecoinMessageSignatureOverhead matches lotus/chain/messagepool/check.go,
// which compares unsigned-message bytes against MaxMessageSize-128.
const filecoinMessageSignatureOverhead = 128

var errAddPiecesMessageTooLarge = errors.New("addPieces exceeds Filecoin message size limit")

type addPiecesBatchTooLargeError struct {
	PieceCount int
	ParamsSize int
}

func (e *addPiecesBatchTooLargeError) Error() string {
	return fmt.Sprintf("addPieces packed size %d bytes exceeds the Filecoin %d-byte message limit (%d pieces)", e.ParamsSize, MaxFilecoinMessageSize, e.PieceCount)
}

func (e *addPiecesBatchTooLargeError) Unwrap() error {
	return errAddPiecesMessageTooLarge
}

func maxAddPiecesParamsSize() int {
	return MaxFilecoinMessageSize - filecoinMessageSignatureOverhead
}

func filecoinInvokeParamsSize(calldata []byte) (int, error) {
	buf := new(bytes.Buffer)
	if err := cbg.WriteByteArray(buf, calldata); err != nil {
		return 0, fmt.Errorf("cbor-wrap addPieces calldata: %w", err)
	}
	return buf.Len(), nil
}

func packAddPiecesCall(abiData *abi.ABI, setId *big.Int, listener common.Address, pieces []contract.CidsCid, extraData []byte) ([]byte, error) {
	return abiData.Pack("addPieces", setId, listener, pieces, extraData)
}

func pieceDataAsCids(pieces []PieceData) []contract.CidsCid {
	out := make([]contract.CidsCid, len(pieces))
	for i, p := range pieces {
		out[i] = contract.CidsCid{Data: p.Data}
	}
	return out
}

// packAddPiecesWithinMessageLimit packs addPieces and rejects the batch when
// the Filecoin-wrapped params would exceed the 64 KiB message cap.
func packAddPiecesWithinMessageLimit(abiData *abi.ABI, setId *big.Int, listener common.Address, pieces []contract.CidsCid, extraData []byte) ([]byte, error) {
	if len(pieces) == 0 {
		return nil, fmt.Errorf("at least one piece is required")
	}

	data, err := packAddPiecesCall(abiData, setId, listener, pieces, extraData)
	if err != nil {
		return nil, fmt.Errorf("pack addPieces: %w", err)
	}
	paramsSize, err := filecoinInvokeParamsSize(data)
	if err != nil {
		return nil, err
	}
	if paramsSize > maxAddPiecesParamsSize() {
		return nil, &addPiecesBatchTooLargeError{PieceCount: len(pieces), ParamsSize: paramsSize}
	}
	return data, nil
}
