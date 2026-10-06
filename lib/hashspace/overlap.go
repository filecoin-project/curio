package hashspace

import (
	"context"
	"os"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/hashspacesolver"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

// OVERLAP_CHECK_INTERVAL is how often a node looks for piece files held
// outside their range owner. A node with a has_misplaced disk checks on every
// refresh instead.
const OVERLAP_CHECK_INTERVAL = 10 * time.Minute

// FixOverlaps schedules a move for every open-pieces or acl-pieces range that
// has files on a local disk which does not own it: files migrated from
// piece-park, a range a layout import took, a move whose
// source cleanup failed, or a duplicate copy. The move copies what the owner
// lacks and then drops the other disk's copies. Between each pair of disks
// one pass moves at most hashspacesolver.CLAIM_STEP_PERCENT of the smaller
// capacity; the next pass runs after those moves finish. Nothing is planned
// while any move is in flight, since an in-flight move holds pieces on two
// disks by design. A local has_misplaced disk with nothing left outside its
// ranges has the flag cleared. It returns the number of moves planned.
func (c *Cluster) FixOverlaps(ctx context.Context) (int, error) {
	if c.open == nil || len(c.roots) == 0 {
		return 0, nil
	}
	c.dropDeadMoves(ctx)
	var busy bool
	if err := c.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM hash_space_move_source)`).Scan(&busy); err != nil {
		return 0, err
	}
	if busy {
		return 0, nil
	}
	var disks []struct {
		StorageID string `db:"storage_id"`
		Capacity  int64  `db:"capacity"`
		Misplaced bool   `db:"has_misplaced"`
	}
	if err := c.db.Select(ctx, &disks, `SELECT storage_id, capacity, has_misplaced FROM hash_space_disk`); err != nil {
		return 0, err
	}
	capOf := map[string]int64{}
	for _, d := range disks {
		capOf[d.StorageID] = d.Capacity
	}
	type pair struct{ from, to string }
	budget := map[pair]int64{}

	type stray struct {
		space string
		start []byte
		end   []byte
		from  string
		to    string
		size  int64
	}
	var found []stray
	hasStray := map[string]bool{}
	for _, kind := range spaceKinds {
		var rs []rangeRow
		if err := c.db.Select(ctx, &rs, `SELECT end_hash, storage_id FROM hash_space_range WHERE space = $1 ORDER BY end_hash`, kind); err != nil {
			return 0, err
		}
		if len(rs) == 0 {
			continue
		}
		for id, root := range c.roots {
			for i, r := range rs {
				if r.StorageID == id {
					continue
				}
				start := rs[(i-1+len(rs))%len(rs)].EndHash
				low, high := hexEncode(start), hexEncode(r.EndHash)
				batch, err := c.listPieceHashes(ctx, id, kind, low, high, "", 1)
				if err != nil {
					return 0, err
				}
				if len(batch) == 0 {
					continue
				}
				hasStray[id] = true
				p := pair{id, r.StorageID}
				left, ok := budget[p]
				if !ok {
					left = min(capOf[id], capOf[r.StorageID]) * hashspacesolver.CLAIM_STEP_PERCENT / 100
				}
				if left <= 0 {
					continue
				}
				end, n, err := /* leadingBytes */ func(low, high string, limit int64) ([]byte, int64, error) {
					// The interval is walked in circle order from low. A
					// wrapping interval is listed as (low, top] then up to high.
					top := strings.Repeat("ff", HASH_BYTES)
					segs := [][2]string{{low, high}}
					switch {
					case low == top:
						segs = [][2]string{{top, high}}
					case low >= high:
						segs = [][2]string{{low, top}, {top, high}}
					}
					var total int64
					cut := ""
					for _, seg := range segs {
						after := ""
						for {
							page, err := c.listPieceHashes(ctx, id, kind, seg[0], seg[1], after, LIST_PAGE)
							if err != nil {
								return nil, 0, err
							}
							for _, h := range page {
								after = h
								path, err := piecePath(root, kind, h)
								if err != nil {
									return nil, 0, err
								}
								info, err := os.Stat(path)
								if os.IsNotExist(err) {
									continue
								}
								if err != nil {
									return nil, 0, err
								}
								if cut != "" && total+info.Size() > limit {
									b, err := decodeHash(cut)
									return b, total, err
								}
								total += info.Size()
								cut = h
							}
							if len(page) < LIST_PAGE {
								break
							}
						}
					}
					b, err := decodeHash(high)
					return b, total, err
				}(low, high, left)
				if err != nil {
					return 0, err
				}
				if n == 0 {
					continue
				}
				budget[p] = left - n
				found = append(found, stray{space: kind, start: start, end: end, from: id, to: r.StorageID, size: n})
			}
		}
	}
	if len(found) == 0 {
		for _, d := range disks {
			if !d.Misplaced || !c.HasLocal(d.StorageID) || hasStray[d.StorageID] {
				continue
			}
			if _, err := c.db.Exec(ctx, `UPDATE hash_space_disk SET has_misplaced = FALSE, updated_at = NOW() WHERE storage_id = $1`, d.StorageID); err != nil {
				return 0, err
			}
			for _, sp := range []*Space{c.open, c.acl} {
				if sp != nil {
					sp.SetMisplacedOn(c.roots[d.StorageID], false)
				}
			}
			log.Infow("misplaced pieces reached their range owners", "storage", d.StorageID)
		}
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
				s.space, s.start, s.end, s.from, s.to, s.size, id)
			if err != nil {
				return false, xerrors.Errorf("scheduling misplaced %s move: %w", s.space, err)
			}
			if n > 0 {
				planned++
				log.Warnw("pieces held outside their range owner; scheduling move",
					"space", s.space, "from", s.from, "to", s.to, "bytes", s.size,
					"start", hexEncode(s.start), "end", hexEncode(s.end))
			}
		}
		return planned > 0, nil
	})
	return planned, err
}
