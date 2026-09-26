// Package openpieces moves PDP pieces between piece-park and the cluster
// open-pieces hash space.
package openpieces

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

var log = logging.Logger("openpieces")

const (
	POLL_INTERVAL = 10 * time.Second
	POLL_BATCH    = 256
	PLACE_MAX     = 8
)

// PlaceTask moves a finalized PDP piece from piece-park into open-pieces on
// the disk its hash maps to. It runs on the node holding that disk.
type PlaceTask struct {
	db      *harmonydb.DB
	hs      *hashspace.Cluster
	local   *paths.Local
	pieceIO piecestore.PieceIO

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewPlaceTask(db *harmonydb.DB, hs *hashspace.Cluster, local *paths.Local, pieceIO piecestore.PieceIO) *PlaceTask {
	t := &PlaceTask{db: db, hs: hs, local: local, pieceIO: pieceIO}
	go t.poll(context.Background())
	return t
}

func (t *PlaceTask) poll(ctx context.Context) {
	ticker := time.NewTicker(POLL_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := t.schedule(ctx); err != nil {
			log.Errorw("scheduling open-pieces placement", "error", err)
		}
	}
}

func (t *PlaceTask) schedule(ctx context.Context) error {
	var refs []int64
	// A placed piece whose parked piece still has non-PDP refs waits here
	// until those refs are gone.
	err := t.db.Select(ctx, &refs, `SELECT hp.pdp_pieceref FROM hash_space_place hp
		WHERE hp.task_id IS NULL
		  AND (
			NOT hp.placed
			OR NOT EXISTS (
				SELECT 1 FROM parked_piece_refs own
				JOIN parked_piece_refs other ON other.piece_id = own.piece_id
				WHERE own.ref_id = hp.piece_ref
				  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_ref = other.ref_id))
		  )
		ORDER BY hp.created_at
		LIMIT $1`, POLL_BATCH)
	if err != nil {
		return xerrors.Errorf("selecting open-pieces placements: %w", err)
	}
	for _, ref := range refs {
		t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE hash_space_place SET task_id = $1
				WHERE pdp_pieceref = $2 AND task_id IS NULL`, id, ref)
			if err != nil {
				return false, xerrors.Errorf("claiming open-pieces placement: %w", err)
			}
			return n > 0, nil
		})
	}
	return nil
}

type parkedPiece struct {
	ID       int64 `db:"id"`
	RawSize  int64 `db:"piece_raw_size"`
	RefCount int64 `db:"ref_count"`
}

func (t *PlaceTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	var rows []struct {
		PdpRef      int64  `db:"pdp_pieceref"`
		PdpPieceCID string `db:"pdp_piece_cid"`
	}
	if err := t.db.Select(ctx, &rows, `SELECT pdp_pieceref, pdp_piece_cid FROM hash_space_place WHERE task_id = $1`, taskID); err != nil {
		return false, xerrors.Errorf("reading open-pieces placement: %w", err)
	}
	if len(rows) == 0 {
		return true, nil
	}
	ref := rows[0].PdpRef

	var parked []parkedPiece
	if err := t.db.Select(ctx, &parked, `SELECT pp.id, pp.piece_raw_size, pp.ref_count
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pr.id = $1`, ref); err != nil {
		return false, xerrors.Errorf("reading parked piece for pdp ref %d: %w", ref, err)
	}
	if len(parked) == 0 {
		return true, t.finish(ctx, ref)
	}
	pp := parked[0]
	pc, err := pieceCidV2(rows[0].PdpPieceCID, pp.RawSize)
	if err != nil {
		return false, err
	}

	// A live PDP ref cancels an unclaimed delete of the same piece; a claimed
	// delete finishes first and placement then starts again from piece-park.
	if _, err := t.db.Exec(ctx, `DELETE FROM hash_space_delete
		WHERE piece_cid = $1 AND task_id IS NULL
		  AND EXISTS (SELECT 1 FROM pdp_piecerefs WHERE id = $2)`, pc.String(), ref); err != nil {
		return false, xerrors.Errorf("cancelling open-pieces delete: %w", err)
	}
	var deletePending bool
	if err := t.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1)`, pc.String()).Scan(&deletePending); err != nil {
		return false, xerrors.Errorf("checking open-pieces delete: %w", err)
	}
	if deletePending {
		return true, t.release(ctx, taskID)
	}

	var placed bool
	if err := t.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM open_piece WHERE piece_cid = $1)`, pc.String()).Scan(&placed); err != nil {
		return false, xerrors.Errorf("checking open piece: %w", err)
	}

	var target string
	if !placed {
		digest, err := hashspace.CIDHash(pc)
		if err != nil {
			return false, err
		}
		target, err = t.hs.Target(ctx, digest)
		if err != nil {
			return false, err
		}
		if !t.hs.HasLocal(target) {
			return true, t.release(ctx, taskID)
		}
		size, err := t.write(ctx, target, pc, pp)
		if err != nil {
			return false, err
		}
		res, err := t.hs.RecordPlaced(ctx, pc, target, size, ref)
		if err != nil {
			return false, err
		}
		switch res {
		case hashspace.PlaceRefGone:
			if has, err := t.hs.HasRow(ctx, pc, target); err == nil && !has {
				if err := t.hs.DropLocal(target, pc); err != nil {
					log.Warnw("dropping unreferenced open piece", "piece", pc, "storage", target, "error", err)
				}
			}
			return true, t.finish(ctx, ref)
		case hashspace.PlaceDeletePending:
			return false, xerrors.Errorf("open-pieces delete of %s queued during placement", pc)
		}
		if err := t.hs.CheckCapacity(ctx, target); err != nil {
			log.Warnw("checking open-pieces capacity", "storage", target, "error", err)
		}
	}
	if _, err := t.db.Exec(ctx, `UPDATE hash_space_place SET placed = TRUE WHERE pdp_pieceref = $1`, ref); err != nil {
		return false, xerrors.Errorf("marking open-pieces placement: %w", err)
	}

	// The open-pieces copy exists at this point. Market, sealing and
	// aggregation read the parked piece through their own parked_piece_refs,
	// so its piece-park copy stays until only PDP refs remain; the poller
	// revisits the placement once that holds.
	shared, err := t.hasNonPDPRefs(ctx, pp.ID)
	if err != nil {
		return false, err
	}
	if shared {
		return true, t.release(ctx, taskID)
	}
	if err := t.pieceIO.RemovePiece(ctx, storiface.PieceNumber(pp.ID)); err != nil {
		return false, xerrors.Errorf("removing piece-park copy of %s: %w", pc, err)
	}
	if shared, err = t.hasNonPDPRefs(ctx, pp.ID); err != nil {
		return false, err
	}
	if shared {
		if err := t.restorePark(ctx, pc, pp); err != nil {
			return false, xerrors.Errorf("restoring piece-park copy of %s for a new non-PDP ref: %w", pc, err)
		}
		return true, t.release(ctx, taskID)
	}
	if err := t.finish(ctx, ref); err != nil {
		return false, err
	}
	return true, nil
}

// write puts pc on target. A sole-reference piece-park file on the same
// filesystem is renamed; otherwise the bytes are copied.
func (t *PlaceTask) write(ctx context.Context, target string, pc cid.Cid, pp parkedPiece) (int64, error) {
	if size, ok, err := t.hs.StatLocal(target, pc); err != nil {
		return 0, err
	} else if ok {
		if size != pp.RawSize {
			return 0, xerrors.Errorf("existing open piece %s is %d bytes, expected %d", pc, size, pp.RawSize)
		}
		return size, nil
	}

	if pp.RefCount == 1 {
		if src, ok := t.local.ExistingLocalFile(storiface.PieceNumber(pp.ID).Ref().ID, storiface.FTPiece); ok {
			size, err := t.hs.AdoptLocal(target, pc, src)
			switch {
			case err == nil:
				return size, nil
			case errors.Is(err, os.ErrExist):
				size, _, err := t.hs.StatLocal(target, pc)
				return size, err
			case !errors.Is(err, hashspace.ErrCrossDevice):
				return 0, xerrors.Errorf("renaming %s into open-pieces: %w", pc, err)
			}
		}
	}

	r, err := t.pieceIO.PieceReader(ctx, storiface.PieceNumber(pp.ID))
	if err != nil {
		return 0, xerrors.Errorf("opening piece-park copy of %s: %w", pc, err)
	}
	defer func() { _ = r.Close() }()
	size, err := t.hs.WriteLocal(target, pc, r)
	if err != nil {
		return 0, err
	}
	if size != pp.RawSize {
		_ = t.hs.DropLocal(target, pc)
		return 0, xerrors.Errorf("open piece %s is %d bytes, expected %d", pc, size, pp.RawSize)
	}
	return size, nil
}

func (t *PlaceTask) hasNonPDPRefs(ctx context.Context, parkedID int64) (bool, error) {
	var shared bool
	err := t.db.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM parked_piece_refs r
			WHERE r.piece_id = $1
			  AND NOT EXISTS (SELECT 1 FROM pdp_piecerefs pr WHERE pr.piece_ref = r.ref_id))`, parkedID).Scan(&shared)
	if err != nil {
		return false, xerrors.Errorf("checking non-PDP refs of parked piece %d: %w", parkedID, err)
	}
	return shared, nil
}

