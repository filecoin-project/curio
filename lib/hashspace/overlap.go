package hashspace

import (
	"context"
	"path/filepath"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

// OVERLAP_CHECK_INTERVAL is how often a node looks for open-pieces files held
// outside their range owner.
const OVERLAP_CHECK_INTERVAL = 10 * time.Minute

// FixOverlaps schedules a move for every open-pieces range that has files on
// a local disk which does not own it: after a layout import took the range,
// a move whose source cleanup failed, or a duplicate copy. The move copies
// what the owner lacks and then drops the other disk's copies. Nothing is
// planned while any move is in flight, since an in-flight move holds pieces
// on two disks by design. It returns the number of moves planned.
func (c *Cluster) FixOverlaps(ctx context.Context) (int, error) {
	if c.open == nil || len(c.roots) == 0 {
		return 0, nil
	}
	var busy bool
	if err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&busy); err != nil {
		return 0, err
	}
	if busy {
		return 0, nil
	}
	var rs []rangeRow
	if err := c.db.Select(ctx, &rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, DIR_OPEN); err != nil {
		return 0, err
	}
	if len(rs) == 0 {
		return 0, nil
	}

	type stray struct {
		start []byte
		end   []byte
		from  string
		to    string
		size  int64
	}
	var found []stray
	for id, root := range c.roots {
		for i, r := range rs {
			if r.StorageID == id {
				continue
			}
			start := rs[(i-1+len(rs))%len(rs)].EndHash
			low, high := hexEncode(start), hexEncode(r.EndHash)
			batch, err := c.listPieceHashes(ctx, id, low, high, "", 1)
			if err != nil {
				return 0, err
			}
			if len(batch) == 0 {
				continue
			}
			n, err := sumInterval(filepath.Join(root, DIR_OPEN), low, high)
			if err != nil {
				return 0, err
			}
			found = append(found, stray{start: start, end: r.EndHash, from: id, to: r.StorageID, size: n})
		}
	}
	if len(found) == 0 {
		return 0, nil
	}

	addMove := harmonytask.AdderFor(tasknames.HashSpaceMove)
	if addMove == nil {
		return 0, nil
	}
	var planned int
	err := c.casTaskTx(ctx, addMove, func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
		planned = 0
		var busy bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&busy); err != nil {
			return false, err
		}
		if busy {
			return false, nil
		}
		if id == 0 {
			return false, harmonytask.ErrNeedTask
		}
		for _, s := range found {
			n, err := tx.Exec(`INSERT INTO hash_space_move_source (space, start_hash, end_hash, from_storage, to_storage, size, task_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (space, start_hash, end_hash) DO NOTHING`,
				DIR_OPEN, s.start, s.end, s.from, s.to, s.size, id)
			if err != nil {
				return false, xerrors.Errorf("scheduling misplaced open pieces move: %w", err)
			}
			if n > 0 {
				planned++
				log.Warnw("open pieces held outside their range owner; scheduling move",
					"from", s.from, "to", s.to, "bytes", s.size,
					"start", hexEncode(s.start), "end", hexEncode(s.end))
			}
		}
		return planned > 0, nil
	})
	return planned, err
}
