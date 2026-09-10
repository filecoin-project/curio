package pdp

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/pdp/contract"
)

func testAddPiecesABI(t *testing.T) *abi.ABI {
	t.Helper()
	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)
	return abiData
}

func dummyPieceCIDs(n, cidLen int) []contract.CidsCid {
	out := make([]contract.CidsCid, n)
	for i := range out {
		cid := make([]byte, cidLen)
		cid[0] = 0x01
		cid[len(cid)-1] = byte(i)
		out[i] = contract.CidsCid{Data: cid}
	}
	return out
}

func packAddPiecesExtraData(t *testing.T, n int, key, value string) []byte {
	t.Helper()
	uint256Type, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	string2DType, err := abi.NewType("string[][]", "", nil)
	require.NoError(t, err)
	bytesType, err := abi.NewType("bytes", "", nil)
	require.NoError(t, err)
	args := abi.Arguments{
		{Type: uint256Type},
		{Type: string2DType},
		{Type: string2DType},
		{Type: bytesType},
	}
	keys := make([][]string, n)
	values := make([][]string, n)
	for i := 0; i < n; i++ {
		keys[i] = []string{key, key + "2", key + "3"}
		values[i] = []string{value, value, value}
	}
	packed, err := args.Pack(big.NewInt(1), keys, values, []byte("signature-bytes-placeholder"))
	require.NoError(t, err)
	return packed
}

func TestPackAddPiecesWithinMessageLimit_FitsAll(t *testing.T) {
	abiData := testAddPiecesABI(t)
	pieces := dummyPieceCIDs(3, 40)
	extra := packAddPiecesExtraData(t, 3, "k", "v")

	data, err := packAddPiecesWithinMessageLimit(abiData, big.NewInt(1), common.Address{}, pieces, extra)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	expected, err := packAddPiecesCall(abiData, big.NewInt(1), common.Address{}, pieces, extra)
	require.NoError(t, err)
	require.Equal(t, expected, data)
}

func TestPackAddPiecesWithinMessageLimit_RejectsOversizedBatch(t *testing.T) {
	abiData := testAddPiecesABI(t)
	const n = 200
	value := strings.Repeat("m", 96)
	pieces := dummyPieceCIDs(n, 40)
	extra := packAddPiecesExtraData(t, n, "filename", value)

	_, err := packAddPiecesWithinMessageLimit(abiData, big.NewInt(1), common.Address{}, pieces, extra)
	require.Error(t, err)
	var tooLarge *addPiecesBatchTooLargeError
	require.ErrorAs(t, err, &tooLarge)
	require.Equal(t, n, tooLarge.PieceCount)
	require.Greater(t, tooLarge.ParamsSize, maxAddPiecesParamsSize())
}

func TestPackAddPiecesWithinMessageLimit_SinglePieceTooLarge(t *testing.T) {
	abiData := testAddPiecesABI(t)
	pieces := dummyPieceCIDs(1, 40)
	value := strings.Repeat("x", maxAddPiecesParamsSize())
	extra := packAddPiecesExtraData(t, 1, "k", value)

	_, err := packAddPiecesWithinMessageLimit(abiData, big.NewInt(1), common.Address{}, pieces, extra)
	require.Error(t, err)
	var tooLarge *addPiecesBatchTooLargeError
	require.ErrorAs(t, err, &tooLarge)
	require.Equal(t, 1, tooLarge.PieceCount)
	require.Greater(t, tooLarge.ParamsSize, maxAddPiecesParamsSize())
}
