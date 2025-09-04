package gc

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/types"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/indexing"
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
	// TODO: Optimize this Do() as it is currently cumbersome, repetitive and slow. Fix this in a new PR
	// TODO: Plug this into PoRep 1.2 and 2.0 clean up as well
	// TODO: Remove Deal from MK12 and Mk20?

	ctx := context.Background()

	// To avoid static naming
	pdpIpni := indexing.NewPDPIPNITask(nil, nil, nil, nil, taskhelp.Max(0))
	pdpIpniName := pdpIpni.TypeDetails().Name

	poRepIpni := indexing.NewIPNITask(nil, nil, nil, nil, nil, taskhelp.Max(0))
	poRepIpniName := poRepIpni.TypeDetails().Name

	var tasks []struct {
		ID       string `db:"id"`
		PieceCid string `db:"piece_cid_v2"`
		PDP      bool   `db:"pdp"`
	}

	err = p.db.Select(ctx, &tasks, `SELECT id, piece_cid_v2, pdp FROM piece_cleanup WHERE task_id = $1`, taskID)
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
		ID       string        `db:"id"`
		SPID     int64         `db:"sp_id"`
		Sector   int64         `db:"sector_num"`
		PieceRef sql.NullInt64 `db:"piece_ref"`
	}

	var toRM *pd

	var pieceDeals []pd

	// Let's piece deals as we need to make a complicated decision about IPNI and Indexing
	err = p.db.Select(ctx, &pieceDeals, `SELECT id,
       												sp_id,
       												sector_num,
       												piece_ref
												FROM market_piece_deal WHERE piece_cid = $1 AND piece_length = $2`, pi.PieceCIDV1.String(), pi.Size)
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
				i) If any of the deals is MK1.2 and is not the deal we are cleaning then keep the indexes and don't publish IPNI rm Ad
				ii) If there are any MK2.0 deal then check if they are PoRep or PDP
			a.) If any of the deals is MK1.2 and is not the deal we are cleaning then keep the indexes
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
		toRM = &pieceDeal

		UUID, err := uuid.Parse(task.ID)
		if err == nil {
			pinfo := abi.PieceInfo{
				PieceCID: pi.PieceCIDV1,
				Size:     pi.Size,
			}
			b := new(bytes.Buffer)
			err = pinfo.MarshalCBOR(b)
			if err != nil {
				return false, xerrors.Errorf("marshaling piece info: %w", err)
			}

			p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				var peer string
				err = tx.QueryRow(`SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, pieceDeal.SPID).Scan(&peer)
				if err != nil {
					return false, xerrors.Errorf("failed to get peer id for provider: %w", err)
				}

				if peer == "" {
					return false, xerrors.Errorf("no peer id found for sp_id %d", pieceDeal.SPID)
				}

				_, err = tx.Exec(`SELECT insert_ipni_task($1, $2, $3, $4, $5, $6, $7, $8, $9)`, UUID.String(), pieceDeal.SPID, pieceDeal.Sector, 0, 0, b.Bytes(), true, peer, id)
				if err != nil {
					if harmonydb.IsErrUniqueContraint(err) {
						log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, UUID)
						return false, nil
					}
					if strings.Contains(err.Error(), "already published") {
						log.Infof("Piece %s in deal %s is already published", pcid2, UUID)
						return false, nil
					}
					return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
				}
				// Fix the harmony_task.name
				n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, poRepIpniName, id)
				if err != nil {
					return false, xerrors.Errorf("failed to update task name: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
				}
				return true, nil
			})
		} else {
			lid, err := ulid.Parse(pieceDeal.ID)
			if err == nil {

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
					var peer string
					err = p.db.QueryRow(ctx, `SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, -1).Scan(&peer)
					if err != nil {
						return false, xerrors.Errorf("failed to get peer id for PDP provider: %w", err)
					}

					if peer == "" {
						return false, xerrors.Errorf("no peer id found for PDP")
					}

					if retv.AnnouncePiece {
						pinfo := types.PdpIpniContext{
							PieceCID: pcid2,
							Payload:  false,
						}
						ctxB, err := pinfo.Marshal()
						if err != nil {
							return false, xerrors.Errorf("failed to marshal pdp context for piece %s: %w", pcid2, err)
						}
						p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
							_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, ctxB, true, lid.String(), peer, id)
							if err != nil {
								if harmonydb.IsErrUniqueContraint(err) {
									log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, UUID)
									return false, nil
								}
								if strings.Contains(err.Error(), "already published") {
									log.Infof("Piece %s in deal %s is already published", pcid2, UUID)
									return false, nil
								}
								return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
							}
							// Fix the harmony_task.name
							n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, pdpIpniName, id)
							if err != nil {
								return false, xerrors.Errorf("failed to update task name: %w", err)
							}
							if n != 1 {
								return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
							}
							return true, nil
						})

					}
					if retv.AnnouncePayload {
						pinfo := types.PdpIpniContext{
							PieceCID: pcid2,
							Payload:  true,
						}
						ctxB, err := pinfo.Marshal()
						if err != nil {
							return false, xerrors.Errorf("failed to marshal pdp context for piece %s: %w", pcid2, err)
						}

						p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
							_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, ctxB, true, lid.String(), peer, id)
							if err != nil {
								if harmonydb.IsErrUniqueContraint(err) {
									log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, UUID)
									return false, nil
								}
								if strings.Contains(err.Error(), "already published") {
									log.Infof("Piece %s in deal %s is already published", pcid2, UUID)
									return false, nil
								}
								return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
							}
							// Fix the harmony_task.name
							n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, pdpIpniName, id)
							if err != nil {
								return false, xerrors.Errorf("failed to update task name: %w", err)
							}
							if n != 1 {
								return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
							}
							return true, nil
						})
					}
				} else {
					// This is a PoRep clean up
					pinfo := abi.PieceInfo{
						PieceCID: pi.PieceCIDV1,
						Size:     pi.Size,
					}
					b := new(bytes.Buffer)
					err = pinfo.MarshalCBOR(b)
					if err != nil {
						return false, xerrors.Errorf("marshaling piece info: %w", err)
					}

					var peer string
					err = p.db.QueryRow(ctx, `SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, pieceDeal.SPID).Scan(&peer)
					if err != nil {
						return false, xerrors.Errorf("failed to get peer id for provider: %w", err)
					}

					if peer == "" {
						return false, xerrors.Errorf("no peer id found for sp_id %d", pieceDeal.SPID)
					}

					p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
						_, err = tx.Exec(`SELECT insert_ipni_task($1, $2, $3, $4, $5, $6, $7, $8, $9)`, lid.String(), pieceDeal.SPID, pieceDeal.Sector, 0, 0, b.Bytes(), true, peer, id)
						if err != nil {
							if harmonydb.IsErrUniqueContraint(err) {
								log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, UUID)
								return false, nil
							}
							if strings.Contains(err.Error(), "already published") {
								log.Infof("Piece %s in deal %s is already published", pcid2, UUID)
								return false, nil
							}
							return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
						}
						// Fix the harmony_task.name
						n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, poRepIpniName, id)
						if err != nil {
							return false, xerrors.Errorf("failed to update task name: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
						}
						return true, nil
					})
				}
			} else {
				return false, xerrors.Errorf("failed to parse piece deal ID %s: %w", pieceDeal.ID, err)
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
		toRM = &pieceDeal
		lo.Uniq(mk12List)
		lo.Uniq(mk20List)
		if isMK12 {
			rmAccounce := true
			if len(mk12List) > 1 {
				// Don't drop index or publish removal we have same piece in another deal
				dropIndex = false
				rmAccounce = false
			}
			if len(mk20List) > 0 {
				for _, d := range mk20List {
					deal, err := mk20.DealFromDB(ctx, p.db, d)
					if err != nil {
						return false, xerrors.Errorf("failed to get deal for id %s: %w", d, err)
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
				pinfo := abi.PieceInfo{
					PieceCID: pi.PieceCIDV1,
					Size:     pi.Size,
				}
				b := new(bytes.Buffer)
				err = pinfo.MarshalCBOR(b)
				if err != nil {
					return false, xerrors.Errorf("marshaling piece info: %w", err)
				}

				p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
					var peer string
					err = tx.QueryRow(`SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, pieceDeal.SPID).Scan(&peer)
					if err != nil {
						return false, xerrors.Errorf("failed to get peer id for provider: %w", err)
					}

					if peer == "" {
						return false, xerrors.Errorf("no peer id found for sp_id %d", pieceDeal.SPID)
					}

					_, err = tx.Exec(`SELECT insert_ipni_task($1, $2, $3, $4, $5, $6, $7, $8, $9)`, pieceDeal.ID, pieceDeal.SPID, pieceDeal.Sector, 0, 0, b.Bytes(), true, peer, id)
					if err != nil {
						if harmonydb.IsErrUniqueContraint(err) {
							log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, pieceDeal.ID)
							return false, nil
						}
						if strings.Contains(err.Error(), "already published") {
							log.Infof("Piece %s in deal %s is already published", pcid2, pieceDeal.ID)
							return false, nil
						}
						// Fix the harmony_task.name
						n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, poRepIpniName, id)
						if err != nil {
							return false, xerrors.Errorf("failed to update task name: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
						}
						return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
					}
					return true, nil
				})
			}
		}

		if isMK20 {
			rmAccounce := true
			rmPiece := true
			if len(mk12List) > 1 {
				// Don't drop index or publish removal we have same piece in another deal
				dropIndex = false
				rmAccounce = false
			}
			if len(mk20List) > 0 {
				for _, d := range mk20List {
					deal, err := mk20.DealFromDB(ctx, p.db, d)
					if err != nil {
						return false, xerrors.Errorf("failed to get deal for id %s: %w", d, err)
					}
					if deal.Products.RetrievalV1 == nil {
						continue
					}
					retv := deal.Products.RetrievalV1

					// For the deal we are processing
					if d.String() == task.ID {
						// If we are cleaning up PDP then check PoRep
						if task.PDP {
							if deal.Products.DDOV1 != nil {
								rmAccounce = false
							}
						} else {
							// If we are cleaning up PoRep then check PDP
							if deal.Products.PDPV1 != nil {
								rmPiece = false
							}
							if retv.AnnouncePayload {
								rmAccounce = false
							}
						}
					} else {
						if retv.AnnouncePiece {
							rmPiece = false
						}
						if retv.AnnouncePayload {
							rmAccounce = false
						}
					}
				}
			}

			if task.PDP {
				var peer string
				err = p.db.QueryRow(ctx, `SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, -1).Scan(&peer)
				if err != nil {
					return false, xerrors.Errorf("failed to get peer id for PDP provider: %w", err)
				}

				if peer == "" {
					return false, xerrors.Errorf("no peer id found for PDP")
				}

				if rmAccounce {
					pinfo := types.PdpIpniContext{
						PieceCID: pcid2,
						Payload:  true,
					}
					ctxB, err := pinfo.Marshal()
					if err != nil {
						return false, xerrors.Errorf("failed to marshal pdp context for piece %s: %w", pcid2, err)
					}

					p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
						_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, ctxB, true, task.ID, peer, id)
						if err != nil {
							if harmonydb.IsErrUniqueContraint(err) {
								log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, task.ID)
								return false, nil
							}
							if strings.Contains(err.Error(), "already published") {
								log.Infof("Piece %s in deal %s is already published", pcid2, task.ID)
								return false, nil
							}
							return false, xerrors.Errorf("failed to publish remove payload ad for piece %s in PDP: %w", pcid2, err)
						}
						// Fix the harmony_task.name
						n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, pdpIpniName, id)
						if err != nil {
							return false, xerrors.Errorf("failed to update task name: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
						}
						return true, nil
					})
				}

				if rmPiece {
					pinfo := types.PdpIpniContext{
						PieceCID: pcid2,
						Payload:  false,
					}
					ctxB, err := pinfo.Marshal()
					if err != nil {
						return false, xerrors.Errorf("failed to marshal pdp context for piece %s: %w", pcid2, err)
					}

					p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
						_, err = tx.Exec(`SELECT insert_pdp_ipni_task($1, $2, $3, $4, $5)`, ctxB, true, task.ID, peer, id)
						if err != nil {
							if harmonydb.IsErrUniqueContraint(err) {
								log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, task.ID)
								return false, nil
							}
							if strings.Contains(err.Error(), "already published") {
								log.Infof("Piece %s in deal %s is already published", pcid2, task.ID)
								return false, nil
							}
							return false, xerrors.Errorf("failed to publish remove piece ad for piece %s in PDP: %w", pcid2, err)
						}
						// Fix the harmony_task.name
						n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, pdpIpniName, id)
						if err != nil {
							return false, xerrors.Errorf("failed to update task name: %w", err)
						}
						if n != 1 {
							return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
						}
						return true, nil
					})
				}
			} else {
				pinfo := abi.PieceInfo{
					PieceCID: pi.PieceCIDV1,
					Size:     pi.Size,
				}
				b := new(bytes.Buffer)
				err = pinfo.MarshalCBOR(b)
				if err != nil {
					return false, xerrors.Errorf("marshaling piece info: %w", err)
				}

				p.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
					var peer string
					err = tx.QueryRow(`SELECT peer_id FROM ipni_peerid WHERE sp_id = $1`, pieceDeal.SPID).Scan(&peer)
					if err != nil {
						return false, xerrors.Errorf("failed to get peer id for provider: %w", err)
					}

					if peer == "" {
						return false, xerrors.Errorf("no peer id found for sp_id %d", pieceDeal.SPID)
					}

					_, err = tx.Exec(`SELECT insert_ipni_task($1, $2, $3, $4, $5, $6, $7, $8, $9)`, pieceDeal.ID, pieceDeal.SPID, pieceDeal.Sector, 0, 0, b.Bytes(), true, peer, id)
					if err != nil {
						if harmonydb.IsErrUniqueContraint(err) {
							log.Infof("Another IPNI announce task already present for piece %s in deal %s", pcid2, pieceDeal.ID)
							return false, nil
						}
						if strings.Contains(err.Error(), "already published") {
							log.Infof("Piece %s in deal %s is already published", pcid2, pieceDeal.ID)
							return false, nil
						}
						return false, xerrors.Errorf("updating IPNI announcing task id: %w", err)
					}
					// Fix the harmony_task.name
					n, err := tx.Exec(`UPDATE harmony_task SET name = $1 WHERE id = $2`, poRepIpniName, id)
					if err != nil {
						return false, xerrors.Errorf("failed to update task name: %w", err)
					}
					if n != 1 {
						return false, xerrors.Errorf("failed to update task name: %d rows updated", n)
					}
					return true, nil
				})
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

	if task.PDP {
		_, err = p.db.Exec(ctx, `SELECT remove_piece_deal($1, $2, $3, $4)`, task.ID, -1, pi.PieceCIDV1.String(), pi.Size)
	} else {
		_, err = p.db.Exec(ctx, `SELECT remove_piece_deal($1, $2, $3, $4)`, task.ID, toRM.SPID, pi.PieceCIDV1.String(), pi.Size)
	}

	if err != nil {
		return false, xerrors.Errorf("failed to remove piece deal: %w", err)
	}

	_, err = p.db.Exec(ctx, `DELETE FROM piece_cleanup WHERE task_id = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to remove piece cleanup task: %w", err)
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
		IAmBored: passcall.Every(5*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
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
								  WHERE task_id IS NULL 
									LIMIT 1`).Scan(&did, &pdp)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to query piece_cleanup: %w", err)
			}

			_, err = tx.Exec(`UPDATE piece_cleanup SET task_id = $1 WHERE id = $2 AND pdp = $3`, id, did, pdp)
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
