// Package openpieces moves PDP pieces between piece-park and the cluster
// open-pieces hash space.
package openpieces

import (
	"context"
	"database/sql"
	"errors"
	"os"

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

const PLACE_MAX = 8

// PlaceAdder is the AddTaskFunc to pass to harmonytask.TxWithTask with
// QueuePlace.
func PlaceAdder() harmonytask.AddTaskFunc {
	return harmonytask.AdderFor(tasknames.HashSpacePlace)
}

// QueuePlace queues the pdp_piecerefs row pdpRef for placement into
// open-pieces from inside a harmonytask.TxWithTask body that uses
// PlaceAdder, in the transaction that creates the row. With id 0 it asks for
// a task, unless no node has open-pieces disks.
func QueuePlace(tx *harmonydb.Tx, id harmonytask.TaskID, pdpRef int64) error {
	if id == 0 {
		var enabled bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM hash_space_disk)`).Scan(&enabled); err != nil {
			return xerrors.Errorf("checking for open-pieces disks: %w", err)
		}
		if enabled {
			return harmonytask.ErrNeedTask
		}
		return nil
	}
	_, err := tx.Exec(`INSERT INTO hash_space_place (pdp_pieceref, pdp_piece_cid, piece_ref, task_id)
		SELECT id, piece_cid, piece_ref, $2 FROM pdp_piecerefs WHERE id = $1
		ON CONFLICT (pdp_pieceref) DO NOTHING`, pdpRef, id)
	if err != nil {
		return xerrors.Errorf("queueing open-pieces placement of pdp ref %d: %w", pdpRef, err)
	}
	return nil
}

// PlaceTask moves finalized PDP pieces from piece-park into open-pieces on
// the disk each hash maps to. It runs on the node holding that disk; pieces
// that map elsewhere are handed to a new task for that node.
type PlaceTask struct {
	db      *harmonydb.DB
	hs      *hashspace.Cluster
	local   *paths.Local
	pieceIO piecestore.PieceIO

	TF promise.Promise[harmonytask.AddTaskFunc]
}

func NewPlaceTask(db *harmonydb.DB, hs *hashspace.Cluster, local *paths.Local, pieceIO piecestore.PieceIO) *PlaceTask {
	return &PlaceTask{db: db, hs: hs, local: local, pieceIO: pieceIO}
}

type parkedPiece struct {
	ID       int64 `db:"id"`
	RawSize  int64 `db:"piece_raw_size"`
	RefCount int64 `db:"ref_count"`
}

type placeRow struct {
	PdpRef      int64  `db:"pdp_pieceref"`
	PdpPieceCID string `db:"pdp_piece_cid"`
}

func (t *PlaceTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (bool, error) {
	var rows []placeRow
	if err := t.db.Select(ctx, &rows, `SELECT pdp_pieceref, pdp_piece_cid FROM hash_space_place WHERE task_id = $1 ORDER BY pdp_pieceref`, taskID); err != nil {
		return false, xerrors.Errorf("reading open-pieces placement: %w", err)
	}
	for _, r := range rows {
		if !stillOwned() {
			return false, xerrors.Errorf("lost ownership of open-pieces placement task %d", taskID)
		}
		if err := t.placeOne(ctx, taskID, r); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (t *PlaceTask) placeOne(ctx context.Context, taskID harmonytask.TaskID, row placeRow) error {
	ref := row.PdpRef
	var parked []parkedPiece
	if err := t.db.Select(ctx, &parked, `SELECT pp.id, pp.piece_raw_size, pp.ref_count
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pr.id = $1`, ref); err != nil {
		return xerrors.Errorf("reading parked piece for pdp ref %d: %w", ref, err)
	}
	if len(parked) == 0 {
		return t.finish(ctx, ref)
	}
	pp := parked[0]
	pc, err := pieceCidV2(row.PdpPieceCID, pp.RawSize)
	if err != nil {
		return err
	}

	// A drop queued before this PDP ref existed skips itself once it sees
	// the ref; one that already started removes the files first, so wait.
	var dropStarted bool
	if err := t.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $1 AND started)`, pc.String()).Scan(&dropStarted); err != nil {
		return xerrors.Errorf("checking open-pieces delete: %w", err)
	}
	if dropStarted {
		return xerrors.Errorf("open-pieces delete of %s is running; placing after it", pc)
	}

	placed, err := t.hs.HasFile(ctx, pc)
	if err != nil {
		return xerrors.Errorf("checking open piece: %w", err)
	}

	if !placed {
		digest, err := hashspace.CIDHash(pc)
		if err != nil {
			return err
		}
		target, err := t.hs.Target(ctx, digest)
		if err != nil {
			return err
		}
		if !t.hs.HasLocal(target) {
			return t.handoff(ctx, taskID, ref)
		}
		_, existed, err := t.hs.StatLocal(target, pc)
		if err != nil {
			return err
		}
		if _, err := t.write(ctx, target, pc, pp); err != nil {
			return err
		}
		var refOK, dropStarted bool
		if err := t.db.QueryRow(ctx, `SELECT
				EXISTS (SELECT 1 FROM pdp_piecerefs WHERE id = $1),
				EXISTS (SELECT 1 FROM hash_space_delete WHERE piece_cid = $2 AND started)`,
			ref, pc.String()).Scan(&refOK, &dropStarted); err != nil {
			return xerrors.Errorf("checking placement of %s: %w", pc, err)
		}
		if !refOK {
			if !existed {
				if err := t.hs.DropLocal(target, pc); err != nil {
					log.Warnw("dropping unreferenced open piece", "piece", pc, "storage", target, "error", err)
				}
			}
			return t.finish(ctx, ref)
		}
		if dropStarted {
			if !existed {
				if err := t.hs.DropLocal(target, pc); err != nil {
					log.Warnw("dropping open piece queued for delete", "piece", pc, "storage", target, "error", err)
				}
			}
			return xerrors.Errorf("open-pieces delete of %s started during placement", pc)
		}
		placed, err = t.hs.HasFile(ctx, pc)
		if err != nil {
			return err
		}
		if !placed {
			return xerrors.Errorf("open piece %s disappeared during placement", pc)
		}
		if err := t.hs.CheckCapacity(ctx, target); err != nil {
			log.Warnw("checking open-pieces capacity", "storage", target, "error", err)
		}
	}

	// Market, sealing and aggregation read the parked piece through their
	// own parked_piece_refs. While they hold one, the piece-park copy stays
	// and the regular piece-park cleanup removes it once every ref, PDP's
	// included, is gone.
	shared, err := t.hasNonPDPRefs(ctx, pp.ID)
	if err != nil {
		return err
	}
	if !shared {
		if err := t.pieceIO.RemovePiece(ctx, storiface.PieceNumber(pp.ID)); err != nil {
			return xerrors.Errorf("removing piece-park copy of %s: %w", pc, err)
		}
		if shared, err = t.hasNonPDPRefs(ctx, pp.ID); err != nil {
			return err
		}
		if shared {
			if err := t.restorePark(ctx, pc, pp); err != nil {
				return xerrors.Errorf("restoring piece-park copy of %s for a new non-PDP ref: %w", pc, err)
			}
		}
	}
	return t.finish(ctx, ref)
}

