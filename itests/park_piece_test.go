package itests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/piece"
)

// TestParkPieceCanAccept_SliceBounds is a regression test for a panic in
// ParkPieceTask.CanAccept where capacity (maxInPark - count - running) could
// exceed len(ids), causing "slice bounds out of range [:N] with capacity M".
//
// The production crash was: maxInPark=15, count=0, running=0, len(ids)=2
// → ids[:15] panicked on a 2-element slice.
func TestParkPieceCanAccept_SliceBounds(t *testing.T) {
	// Slow down the background piece poller so it doesn't interfere.
	oldInterval := piece.PieceParkPollInterval
	piece.PieceParkPollInterval = time.Hour
	t.Cleanup(func() { piece.PieceParkPollInterval = oldInterval })

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	// Set up a local storage path.
	root := t.TempDir()
	storageDir := filepath.Join(root, "storage")
	require.NoError(t, os.MkdirAll(storageDir, 0755))

	storageID := storiface.ID(uuid.New().String())
	meta := &storiface.LocalStorageMeta{
		ID:       storageID,
		Weight:   10,
		CanSeal:  true,
		CanStore: true,
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storageDir, "sectorstore.json"), mb, 0644))

	bls := &paths.BasicLocalStorage{PathToJSON: filepath.Join(root, "storage.json")}
	index := paths.NewDBIndex(nil, db)
	ls, err := paths.NewLocal(ctx, bls, index, "")
	require.NoError(t, err)
	require.NoError(t, ls.OpenPath(ctx, storageDir))

	remote, err := paths.NewRemote(ls, index, nil, 20, &paths.DefaultPartialFileHandler{})
	require.NoError(t, err)

	sc := ffi.NewSealCalls(remote, ls, index)

	// Create ParkPieceTask with maxInPark=15, matching the production crash.
	const maxInPark = 15
	ppt, err := piece.NewParkPieceTask(db, sc, remote, 10, maxInPark, nil, 10)
	require.NoError(t, err)

	// Create a real TaskEngine so RunningCount works.
	engine, err := harmonytask.New(db, []harmonytask.TaskInterface{ppt}, "testhost:1234", nil)
	require.NoError(t, err)
	t.Cleanup(func() { engine.GracefullyTerminate() })

	// --- Regression case ---
	// 2 IDs, maxInPark=15, 0 pieces in storage, 0 running.
	// Before the fix this panicked: ids[:15] on a 2-element slice.
	ids := []harmonytask.TaskID{1, 2}
	require.NotPanics(t, func() {
		accepted, err := ppt.CanAccept(ids, engine)
		require.NoError(t, err)
		require.Equal(t, ids, accepted, "should accept all available IDs when capacity exceeds len(ids)")
	})

	// --- Partial capacity ---
	// Fill storage to maxInPark-1 pieces so only 1 slot remains.
	for i := range maxInPark - 1 {
		_, err := db.Exec(ctx, `INSERT INTO sector_location
			(miner_id, sector_num, sector_filetype, storage_id, is_primary)
			VALUES ($1, $2, $3, $4, $5)`,
			0, i, int(storiface.FTPiece), string(storageID), true)
		require.NoError(t, err)
	}

	accepted, err := ppt.CanAccept(ids, engine)
	require.NoError(t, err)
	require.Len(t, accepted, 1, "should cap to remaining capacity")

	// --- At capacity ---
	// Add one more piece → count == maxInPark → reject all.
	_, err = db.Exec(ctx, `INSERT INTO sector_location
		(miner_id, sector_num, sector_filetype, storage_id, is_primary)
		VALUES ($1, $2, $3, $4, $5)`,
		0, maxInPark, int(storiface.FTPiece), string(storageID), true)
	require.NoError(t, err)

	accepted, err = ppt.CanAccept(ids, engine)
	require.NoError(t, err)
	require.Nil(t, accepted, "should reject all when at capacity")
}
