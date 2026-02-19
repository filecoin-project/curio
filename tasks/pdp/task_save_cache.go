package pdp

import (
	"context"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/savecache"
	"github.com/filecoin-project/curio/market/indexstore"
)

const MinSizeForCache = uint64(100 * 1024 * 1024)

type TaskPDPSaveCache struct {
	db  *harmonydb.DB
	cpr *cachedreader.CachedPieceReader
	idx *indexstore.IndexStore
}

func NewTaskPDPSaveCache(db *harmonydb.DB, cpr *cachedreader.CachedPieceReader, idx *indexstore.IndexStore) *TaskPDPSaveCache {
	return &TaskPDPSaveCache{
		db:  db,
		cpr: cpr,
		idx: idx,
	}
}

func (t *TaskPDPSaveCache) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var tasks []struct {
		ID       int64  `db:"id"`
		PieceCID string `db:"piece_cid"`
		RawSize  uint64 `db:"piece_raw_size"`
	}

	err = t.db.Select(ctx, &tasks, `
		SELECT pr.id, pr.piece_cid, pp.piece_raw_size
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs pprf ON pprf.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = pprf.piece_id
		WHERE pr.save_cache_task_id = $1 AND pr.needs_save_cache = TRUE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting save cache task params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 save cache task, got %d", len(tasks))
	}

	task := tasks[0]

	pcidV1, err := cid.Parse(task.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse piece cid: %w", err)
	}

	// Construct v2 CID from v1 + raw size (needed for Cassandra cache layer keying)
	pcidV2, err := commcid.PieceCidV2FromV1(pcidV1, task.RawSize)
	if err != nil {
		return false, xerrors.Errorf("failed to construct piece cid v2: %w", err)
	}

	// Build the merkle tree and save a middle layer for fast proving
	// Only for pieces larger than 100 MiB
	if task.RawSize > MinSizeForCache {
		has, _, err := t.idx.GetPDPLayerIndex(ctx, pcidV2)
		if err != nil {
			return false, xerrors.Errorf("failed to check if piece has PDP layer: %w", err)
		}

		if !has {
			cp := savecache.NewCommPWithSize(task.RawSize)
			reader, _, err := t.cpr.GetSharedPieceReader(ctx, pcidV1)
			if err != nil {
				return false, xerrors.Errorf("failed to get shared piece reader: %w", err)
			}
			defer func() {
				_ = reader.Close()
			}()

			n, err := io.CopyBuffer(cp, reader, make([]byte, 4<<20))
			if err != nil {
				return false, xerrors.Errorf("failed to copy piece data to commP: %w", err)
			}
			if uint64(n) != task.RawSize {
				return false, xerrors.Errorf("copied size does not match expected piece size: %d != %d", n, task.RawSize)
			}

			digest, _, lidx, _, snap, err := cp.DigestWithSnapShot()
			if err != nil {
				return false, xerrors.Errorf("failed to get piece digest: %w", err)
			}

			computedV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
			if err != nil {
				return false, xerrors.Errorf("failed to create commP: %w", err)
			}

			if !computedV2.Equals(pcidV2) {
				return false, xerrors.Errorf("commP cid does not match piece cid: %s != %s", computedV2.String(), pcidV2.String())
			}

			leafs := make([]indexstore.NodeDigest, len(snap))
			for i, s := range snap {
				leafs[i] = indexstore.NodeDigest{
					Layer: lidx,
					Hash:  s.Hash,
					Index: int64(i),
				}
			}

			err = t.idx.AddPDPLayer(ctx, pcidV2, leafs)
			if err != nil {
				return false, xerrors.Errorf("failed to add PDP layer cache: %w", err)
			}
		}
	}

	// Mark task as completed
	n, err := t.db.Exec(ctx, `UPDATE pdp_piecerefs SET needs_save_cache = FALSE, save_cache_task_id = NULL
								WHERE id = $1 AND save_cache_task_id = $2`, task.ID, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_piecerefs: %w", err)
	}

	if n != 1 {
		return false, xerrors.Errorf("failed to update pdp_piecerefs: expected 1 row but %d rows updated", n)
	}

	return true, nil
}

func (t *TaskPDPSaveCache) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}

func (t *TaskPDPSaveCache) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPv0_SaveCache",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(3*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *TaskPDPSaveCache) schedule(_ context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var pendings []struct {
				ID int64 `db:"id"`
			}

			err := tx.Select(&pendings, `SELECT id FROM pdp_piecerefs
				WHERE save_cache_task_id IS NULL
				AND needs_save_cache = TRUE
				ORDER BY created_at ASC LIMIT 1`)
			if err != nil {
				return false, xerrors.Errorf("getting pending save cache tasks: %w", err)
			}

			if len(pendings) == 0 {
				return false, nil
			}

			pending := pendings[0]
			n, err := tx.Exec(`UPDATE pdp_piecerefs SET save_cache_task_id = $1
				WHERE save_cache_task_id IS NULL AND id = $2`, id, pending.ID)
			if err != nil {
				return false, xerrors.Errorf("updating save cache task id: %w", err)
			}
			if n == 0 {
				return false, nil // Another task already claimed this piece
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})
	}

	return nil
}

func (t *TaskPDPSaveCache) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &TaskPDPSaveCache{}
var _ = harmonytask.Reg(&TaskPDPSaveCache{})