// restorePark writes the piece-park copy back from open-pieces when a non-PDP
// ref attached while the copy was being removed.
func (t *PlaceTask) restorePark(ctx context.Context, pc cid.Cid, pp parkedPiece) error {
	locs, err := t.hs.Locations(ctx, pc.String())
	if err != nil {
		return err
	}
	lastErr := xerrors.Errorf("%s has no open-pieces location", pc)
	for _, l := range locs {
		r, err := t.hs.Open(ctx, l.StorageID, pc)
		if err != nil {
			lastErr = err
			continue
		}
		err = t.pieceIO.WritePiece(ctx, nil, storiface.PieceNumber(pp.ID), pp.RawSize, r, storiface.PathStorage)
		_ = r.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func (t *PlaceTask) finish(ctx context.Context, ref int64) error {
	if _, err := t.db.Exec(ctx, `DELETE FROM hash_space_place WHERE pdp_pieceref = $1`, ref); err != nil {
		return xerrors.Errorf("removing open-pieces placement: %w", err)
	}
	return nil
}

// release hands the placement back to the poller, e.g. when its target disk
// is on another node.
func (t *PlaceTask) release(ctx context.Context, taskID harmonytask.TaskID) error {
	if _, err := t.db.Exec(ctx, `UPDATE hash_space_place SET task_id = NULL WHERE task_id = $1`, taskID); err != nil {
		return xerrors.Errorf("releasing open-pieces placement: %w", err)
	}
	return nil
}

func (t *PlaceTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	ctx := context.Background()
	var rows []struct {
		TaskID      int64         `db:"task_id"`
		PdpPieceCID string        `db:"pdp_piece_cid"`
		Placed      bool          `db:"placed"`
		RawSize     sql.NullInt64 `db:"piece_raw_size"`
	}
	taskIDs := make([]int64, len(ids))
	for i, id := range ids {
		taskIDs[i] = int64(id)
	}
	if err := t.db.Select(ctx, &rows, `SELECT hp.task_id, hp.pdp_piece_cid, hp.placed, pp.piece_raw_size
		FROM hash_space_place hp
		LEFT JOIN pdp_piecerefs pr ON pr.id = hp.pdp_pieceref
		LEFT JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		LEFT JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE hp.task_id = ANY($1)`, taskIDs); err != nil {
		return nil, xerrors.Errorf("reading open-pieces placements: %w", err)
	}

	// Placed pieces, and placements whose PDP ref is gone, only need
	// piece-park or queue cleanup, which any node can do.
	var out []harmonytask.TaskID
	pending := map[string][]int64{}
	for _, r := range rows {
		if r.Placed || !r.RawSize.Valid {
			out = append(out, harmonytask.TaskID(r.TaskID))
			continue
		}
		pc, err := pieceCidV2(r.PdpPieceCID, r.RawSize.Int64)
		if err != nil {
			continue
		}
		pending[pc.String()] = append(pending[pc.String()], r.TaskID)
	}
	if len(pending) == 0 {
		return out, nil
	}

	cids := make([]string, 0, len(pending))
	for c := range pending {
		cids = append(cids, c)
	}
	var existing []string
	if err := t.db.Select(ctx, &existing, `SELECT DISTINCT piece_cid FROM open_piece WHERE piece_cid = ANY($1)`, cids); err != nil {
		return nil, xerrors.Errorf("reading open pieces: %w", err)
	}
	for _, c := range existing {
		for _, id := range pending[c] {
			out = append(out, harmonytask.TaskID(id))
		}
		delete(pending, c)
	}

	resolve, err := t.hs.Targets(ctx)
	if err != nil {
		return nil, err
	}
	for c, tids := range pending {
		pc, err := cid.Parse(c)
		if err != nil {
			continue
		}
		digest, err := hashspace.CIDHash(pc)
		if err != nil {
			continue
		}
		target, err := resolve(digest)
		if err != nil || !t.hs.HasLocal(target) {
			continue
		}
		for _, id := range tids {
			out = append(out, harmonytask.TaskID(id))
		}
	}
	return out, nil
}

func pieceCidV2(pdpPieceCID string, rawSize int64) (cid.Cid, error) {
	v1, err := cid.Parse(pdpPieceCID)
	if err != nil {
		return cid.Undef, xerrors.Errorf("parsing piece cid %s: %w", pdpPieceCID, err)
	}
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(rawSize))
	if err != nil {
		return cid.Undef, xerrors.Errorf("piece cid v2 for %s: %w", pdpPieceCID, err)
	}
	return v2, nil
}

func (t *PlaceTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(PLACE_MAX),
		Name: tasknames.HashSpacePlace,
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 10,
	}
}

func (t *PlaceTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &PlaceTask{}
var _ = harmonytask.Reg(&PlaceTask{})
