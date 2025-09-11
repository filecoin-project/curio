package gc

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
)

type PieceCleanupTask struct {
	db         *harmonydb.DB
	indexStore *indexstore.IndexStore
	TF         promise.Promise[harmonytask.AddTaskFunc]
}

func NewPieceCleanupTask(db *harmonydb.DB, indexStore *indexstore.IndexStore) *PieceCleanupTask {
	return &PieceCleanupTask{
		db:         db,
		indexStore: indexStore,
	}
}

func (p *PieceCleanupTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	// TODO: Plug this into PoRep 1.2 and 2.0 clean up as well
	// TODO: Remove Deal from MK12 and Mk20?

	ctx := context.Background()

	var tasks []struct {
		ID       string        `db:"id"`
		PieceCid string        `db:"piece_cid_v2"`
		PDP      bool          `db:"pdp"`
		Sector   int64         `db:"sector_number"`
		SPID     int64         `db:"sp_id"`
		PieceRef sql.NullInt64 `db:"piece_ref"`
	}

	err = p.db.Select(ctx, &tasks, `SELECT id, piece_cid_v2, pdp, sp_id, sector_number, piece_ref FROM piece_cleanup WHERE cleanup_task_id = $1 AND after_cleanup = FALSE`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to get piece cleanup task: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 piece cleanup task but got %d", len(tasks))
	}

	task := tasks[0]

	var isMK12 bool
	var isMK20 bool
	_, err = uuid.Parse(task.ID)
	if err == nil {
		isMK12 = true
	} else {
		_, err = ulid.Parse(task.ID)
		if err == nil {
			isMK20 = true
		}
		if err != nil {
			return false, xerrors.Errorf("failed to parse task ID %s: %w", task.ID, err)
		}
	}

	pcid2, err := cid.Parse(task.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("failed to parse piece cid: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid2)
	if err != nil {
		return false, xerrors.Errorf("failed to get piece info for piece %s: %w", pcid2, err)
	}

	// Did we index this piece?
	var indexed bool
	err = p.db.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1 AND piece_size = $2`, pi.PieceCIDV1.String(), pi.Size).Scan(&indexed)
	if err != nil {
		return false, xerrors.Errorf("failed to check if piece if indexe: %w", err)
	}

	dropIndex := true

	type pd struct {
		ID string `db:"id"`
	}

	var pieceDeals []pd

	// Let's piece deals as we need to make a complicated decision about IPNI and Indexing
	err = p.db.Select(ctx, &pieceDeals, `SELECT id FROM market_piece_deal WHERE piece_cid = $1 AND piece_length = $2`, pi.PieceCIDV1.String(), pi.Size)
	if err != nil {
		return false, xerrors.Errorf("failed to get piece deals: %w", err)
	}

	if len(pieceDeals) == 0 {
		// This could be due to partial clean up
		log.Infof("No piece deals found for piece %s", taskID)
		return false, nil
	}
	/*
		Get a list of piece deals
		1. If only single row then check
			a) MK1.2
				i) publish IPNI removal ad
				ii) Drop index
			b) MK2.0
				i) Publish IPNI Ad based on attached product
				ii) Drop index if any
				iii) Drop Aggregate index
		2. If multiple rows then, check if
			a.) MK1.2
				i) If any of the deals is MK1.2 and is not the deal we are cleaning, then keep the indexes and don't publish IPNI rm Ad
				ii) If there are any MK2.0 deal then check if they are PoRep or PDP
			a.) If any of the deals is MK1.2 and is not the deal we are cleaning, then keep the indexes
			b.) If 2 rows, with same ID then we have PoRep and PDP for same deal. Clean up based on product.
			c.) If we have multiple rows with different MK2.0 deals then we need to make a complex decision
				i) Check if any of them apart from deal we are cleaning is paying to keep index. If yes, then don't remove them
				ii) Check if any of them is paying to keep IPNI payload announced apart from deal we are cleaning. If yes, then don't publish RM ad
				iii) Don't publish RM ad for IPNI piece if we have any other PDP deals
	*/

	if len(pieceDeals) == 1 {
		// Single piece deal, then drop index if deal ID matches
		pieceDeal := pieceDeals[0]
		if task.ID != pieceDeal.ID {
			return false, xerrors.Errorf("piece deal ID %s does not match task ID %s", pieceDeal.ID, task.ID)
		}

		if isMK12 {
			err := p.ipniRemoval(ctx, task.ID, false, true, task.PDP)
			if err != nil {
				return false, xerrors.Errorf("failed to publish IPNI removal ad for piece %s, Single MK12 deal %s: %w", pcid2, task.ID, err)
			}
		}

		if isMK20 {
			lid, err := ulid.Parse(pieceDeal.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to parse piece deal ID %s: %w", pieceDeal.ID, err)
			}

			deal, err := mk20.DealFromDB(ctx, p.db, lid)
			if err != nil {
				return false, xerrors.Errorf("failed to get deal for id %s: %w", lid, err)
			}

			if deal.Products.RetrievalV1 == nil {
				// Return early, we don't need to drop index or publish rm ads
				return true, nil
			}

			retv := deal.Products.RetrievalV1

			if task.PDP {
				// Let's publish PDP removal first
				err := p.ipniRemoval(ctx, task.ID, retv.AnnouncePiece, retv.AnnouncePayload, task.PDP)
				if err != nil {
					return false, xerrors.Errorf("failed to update piece cleanup for single MK20 deal: %w", err)
				}
			} else {
				err := p.ipniRemoval(ctx, task.ID, false, true, task.PDP)
				if err != nil {
					return false, xerrors.Errorf("failed to update payload cleanup for single MK20 deal: %w", err)
				}
			}
		}
	} else {
		// If we have multiple rows
		var mk12List []uuid.UUID
		var mk20List []ulid.ULID
		var pieceDeal pd
		for _, pDeal := range pieceDeals {
			if pDeal.ID == task.ID {
				pieceDeal = pDeal
				continue
			}

			uid, err := uuid.Parse(pDeal.ID)
			if err == nil {
				mk12List = append(mk12List, uid)
				continue
			}
			lid, serr := ulid.Parse(pDeal.ID)
			if serr == nil {
				mk20List = append(mk20List, lid)
				continue
			}
			return false, xerrors.Errorf("failed to parse piece deal ID %s: %w, %w", pieceDeal.ID, err, serr)

		}
		lo.Uniq(mk12List)
		lo.Uniq(mk20List)
		if isMK12 {
			rmAccounce := true
			if len(mk12List) > 1 {
				// Don't drop index or publish removal we have same the piece in another deal
				dropIndex = false
				rmAccounce = false
			}
			if len(mk20List) > 0 {
				for _, d := range mk20List {
					deal, err := mk20.DealFromDB(ctx, p.db, d)
					if err != nil {
						return false, xerrors.Errorf("failed to get deal for id %s: %w", d, err)
					}
					// If MK20 deal is not DDO, then the context would be different and we can skip
					if deal.Products.DDOV1 == nil {
						continue
					}
					if deal.Products.RetrievalV1 == nil {
						continue
					}
					retv := deal.Products.RetrievalV1
					if retv.Indexing {
						dropIndex = false
					}
					if retv.AnnouncePayload {
						// No need to publish rm Ad as another MK20 deal is paying for it
						rmAccounce = false
						break
					}
				}
			}
			if rmAccounce {
				err := p.ipniRemoval(ctx, task.ID, false, true, task.PDP)
				if err != nil {
					return false, xerrors.Errorf("failed to update piece cleanup for MK12 deal: %w", err)
				}
			}
		}

		if isMK20 {
			// If this is PDP then we need to check if we have any other PDP deals because contextID is different for PDP and PoRep deals
			if task.PDP {
				rmPayload := true
				rmPiece := true

				if len(mk20List) > 0 {
					for _, d := range mk20List {
						deal, err := mk20.DealFromDB(ctx, p.db, d)
						if err != nil {
							return false, xerrors.Errorf("failed to get deal for id %s: %w", d, err)
						}
						if deal.Products.PDPV1 == nil {
							continue
						}
						if deal.Products.RetrievalV1 == nil {
							continue
						}
						retv := deal.Products.RetrievalV1

						if retv.AnnouncePiece {
							rmPiece = false
						}
						if retv.AnnouncePayload {
							rmPayload = false
						}

					}
				}

				err := p.ipniRemoval(ctx, task.ID, rmPiece, rmPayload, task.PDP)
				if err != nil {
					return false, xerrors.Errorf("failed to update piece and payload cleanup for MK20 PDP deal: %w", err)
				}
			} else {
				rmAnnounce := true

				// If we have another PoRep MK12 deal then don't announce removal
				if len(mk12List) > 0 {
					rmAnnounce = false
				}

				if len(mk20List) > 0 {
					for _, d := range mk20List {
						deal, err := mk20.DealFromDB(ctx, p.db, d)
						if err != nil {
							return false, xerrors.Errorf("failed to get deal for id %s: %w", d, err)
						}
						// If MK20 deal is not DDO, then the context would be different and we can skip
						if deal.Products.DDOV1 == nil {
							continue
						}
						if deal.Products.RetrievalV1 == nil {
							continue
						}
						if !deal.Products.RetrievalV1.Indexing {
							dropIndex = false
						}

						if !deal.Products.RetrievalV1.AnnouncePayload {
							rmAnnounce = false
						}
					}
				}

				if rmAnnounce {
					err := p.ipniRemoval(ctx, task.ID, false, true, task.PDP)
					if err != nil {
						return false, xerrors.Errorf("failed to update payload cleanup for MK20 deal: %w", err)
					}
				}
			}
		}
	}

	if dropIndex {
		err = dropIndexes(ctx, p.indexStore, pcid2)
		if err != nil {
			return false, xerrors.Errorf("failed to drop indexes for piece %s: %w", pcid2, err)
		}
		err = dropAggregateIndex(ctx, p.indexStore, pcid2)
		if err != nil {
			return false, xerrors.Errorf("failed to drop aggregate index for piece %s: %w", pcid2, err)
		}
	}

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		if task.PDP {
			_, err = tx.Exec(`SELECT remove_piece_deal($1, $2, $3, $4)`, task.ID, -1, pi.PieceCIDV1.String(), pi.Size)
		} else {
			_, err = tx.Exec(`SELECT remove_piece_deal($1, $2, $3, $4)`, task.ID, task.SPID, pi.PieceCIDV1.String(), pi.Size)
		}

		if err != nil {
			return false, xerrors.Errorf("failed to remove piece deal: %w", err)
		}

		// Drop piece ref
		if task.PieceRef.Valid {
			_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, task.PieceRef.Int64)
			if err != nil {
				return false, xerrors.Errorf("failed to remove parked piece ref: %w", err)
			}
		}

		_, err = tx.Exec(`UPDATE piece_cleanup SET 
                         cleanup_task_id = NULL, 
                         after_cleanup = TRUE,
                         piece_cid = $1,
                         piece_size = $2,
                     WHERE task_id = $3`, pi.PieceCIDV1.String(), pi.Size, task.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to mark complete cleanup task: %w", err)
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("failed to commit piece cleanup: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit piece cleanup")
	}

	return true, nil
}

func dropIndexes(ctx context.Context, indexStore *indexstore.IndexStore, pieceCid cid.Cid) error {
	err := indexStore.RemoveIndexes(ctx, pieceCid)
	if err != nil {
		return xerrors.Errorf("failed to remove indexes for piece %s: %w", pieceCid, err)
	}
	return nil
}

func dropAggregateIndex(ctx context.Context, indexStore *indexstore.IndexStore, pieceCid cid.Cid) error {
	err := indexStore.RemoveAggregateIndex(ctx, pieceCid)
	if err != nil {
		return xerrors.Errorf("failed to remove aggregate index for piece %s: %w", pieceCid, err)
	}
	return nil
}

func (p *PieceCleanupTask) ipniRemoval(ctx context.Context, id string, rmPiece, rmPayload, pdp bool) error {
	n, err := p.db.Exec(ctx, `UPDATE piece_cleanup SET announce = $1, announce_payload = $2  WHERE id = $3 and pdp = $4`, rmPiece, rmPayload, id, pdp)
	if err != nil {
		return xerrors.Errorf("failed to update piece and payload cleanup for deal: %w", err)
	}

	if n != 1 {
		return xerrors.Errorf("failed to update piece and payload cleanup for deal: %d rows updated", n)
	}
	return nil
}

func (p *PieceCleanupTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

func (p *PieceCleanupTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(50),
		Name: "PieceCleanup",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(30*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return p.schedule(context.Background(), taskFunc)
		}),
	}
}

func (p *PieceCleanupTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var did string
			var pdp bool
			err := tx.QueryRow(`SELECT id, pdp FROM piece_cleanup 
								  WHERE cleanup_task_id IS NULL
								  AND after_cleanup = FALSE 
									LIMIT 1`).Scan(&did, &pdp)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query piece_cleanup: %w", err)
			}

			_, err = tx.Exec(`UPDATE piece_cleanup SET cleanup_task_id = $1 WHERE id = $2 AND pdp = $3 AND after_cleanup = FALSE`, id, did, pdp)
			if err != nil {
				return false, xerrors.Errorf("failed to update piece_cleanup: %w", err)
			}

			stop = false // we found a task to schedule, keep going
			return true, nil
		})

	}

	return nil
}

func (p *PieceCleanupTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &PieceCleanupTask{}
var _ = harmonytask.Reg(&PieceCleanupTask{})
