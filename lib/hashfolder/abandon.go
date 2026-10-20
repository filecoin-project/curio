package hashfolder

import (
	"bytes"
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/hashspacesolver"
)

// MarkMoveStarted records that HashSpaceMove has begun this row. A later
// retry does not clear the stamp. The caller has the move row.
func (c *Cluster) MarkMoveStarted(ctx context.Context, id int64) error {
	_, err := c.db.Exec(ctx, `UPDATE hash_space_move_source SET started_at = COALESCE(started_at, NOW()) WHERE id = $1`, id)
	return err
}

// abandonStaleMoves drops move rows HashSpaceMove has not started within
// MOVE_STALE. The destination is the side that never ran, so its rows of the
// stuck range are marked problem and the range is owned by both disks.
func (c *Cluster) abandonStaleMoves(ctx context.Context) {
	var rows []struct {
		TaskID    int64  `db:"task_id"`
		ToStorage string `db:"to_storage"`
	}
	if err := c.db.Select(ctx, &rows, `SELECT DISTINCT task_id, to_storage FROM hash_space_move_source
		WHERE started_at IS NULL AND created_at < NOW() - ($1 * INTERVAL '1 second')`, int64(MOVE_STALE/time.Second)); err != nil {
		log.Warnw("reading stale hash space moves", "error", err)
		return
	}
	seen := map[int64]struct{}{}
	for _, row := range rows {
		if _, ok := seen[row.TaskID]; ok {
			continue
		}
		seen[row.TaskID] = struct{}{}
		if err := c.AbandonMoveTask(ctx, row.TaskID, row.ToStorage); err != nil {
			log.Warnw("abandoning stale hash space move", "task", row.TaskID, "error", err)
		}
	}
}

