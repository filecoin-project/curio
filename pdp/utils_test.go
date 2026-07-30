package pdp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/pdp/contract"
)

func packCreatePayload(t *testing.T, payer common.Address, clientDataSetId int64, keys []string) []byte {
	t.Helper()
	args, err := createPayloadArgs()
	require.NoError(t, err)
	values := make([]string, len(keys))
	payload, err := args.Pack(payer, big.NewInt(clientDataSetId), keys, values, []byte("sig"))
	require.NoError(t, err)
	return payload
}

func packCombinedExtraData(t *testing.T, createPayload, addPayload []byte) []byte {
	t.Helper()
	bytesType, err := abi.NewType("bytes", "", nil)
	require.NoError(t, err)
	outer := abi.Arguments{{Type: bytesType}, {Type: bytesType}}
	extra, err := outer.Pack(createPayload, addPayload)
	require.NoError(t, err)
	return extra
}

func TestDecodeFWSSCreateIdentityFromExtraData_CombinedAndBare(t *testing.T) {
	payer := common.HexToAddress("0x1111111111111111111111111111111111111111")
	createPayload := packCreatePayload(t, payer, 42, []string{"withIPFSIndexing"})

	combined := packCombinedExtraData(t, createPayload, []byte{0x01})
	got, err := DecodeFWSSCreateIdentityFromExtraData(combined)
	require.NoError(t, err)
	require.Equal(t, payer, got.Payer)
	require.Equal(t, int64(42), got.ClientDataSetId.Int64())
	require.Equal(t, []string{"withIPFSIndexing"}, got.MetadataKeys)

	gotBare, err := DecodeFWSSCreateIdentityFromExtraData(createPayload)
	require.NoError(t, err)
	require.Equal(t, payer, gotBare.Payer)
	require.Equal(t, int64(42), gotBare.ClientDataSetId.Int64())
}

func TestCreateIdentityFromPDPCalldata_CreateDataSetAndAddPieces(t *testing.T) {
	payer := common.HexToAddress("0x2222222222222222222222222222222222222222")
	createPayload := packCreatePayload(t, payer, 7, nil)
	listener := common.HexToAddress("0x3333333333333333333333333333333333333333")

	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)

	createCalldata, err := pdpABI.Pack("createDataSet", listener, createPayload)
	require.NoError(t, err)
	got, err := CreateIdentityFromPDPCalldata(createCalldata)
	require.NoError(t, err)
	require.Equal(t, payer, got.Payer)
	require.Equal(t, int64(7), got.ClientDataSetId.Int64())

	combined := packCombinedExtraData(t, createPayload, []byte{})
	addCalldata, err := pdpABI.Pack("addPieces", big.NewInt(0), listener, []contract.CidsCid{}, combined)
	require.NoError(t, err)
	gotAdd, err := CreateIdentityFromPDPCalldata(addCalldata)
	require.NoError(t, err)
	require.Equal(t, payer, gotAdd.Payer)
	require.Equal(t, int64(7), gotAdd.ClientDataSetId.Int64())

	existing, err := pdpABI.Pack("addPieces", big.NewInt(99), common.Address{}, []contract.CidsCid{}, []byte{0x01})
	require.NoError(t, err)
	_, err = CreateIdentityFromPDPCalldata(existing)
	require.Error(t, err)
}

func TestIPNIFromExtraData(t *testing.T) {
	payer := common.HexToAddress("0x1111111111111111111111111111111111111111")

	known, ipni := IPNIFromExtraData(nil)
	require.False(t, known)
	require.False(t, ipni)

	withIndex := packCreatePayload(t, payer, 1, []string{"withIPFSIndexing"})
	known, ipni = IPNIFromExtraData(withIndex)
	require.True(t, known)
	require.True(t, ipni)

	combined := packCombinedExtraData(t, withIndex, []byte{0x01})
	mustIndex, err := CheckIfIndexingNeededFromExtraData(combined)
	require.NoError(t, err)
	require.True(t, mustIndex)

	without := packCreatePayload(t, payer, 2, []string{"other"})
	known, ipni = IPNIFromExtraData(without)
	require.True(t, known)
	require.False(t, ipni)

	known, ipni = IPNIFromExtraData([]byte{0xde, 0xad})
	require.False(t, known)
	require.False(t, ipni)
}

func TestFWSSPayerFromExtraData(t *testing.T) {
	payer := common.HexToAddress("0x4444444444444444444444444444444444444444")
	createPayload := packCreatePayload(t, payer, 1, nil)
	combined := packCombinedExtraData(t, createPayload, []byte{})
	got, err := FWSSPayerFromExtraData(combined)
	require.NoError(t, err)
	require.Equal(t, payer, got)
}

