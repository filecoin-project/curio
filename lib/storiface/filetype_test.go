package storiface

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
)

func TestParseSectorID(t *testing.T) {
	id, err := ParseSectorID("s-t01000-5")
	require.NoError(t, err)
	require.Equal(t, abi.SectorID{Miner: 1000, Number: 5}, id)

	for _, name := range []string{
		"s-t01000-5.tmp",
		"s-t01000-5.bak",
		"s-t01000-05",
		"fetching",
		"s-t01000-5/",
		"",
	} {
		_, err := ParseSectorID(name)
		require.Error(t, err, name)
	}
}

func TestSectorFileTypeIsDirectory(t *testing.T) {
	require.True(t, FTCache.IsDirectory())
	require.True(t, FTUpdateCache.IsDirectory())
	require.False(t, FTSealed.IsDirectory())
	require.False(t, FTUnsealed.IsDirectory())
	require.False(t, FTUpdate.IsDirectory())
	require.False(t, FTPiece.IsDirectory())
	require.False(t, FTKey.IsDirectory())
}