// handoff moves one placement to a new task, so the node holding its target
// disk picks it up.
func (t *PlaceTask) handoff(ctx context.Context, from harmonytask.TaskID, ref int64) error {
	_, err := harmonytask.TxWithTask(ctx, t.db, t.TF.Val(ctx), func(tx *harmonydb.Tx, id harmonytask.TaskID) (bool, error) {
		if id == 0 {
			return false, harmonytask.ErrNeedTask
		}
		n, err := tx.Exec(`UPDATE hash_space_place SET task_id = $1 WHERE pdp_pieceref = $2 AND task_id = $3`, id, ref, from)
		return n > 0, err
	})
	if err != nil {
		return xerrors.Errorf("handing off open-pieces placement of pdp ref %d: %w", ref, err)
	}
	return nil
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

func (t *PlaceTask) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	ctx := context.Background()
	var rows []struct {
		TaskID      int64         `db:"task_id"`
		PdpPieceCID string        `db:"pdp_piece_cid"`
		RawSize     sql.NullInt64 `db:"piece_raw_size"`
	}
	taskIDs := make([]int64, len(ids))
	for i, id := range ids {
		taskIDs[i] = int64(id)
	}
	if err := t.db.Select(ctx, &rows, `SELECT hp.task_id, hp.pdp_piece_cid, pp.piece_raw_size
		FROM hash_space_place hp
		LEFT JOIN pdp_piecerefs pr ON pr.id = hp.pdp_pieceref
		LEFT JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		LEFT JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE hp.task_id = ANY($1)`, taskIDs); err != nil {
		return nil, xerrors.Errorf("reading open-pieces placements: %w", err)
	}

	// A task is taken when any of its pieces can be handled here: its PDP
	// ref is gone, or its target disk is local. A file that is already on
	// that disk is noticed in Do. Tasks with no rows left are taken
	// anywhere so they finish.
	accept := map[int64]bool{}
	for _, id := range taskIDs {
		accept[id] = true
	}
	pending := map[string][]int64{}
	for _, r := range rows {
		accept[r.TaskID] = false
	}
	for _, r := range rows {
		if !r.RawSize.Valid {
			accept[r.TaskID] = true
			continue
		}
		pc, err := pieceCidV2(r.PdpPieceCID, r.RawSize.Int64)
		if err != nil {
			continue
		}
		pending[pc.String()] = append(pending[pc.String()], r.TaskID)
	}

	if len(pending) > 0 {
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
				accept[id] = true
			}
		}
	}

	var out []harmonytask.TaskID
	for _, id := range ids {
		if accept[int64(id)] {
			out = append(out, id)
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
