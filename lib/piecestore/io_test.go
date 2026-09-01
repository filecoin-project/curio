package piecestore

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
)

func TestWriteUploadPieceDataUnknownSize(t *testing.T) {
	body := bytes.Repeat([]byte{0x5a}, 1024)
	const maxSize = int64(4096)

	f, err := os.CreateTemp(t.TempDir(), "piece-")
	require.NoError(t, err)

	pieceInfo, rawSize, err := writeUploadPieceData(f, maxSize, bytes.NewReader(body), false)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, uint64(len(body)), rawSize)

	stored, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, body, stored)

	calc := &commp.Calc{}
	t.Cleanup(calc.Reset)
	_, err = calc.Write(body)
	require.NoError(t, err)
	digest, paddedSize, err := calc.Digest()
	require.NoError(t, err)
	expectedCID, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	require.True(t, expectedCID.Equals(pieceInfo.PieceCID))
	require.Equal(t, paddedSize, uint64(pieceInfo.Size))
}

func TestWriteUploadPieceDataRejectsOversizedStream(t *testing.T) {
	const maxSize = int64(1024)
	body := bytes.Repeat([]byte{0x6b}, int(maxSize)+1)

	f, err := os.CreateTemp(t.TempDir(), "piece-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	_, _, err = writeUploadPieceData(f, maxSize, bytes.NewReader(body), false)
	require.ErrorIs(t, err, ErrPieceTooLarge)
}
