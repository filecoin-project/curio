package pdp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePieceRead(t *testing.T) {
	restricted, err := ParsePieceRead("")
	require.NoError(t, err)
	require.False(t, restricted)

	restricted, err = ParsePieceRead("public")
	require.NoError(t, err)
	require.False(t, restricted)

	restricted, err = ParsePieceRead(" PUBLIC ")
	require.NoError(t, err)
	require.False(t, restricted)

	restricted, err = ParsePieceRead("restricted")
	require.NoError(t, err)
	require.True(t, restricted)

	restricted, err = ParsePieceRead("Restricted")
	require.NoError(t, err)
	require.True(t, restricted)

	_, err = ParsePieceRead("private")
	require.Error(t, err)
}
