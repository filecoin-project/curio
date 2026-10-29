package hashfolder

import (
	"context"
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

	owner := LocalDrive{StorageID: "owner", Root: t.TempDir()}
	claim := LocalDrive{StorageID: "claim", Root: t.TempDir()}
	// The claim holds every folder byte, so its share is already met.
	writeClaim(t, claim.Root, 1_000_000)

	c := restartClaim(t, db, owner, claim)
	var used int64
	require.NoError(t, db.QueryRow(ctx, `SELECT used_open FROM hash_space_disk WHERE storage_id = $1`, claim.StorageID).Scan(&used))
	require.Equal(t, int64(1_000_000), used)

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

	owner := LocalDrive{StorageID: "owner", Root: t.TempDir()}
	claim := LocalDrive{StorageID: "claim", Root: t.TempDir()}
	writeClaim(t, claim.Root, 1)

	// Seed the owner, then give its layout real bytes before the claim joins
	// so the published range size is what the solver can move.
	first, err := NewCluster(ctx, db, []LocalDrive{owner}, nil)
	require.NoError(t, err)
	require.NoError(t, first.Close())
	layout, err := readLayout(layoutPath(owner.Root, DIR_OPEN))
	require.NoError(t, err)
	layout.Used = 8_000_000
	require.NoError(t, writeLayout(owner.Root, DIR_OPEN, layout))

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
