package hashfolder

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
)

// claimTestTask exists so a test can start a task engine. Raise refuses to
// solve a claim until AdderFor returns non-nil, and the satisfied path never
// asks this task to run.
type claimTestTask struct{}

func (claimTestTask) Do(context.Context, harmonytask.TaskID, func() bool) (bool, error) {
	return true, nil
}

func (claimTestTask) CanAccept([]harmonytask.TaskID, *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return nil, nil
}

func (claimTestTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{Name: "hsclaimtest"}
}

func (claimTestTask) Adder(harmonytask.AddTaskFunc) {}

func init() {
	harmonytask.Reg(claimTestTask{})
}

func pendingClaims(t *testing.T, db *harmonydb.DB, storageID string) int {
	t.Helper()
	var n int
	err := db.QueryRow(context.Background(), `SELECT COUNT(*) FROM hash_space_pending_event WHERE storage_id = $1 AND event_kind = $2`, storageID, EVENT_CLAIM).Scan(&n)
	require.NoError(t, err)
	return n
}

func startClaimEngine(t *testing.T, db *harmonydb.DB) {
	t.Helper()
	reg, err := resources.RegisterWithResources(db, "hashfolder-claim-test:0", resources.Resources{Cpu: 1, Ram: 1 << 20})
	require.NoError(t, err)
	engine, err := harmonytask.NewWithReg(db, []harmonytask.TaskInterface{claimTestTask{}}, "hashfolder-claim-test:0", nil, reg)
	require.NoError(t, err)
	t.Cleanup(engine.GracefullyTerminate)
}

// pieceDrive is a temp root that accepts piece park, capped at maxStorage.
func pieceDrive(t *testing.T, id string, maxStorage int64) LocalDrive {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, sectorStoreFile), fmt.Appendf(nil, `{"MaxStorage":%d}`, maxStorage), 0o644))
	return LocalDrive{StorageID: id, Root: root}
}

func openPieceHash(tag byte) string {
	var sum [HASH_BYTES]byte
	sum[0] = tag
	return hex.EncodeToString(sum[:])
}

// writeOpenBytes writes an n-byte file into root's open-pieces folder.
// tag selects the hash so two files on one root do not collide. The folder
// has no layout yet, so migratePiecePark sums the file into a claim.
func writeOpenBytes(t *testing.T, root string, tag byte, n int64) {
	t.Helper()
	p, err := piecePath(root, DIR_OPEN, openPieceHash(tag))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	f, err := os.Create(p)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(n))
	require.NoError(t, f.Close())
}

// restartClaim joins claimRoot into a cluster that already has owner, then
// starts again after the claim flag has been rewritten off the layout. The
// pending row is the only record of the claim on the second start.
func restartClaim(t *testing.T, db *harmonydb.DB, owner, claim LocalDrive) *Cluster {
	t.Helper()
	ctx := context.Background()
	first, err := NewCluster(ctx, db, []LocalDrive{owner}, nil)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	joined, err := NewCluster(ctx, db, []LocalDrive{owner, claim}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, pendingClaims(t, db, claim.StorageID))
	require.NoError(t, joined.Close())

	restarted, err := NewCluster(ctx, db, []LocalDrive{owner, claim}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	require.Equal(t, 1, pendingClaims(t, db, claim.StorageID), "a restart must keep the claim row and not add another")
	return restarted
}

func TestClaimPendingConsumedWhenSatisfied(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	t.Cleanup(harmonytask.SetEngineForTest(nil))

	const (
		diskCap = int64(4_000_000)
		held    = int64(1_000_000)
	)
	owner := pieceDrive(t, "owner", diskCap)
	claim := pieceDrive(t, "claim", diskCap)
	// The claim folder holds every piece byte, so its share is already met.
	// migratePiecePark writes the claim from those files when the disk joins.
	writeOpenBytes(t, claim.Root, 0x11, held)

	c := restartClaim(t, db, owner, claim)
	var used int64
	require.NoError(t, db.QueryRow(ctx, `SELECT used_open FROM hash_space_disk WHERE storage_id = $1`, claim.StorageID).Scan(&used))
	require.Equal(t, held, used)

	startClaimEngine(t, db)
	require.NoError(t, c.reevaluate(ctx))

	require.Zero(t, pendingClaims(t, db, claim.StorageID))
	var ranges, moves int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_range WHERE storage_id = $1`, claim.StorageID).Scan(&ranges))
	require.Zero(t, ranges, "a satisfied claim is consumed without taking ranges")
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM hash_space_move_source`).Scan(&moves))
	require.Zero(t, moves)
}

func TestClaimPendingKeptWhileStepPlanned(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	t.Cleanup(harmonytask.SetEngineForTest(nil))

	const diskCap = int64(20_000_000)
	owner := pieceDrive(t, "owner", diskCap)
	claim := pieceDrive(t, "claim", diskCap)

	// Seed the owner, then write bytes through its open-pieces space so the
	// layout records the file. The claim folder holds one byte, under its
	// share, and gains its claim layout from migratePiecePark when it joins.
	first, err := NewCluster(ctx, db, []LocalDrive{owner}, nil)
	require.NoError(t, err)
	w, err := first.space(DIR_OPEN).WriteHashOn(owner.Root, openPieceHash(0x22))
	require.NoError(t, err)
	_, err = w.Write(make([]byte, 8_000_000))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, first.Close())
	writeOpenBytes(t, claim.Root, 0x11, 1)

	joined, err := NewCluster(ctx, db, []LocalDrive{owner, claim}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, pendingClaims(t, db, claim.StorageID))
	require.NoError(t, joined.Close())

	c, err := NewCluster(ctx, db, []LocalDrive{owner, claim}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	require.Equal(t, 1, pendingClaims(t, db, claim.StorageID))

	startClaimEngine(t, db)
	require.NoError(t, c.reevaluate(ctx))

	require.Equal(t, 1, pendingClaims(t, db, claim.StorageID), "a claim step leaves the row for the next solve")
	var tos []string
	require.NoError(t, db.Select(ctx, &tos, `SELECT to_storage FROM hash_space_move_source`))
	require.NotEmpty(t, tos)
	for _, to := range tos {
		require.Equal(t, claim.StorageID, to)
	}
}
