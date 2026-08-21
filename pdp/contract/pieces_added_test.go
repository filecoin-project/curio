package contract

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

func TestUnpackPackedCid(t *testing.T) {
	t.Parallel()

	var header, root [32]byte
	// Leading zeros only (right-aligned header): 0x01559120
	header[28] = 0x01
	header[29] = 0x55
	header[30] = 0x91
	header[31] = 0x20
	for i := 0; i < 32; i++ {
		root[i] = byte(i + 1)
	}

	got := UnpackPackedCid(header, root)
	require.Equal(t, []byte{0x01, 0x55, 0x91, 0x20}, got[:4])
	require.Equal(t, root[:], got[4:])
	require.Len(t, got, 36)
}

func TestUnpackPackedCidPreservesInternalZeros(t *testing.T) {
	t.Parallel()

	var header, root [32]byte
	// Header bytes with an internal zero after the first nonzero byte.
	header[27] = 0x01
	header[28] = 0x00
	header[29] = 0x55
	header[30] = 0x91
	header[31] = 0x20
	root[0] = 0xaa

	got := UnpackPackedCid(header, root)
	require.Equal(t, []byte{0x01, 0x00, 0x55, 0x91, 0x20}, got[:5])
	require.Equal(t, byte(0xaa), got[5])
	require.Len(t, got, 37)
}

func packPiecesAddedV1Log(t *testing.T, setID uint64, pieceIDs []*big.Int, cids [][]byte) types.Log {
	t.Helper()
	pdpABI, err := PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)
	event := pdpABI.Events["PiecesAdded"]

	cidStructs := make([]CidsCid, len(cids))
	for i := range cids {
		cidStructs[i] = CidsCid{Data: cids[i]}
	}
	data, err := event.Inputs.NonIndexed().Pack(pieceIDs, cidStructs)
	require.NoError(t, err)

	setTopic := common.BigToHash(big.NewInt(int64(setID)))
	return types.Log{
		Address: common.HexToAddress("0xBADd0B92C1c71d02E7d520f64c0876538fa2557F"),
		Topics:  []common.Hash{event.ID, setTopic},
		Data:    data,
	}
}

func packPiecesAddedV2Log(t *testing.T, setID, firstPieceID uint64, packed []CidsPackedCid) types.Log {
	t.Helper()
	pdpABI, err := PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)
	event := pdpABI.Events["PiecesAddedV2"]

	data, err := event.Inputs.NonIndexed().Pack(big.NewInt(int64(firstPieceID)), packed)
	require.NoError(t, err)

	setTopic := common.BigToHash(big.NewInt(int64(setID)))
	return types.Log{
		Address: common.HexToAddress("0xBADd0B92C1c71d02E7d520f64c0876538fa2557F"),
		Topics:  []common.Hash{event.ID, setTopic},
		Data:    data,
	}
}

func sampleCID(suffix byte) []byte {
	// Minimal-looking PieceCIDv2-ish bytes: prefix + padding/height stub + 32-byte root.
	cid := make([]byte, 40)
	copy(cid, []byte{0x01, 0x55, 0x91, 0x20, 0x22, 0x00, 0x06})
	cid[7] = suffix
	for i := 8; i < 40; i++ {
		cid[i] = byte(i) ^ suffix
	}
	return cid
}

func packCidForTest(t *testing.T, cid []byte) CidsPackedCid {
	t.Helper()
	require.GreaterOrEqual(t, len(cid), 32)
	require.LessOrEqual(t, len(cid), 64)
	headerLen := len(cid) - 32
	var header, root [32]byte
	copy(header[32-headerLen:], cid[:headerLen])
	copy(root[:], cid[headerLen:])
	return CidsPackedCid{Header: header, Root: root}
}

