package hashspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/hashfolder"
)

// TestDropRemovesACLPiece writes a piece only into acl-pieces, runs HashSpaceDrop,
// and checks the acl-pieces file is gone.
func TestDropRemovesACLPiece(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "sectorstore.json"), []byte(`{"MaxStorage":1000000}`), 0o644))
	drive := hashfolder.LocalDrive{StorageID: "drop-acl-test", Root: root}
	hs, err := hashfolder.NewCluster(ctx, db, []hashfolder.LocalDrive{drive}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, hs.Close()) })

	raw := bytes.Repeat([]byte{0xac}, 127)
	calc := &commp.Calc{}
	_, err = calc.Write(raw)
	require.NoError(t, err)
	digest, _, err := calc.Digest()
	require.NoError(t, err)
	pc, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(len(raw)))
	require.NoError(t, err)

	digestHash, err := hashfolder.CIDHash(pc)
	require.NoError(t, err)
	target, err := hs.ACL.Target(ctx, digestHash)
	require.NoError(t, err)
	_, err = hs.ACL.WriteLocal(target, pc, hashfolder.WithSize(bytes.NewReader(raw), int64(len(raw))))
	require.NoError(t, err)
	inACL, err := hs.ACL.HasFile(ctx, pc)
	require.NoError(t, err)
	require.True(t, inACL)
	inOpen, err := hs.Public.HasFile(ctx, pc)
	require.NoError(t, err)
	require.False(t, inOpen)

	const taskID harmonytask.TaskID = 9001
	_, err = db.Exec(ctx, `INSERT INTO hash_space_delete (piece_cid, task_id) VALUES ($1, $2)`, pc.String(), taskID)
	require.NoError(t, err)

	drop := NewDropTask(db, hs)
	done, err := drop.Do(ctx, taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)

	inACL, err = hs.ACL.HasFile(ctx, pc)
	require.NoError(t, err)
	require.False(t, inACL, "HashSpaceDrop must remove acl-pieces copies")
	var left int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_delete WHERE piece_cid = $1`, pc.String()).Scan(&left))
	require.Equal(t, 0, left)
}
