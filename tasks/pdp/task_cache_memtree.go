package pdp

import (
	"context"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
)

var logCache = logging.Logger("pdp-cache-memtree")

// CacheMemtreeTask builds and caches merkle trees for pieces to optimize proving time
type CacheMemtreeTask struct {
	db *harmonydb.DB
	sc *ffi.SealCalls
	max int
}

func NewCacheMemtreeTask(db *harmonydb.DB, sc *ffi.SealCalls, max int) *CacheMemtreeTask {
	return &CacheMemtreeTask{
		db:  db,
		sc:  sc,
		max: max,
	}
}

func (c *CacheMemtreeTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Get the piece to cache
	var cacheEntry struct {
		ID              int64  `db:"id"`
		PieceCID        string `db:"piece_cid"`
		PiecePaddedSize int64  `db:"piece_padded_size"`
	}

	err = c.db.QueryRow(ctx, `
		SELECT id, piece_cid, piece_padded_size
		FROM pdp_piece_memtrees
		WHERE cache_task_id = $1
	`, taskID).Scan(&cacheEntry.ID, &cacheEntry.PieceCID, &cacheEntry.PiecePaddedSize)

	if err != nil {
		return false, xerrors.Errorf("getting cache entry: %w", err)
	}

	logCache.Infow("building merkle tree cache", "piece_cid", cacheEntry.PieceCID, "padded_size", cacheEntry.PiecePaddedSize)

	// Find the piece in parked_pieces to get raw size
	var pieceID int64
	var rawSize int64
	err = c.db.QueryRow(ctx, `
		SELECT id, piece_raw_size FROM parked_pieces WHERE piece_cid = $1
	`, cacheEntry.PieceCID).Scan(&pieceID, &rawSize)

	if err != nil {
		return false, c.markFailed(ctx, cacheEntry.ID, err)
	}

	// Get piece reader
	pieceReader, err := c.sc.PieceReader(ctx, storiface.PieceNumber(pieceID))
	if err != nil {
		return false, c.markFailed(ctx, cacheEntry.ID, err)
	}
	defer pieceReader.Close()

	// Build the merkle tree
	memtreeData, err := proof.BuildSha254Memtree(pieceReader, abi.UnpaddedPieceSize(rawSize))
	if err != nil {
		return false, c.markFailed(ctx, cacheEntry.ID, err)
	}

	logCache.Infow("merkle tree built, storing in cache", 
		"piece_cid", cacheEntry.PieceCID, 
		"padded_size", cacheEntry.PiecePaddedSize,
		"tree_size", len(memtreeData))

	// Store tree data in database
	_, err = c.db.Exec(ctx, `
		UPDATE pdp_piece_memtrees
		SET tree_data = $1, tree_size = $2, status = 'completed', completed_at = NOW(), cache_task_id = NULL
		WHERE id = $3
	`, memtreeData, len(memtreeData), cacheEntry.ID)

	if err != nil {
		return false, c.markFailed(ctx, cacheEntry.ID, err)
	}

	return true, nil
}

func (c *CacheMemtreeTask) markFailed(ctx context.Context, cacheID int64, err error) error {
	_, execErr := c.db.Exec(ctx, `
		UPDATE pdp_piece_memtrees
		SET status = 'failed', error_msg = $1, cache_task_id = NULL
		WHERE id = $2
	`, err.Error(), cacheID)

	if execErr != nil {
		return xerrors.Errorf("failed to mark cache as failed: %w and original error: %w", execErr, err)
	}

	return xerrors.Errorf("cache task failed: %w", err)
}

func (c *CacheMemtreeTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	id := ids[0]
	return &id, nil
}

func (c *CacheMemtreeTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name:     "PDP_CacheMemtree",
		Cost:     resources.Resources{CPU: 2, RAM: 4 * (1 << 30)}, // 2 CPU, 4GB RAM
		MaxItems: c.max,
	}
}

func (c *CacheMemtreeTask) Select(ctx context.Context) ([]harmonytask.TaskID, error) {
	var ids []int64

	// Find pending cache entries without assigned task_id
	err := c.db.Select(ctx, &ids, `
		SELECT id FROM pdp_piece_memtrees
		WHERE status = 'pending' AND cache_task_id IS NULL
		LIMIT $1
	`, c.max)

	if err != nil {
		return nil, xerrors.Errorf("selecting pending caches: %w", err)
	}

	// Assign task IDs to selected entries
	taskIDs := make([]harmonytask.TaskID, len(ids))
	for i, id := range ids {
		taskID := harmonytask.TaskID(id)
		taskIDs[i] = taskID

		// Mark as assigned
		_, err := c.db.Exec(ctx, `
			UPDATE pdp_piece_memtrees
			SET cache_task_id = $1, status = 'caching', started_at = NOW()
			WHERE id = $2 AND status = 'pending'
		`, taskID, id)

		if err != nil {
			logCache.Warnw("failed to assign cache task", "cache_id", id, "error", err)
		}
	}

	return taskIDs, nil
}

func (c *CacheMemtreeTask) Cleanup(ctx context.Context, taskID harmonytask.TaskID) error {
	// Reset cache entry if task was interrupted
	_, err := c.db.Exec(ctx, `
		UPDATE pdp_piece_memtrees
		SET cache_task_id = NULL, status = 'pending', started_at = NULL
		WHERE cache_task_id = $1
	`, taskID)
	return err
}

var _ harmonytask.TaskInterface = (*CacheMemtreeTask)(nil)

// ScheduleCacheTaskForPiece creates a pending cache entry for a piece
// (called opportunistically when a cache miss is detected during proving)
func ScheduleCacheTaskForPiece(ctx context.Context, db *harmonydb.DB, pieceCID string, paddedSize int64) error {
	_, err := db.Exec(ctx, `
		INSERT INTO pdp_piece_memtrees (piece_cid, piece_padded_size, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (piece_cid, piece_padded_size) DO NOTHING
	`, pieceCID, paddedSize)

	if err != nil {
		return xerrors.Errorf("scheduling cache task: %w", err)
	}

	return nil
}