func TestPiecesFromReceiptLegacyV1(t *testing.T) {
	t.Parallel()

	cid0 := sampleCID(0x11)
	cid1 := sampleCID(0x22)
	log := packPiecesAddedV1Log(t, 7,
		[]*big.Int{big.NewInt(10), big.NewInt(11)},
		[][]byte{cid0, cid1},
	)
	receipt := &types.Receipt{Logs: []*types.Log{&log}}

	pieces, err := PiecesFromReceipt(receipt)
	require.NoError(t, err)
	require.Len(t, pieces, 2)
	require.Equal(t, uint64(10), pieces[0].PieceID)
	require.Equal(t, cid0, pieces[0].CID)
	require.Equal(t, uint64(11), pieces[1].PieceID)
	require.Equal(t, cid1, pieces[1].CID)
}

func TestPiecesFromReceiptV2Single(t *testing.T) {
	t.Parallel()

	cid0 := sampleCID(0x31)
	cid1 := sampleCID(0x32)
	log := packPiecesAddedV2Log(t, 3, 5, []CidsPackedCid{
		packCidForTest(t, cid0),
		packCidForTest(t, cid1),
	})
	receipt := &types.Receipt{Logs: []*types.Log{&log}}

	pieces, err := PiecesFromReceipt(receipt)
	require.NoError(t, err)
	require.Len(t, pieces, 2)
	require.Equal(t, uint64(5), pieces[0].PieceID)
	require.Equal(t, cid0, pieces[0].CID)
	require.Equal(t, uint64(6), pieces[1].PieceID)
	require.Equal(t, cid1, pieces[1].CID)
}

func TestPiecesFromReceiptV2MultiBatchFlatten(t *testing.T) {
	t.Parallel()

	firstBatch := make([]CidsPackedCid, 100)
	firstCIDs := make([][]byte, 100)
	for i := 0; i < 100; i++ {
		firstCIDs[i] = sampleCID(byte(i))
		firstBatch[i] = packCidForTest(t, firstCIDs[i])
	}
	cid100 := sampleCID(0xff)
	log0 := packPiecesAddedV2Log(t, 1, 0, firstBatch)
	log1 := packPiecesAddedV2Log(t, 1, 100, []CidsPackedCid{packCidForTest(t, cid100)})

	// Intentionally out of order in the receipt to exercise sorting.
	receipt := &types.Receipt{Logs: []*types.Log{&log1, &log0}}

	pieces, err := PiecesFromReceipt(receipt)
	require.NoError(t, err)
	require.Len(t, pieces, 101)
	require.Equal(t, uint64(0), pieces[0].PieceID)
	require.Equal(t, firstCIDs[0], pieces[0].CID)
	require.Equal(t, uint64(99), pieces[99].PieceID)
	require.Equal(t, firstCIDs[99], pieces[99].CID)
	require.Equal(t, uint64(100), pieces[100].PieceID)
	require.Equal(t, cid100, pieces[100].CID)
}

func TestPiecesFromReceiptPrefersV2(t *testing.T) {
	t.Parallel()

	v1CID := sampleCID(0x41)
	v2CID := sampleCID(0x42)
	v1 := packPiecesAddedV1Log(t, 1, []*big.Int{big.NewInt(0)}, [][]byte{v1CID})
	v2 := packPiecesAddedV2Log(t, 1, 7, []CidsPackedCid{packCidForTest(t, v2CID)})
	receipt := &types.Receipt{Logs: []*types.Log{&v1, &v2}}

	pieces, err := PiecesFromReceipt(receipt)
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	require.Equal(t, uint64(7), pieces[0].PieceID)
	require.Equal(t, v2CID, pieces[0].CID)
}

func TestPiecesFromReceiptMissingBoth(t *testing.T) {
	t.Parallel()

	receipt := &types.Receipt{Logs: []*types.Log{{
		Topics: []common.Hash{common.HexToHash("0x01")},
		Data:   []byte{0x00},
	}}}
	_, err := PiecesFromReceipt(receipt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "neither PiecesAdded nor PiecesAddedV2")
}
