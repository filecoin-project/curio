package indexing

import (
	"bytes"
	"context"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

const CheckIndexInterval = 9 * time.Minute

var MaxOngoingIndexingTasks = 20

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

	return true, nil
}

func (c *CheckIndexesTask) checkIndexing(ctx context.Context, taskID harmonytask.TaskID) error {
	type checkEntry struct {
		PieceCid string `db:"piece_cid"`
		PieceLen int64  `db:"piece_length"`
		PieceOff int64  `db:"piece_offset"`
		SPID     int64  `db:"sp_id"`
		SectorID int64  `db:"sector_num"`
		RawSize  int64  `db:"raw_size"`
	}
	var toCheckList []checkEntry
	err := c.db.Select(ctx, &toCheckList, `
			SELECT mm.piece_cid, mpd.piece_length, mpd.piece_offset, mpd.sp_id, mpd.sector_num, mpd.raw_size
			FROM market_piece_metadata mm
			LEFT JOIN market_piece_deal mpd ON mm.piece_cid = mpd.piece_cid
			WHERE mm.indexed = true
		`)
	if err != nil {
		return err
	}

	toCheck := make(map[string][]checkEntry)
	for _, e := range toCheckList {
		toCheck[e.PieceCid] = append(toCheck[e.PieceCid], e)
	}

	// Check the number of ongoing indexing tasks
	var ongoingIndexingTasks int64
	err = c.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM market_mk12_deal_pipeline
		WHERE indexing_created_at IS NOT NULL AND indexed = false
	`).Scan(&ongoingIndexingTasks)
	if err != nil {
		return xerrors.Errorf("counting ongoing indexing tasks: %w", err)
	}
	if ongoingIndexingTasks >= int64(MaxOngoingIndexingTasks) {
		log.Warnw("too many ongoing indexing tasks, skipping check indexes task", "task", taskID, "ongoing", ongoingIndexingTasks)
		return nil
	}

	var have, missing int64

	for p, cent := range toCheck {
		pieceCid, err := cid.Parse(p)
		if err != nil {
			return xerrors.Errorf("parsing piece cid: %w", err)
		}

		// Check if the piece is present in the index store
		hasEnt, err := c.indexStore.CheckHasPiece(ctx, pieceCid)
		if err != nil {
			return xerrors.Errorf("getting piece hash range: %w", err)
		}

		if hasEnt {
			have++
			continue
		}

		// Index not present, flag for repair
		missing++
		log.Warnw("piece missing in indexstore", "piece", pieceCid, "task", taskID)

		var uuids []struct {
			DealUUID string `db:"uuid"`
		}
		err = c.db.Select(ctx, &uuids, `
			SELECT uuid
			FROM market_mk12_deals
			WHERE piece_cid = $1
		`, pieceCid.String())
		if err != nil {
			return xerrors.Errorf("getting deal uuids: %w", err)
		}
		if len(uuids) == 0 {
			log.Warnw("no deals for unindexed piece", "piece", pieceCid, "task", taskID)
			continue
		}

		// Check the number of ongoing indexing tasks again
		err = c.db.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM market_mk12_deal_pipeline
			WHERE indexing_created_at IS NOT NULL AND indexed = false
		`).Scan(&ongoingIndexingTasks)
		if err != nil {
			return xerrors.Errorf("counting ongoing indexing tasks: %w", err)
		}
		if ongoingIndexingTasks >= int64(MaxOngoingIndexingTasks) {
			log.Warnw("too many ongoing indexing tasks, stopping processing missing pieces", "task", taskID, "ongoing", ongoingIndexingTasks)
			break
		}

		// Collect deal UUIDs
		dealUUIDs := make([]string, 0, len(uuids))
		for _, u := range uuids {
			dealUUIDs = append(dealUUIDs, u.DealUUID)
		}

		// Get deal details from market_mk12_deals
		var deals []struct {
			UUID      string    `db:"uuid"`
			SPID      int64     `db:"sp_id"`
			PieceCID  string    `db:"piece_cid"`
			PieceSize int64     `db:"piece_size"`
			Offline   bool      `db:"offline"`
			URL       *string   `db:"url"`
			Headers   []byte    `db:"url_headers"`
			CreatedAt time.Time `db:"created_at"`
		}
		err = c.db.Select(ctx, &deals, `
			SELECT uuid, sp_id, piece_cid, piece_size, offline, url, url_headers, created_at
			FROM market_mk12_deals
			WHERE uuid = ANY($1)
		`, dealUUIDs)
		if err != nil {
			return xerrors.Errorf("getting deal details: %w", err)
		}

		// Use the first deal for processing
		deal := deals[0]

		var sourceSector *storiface.SectorRef
		var sourceOff, rawSize int64
		for _, entry := range cent {
			if entry.SPID != deal.SPID {
				continue
			}
			if sourceSector = c.findSourceSector(ctx, entry.SPID, entry.SectorID); sourceSector == nil {
				// No unsealed copy
				continue
			}
			sourceOff = entry.PieceOff
			rawSize = entry.RawSize
			break
		}

		if sourceSector == nil {
			log.Infow("no unsealed copy of sector found for reindexing", "piece", pieceCid, "task", taskID, "deals", len(deals), "have", have, "missing", missing, "ongoing", ongoingIndexingTasks)
			continue
		}

		var added bool

		_, err = c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			added = false

			// Insert into market_mk12_deal_pipeline
			n, err := tx.Exec(`
			INSERT INTO market_mk12_deal_pipeline (
				uuid, sp_id, piece_cid, piece_size, raw_size, offline, url, headers, created_at,
				sector, sector_offset, reg_seal_proof,
				started, after_psd, after_commp, after_find_deal, sealed, complete,
				indexed, indexing_created_at, indexing_task_id, should_index
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			        true, true, true, true, true, true,
			        false, NOW(), NULL, true)
			ON CONFLICT (uuid) DO NOTHING
		`, deal.UUID, deal.SPID, deal.PieceCID, deal.PieceSize, rawSize, deal.Offline, deal.URL, deal.Headers, deal.CreatedAt,
				sourceSector.ID.Number, sourceOff, int64(sourceSector.ProofType))
			if err != nil {
				return false, xerrors.Errorf("upserting into deal pipeline for uuid %s: %w", deal.UUID, err)
			}
			if n == 0 {
				return false, nil
			}
			added = true

			_, err = tx.Exec(`UPDATE market_piece_metadata SET indexed = FALSE WHERE piece_cid = $1`, pieceCid.String())
			if err != nil {
				return false, xerrors.Errorf("updating market_piece_metadata.indexed column: %w", err)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("inserting into market_mk12_deal_pipeline: %w", err)
		}

		if added {
			log.Infow("added reindexing pipeline entry", "uuid", deal.UUID, "task", taskID, "piece", deal.PieceCID)
			ongoingIndexingTasks++
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
		PieceCid string `db:"piece_cid"`
		SpID     int64  `db:"sp_id"`
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
                FROM market_mk12_deals WHERE announce_to_ipni=true`)
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
	err = c.db.Select(ctx, &announcePiecePipelines, `SELECT piece_cid, sp_id FROM market_mk12_deal_pipeline WHERE announce=true`)
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

	var have, missisg, issues int64
	defer func() {
		log.Infow("IPNI Ad check", "have", have, "missisg", missisg, "issues", issues, "err", err)
	}()

	for _, deal := range toCheck {
		if _, ok := announcablePipelines[pieceSP{deal.PieceCID, deal.SpID}]; ok {
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
		err = c.db.Select(ctx, &sourceSector, `SELECT sector_num, piece_offset, raw_size FROM market_piece_deal WHERE piece_cid=$1 AND sp_id = $2`, deal.PieceCID, deal.SpID)
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

		missisg++

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
		IAmBored: harmonytask.SingletonTaskAdder(CheckIndexInterval, c),
	}
}

func (c *CheckIndexesTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ = harmonytask.Reg(&CheckIndexesTask{})
