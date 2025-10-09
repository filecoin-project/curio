package pdp

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
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
	"github.com/filecoin-project/curio/market/mk20"
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
	var saveCaches []struct {
		ID        string `db:"id"`
		PieceCid  string `db:"piece_cid_v2"`
		DataSetID int64  `db:"data_set_id"`
		PieceRef  string `db:"piece_ref"`
	}

	err = t.db.Select(ctx, &saveCaches, `SELECT id, piece_cid_v2, data_set_id, piece_ref FROM pdp_pipeline WHERE save_cache_task_id = $1 AND after_save_cache = FALSE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to select rows from pipeline: %w", err)
	}

	if len(saveCaches) == 0 {
		return false, xerrors.Errorf("no saveCaches found for taskID %d", taskID)
	}

	if len(saveCaches) > 1 {
		return false, xerrors.Errorf("multiple saveCaches found for taskID %d", taskID)
	}

	sc := saveCaches[0]

	pcid, err := cid.Parse(sc.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("failed to parse piece cid: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid)
	if err != nil {
		return false, xerrors.Errorf("failed to get piece info: %w", err)
	}

	// Let's build the merkle Tree again (commP) and save a middle layer for fast proving
	// for pieces larger than 100 MiB
	if pi.RawSize > MinSizeForCache {
		has, _, err := t.idx.GetPDPLayerIndex(ctx, pcid)
		if err != nil {
			return false, xerrors.Errorf("failed to check if piece has PDP layer: %w", err)
		}

		if !has {
			cp := savecache.NewCommPWithSize(pi.RawSize)
			reader, _, err := t.cpr.GetSharedPieceReader(ctx, pcid, false)
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

			digest, _, lidx, _, snap, err := cp.DigestWithSnapShot()
			if err != nil {
				return false, xerrors.Errorf("failed to get piece digest: %w", err)
			}

			pcid2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
			if err != nil {
				return false, xerrors.Errorf("failed to create commP: %w", err)
			}

			if pcid2.Equals(pcid) {
				return false, xerrors.Errorf("commP cid does not match piece cid: %s != %s", pcid2.String(), pcid.String())
			}

			leafs := make([]indexstore.NodeDigest, len(snap))
			for i, s := range snap {
				leafs[i] = indexstore.NodeDigest{
					Layer: lidx,
					Hash:  s.Hash,
					Index: int64(i),
				}
			}

			err = t.idx.AddPDPLayer(ctx, pcid, leafs)
			if err != nil {
				return false, xerrors.Errorf("failed to add PDP layer cache: %w", err)
			}
		}
	}

	n, err := t.db.Exec(ctx, `UPDATE pdp_pipeline SET after_save_cache = TRUE, save_cache_task_id = NULL, indexing_created_at = NOW() WHERE save_cache_task_id = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
	}

	if n != 1 {
		return false, xerrors.Errorf("failed to update pdp_pipeline: expected 1 row but %d rows updated", n)
	}

	return true, nil
}

func (t *TaskPDPSaveCache) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (t *TaskPDPSaveCache) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PDPSaveCache",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(2*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *TaskPDPSaveCache) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			err := tx.QueryRow(`SELECT id FROM pdp_pipeline 
								  WHERE save_cache_task_id IS NULL 
									AND after_save_cache = FALSE
									AND after_add_piece_msg = TRUE`).Scan(&did)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query pdp_pipeline: %w", err)
			}
			if did == "" {
				return false, xerrors.Errorf("no valid deal ID found for scheduling")
			}

			_, err = tx.Exec(`UPDATE pdp_pipeline SET save_cache_task_id = $1 WHERE id = $2 AND after_save_cache = FALSE AND after_add_piece_msg = TRUE AND save_cache_task_id IS NULL`, id, did)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
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