// AbandonMoveTask gives each of the task's remaining intervals to both of its
// disks, marks the troubled owner's rows problem, and deletes those move rows.
// troubled is the disk that failed: the destination when it is full or missing,
// the source when the source cannot be read. New files in a problem range go
// to the other owner. When no move rows remain, queued events are solved.
func (c *Cluster) AbandonMoveTask(ctx context.Context, taskID int64, troubled string) error {
	var left int
	err := c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var ms []moveSourceRow
		if err := tx.Select(&ms, `SELECT id, space, start_hash, end_hash, from_storage, to_storage, size
			FROM hash_space_move_source WHERE task_id = $1 ORDER BY id`, taskID); err != nil {
			return false, err
		}
		if len(ms) == 0 {
			left = 0
			return false, nil
		}
		bySpace := map[string][]moveSourceRow{}
		for _, m := range ms {
			bySpace[m.Space] = append(bySpace[m.Space], m)
		}
		for space, moves := range bySpace {
			var rs []rangeRow
			if err := tx.Select(&rs, `SELECT end_hash, storage_id, size, problem FROM hash_space_range WHERE space = $1 ORDER BY end_hash, storage_id`, space); err != nil {
				return false, err
			}
			var err error
			for _, m := range moves {
				rs, err = coOwnRanges(rs, m.StartHash, m.EndHash, m.FromStorage, m.ToStorage)
				if err != nil {
					return false, xerrors.Errorf("move source %d: %w", m.ID, err)
				}
				rs = markProblem(rs, m.StartHash, m.EndHash, troubled)
			}
			if err := writeSpaceRanges(tx, space, rs); err != nil {
				return false, err
			}
		}
		if _, err := tx.Exec(`DELETE FROM hash_space_move_source WHERE task_id = $1`, taskID); err != nil {
			return false, err
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM hash_space_move_source`).Scan(&left); err != nil {
			return false, err
		}
		log.Warnw("abandoned hash space move", "task", taskID, "problem", troubled, "ranges", len(ms))
		for _, m := range ms {
			log.Warnw("hash space move interval abandoned",
				"task", taskID, "problem", troubled, "move_source", m.ID, "space", m.Space,
				"from", m.FromStorage, "to", m.ToStorage,
				"start", hexEncode(m.StartHash), "end", hexEncode(m.EndHash), "bytes", m.Size)
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if left != 0 {
		return nil
	}
	if c.HasLocal(troubled) {
		if root, err := c.rootOf(troubled); err != nil {
			log.Warnw("publishing hash space sizes", "storage", troubled, "error", err)
		} else if _, err := c.publishDisk(ctx, troubled, root); err != nil {
			log.Warnw("publishing hash space sizes", "storage", troubled, "error", err)
		}
	} else if err := c.accountStorage(ctx, troubled); err != nil {
		log.Warnw("publishing hash space sizes", "storage", troubled, "error", err)
	}
	if err := c.reevaluate(ctx); err != nil {
		return err
	}
	if _, err := c.FixOverlaps(ctx); err != nil {
		log.Warnw("checking for misplaced open pieces", "error", err)
	}
	return nil
}

// shedProblemOwners drops a problem owner's row wherever another disk also
// owns that range. The other owner stays. Nothing is written when no such
// row exists.
func (c *Cluster) shedProblemOwners(ctx context.Context) error {
	var n int
	err := c.casTx(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var err error
		n, err = shedProblemOwnersTx(tx)
		return n > 0, err
	})
	if err == nil && n > 0 {
		log.Infow("dropped problem hash space co-owners", "ranges", n)
	}
	return err
}

// shedProblemOwnersTx deletes problem rows that still have another owner.
// The caller holds the map transaction. It returns how many rows were deleted.
func shedProblemOwnersTx(tx *harmonydb.Tx) (int, error) {
	n, err := tx.Exec(`DELETE FROM hash_space_range r
		WHERE r.problem
		AND EXISTS (
			SELECT 1 FROM hash_space_range o
			WHERE o.space = r.space AND o.end_hash = r.end_hash AND o.storage_id <> r.storage_id
		)`)
	return n, err
}

// deleteMovedSource removes the source copies of a move whose range is already
// owned by the destination. A missing file is fine. The caller has committed
// that ownership.
func (c *Cluster) deleteMovedSource(ctx context.Context, m *MoveSource) error {
	low, high := hexEncode(m.StartHash), hexEncode(m.EndHash)
	after := ""
	var removed int
	var lastLog time.Time
	// logRemoval records how many source files are gone. done is the final
	// line; a non-nil err is where the walk stopped.
	logRemoval := func(done bool, err error) {
		if err != nil {
			log.Warnw("removing moved hash space pieces from source stopped",
				"move_source", m.ID, "space", m.Space, "from", m.FromStorage, "removed", removed, "error", err)
			return
		}
		if !done && time.Since(lastLog) < 30*time.Second {
			return
		}
		lastLog = time.Now()
		log.Infow("removing moved hash space pieces from source",
			"move_source", m.ID, "space", m.Space, "from", m.FromStorage, "to", m.ToStorage, "removed", removed, "done", done)
	}
	for {
		batch, err := c.listPieceHashes(ctx, m.FromStorage, m.Space, low, high, after, LIST_PAGE)
		if err != nil {
			err = xerrors.Errorf("listing source %s after move %d: %w", m.FromStorage, m.ID, err)
			logRemoval(false, err)
			return err
		}
		if len(batch) == 0 {
			logRemoval(true, nil)
			return nil
		}
		for _, h := range batch {
			after = h
			if err := c.deleteHash(ctx, m.FromStorage, m.Space, h); err != nil {
				err = xerrors.Errorf("removing %s from %s: %w", h, m.FromStorage, err)
				logRemoval(false, err)
				return err
			}
			removed++
			logRemoval(false, nil)
		}
		if len(batch) < LIST_PAGE {
			logRemoval(true, nil)
			return nil
		}
	}
}

// coOwnRanges makes both from and to owners of (start, end]. The interval
// must lie in a range owned by from. The piece is not merged into a neighbor,
// so each disk gains that range. Other co-owners of ends this does not rewrite
// are kept.
func coOwnRanges(rs []rangeRow, start, end []byte, from, to string) ([]rangeRow, error) {
	if from == "" || to == "" || from == to {
		return rs, nil
	}
	ends := onePerEndPrefer(rs, from)
	next, err := coOwnOne(ends, start, end, from, to)
	if err != nil {
		return nil, err
	}
	return keepCoOwners(rs, ends, next), nil
}

// coOwnOne splits (start, end] out of from's range and records from and to
// on that piece. ends must be one owner per end, and from must own the
// interval. Neighbors are not merged onto the shared piece.
func coOwnOne(ends []rangeRow, start, end []byte, from, to string) ([]rangeRow, error) {
	n := len(ends)
	if bytes.Equal(start, end) {
		if n != 1 || ends[0].StorageID != from {
			return nil, xerrors.Errorf("full-circle co-own needs one range owned by %s", from)
		}
		return []rangeRow{
			{EndHash: cloneBytes(ends[0].EndHash), StorageID: from, Size: ends[0].Size},
			{EndHash: cloneBytes(ends[0].EndHash), StorageID: to, Size: ends[0].Size},
		}, nil
	}
	idx := -1
	for i, r := range ends {
		rStart := ends[(i-1+n)%n].EndHash
		startOK := bytes.Equal(start, rStart) ||
			(hashspacesolver.Contains(rStart, r.EndHash, start) && !bytes.Equal(start, r.EndHash))
		if startOK && hashspacesolver.Contains(start, r.EndHash, end) && r.StorageID == from {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, xerrors.Errorf("no range of %s covers (%x, %x]", from, start, end)
	}
	rStart := ends[(idx-1+n)%n].EndHash
	full := hashspacesolver.Range{StartHash: rStart, EndHash: ends[idx].EndHash, Size: ends[idx].Size}
	var prefix int64
	if !bytes.Equal(start, rStart) {
		var err error
		if prefix, err = hashspacesolver.SliceSize(full, rStart, start); err != nil {
			return nil, err
		}
	}
	size, err := hashspacesolver.SliceSize(full, start, end)
	if err != nil {
		return nil, err
	}
	suffix := ends[idx].Size - prefix - size
	if suffix < 0 {
		return nil, xerrors.Errorf("range (%x, %x] holds %d, split into %d + %d", rStart, ends[idx].EndHash, ends[idx].Size, prefix, size)
	}
	out := make([]rangeRow, 0, n+3)
	for i, r := range ends {
		if i != idx {
			out = append(out, r)
			continue
		}
		if !bytes.Equal(start, rStart) {
			out = append(out, rangeRow{EndHash: cloneBytes(start), StorageID: from, Size: prefix})
		}
		out = append(out,
			rangeRow{EndHash: cloneBytes(end), StorageID: from, Size: size},
			rangeRow{EndHash: cloneBytes(end), StorageID: to, Size: size},
		)
		if !bytes.Equal(end, r.EndHash) {
			out = append(out, rangeRow{EndHash: r.EndHash, StorageID: from, Size: suffix})
		}
	}
	sortRanges(out)
	return out, nil
}

// markProblem sets problem on owner's rows whose end lies in (start, end].
// Those rows are the stuck range. Other ranges on that disk are unchanged.
func markProblem(rs []rangeRow, start, end []byte, owner string) []rangeRow {
	for i := range rs {
		if rs[i].StorageID == owner && hashspacesolver.Contains(start, end, rs[i].EndHash) {
			rs[i].Problem = true
		}
	}
	return rs
}

// dropOtherOwners removes owners other than owner from ranges whose end lies
// in (start, end]. Ends outside the interval are unchanged.
func dropOtherOwners(rs []rangeRow, start, end []byte, owner string) []rangeRow {
	ends := onePerEnd(rs)
	inside := map[string]bool{}
	for _, r := range ends {
		if hashspacesolver.Contains(start, end, r.EndHash) {
			inside[string(r.EndHash)] = true
		}
	}
	out := make([]rangeRow, 0, len(rs))
	for _, r := range rs {
		if inside[string(r.EndHash)] && r.StorageID != owner {
			continue
		}
		out = append(out, r)
	}
	return out
}

// onePerEnd returns one row per end hash, the lowest storage id. rs is not
// modified. The result walks the circle without a second owner looking like
// another range.
func onePerEnd(rs []rangeRow) []rangeRow {
	return onePerEndPrefer(rs, "")
}

// onePerEndPrefer returns one row per end hash. An end owned by prefer uses
// that row; otherwise the lowest storage id. rs is not modified.
func onePerEndPrefer(rs []rangeRow, prefer string) []rangeRow {
	if len(rs) == 0 {
		return nil
	}
	cp := append([]rangeRow(nil), rs...)
	sortRanges(cp)
	// sortRanges orders by end only. Stable-enough: walk and pick.
	out := make([]rangeRow, 0, len(cp))
	for i := 0; i < len(cp); {
		j := i + 1
		for j < len(cp) && bytes.Equal(cp[j].EndHash, cp[i].EndHash) {
			j++
		}
		pick := cp[i]
		for _, r := range cp[i:j] {
			if prefer != "" && r.StorageID == prefer {
				pick = r
				break
			}
			if r.StorageID < pick.StorageID {
				pick = r
			}
		}
		out = append(out, pick)
		i = j
	}
	return out
}

// ownerForWrite is the disk a new file in this range is written to. rows are
// the owners of one end. A problem owner is skipped when another owner is
// not marked. rows must be non-empty.
func ownerForWrite(rows []rangeRow) string {
	chosen := ""
	good := 0
	for _, r := range rows {
		if !r.Problem {
			good++
			chosen = r.StorageID
		}
	}
	if good == 1 {
		return chosen
	}
	pool := rows
	if good > 1 {
		pool = nil
		for _, r := range rows {
			if !r.Problem {
				pool = append(pool, r)
			}
		}
	}
	chosen = pool[0].StorageID
	for _, r := range pool[1:] {
		if r.StorageID < chosen {
			chosen = r.StorageID
		}
	}
	return chosen
}

// rowsAt returns every row that owns the range ending at end.
func rowsAt(rs []rangeRow, end []byte) []rangeRow {
	var out []rangeRow
	for _, r := range rs {
		if bytes.Equal(r.EndHash, end) {
			out = append(out, r)
		}
	}
	return out
}

// ownersOfEnd lists every disk that owns the range ending at end. rs may
// contain more than one row for that end.
func ownersOfEnd(rs []rangeRow, end []byte) []string {
	var ids []string
	for _, r := range rs {
		if bytes.Equal(r.EndHash, end) {
			ids = append(ids, r.StorageID)
		}
	}
	return ids
}

// ownsEnd reports whether storageID owns the range ending at end.
func ownsEnd(rs []rangeRow, end []byte, storageID string) bool {
	for _, r := range rs {
		if bytes.Equal(r.EndHash, end) && r.StorageID == storageID {
			return true
		}
	}
	return false
}

// keepCoOwners adds co-owners from orig onto next for ends whose representative
// owner did not change. Their size is that owner's size in next. ends is the
// one-owner circle next was built from.
func keepCoOwners(orig, ends, next []rangeRow) []rangeRow {
	rep := map[string]string{}
	for _, r := range ends {
		rep[string(r.EndHash)] = r.StorageID
	}
	now := map[string]string{}
	sizeOf := map[string]int64{}
	for _, r := range next {
		end := string(r.EndHash)
		if _, ok := now[end]; ok {
			continue
		}
		now[end] = r.StorageID
		sizeOf[end] = r.Size
	}
	out := append([]rangeRow(nil), next...)
	seen := map[string]bool{}
	for _, r := range next {
		seen[string(r.EndHash)+"\x00"+r.StorageID] = true
	}
	for _, r := range orig {
		end := string(r.EndHash)
		if rep[end] == "" || r.StorageID == rep[end] {
			continue
		}
		if now[end] != rep[end] || seen[end+"\x00"+r.StorageID] {
			continue
		}
		r.Size = sizeOf[end]
		out = append(out, r)
		seen[end+"\x00"+r.StorageID] = true
	}
	sortRanges(out)
	return out
}

// groupOwners collapses rows that share an end hash into one solver range.
// The primary owner is the one that is not problem when exactly one such
// owner exists; otherwise the lowest storage id. known reports whether a
// storage id is a hash space disk. Problem lists owners marked problem.
func groupOwners(rs []rangeRow, known map[string]bool) ([]hashspacesolver.Range, error) {
	var out []hashspacesolver.Range
	for i := 0; i < len(rs); {
		j := i + 1
		for j < len(rs) && bytes.Equal(rs[j].EndHash, rs[i].EndHash) {
			j++
		}
		owners := rs[i:j]
		for _, r := range owners {
			if !known[r.StorageID] {
				return nil, xerrors.Errorf("range owner %s is not a hash space disk", r.StorageID)
			}
		}
		primary := owners[0]
		non := 0
		for _, r := range owners {
			if !r.Problem {
				non++
				primary = r
			}
		}
		if non != 1 {
			primary = owners[0]
			for _, r := range owners[1:] {
				if r.StorageID < primary.StorageID {
					primary = r
				}
			}
		}
		rg := hashspacesolver.Range{EndHash: primary.EndHash, Size: primary.Size, StorageID: primary.StorageID}
		for _, r := range owners {
			if r.Problem {
				rg.Problem = append(rg.Problem, r.StorageID)
			}
			if r.StorageID == primary.StorageID {
				continue
			}
			rg.Also = append(rg.Also, r.StorageID)
		}
		out = append(out, rg)
		i = j
	}
	return out, nil
}
