package indexing

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
)

const CheckIndexInterval = time.Hour * 6

var MaxOngoingIndexingTasks = 40

type CheckIndexesTask struct {
	db         *harmonydb.DB
	indexStore *indexstore.IndexStore
}

func NewCheckIndexesTask(db *harmonydb.DB, indexStore *indexstore.IndexStore) *CheckIndexesTask {
	return &CheckIndexesTask{
		db:         db,
		indexStore: indexStore,
	}
}

func (c *CheckIndexesTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	{
		/* if market_mk12_deal_pipeline_migration has entries don't run checks */
		var migrationCount int64
		err = c.db.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk12_deal_pipeline_migration LIMIT 1`).Scan(&migrationCount)
		if err != nil {
			return false, xerrors.Errorf("querying migration count: %w", err)
		}
		if migrationCount > 0 {
			log.Infow("skipping check indexes task because market_mk12_deal_pipeline_migration has entries", "task", taskID)
			return true, nil
		}
	}

	err = c.checkIndexing(ctx, taskID)
	if err != nil {
		return false, xerrors.Errorf("checking indexes: %w", err)
	}

	err = c.checkIPNI(ctx, taskID)
	if err != nil {
		return false, xerrors.Errorf("checking IPNI: %w", err)
	}

	err = c.checkIPNIMK20(ctx, taskID)
	if err != nil {
		return false, xerrors.Errorf("checking IPNI for MK20 deals: %w", err)
	}

	return true, nil
}

func (c *CheckIndexesTask) checkIndexing(ctx context.Context, taskID harmonytask.TaskID) error {
	type checkEntry struct {
		ID       string        `db:"id"`
		PieceCid string        `db:"piece_cid"`
		PieceLen int64         `db:"piece_length"`
		PieceOff int64         `db:"piece_offset"`
		SPID     int64         `db:"sp_id"`
		SectorID int64         `db:"sector_num"`
		RawSize  int64         `db:"raw_size"`
		PieceRef sql.NullInt64 `db:"piece_ref"`
	}
	var toCheckList []checkEntry
	err := c.db.Select(ctx, &toCheckList, `
			SELECT mm.piece_cid, mpd.piece_length, mpd.piece_offset, mpd.sp_id, mpd.sector_num, mpd.raw_size, mpd.piece_ref, mpd.id
			FROM market_piece_metadata mm
			LEFT JOIN market_piece_deal mpd ON mm.piece_cid = mpd.piece_cid AND mm.piece_size = mpd.piece_length
			WHERE mm.indexed = true AND mpd.sp_id > 0 AND mpd.sector_num > 0
		`)
	if err != nil {
		return err
	}

	toCheck := make(map[abi.PieceInfo][]checkEntry)
	for _, e := range toCheckList {
		pCid, err := cid.Parse(e.PieceCid)
		if err != nil {
			return xerrors.Errorf("parsing piece cid: %w", err)
		}
		pi := abi.PieceInfo{PieceCID: pCid, Size: abi.PaddedPieceSize(e.PieceLen)}
		toCheck[pi] = append(toCheck[pi], e)
	}

	// Check the number of ongoing indexing tasks
	var ongoingIndexingTasks int64
	err = c.db.QueryRow(ctx, `SELECT
								  (
									SELECT COUNT(*)
									FROM market_mk12_deal_pipeline
									WHERE indexing_created_at IS NOT NULL AND indexed = false
								  ) +
								  (
									SELECT COUNT(*)
									FROM market_mk20_pipeline
									WHERE indexing_created_at IS NOT NULL AND indexed = false
								  ) AS total_pending_indexing;`).Scan(&ongoingIndexingTasks)
	if err != nil {
		return xerrors.Errorf("counting ongoing indexing tasks: %w", err)
	}
	if ongoingIndexingTasks >= int64(MaxOngoingIndexingTasks) {
		log.Warnw("too many ongoing indexing tasks, skipping check indexes task", "task", taskID, "ongoing", ongoingIndexingTasks)
		return nil
	}

	var have, missing int64

	for p, cents := range toCheck {
		pieceCid, err := commcid.PieceCidV2FromV1(p.PieceCID, uint64(cents[0].RawSize))
		if err != nil {
			return xerrors.Errorf("getting piece commP: %w", err)
		}

		// Check if the pieceV2 is present in the index store
		hasEnt, err := c.indexStore.CheckHasPiece(ctx, pieceCid)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}

		if hasEnt {
			have++
			continue
		}

		// Check if the pieceV1 is present in the index store
		hasEnt, err = c.indexStore.CheckHasPiece(ctx, p.PieceCID)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}

		if hasEnt {
			continue
		}

		// Index not present, flag for repair
		missing++
		log.Warnw("piece missing in indexstore", "piece", pieceCid, "task", taskID)

		for _, cent := range cents {
			var isMK12 bool
			var id ulid.ULID

			id, err := ulid.Parse(cent.ID)
			if err != nil {
				serr := err
				_, err = uuid.Parse(cent.ID)
				if err != nil {
					return xerrors.Errorf("parsing deal id: %w, %w", serr, err)
				}
				isMK12 = true
			}

			var scheduled bool

			if isMK12 {
				// Get deal details from market_mk12_deals
				var mk12deals []struct {
					UUID      string    `db:"uuid"`
					SPID      int64     `db:"sp_id"`
					PieceCID  string    `db:"piece_cid"`
					PieceSize int64     `db:"piece_size"`
					Offline   bool      `db:"offline"`
					CreatedAt time.Time `db:"created_at"`
				}
				err = c.db.Select(ctx, &mk12deals, `SELECT
											  uuid,
											  sp_id,
											  piece_cid,
											  piece_size,
											  offline,
											  created_at,
											  FALSE AS ddo
											FROM market_mk12_deals
											WHERE uuid = $1
											
											UNION ALL
											
											SELECT
											  uuid,
											  sp_id,
											  piece_cid,
											  piece_size,
											  TRUE AS offline,
											  created_at,
											  TRUE AS ddo
											FROM market_direct_deals
											WHERE uuid = $1;
											`, cent.ID)
				if err != nil {
					return xerrors.Errorf("getting deal details: %w", err)
				}

				if len(mk12deals) == 0 {
					log.Warnw("no mk12 deals for unindexed piece", "piece", pieceCid, "task", taskID)
					continue
				}

				mk12deal := mk12deals[0]

				if cent.PieceRef.Valid {
					continue // This is mk20 deal
				}
				if cent.SPID != mk12deal.SPID {
					continue
				}
				sourceSector := c.findSourceSector(ctx, cent.SPID, cent.SectorID)
				if sourceSector == nil {
					continue
				}

				var added bool

				_, err = c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
					added = false

					// Insert into market_mk12_deal_pipeline
					n, err := tx.Exec(`
								INSERT INTO market_mk12_deal_pipeline (
									uuid, sp_id, piece_cid, piece_size, raw_size, offline, created_at,
									sector, sector_offset, reg_seal_proof,
									started, after_psd, after_commp, after_find_deal, sealed, complete,
									indexed, indexing_created_at, indexing_task_id, should_index
								)
								VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
										true, true, true, true, true, true,
										false, NOW(), NULL, true)
								ON CONFLICT (uuid) DO NOTHING
							`, mk12deal.UUID, mk12deal.SPID, mk12deal.PieceCID, mk12deal.PieceSize, cent.RawSize, mk12deal.Offline, mk12deal.CreatedAt,
						sourceSector.ID.Number, cent.PieceOff, int64(sourceSector.ProofType))
					if err != nil {
						return false, xerrors.Errorf("upserting into deal pipeline for uuid %s: %w", mk12deal.UUID, err)
					}
					if n == 0 {
						return false, nil
					}
					added = true

					_, err = tx.Exec(`UPDATE market_piece_metadata SET indexed = FALSE WHERE piece_cid = $1 AND piece_size = $2`, p.PieceCID.String(), p.Size)
					if err != nil {
						return false, xerrors.Errorf("updating market_piece_metadata.indexed column: %w", err)
					}

					return true, nil
				}, harmonydb.OptionRetry())
				if err != nil {
					return xerrors.Errorf("inserting into market_mk12_deal_pipeline: %w", err)
				}

				if added {
					log.Infow("added reindexing pipeline entry", "uuid", mk12deal.UUID, "task", taskID, "piece", pieceCid)
					ongoingIndexingTasks++
					scheduled = true
				}
			} else {
				if !cent.PieceRef.Valid {
					continue
				}

				deal, err := mk20.DealFromDB(ctx, c.db, id)
				if err != nil {
					log.Warnw("failed to get deal from db", "id", id.String(), "task", taskID)
					continue
				}

				spid, err := address.IDFromAddress(deal.Products.DDOV1.Provider)
				if err != nil {
					return xerrors.Errorf("parsing provider address: %w", err)
				}

				pi, err := deal.PieceInfo()
				if err != nil {
					return xerrors.Errorf("getting piece info: %w", err)
				}

				if uint64(cent.SPID) != spid {
					continue
				}

				pieceIDUrl := url.URL{
					Scheme: "pieceref",
					Opaque: fmt.Sprintf("%d", cent.PieceRef.Int64),
				}

				data := deal.Data
				ddo := deal.Products.DDOV1

				aggregation := 0
				if data.Format.Aggregate != nil {
					aggregation = int(data.Format.Aggregate.Type)
				}

				var added bool

				_, err = c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
					n, err := tx.Exec(`INSERT INTO market_mk20_pipeline (
									id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size, raw_size, 
                                  	offline, url, indexing, announce, duration, piece_aggregation,
                                  	started, downloaded, after_commp, aggregated, sector, reg_seal_proof, sector_offset, sealed,
                                  	indexing_created_at, complete) 
									VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 
									        TRUE, TRUE, TRUE, TRUE, $15, 0, $16, TRUE, NOW(), TRUE)`, // We can use reg_seal_proof = 0 because process_piece_deal will prevent new entry from being created
						deal.Identifier.String(), spid, ddo.ContractAddress, deal.Client, deal.Data.PieceCID.String(), pi.PieceCIDV1.String(), pi.Size, int64(pi.RawSize),
						false, pieceIDUrl.String(), true, false, ddo.Duration, aggregation,
						cent.SectorID, cent.PieceOff)
					if err != nil {
						return false, xerrors.Errorf("inserting mk20 pipeline: %w", err)
					}
					if n != 1 {
						return false, xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
					}

					added = true

					_, err = tx.Exec(`UPDATE market_piece_metadata SET indexed = FALSE WHERE piece_cid = $1 AND piece_size = $2`, p.PieceCID.String(), p.Size)
					if err != nil {
						return false, xerrors.Errorf("updating market_piece_metadata.indexed column: %w", err)
					}
					return true, nil
				}, harmonydb.OptionRetry())
				if err != nil {
					return xerrors.Errorf("inserting into market_mk20_pipeline: %w", err)
				}

				if added {
					log.Infow("added reindexing pipeline entry", "id", id, "task", taskID, "piece", pieceCid)
					ongoingIndexingTasks++
					scheduled = true
				}
			}

			if scheduled {
				break // Break out of PieceDeal loop
			}
		}

		if ongoingIndexingTasks >= int64(MaxOngoingIndexingTasks) {
			log.Warnw("reached max ongoing indexing tasks, stopping processing missing pieces", "task", taskID, "ongoing", ongoingIndexingTasks)
			break
		}
	}

	log.Infow("checking indexes", "have", have, "missing", missing, "task", taskID)

	return nil
}

func (c *CheckIndexesTask) checkIPNI(ctx context.Context, taskID harmonytask.TaskID) (err error) {
	type pieceSP struct {
		PieceCid  string              `db:"piece_cid"`
		PieceSize abi.PaddedPieceSize `db:"piece_size"`
		SpID      int64               `db:"sp_id"`
	}

	// get candidates to check
	// SELECT DISTINCT piece_cid FROM market_mk12_deals WHERE announce_to_ipni=true
	var toCheck []struct {
		PieceCID  string              `db:"piece_cid"`
		SpID      int64               `db:"sp_id"`
		PieceSize abi.PaddedPieceSize `db:"piece_size"`

		UUID      string    `db:"uuid"`
		Offline   bool      `db:"offline"`
		URL       *string   `db:"url"`
		Headers   []byte    `db:"url_headers"`
		CreatedAt time.Time `db:"created_at"`
	}
	err = c.db.Select(ctx, &toCheck, `SELECT DISTINCT piece_cid, sp_id, piece_size,
                uuid, offline, url, url_headers, created_at
                FROM market_mk12_deals WHERE fast_retrieval=true AND announce_to_ipni=true`)
	if err != nil {
		return xerrors.Errorf("getting ipni tasks: %w", err)
	}

	// get ipni_peerid
	var ipniPeerIDs []struct {
		SpID   int64  `db:"sp_id"`
		PeerID string `db:"peer_id"`
	}
	err = c.db.Select(ctx, &ipniPeerIDs, `SELECT sp_id, peer_id FROM ipni_peerid`)
	if err != nil {
		return xerrors.Errorf("getting ipni tasks: %w", err)
	}

	spToPeer := map[int64]string{}
	for _, d := range ipniPeerIDs {
		spToPeer[d.SpID] = d.PeerID
	}

	// get already running pipelines with announce=true
	var announcePiecePipelines []pieceSP
	err = c.db.Select(ctx, &announcePiecePipelines, `SELECT piece_cid, piece_size, sp_id FROM market_mk12_deal_pipeline WHERE announce=true`)
	if err != nil {
		return xerrors.Errorf("getting ipni tasks: %w", err)
	}

	announcablePipelines := map[pieceSP]struct{}{}
	for _, pipeline := range announcePiecePipelines {
		announcablePipelines[pipeline] = struct{}{}
	}

	// get ongoing ipni_task count
	var ongoingIpniTasks int64
	err = c.db.QueryRow(ctx, `SELECT COUNT(1) FROM ipni_task`).Scan(&ongoingIpniTasks)
	if err != nil {
		return xerrors.Errorf("getting ipni tasks: %w", err)
	}
	if ongoingIpniTasks >= int64(MaxOngoingIndexingTasks) {
		log.Debugw("too many ongoing ipni tasks, skipping ipni index checks", "task", taskID, "ongoing", ongoingIpniTasks)
		return nil
	}

	var have, missing, issues int64
	defer func() {
		log.Infow("IPNI Ad check", "have", have, "missing", missing, "issues", issues, "err", err)
	}()

	for _, deal := range toCheck {
		if _, ok := announcablePipelines[pieceSP{deal.PieceCID, deal.PieceSize, deal.SpID}]; ok {
			// pipeline for the piece already running
			have++
			continue
		}

		// pipeline not running, check if it has an ipni entry
		var pi abi.PieceInfo
		pi.PieceCID = cid.MustParse(deal.PieceCID)
		pi.Size = deal.PieceSize

		var ctxIdBuf bytes.Buffer
		err := pi.MarshalCBOR(&ctxIdBuf)
		if err != nil {
			return xerrors.Errorf("marshaling piece info: %w", err)
		}

		ctxId := ctxIdBuf.Bytes()

		provider, ok := spToPeer[deal.SpID]
		if !ok {
			issues++
			log.Warnw("no peer id for spid", "spid", deal.SpID, "checkPiece", deal.PieceCID)
			continue
		}

		var hasEnt int64
		err = c.db.QueryRow(ctx, `SELECT count(1) FROM ipni WHERE context_id=$1 AND provider=$2`, ctxId, provider).Scan(&hasEnt)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}
		if hasEnt > 0 {
			// has the entry
			have++
			continue
		}

		hasIndex, err := c.indexStore.CheckHasPiece(ctx, pi.PieceCID)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}
		if !hasIndex {
			log.Warnw("no index for piece with missing IPNI Ad", "piece", pi.PieceCID, "checkPiece", deal.PieceCID)
			issues++
			continue
		}

		// Now we know that:
		// * There are no migration tasks running
		// * There is a deal that expects an Ad for this piece/SP
		// * There is no deal pipeline for that piece active
		// * There is no ipni Ad for the provider with a matching contextID
		// * The piece is indexed in indexstore
		// So to fix that we want to spawn a new pipeline which will re-run the IPNI task

		var sourceSector []struct {
			SectorNum   int64 `db:"sector_num"`
			PieceOffset int64 `db:"piece_offset"`
			RawSize     int64 `db:"raw_size"`
		}
		err = c.db.Select(ctx, &sourceSector, `SELECT sector_num, piece_offset, raw_size FROM market_piece_deal WHERE piece_cid=$1 AND piece_length = $2 AND sp_id = $3`, deal.PieceCID, deal.PieceSize, deal.SpID)
		if err != nil {
			return xerrors.Errorf("getting source sector: %w", err)
		}
		if len(sourceSector) == 0 {
			log.Warnw("no source sector for piece", "piece", pi.PieceCID, "checkPiece", deal.PieceCID)
			issues++
			continue
		}

		var srcSector *storiface.SectorRef
		var sourceOff, rawSize int64
		for _, entry := range sourceSector {
			if srcSector = c.findSourceSector(ctx, deal.SpID, entry.SectorNum); sourceSector == nil {
				// No unsealed copy
				continue
			}
			sourceOff = entry.PieceOffset
			rawSize = entry.RawSize
			break
		}
		if srcSector == nil {
			log.Warnw("no unsealed sector for ipni reindexing", "piece", pi.PieceCID, "checkPiece", deal.PieceCID)
			issues++
			continue
		}

		missing++

		n, err := c.db.Exec(ctx, `
					INSERT INTO market_mk12_deal_pipeline (
						uuid, sp_id, piece_cid, piece_size, raw_size, offline, url, headers, created_at,
						sector, sector_offset, reg_seal_proof,
						started, after_psd, after_commp, after_find_deal, sealed, complete, announce,
						indexed, indexing_created_at, indexing_task_id, should_index
					)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
					        true, true, true, true, true, false, true,
					        true, NOW(), NULL, true)
					ON CONFLICT (uuid) DO NOTHING
				`, deal.UUID, deal.SpID, deal.PieceCID, deal.PieceSize, rawSize, deal.Offline, deal.URL, deal.Headers, deal.CreatedAt,
			srcSector.ID.Number, sourceOff, srcSector.ProofType)
		if err != nil {
			return xerrors.Errorf("upserting into deal pipeline for uuid %s: %w", deal.UUID, err)
		}
		if n == 0 {
			continue
		}

		log.Infow("created IPNI reindexing pipeline", "piece", pi.PieceCID, "spid", deal.SpID)

		ongoingIpniTasks++
		if ongoingIpniTasks >= int64(MaxOngoingIndexingTasks) {
			return nil
		}

	}

	return nil
}

func (c *CheckIndexesTask) checkIPNIMK20(ctx context.Context, taskID harmonytask.TaskID) (err error) {
	var ids []struct {
		ID string `db:"id"`
	}

	err = c.db.Select(ctx, &ids, `SELECT m.id
									FROM market_mk20_deal AS m
									LEFT JOIN ipni AS i
									  ON m.piece_cid_v2 = i.piece_cid_v2
									LEFT JOIN market_mk20_pipeline AS p
									  ON m.id = p.id
									LEFT JOIN market_mk20_pipeline_waiting AS w
									  ON m.id = w.id
									WHERE m.piece_cid_v2 IS NOT NULL
									  AND m.ddo_v1 IS NOT NULL
									  AND m.ddo_v1 != 'null'
									  AND (m.retrieval_v1->>'announce_payload')::boolean = TRUE
									  AND i.piece_cid_v2 IS NULL
									  AND p.id IS NULL
									  AND w.id IS NULL;`)
	if err != nil {
		return xerrors.Errorf("getting mk20 deals which are not announced: %w", err)
	}

	if len(ids) == 0 {
		return nil
	}

	var ipniPeerIDs []struct {
		SpID   int64  `db:"sp_id"`
		PeerID string `db:"peer_id"`
	}
	err = c.db.Select(ctx, &ipniPeerIDs, `SELECT sp_id, peer_id FROM ipni_peerid`)
	if err != nil {
		return xerrors.Errorf("getting ipni tasks: %w", err)
	}

	spToPeer := map[int64]string{}
	for _, d := range ipniPeerIDs {
		spToPeer[d.SpID] = d.PeerID
	}

	var ongoingIpniTasks int64
	err = c.db.QueryRow(ctx, `SELECT COUNT(1) FROM ipni_task`).Scan(&ongoingIpniTasks)
	if err != nil {
		return xerrors.Errorf("getting ipni tasks: %w", err)
	}
	if ongoingIpniTasks >= int64(MaxOngoingIndexingTasks) {
		log.Debugw("too many ongoing ipni tasks, skipping ipni index checks", "task", taskID, "ongoing", ongoingIpniTasks)
		return nil
	}

	var have, missing, issues int64
	for _, i := range ids {
		id, err := ulid.Parse(i.ID)
		if err != nil {
			return xerrors.Errorf("parsing deal id: %w", err)
		}

		deal, err := mk20.DealFromDB(ctx, c.db, id)
		if err != nil {
			return xerrors.Errorf("getting deal from db: %w", err)
		}

		spid, err := address.IDFromAddress(deal.Products.DDOV1.Provider)
		if err != nil {
			return xerrors.Errorf("parsing provider address: %w", err)
		}

		pinfo, err := deal.PieceInfo()
		if err != nil {
			return xerrors.Errorf("getting piece info: %w", err)
		}

		pi := abi.PieceInfo{
			PieceCID: pinfo.PieceCIDV1,
			Size:     pinfo.Size,
		}

		var ctxIdBuf bytes.Buffer
		err = pi.MarshalCBOR(&ctxIdBuf)
		if err != nil {
			return xerrors.Errorf("marshaling piece info: %w", err)
		}

		ctxId := ctxIdBuf.Bytes()

		provider, ok := spToPeer[int64(spid)]
		if !ok {
			issues++
			log.Warnw("no peer id for spid", "spid", spid, "checkPiece", deal.Data.PieceCID.String())
			continue
		}

		var hasEnt int64
		err = c.db.QueryRow(ctx, `SELECT count(1) FROM ipni WHERE context_id=$1 AND provider=$2`, ctxId, provider).Scan(&hasEnt)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}
		if hasEnt > 0 {
			// has the entry
			have++
			continue
		}

		hasIndex, err := c.indexStore.CheckHasPiece(ctx, deal.Data.PieceCID)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}
		if !hasIndex {
			log.Warnw("no index for piece with missing IPNI Ad", "piece", deal.Data.PieceCID.String(), "checkPiece", pi.PieceCID)
			issues++
			continue
		}

		var sourceSector []struct {
			SectorNum   int64         `db:"sector_num"`
			PieceOffset int64         `db:"piece_offset"`
			RawSize     int64         `db:"raw_size"`
			PieceRef    sql.NullInt64 `db:"piece_ref"`
		}
		err = c.db.Select(ctx, &sourceSector, `SELECT sector_num, piece_offset, raw_size, piece_ref FROM market_piece_deal WHERE id = $1`, id.String())
		if err != nil {
			return xerrors.Errorf("getting source sector: %w", err)
		}
		if len(sourceSector) == 0 {
			log.Warnw("no source sector for piece", "piece", deal.Data.PieceCID.String(), "checkPiece", pi.PieceCID)
			issues++
			continue
		}

		src := sourceSector[0]

		if !src.PieceRef.Valid {
			log.Warnw("no piece ref for ipni reindexing", "piece", pi.PieceCID, "checkPiece", deal.Data.PieceCID.String())
			missing++
			continue
		}

		pieceIDUrl := url.URL{
			Scheme: "pieceref",
			Opaque: fmt.Sprintf("%d", src.PieceRef.Int64),
		}

		data := deal.Data
		ddo := deal.Products.DDOV1

		aggregation := 0
		if data.Format.Aggregate != nil {
			aggregation = int(data.Format.Aggregate.Type)
		}

		n, err := c.db.Exec(ctx, `INSERT INTO market_mk20_pipeline (
									id, sp_id, contract, client, piece_cid_v2, piece_cid, piece_size, raw_size, 
                                  	offline, url, indexing, announce, duration, piece_aggregation,
                                  	started, downloaded, after_commp, aggregated, sector, reg_seal_proof, sector_offset, sealed,
                                  	indexing_created_at, indexed, complete) 
									VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 
									        TRUE, TRUE, TRUE, TRUE, $15, 0, $16, TRUE, NOW(), TRUE, FALSE)`, // We can use reg_seal_proof = 0 because process_piece_deal will prevent new entry from being created
			deal.Identifier.String(), spid, ddo.ContractAddress, deal.Client, data.PieceCID.String(), pinfo.PieceCIDV1.String(), pinfo.Size, int64(pinfo.RawSize),
			false, pieceIDUrl.String(), true, true, ddo.Duration, aggregation,
			src.SectorNum, src.PieceOffset)
		if err != nil {
			return xerrors.Errorf("inserting mk20 pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
		}
		log.Infow("created IPNI reindexing pipeline", "piece", pi.PieceCID, "spid", spid)
		ongoingIpniTasks++
		if ongoingIpniTasks >= int64(MaxOngoingIndexingTasks) {
			return nil
		}
	}

	return nil
}

func (c *CheckIndexesTask) findSourceSector(ctx context.Context, spid, sectorNum int64) *storiface.SectorRef {
	var sourceSector *storiface.SectorRef
	var qres []struct {
		RegSealProof int64 `db:"reg_seal_proof"`
		StorageCount int64 `db:"storage"`
	}

	err := c.db.Select(ctx, &qres, `
				SELECT sm.reg_seal_proof, COUNT(sl.storage_id) AS storage
				FROM sectors_meta sm
				INNER JOIN sector_location sl ON sl.sector_num = sm.sector_num AND sl.miner_id = sm.sp_id
				WHERE sm.sector_num = $1 AND sm.sp_id = $2 AND sl.sector_filetype = $3
				GROUP BY sm.reg_seal_proof
			`, sectorNum, spid, storiface.FTUnsealed)
	if err != nil {
		log.Warnw("error querying sector storage", "error", err, "sector_num", sectorNum, "sp_id", spid)
		return nil
	}
	if len(qres) == 0 || qres[0].StorageCount == 0 {
		// No unsealed copy
		return nil
	}

	// We have an unsealed copy
	sourceSector = &storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(spid),
			Number: abi.SectorNumber(sectorNum),
		},
		ProofType: abi.RegisteredSealProof(qres[0].RegSealProof),
	}
	return sourceSector
}

func (c *CheckIndexesTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (c *CheckIndexesTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "CheckIndex",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 32 << 20,
		},
		IAmBored:    harmonytask.SingletonTaskAdder(CheckIndexInterval, c),
		MaxFailures: 3,
	}
}

func (c *CheckIndexesTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ = harmonytask.Reg(&CheckIndexesTask{})
