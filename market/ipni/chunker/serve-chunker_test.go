package chunker

import (
	"bytes"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/market/ipni/types"
)

func TestEncryptedPieceContext(t *testing.T) {
	piece, err := cid.Parse("bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq")
	require.NoError(t, err)
	otherBytes := append([]byte{0x01, 0x55, 0x00, 4}, bytes.Repeat([]byte{0x42}, 4)...)
	other, err := cid.Cast(otherBytes)
	require.NoError(t, err)

	encrypted, err := (&types.PdpIpniContext{
		PieceCID: piece,
		Flags:    types.PDP_IPNI_FLAG_PAYLOAD | types.PDP_IPNI_FLAG_ENCRYPTED,
	}).Marshal()
	require.NoError(t, err)
	public, err := (&types.PdpIpniContext{
		PieceCID: piece,
		Flags:    types.PDP_IPNI_FLAG_PAYLOAD,
	}).Marshal()
	require.NoError(t, err)
	otherEncrypted, err := (&types.PdpIpniContext{
		PieceCID: other,
		Flags:    types.PDP_IPNI_FLAG_ENCRYPTED,
	}).Marshal()
	require.NoError(t, err)

	require.True(t, encryptedPieceContext(encrypted, piece))
	require.False(t, encryptedPieceContext(public, piece))
	require.False(t, encryptedPieceContext(otherEncrypted, piece))
	require.False(t, encryptedPieceContext([]byte{0x03, 0xff}, piece))
	require.False(t, encryptedPieceContext(nil, piece))
}
