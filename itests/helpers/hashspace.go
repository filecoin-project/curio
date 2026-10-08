package helpers

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashspace"
)

// NewHashSpaceCluster joins a single temporary disk to the open-pieces hash
// space of db. The cluster is closed when the test ends.
func NewHashSpaceCluster(t *testing.T, ctx context.Context, db *harmonydb.DB, storageID string) *hashspace.Cluster {
	t.Helper()

	hs, err := hashspace.NewCluster(ctx, db, []hashspace.LocalDrive{{StorageID: storageID, Root: t.TempDir()}}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = hs.Close() })
	return hs
}

// PlaceOpenPiece writes the fixture's bytes into open-pieces on the disk its
// hash maps to, named as /piece/ looks it up.
func PlaceOpenPiece(t *testing.T, ctx context.Context, hs *hashspace.Cluster, piece PieceFixture) {
	t.Helper()

	digest, err := hashspace.CIDHash(piece.PieceCIDV2)
	require.NoError(t, err)
	target, err := hs.Target(ctx, digest)
	require.NoError(t, err)
	n, err := hs.WriteLocal(target, piece.PieceCIDV2, bytes.NewReader(piece.CarBytes[:piece.RawSize]))
	require.NoError(t, err)
	require.Equal(t, piece.RawSize, n)
}

// RequireNotInOpenPieces fails if any disk that may hold the piece has it.
func RequireNotInOpenPieces(t *testing.T, ctx context.Context, hs *hashspace.Cluster, piece PieceFixture) {
	t.Helper()

	has, err := hs.HasFile(ctx, piece.PieceCIDV2)
	require.NoError(t, err)
	require.False(t, has, "piece %s must not be in open-pieces", piece.PieceCIDV2)
}
