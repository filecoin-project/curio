package storage_market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/market/mk20"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/proofs"
	"github.com/filecoin-project/lotus/chain/types"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
)

type MK20PipelinePiece struct {
	ID               string  `db:"id"`
	SPID             int64   `db:"sp_id"`
	Client           string  `db:"client"`
	Contract         string  `db:"contract"`
	PieceCID         string  `db:"piece_cid"`
	PieceSize        int64   `db:"piece_size"`
	RawSize          int64   `db:"raw_size"`
	Offline          bool    `db:"offline"`
	URL              *string `db:"url"` // Nullable fields use pointers
	Indexing         bool    `db:"indexing"`
	Announce         bool    `db:"announce"`
	AllocationID     *int64  `db:"allocation_id"` // Nullable fields use pointers
	Duration         *int64  `db:"duration"`      // Nullable fields use pointers
	PieceAggregation int     `db:"piece_aggregation"`

	Started bool `db:"started"`

	Downloaded bool `db:"downloaded"`

	CommTaskID *int64 `db:"commp_task_id"`
	AfterCommp bool   `db:"after_commp"`

	DealAggregation   int    `db:"deal_aggregation"`
	AggregationIndex  int64  `db:"aggr_index"`
	AggregationTaskID *int64 `db:"agg_task_id"`
	Aggregated        bool   `db:"aggregated"`

	Sector       *int64 `db:"sector"`         // Nullable fields use pointers
	RegSealProof *int   `db:"reg_seal_proof"` // Nullable fields use pointers
	SectorOffset *int64 `db:"sector_offset"`  // Nullable fields use pointers

	IndexingCreatedAt *time.Time `db:"indexing_created_at"` // Nullable fields use pointers
	IndexingTaskID    *int64     `db:"indexing_task_id"`
	Indexed           bool       `db:"indexed"`
}

func (d *CurioStorageDealMarket) processMK20Deals(ctx context.Context) {
	go d.pipelineInsertLoop(ctx)
	// Catch any panics if encountered as we are working with user provided data
	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1<<16)
			n := runtime.Stack(trace, false)

			log.Errorf("panic occurred: %v\n%s", r, trace[:n])
		}
	}()
	d.processMK20DealPieces(ctx)
	d.processMK20DealAggregation(ctx)
	d.processMK20DealIngestion(ctx)
}

func (d *CurioStorageDealMarket) pipelineInsertLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.insertDDODealInPipeline(ctx)
		}
	}
}

func (d *CurioStorageDealMarket) insertDDODealInPipeline(ctx context.Context) {
	var deals []string
	rows, err := d.db.Query(ctx, `SELECT id from market_mk20_pipeline_waiting WHERE waiting_for_data = FALSE`)
	if err != nil {
		log.Errorf("querying mk20 pipeline waiting: %s", err)
		return
	}
	for rows.Next() {
		var dealID string
		err = rows.Scan(&dealID)
		if err != nil {
			log.Errorf("scanning mk20 pipeline waiting: %s", err)
			return
		}
		deals = append(deals, dealID)
	}

	if err := rows.Err(); err != nil {
		log.Errorf("iterating over mk20 pipeline waiting: %s", err)
		return
	}
	var dealIDs []ulid.ULID
	for _, dealID := range deals {
		id, err := ulid.Parse(dealID)
		if err != nil {
			log.Errorf("parsing deal id: %s", err)
			return
		}
		dealIDs = append(dealIDs, id)
	}
	if len(dealIDs) == 0 {
		return
	}
	for _, id := range dealIDs {
		comm, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			deal, err := mk20.DealFromTX(tx, id)
			if err != nil {
				return false, xerrors.Errorf("getting deal from db: %w", err)
			}
			err = insertPiecesInTransaction(ctx, tx, deal)
			if err != nil {
				return false, xerrors.Errorf("inserting pieces in db: %w", err)
			}
			_, err = tx.Exec(`DELETE FROM market_mk20_pipeline_waiting WHERE id = $1`, id.String())
			if err != nil {
				return false, xerrors.Errorf("deleting deal from mk20 pipeline waiting: %w", err)
			}
			return true, nil
		})
		if err != nil {
			log.Errorf("inserting deal in pipeline: %s", err)
			continue
		}
		if !comm {
			log.Errorf("inserting deal in pipeline: commit failed")
			continue
		}
	}
}

func insertPiecesInTransaction(ctx context.Context, tx *harmonydb.Tx, deal *mk20.Deal) error {
	spid, err := address.IDFromAddress(deal.Products.DDOV1.Provider)
	if err != nil {
		return fmt.Errorf("getting provider ID: %w", err)
	}

	ddo := deal.Products.DDOV1
	data := deal.Data
	dealID := deal.Identifier.String()

	var allocationID interface{}
	if ddo.AllocationId != nil {
		allocationID = *ddo.AllocationId
	} else {
		allocationID = nil
	}

	var aggregation interface{}
	if data.Format.Aggregate != nil {
		aggregation = data.Format.Aggregate.Type
	} else {
		aggregation = nil
	}

	// Insert pipeline when Data source is HTTP
	if data.SourceHTTP != nil {
		var pieceID int64
		// Attempt to select the piece ID first
		err = tx.QueryRow(`SELECT id FROM parked_pieces WHERE piece_cid = $1 AND piece_padded_size = $2`, data.PieceCID.String(), data.Size).Scan(&pieceID)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Piece does not exist, attempt to insert
				err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
							VALUES ($1, $2, $3, TRUE)
							ON CONFLICT (piece_cid, piece_padded_size, long_term, cleanup_task_id) DO NOTHING
							RETURNING id`, data.PieceCID.String(), int64(data.Size), int64(data.SourceHTTP.RawSize)).Scan(&pieceID)
				if err != nil {
					return xerrors.Errorf("inserting new parked piece and getting id: %w", err)
				}
			} else {
				// Some other error occurred during select
				return xerrors.Errorf("checking existing parked piece: %w", err)
			}
		}

		var refIds []int64

		// Add parked_piece_refs
		for _, src := range data.SourceHTTP.URLs {
			var refID int64

			headers, err := json.Marshal(src.Headers)
			if err != nil {
				return xerrors.Errorf("marshaling headers: %w", err)
			}

			err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
        			VALUES ($1, $2, $3, TRUE) RETURNING ref_id`, pieceID, src.URL, headers).Scan(&refID)
			if err != nil {
				return xerrors.Errorf("inserting parked piece ref: %w", err)
			}
			refIds = append(refIds, refID)
		}

		n, err := tx.Exec(`INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids) VALUES ($1, $2, $3, $4)`,
			dealID, data.PieceCID.String(), data.Size, refIds)
		if err != nil {
			return xerrors.Errorf("inserting mk20 download pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("inserting mk20 download pipeline: %d rows affected", n)
		}

		n, err = tx.Exec(`INSERT INTO market_mk20_pipeline (
            id, sp_id, contract, client, piece_cid,
            piece_size, raw_size, offline, indexing, announce,
            allocation_id, duration, piece_aggregation, started) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, TRUE)`,
			dealID, spid, ddo.ContractAddress, ddo.Client.String(), data.PieceCID.String(),
			data.Size, int64(data.SourceHTTP.RawSize), false, ddo.Indexing, ddo.AnnounceToIPNI,
			allocationID, ddo.Duration, aggregation)
		if err != nil {
			return xerrors.Errorf("inserting mk20 pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
		}
		return nil
	}

	// INSERT Pipeline when data source is offline
	if deal.Data.SourceOffline != nil {
		n, err := tx.Exec(`INSERT INTO market_mk20_pipeline (
            id, sp_id, contract, client, piece_cid,
            piece_size, raw_size, offline, indexing, announce,
            allocation_id, duration, piece_aggregation) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			dealID, spid, ddo.ContractAddress, ddo.Client.String(), data.PieceCID.String(),
			data.Size, int64(data.SourceHTTP.RawSize), true, ddo.Indexing, ddo.AnnounceToIPNI,
			allocationID, ddo.Duration, aggregation)
		if err != nil {
			return xerrors.Errorf("inserting mk20 pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("inserting mk20 pipeline: %d rows affected", n)
		}
		return nil
	}

	// Insert pipeline when data source is aggregate
	if deal.Data.SourceAggregate != nil {

		// Find all unique pieces where data source is HTTP
		type downloadkey struct {
			ID       string
			PieceCID cid.Cid
			Size     abi.PaddedPieceSize
		}
		toDownload := make(map[downloadkey][]mk20.HttpUrl)
		existing := make(map[downloadkey]*int64)
		offlinelist := make(map[downloadkey]struct{})

		for _, piece := range deal.Data.SourceAggregate.Pieces {
			if piece.SourceHTTP != nil {
				urls, ok := toDownload[downloadkey{ID: dealID, PieceCID: piece.PieceCID, Size: piece.Size}]
				if ok {
					toDownload[downloadkey{ID: dealID, PieceCID: piece.PieceCID, Size: piece.Size}] = append(urls, piece.SourceHTTP.URLs...)
				} else {
					toDownload[downloadkey{ID: dealID, PieceCID: piece.PieceCID, Size: piece.Size}] = piece.SourceHTTP.URLs
					existing[downloadkey{ID: dealID, PieceCID: piece.PieceCID, Size: piece.Size}] = nil
				}
			}
			if piece.SourceOffline != nil {
				offlinelist[downloadkey{ID: dealID, PieceCID: piece.PieceCID, Size: piece.Size}] = struct{}{}
			}
		}

		pqBatch := &pgx.Batch{}
		pqBatchSize := 20000

		for k, _ := range toDownload {
			pqBatch.Queue(`SELECT id FROM parked_pieces WHERE piece_cid = $1 AND piece_padded_size = $2`, k.PieceCID.String(), int64(k.Size)).QueryRow(func(row pgx.Row) error {
				var id int64
				err = row.Scan(&id)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return nil
					}
					return xerrors.Errorf("scanning parked piece id: %w", err)
				}
				existing[k] = &id
				return nil
			})
			if pqBatch.Len() > pqBatchSize {
				res := tx.SendBatch(ctx, pqBatch)
				if err := res.Close(); err != nil {
					return xerrors.Errorf("closing parked piece query batch: %w", err)
				}
				pqBatch = &pgx.Batch{}
			}
		}

		if pqBatch.Len() > 0 {
			res := tx.SendBatch(ctx, pqBatch)
			if err := res.Close(); err != nil {
				return xerrors.Errorf("closing parked piece query batch: %w", err)
			}
		}

		piBatch := &pgx.Batch{}
		piBatchSize := 10000
		for k, v := range existing {
			if v == nil {
				piBatch.Queue(`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
				 VALUES ($1, $2, $3, FALSE)
				 ON CONFLICT (piece_cid, piece_padded_size, long_term, cleanup_task_id) DO NOTHING
				 RETURNING id`, k.PieceCID.String(), int64(k.Size), int64(k.Size)).QueryRow(func(row pgx.Row) error {
					var id int64
					err = row.Scan(&id)
					if err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return nil
						}
						return xerrors.Errorf("scanning parked piece id: %w", err)
					}
					v = &id
					return nil
				})
				if piBatch.Len() > piBatchSize {
					res := tx.SendBatch(ctx, piBatch)
					if err := res.Close(); err != nil {
						return xerrors.Errorf("closing parked piece insert batch: %w", err)
					}
					piBatch = &pgx.Batch{}
				}
			}
		}

		if piBatch.Len() > 0 {
			res := tx.SendBatch(ctx, piBatch)
			if err := res.Close(); err != nil {
				return xerrors.Errorf("closing parked piece insert batch: %w", err)
			}
		}

		prBatch := &pgx.Batch{}
		prBatchSize := 10000
		downloadMap := make(map[downloadkey][]int64)

		for k, v := range existing {
			if v == nil {
				return xerrors.Errorf("missing parked piece for %s", k.PieceCID.String())
			}
			var refIds []int64
			urls := toDownload[downloadkey{PieceCID: k.PieceCID, Size: k.Size}]
			for _, src := range urls {
				headers, err := json.Marshal(src.Headers)
				if err != nil {
					return xerrors.Errorf("marshal headers: %w", err)
				}
				prBatch.Queue(`INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term) VALUES ($1, $2, $3, FALSE) RETURNING ref_id`,
					*v, src.URL, headers).QueryRow(func(row pgx.Row) error {
					var id int64
					err = row.Scan(&id)
					if err != nil {
						return xerrors.Errorf("scanning parked piece ref id: %w", err)
					}
					refIds = append(refIds, id)
					return nil
				})

				if prBatch.Len() > prBatchSize {
					res := tx.SendBatch(ctx, prBatch)
					if err := res.Close(); err != nil {
						return xerrors.Errorf("closing parked piece ref insert batch: %w", err)
					}
					prBatch = &pgx.Batch{}
				}
			}
			downloadMap[downloadkey{ID: dealID, PieceCID: k.PieceCID, Size: k.Size}] = refIds

		}

		if prBatch.Len() > 0 {
			res := tx.SendBatch(ctx, prBatch)
			if err := res.Close(); err != nil {
				return xerrors.Errorf("closing parked piece ref insert batch: %w", err)
			}
		}

		mdBatch := &pgx.Batch{}
		mdBatchSize := 20000
		for k, v := range downloadMap {
			mdBatch.Queue(`INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids) VALUES ($1, $2, $3, $4)`,
				k.ID, k.PieceCID.String(), k.Size, v)
			if mdBatch.Len() > mdBatchSize {
				res := tx.SendBatch(ctx, mdBatch)
				if err := res.Close(); err != nil {
					return xerrors.Errorf("closing mk20 download pipeline insert batch: %w", err)
				}
				mdBatch = &pgx.Batch{}
			}
		}
		if mdBatch.Len() > 0 {
			res := tx.SendBatch(ctx, mdBatch)
			if err := res.Close(); err != nil {
				return xerrors.Errorf("closing mk20 download pipeline insert batch: %w", err)
			}
		}

		pBatch := &pgx.Batch{}
		pBatchSize := 4000
		for i, piece := range deal.Data.SourceAggregate.Pieces {
			var offline bool
			if piece.SourceOffline != nil {
				offline = true
			}
			rawSize, err := piece.RawSize()
			if err != nil {
				return xerrors.Errorf("getting raw size: %w", err)
			}
			pBatch.Queue(`INSERT INTO market_mk20_pipeline (id, sp_id, contract, client, piece_cid,
            piece_size, raw_size, offline, indexing, announce, allocation_id, duration, 
            piece_aggregation, deal_aggregation, aggr_index, started) 
        	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
				dealID, spid, ddo.ContractAddress, ddo.Client.String(), piece.PieceCID.String(),
				piece.Size, rawSize, offline, ddo.Indexing, ddo.AnnounceToIPNI, allocationID, ddo.Duration,
				0, data.Format.Aggregate.Type, i, !offline)
			if pBatch.Len() > pBatchSize {
				res := tx.SendBatch(ctx, pBatch)
				if err := res.Close(); err != nil {
					return xerrors.Errorf("closing mk20 pipeline insert batch: %w", err)
				}
				pBatch = &pgx.Batch{}
			}
		}
		if pBatch.Len() > 0 {
			res := tx.SendBatch(ctx, pBatch)
			if err := res.Close(); err != nil {
				return xerrors.Errorf("closing mk20 pipeline insert batch: %w", err)
			}
		}
		return nil
	}

	return xerrors.Errorf("unknown data source type")
}

func (d *CurioStorageDealMarket) processMK20DealPieces(ctx context.Context) {
	var pieces []MK20PipelinePiece
	err := d.db.Select(ctx, &pieces, `SELECT 
											id,
											sp_id,
											contract,
											piece_index,
											piece_cid,
											piece_size,
											raw_size,
											offline,
											url,
											indexing,
											announce,
											verified,
											allocation_id,
											duration,
											piece_aggregation,
											started,
											downloaded,
											deal_aggregation,
											aggr_index,
											agg_task_id,
											aggregated,
											sector,
											reg_seal_proof,
											sector_offset,
											indexing_created_at,
											indexing_task_id,
											indexed
										FROM 
											market_mk20_pipeline
										WHERE complete = false ORDER BY created_at ASC;
										`)
	if err != nil {
		log.Errorw("failed to get deals from DB", "error", err)
		return
	}

	for _, piece := range pieces {
		err := d.processMk20Pieces(ctx, piece)
		if err != nil {
			log.Errorw("failed to process deal", "ID", piece.ID, "SP", piece.SPID, "Contract", piece.Contract, "Piece CID", piece.PieceCID, "Piece Size", piece.PieceSize, "error", err)
			continue
		}
	}

}

func (d *CurioStorageDealMarket) processMk20Pieces(ctx context.Context, piece MK20PipelinePiece) error {
	err := d.downloadMk20Deal(ctx, piece)
	if err != nil {
		return err
	}

	err = d.findOfflineURLMk20Deal(ctx, piece)
	if err != nil {
		return err
	}

	err = d.createCommPMk20Piece(ctx, piece)
	if err != nil {
		return err
	}

	err = d.addDealOffset(ctx, piece)
	if err != nil {
		return err
	}

	return nil
}

// downloadMk20Deal handles the downloading process of an MK20 pipeline piece by scheduling it in the database and updating its status.
// If the pieces are part of an aggregation deal then we download for short term otherwise we check if piece needs to be indexed.
// If indexing is true then we download for long term to avoid the need to have unsealed copy
func (d *CurioStorageDealMarket) downloadMk20Deal(ctx context.Context, piece MK20PipelinePiece) error {
	if !piece.Downloaded && piece.Started {
		_, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var refid int64
			err = tx.QueryRow(`SELECT ref_id FROM (
									  SELECT unnest(dp.ref_ids) AS ref_id
									  FROM market_mk20_download_pipeline dp
									  WHERE dp.id = $1 AND dp.piece_cid = $2 AND dp.piece_size = $3
									) u
									JOIN parked_piece_refs pr ON pr.ref_id = u.ref_id
									JOIN parked_pieces pp ON pp.id = pr.piece_id
									WHERE pp.complete = TRUE
									LIMIT 1;`, piece.ID, piece.PieceCID, piece.PieceSize).Scan(&refid)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("failed to check if the piece is downloaded: %w", err)
			}
			_, err = tx.Exec(`
									DELETE FROM parked_piece_refs
									WHERE ref_id IN (
									  SELECT unnest(dp.ref_ids)
									  FROM market_mk20_download_pipeline dp
									  WHERE dp.id = $1
										AND dp.piece_cid = $2
										AND dp.piece_size = $3
									)
									AND ref_id != $4;
									`, piece.ID, piece.PieceCID, piece.PieceSize, refid)
			if err != nil {
				return false, xerrors.Errorf("failed to delete parked piece refs: %w", err)
			}

			pieceIDUrl := url.URL{
				Scheme: "pieceref",
				Opaque: fmt.Sprintf("%d", refid),
			}

			_, err = tx.Exec(`UPDATE market_mk20_pipeline SET downloaded = TRUE, url = $1 
                                   WHERE id = $2
                                   AND piece_cid = $3
                                   AND piece_size = $4`,
				pieceIDUrl.String(), piece.ID, piece.PieceCID, piece.PieceSize)
			if err != nil {
				return false, xerrors.Errorf("failed to update pipeline piece table: %w", err)
			}
			piece.Downloaded = true
			return true, nil
		}, harmonydb.OptionRetry())

		if err != nil {
			return xerrors.Errorf("failed to schedule the deal for download: %w", err)
		}
	}
	return nil
}

// findOfflineURLMk20Deal find the URL for offline piece. In MK20, we don't work directly with remote pieces, we download them
// locally and then decide to aggregate, long term or remove them
func (d *CurioStorageDealMarket) findOfflineURLMk20Deal(ctx context.Context, piece MK20PipelinePiece) error {
	if piece.Offline && !piece.Downloaded && !piece.Started {
		comm, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var updated bool
			err = tx.QueryRow(`
								WITH offline_match AS (
								  SELECT url, headers, raw_size
								  FROM market_mk20_offline_urls
								  WHERE id = $1 AND piece_cid = $2 AND piece_size = $3
								),
								existing_piece AS (
								  SELECT id AS piece_id
								  FROM parked_pieces
								  WHERE piece_cid = $2 AND piece_padded_size = $3
								),
								inserted_piece AS (
								  INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
								  SELECT $2, $3, o.raw_size, NOT (p.deal_aggregation > 0)
								  FROM offline_match o, market_mk20_pipeline p
								  WHERE p.id = $1 AND p.piece_cid = $2 AND p.piece_size = $3
									AND NOT EXISTS (SELECT 1 FROM existing_piece)
								  RETURNING id AS piece_id
								),
								selected_piece AS (
								  SELECT piece_id FROM existing_piece
								  UNION ALL
								  SELECT piece_id FROM inserted_piece
								),
								inserted_refs AS (
								  INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
								  SELECT
									s.piece_id,
									o.url,
									o.headers,
									NOT (p.deal_aggregation > 0)
								  FROM selected_piece s
								  JOIN offline_match o ON true
								  JOIN market_mk20_pipeline p ON p.id = $1 AND p.piece_cid = $2 AND p.piece_size = $3
								  RETURNING ref_id
								),
								upsert_pipeline AS (
								  INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids)
								  SELECT $1, $2, $3, array_agg(ref_id)
								  FROM inserted_refs
								  ON CONFLICT (id, piece_cid, piece_size) DO UPDATE
								  SET ref_ids = (
									SELECT array(
									  SELECT DISTINCT unnest(dp.ref_ids) || unnest(EXCLUDED.ref_ids)
									)
								  )
								  FROM market_mk20_download_pipeline dp
								  WHERE dp.id = EXCLUDED.id AND dp.piece_cid = EXCLUDED.piece_cid AND dp.piece_size = EXCLUDED.piece_size
								  RETURNING id
								),
								mark_started AS (
								  UPDATE market_mk20_pipeline
								  SET started = TRUE
								  WHERE id = $1 AND piece_cid = $2 AND piece_size = $3
									AND EXISTS (SELECT 1 FROM offline_match)
								  RETURNING id
								)
								SELECT EXISTS (SELECT 1 FROM mark_started);
								`, piece.ID, piece.PieceCID, piece.PieceSize).Scan(&updated)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return false, xerrors.Errorf("failed to update the pipeline for deal %s: %w", piece.ID, err)
				}
			}

			if updated {
				return true, nil
			}

			// Check if We can find the URL for this piece on remote servers
			for rUrl, headers := range d.urls {
				// Create a new HTTP request
				urlString := fmt.Sprintf("%s?id=%s", rUrl, piece.PieceCID)
				req, err := http.NewRequest(http.MethodHead, urlString, nil)
				if err != nil {
					return false, xerrors.Errorf("error creating request: %w", err)
				}

				req.Header = headers

				// Create a client and make the request
				client := &http.Client{
					Timeout: 10 * time.Second,
				}
				resp, err := client.Do(req)
				if err != nil {
					return false, xerrors.Errorf("error making GET request: %w", err)
				}

				// Check the response code for 404
				if resp.StatusCode != http.StatusOK {
					if resp.StatusCode != 404 {
						return false, xerrors.Errorf("not ok response from HTTP server: %s", resp.Status)
					}
					continue
				}

				hdrs, err := json.Marshal(headers)
				if err != nil {
					return false, xerrors.Errorf("marshaling headers: %w", err)
				}

				rawSizeStr := resp.Header.Get("Content-Length")
				if rawSizeStr == "" {
					continue
				}
				rawSize, err := strconv.ParseInt(rawSizeStr, 10, 64)
				if err != nil {
					return false, xerrors.Errorf("failed to parse the raw size: %w", err)
				}

				if rawSize != piece.RawSize {
					continue
				}

				if abi.PaddedPieceSize(piece.PieceSize) != padreader.PaddedSize(uint64(rawSize)).Padded() {
					continue
				}

				_, err = tx.Exec(`WITH pipeline_piece AS (
										  SELECT id, piece_cid, piece_size, deal_aggregation
										  FROM market_mk20_pipeline
										  WHERE id = $1 AND piece_cid = $2 AND piece_size = $3
										),
										existing_piece AS (
										  SELECT id AS piece_id
										  FROM parked_pieces
										  WHERE piece_cid = $2 AND piece_padded_size = $3
										),
										inserted_piece AS (
										  INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
										  SELECT $2, $3, $4, NOT (p.deal_aggregation > 0)
										  FROM pipeline_piece p
										  WHERE NOT EXISTS (SELECT 1 FROM existing_piece)
										  RETURNING id AS piece_id
										),
										selected_piece AS (
										  SELECT piece_id FROM existing_piece
										  UNION ALL
										  SELECT piece_id FROM inserted_piece
										),
										inserted_ref AS (
										  INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
										  SELECT
											s.piece_id,
											$5,
											$6,
											NOT (p.deal_aggregation > 0)
										  FROM selected_piece s
										  JOIN pipeline_piece p ON true
										  RETURNING ref_id
										),
										upsert_pipeline AS (
										  INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, ref_ids)
										  SELECT $1, $2, $3, array_agg(ref_id)
										  FROM inserted_ref
										  ON CONFLICT (id, piece_cid, piece_size) DO UPDATE
										  SET ref_ids = (
											SELECT array(
											  SELECT DISTINCT unnest(dp.ref_ids) || unnest(EXCLUDED.ref_ids)
											)
										  )
										  FROM market_mk20_download_pipeline dp
										  WHERE dp.id = EXCLUDED.id AND dp.piece_cid = EXCLUDED.piece_cid AND dp.piece_size = EXCLUDED.piece_size
										),
										mark_started AS (
										  UPDATE market_mk20_pipeline
										  SET started = TRUE
										  WHERE id = $1 AND piece_cid = $2 AND piece_size = $3 AND started = FALSE
										)`, piece.ID, piece.PieceCID, piece.PieceSize, rUrl, hdrs, rawSize)
				if err != nil {
					return false, xerrors.Errorf("failed to update pipeline piece table: %w", err)
				}

				return true, nil
			}
			return false, nil

		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("deal %s: %w", piece.ID, err)
		}

		if comm {
			log.Infow("URL attached for offline deal piece", "deal piece", piece)
		}
	}

	return nil
}

// createCommPMk20Piece handles the creation of a CommP task for an MK20 pipeline piece, updating its status based on piece attributes.
func (d *CurioStorageDealMarket) createCommPMk20Piece(ctx context.Context, piece MK20PipelinePiece) error {
	if piece.Downloaded && !piece.AfterCommp && piece.CommTaskID == nil {
		// Skip commP is configured to do so
		if d.cfg.Market.StorageMarketConfig.MK12.SkipCommP {
			_, err := d.db.Exec(ctx, `UPDATE market_mk20_pipeline SET after_commp = TRUE, commp_task_id = NULL
										 	WHERE id = $1 
											  AND sp_id = $2 
											  AND piece_cid = $3
											  AND piece_size = $4
											  AND raw_size = $5
										 	  AND aggr_index = $6
											  AND downloaded = TRUE
											  AND after_commp = FALSE`, piece.ID, piece.SPID, piece.PieceCID, piece.PieceSize, piece.RawSize, piece.AggregationIndex)
			if err != nil {
				return xerrors.Errorf("marking piece as after commP: %w", err)
			}
			log.Infow("commP skipped successfully", "deal piece", piece)
			return nil
		}

		if d.adders[pollerCommP].IsSet() {
			d.adders[pollerCommP].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// update
				n, err := tx.Exec(`UPDATE market_mk20_pipeline SET commp_task_id = $1 
                                		 WHERE id = $2 
										  AND sp_id = $3 
										  AND piece_cid = $4
										  AND piece_size = $5
										  AND raw_size = $6
                                		  AND aggr_index = $7
										  AND downloaded = TRUE
										  AND after_commp = FALSE
										  AND commp_task_id = NULL`, id, piece.ID, piece.SPID, piece.PieceCID, piece.PieceSize, piece.RawSize, piece.AggregationIndex)
				if err != nil {
					return false, xerrors.Errorf("creating commP task for deal piece: %w", err)
				}

				// commit only if we updated the piece
				return n > 0, nil
			})
			log.Infow("commP task created successfully", "deal piece", piece)
		}

		return nil
	}
	return nil
}

func (d *CurioStorageDealMarket) addDealOffset(ctx context.Context, piece MK20PipelinePiece) error {
	// Get the deal offset if sector has started sealing
	if piece.Sector != nil && piece.RegSealProof == nil {
		_, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			type pieces struct {
				Cid   string              `db:"piece_cid"`
				Size  abi.PaddedPieceSize `db:"piece_size"`
				Index int64               `db:"piece_index"`
			}

			var pieceList []pieces
			err = tx.Select(&pieceList, `SELECT piece_cid, piece_size, piece_index
												FROM sectors_sdr_initial_pieces
												WHERE sp_id = $1 AND sector_number = $2
												
												UNION ALL
												
												SELECT piece_cid, piece_size, piece_index
												FROM sectors_snap_initial_pieces
												WHERE sp_id = $1 AND sector_number = $2
												
												ORDER BY piece_index ASC;`, piece.SPID, piece.Sector)
			if err != nil {
				return false, xerrors.Errorf("getting pieces for sector: %w", err)
			}

			if len(pieceList) == 0 {
				// Sector might be waiting for more deals
				return false, nil
			}

			var offset abi.UnpaddedPieceSize

			for _, p := range pieceList {
				_, padLength := proofs.GetRequiredPadding(offset.Padded(), p.Size)
				offset += padLength.Unpadded()
				if p.Cid == piece.PieceCID && p.Size == abi.PaddedPieceSize(piece.PieceSize) {
					n, err := tx.Exec(`UPDATE market_mk20_pipeline SET sector_offset = $1 WHERE id = $2 AND sector = $3 AND sector_offset IS NULL`, offset.Padded(), piece.ID, piece.Sector)
					if err != nil {
						return false, xerrors.Errorf("updating deal offset: %w", err)
					}
					if n != 1 {
						return false, xerrors.Errorf("expected to update 1 deal, updated %d", n)
					}
					offset += p.Size.Unpadded()
					return true, nil
				}

			}
			return false, xerrors.Errorf("failed to find deal offset for piece %s", piece.PieceCID)
		}, harmonydb.OptionRetry())
		if err != nil {
			return xerrors.Errorf("failed to get deal offset: %w", err)
		}
	}
	return nil
}

func (d *CurioStorageDealMarket) processMK20DealAggregation(ctx context.Context) {
	if !d.adders[pollerAggregate].IsSet() {
		return
	}

	var deals []struct {
		ID    string `db:"id"`
		Count int    `db:"count"`
	}

	err := d.db.Select(ctx, &deals, `SELECT id, COUNT(*) AS count
										FROM market_mk20_pipeline
										GROUP BY id
										HAVING bool_and(after_commp)
										   AND bool_and(NOT aggregated)
										   AND bool_and(agg_task_id IS NULL);`)
	if err != nil {
		log.Errorf("getting deals to aggregate: %w", err)
		return
	}

	for _, deal := range deals {
		d.adders[pollerAggregate].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			n, err := tx.Exec(`UPDATE market_mk20_pipeline SET agg_task_id = $1 
                            		WHERE id = $2 
                            		  AND after_commp = TRUE 
                            		  AND NOT aggregated 
                            		  AND agg_task_id IS NULL`, id, deal.ID)
			if err != nil {
				return false, xerrors.Errorf("creating aggregation task for deal: %w", err)
			}
			return n == deal.Count, nil
		})
	}

}

func (d *CurioStorageDealMarket) processMK20DealIngestion(ctx context.Context) {

	head, err := d.api.ChainHead(ctx)
	if err != nil {
		log.Errorf("getting chain head: %w", err)
		return
	}

	var deals []struct {
		ID           string `db:"id"`
		SPID         int64  `db:"sp_id"`
		Client       string `db:"client"`
		PieceCID     string `db:"piece_cid"`
		PieceSize    int64  `db:"piece_size"`
		RawSize      int64  `db:"raw_size"`
		AllocationID *int64 `db:"allocation_id"`
		Duration     int64  `db:"duration"`
		Url          string `db:"url"`
		Count        int    `db:"unassigned_count"`
	}

	err = d.db.Select(ctx, &deals, `SELECT 
											  id,
											  MIN(sp_id) AS sp_id,
											  MIN(client) AS client,
											  MIN(piece_cid) AS piece_cid,
											  MIN(piece_size) AS piece_size,
											  MIN(raw_size) AS raw_size,
											  MIN(allocation_id) AS allocation_id,
											  MIN(duration) AS duration,
											  MIN(url) AS url,
											  COUNT(*) AS unassigned_count
											FROM market_mk20_pipeline
											WHERE aggregated = TRUE AND sector IS NULL
											GROUP BY id;`)
	if err != nil {
		log.Errorf("getting deals for ingestion: %w", err)
		return
	}

	for _, deal := range deals {
		if deal.Count != 1 {
			log.Errorf("unexpected count for deal: %s", deal.ID)
			continue
		}

		pcid, err := cid.Parse(deal.PieceCID)
		if err != nil {
			log.Errorw("failed to parse aggregate piece cid", "deal", deal, "error", err)
			continue
		}

		client, err := address.NewFromString(deal.Client)
		if err != nil {
			log.Errorw("failed to parse client address", "deal", deal, "error", err)
			continue
		}

		clientId, err := address.IDFromAddress(client)
		if err != nil {
			log.Errorw("failed to parse client id", "deal", deal, "error", err)
			continue
		}

		aurl, err := url.Parse(deal.Url)
		if err != nil {
			log.Errorf("failed to parse aggregate url: %w", err)
			continue
		}
		if aurl.Scheme != "pieceref" {
			log.Errorw("aggregate url is not a pieceref: %s", deal)
			continue
		}

		start := head.Height() + 2*builtin.EpochsInDay
		end := start + abi.ChainEpoch(deal.Duration)
		var vak *miner.VerifiedAllocationKey
		if deal.AllocationID != nil {
			alloc, err := d.api.StateGetAllocation(ctx, client, verifreg.AllocationId(*deal.AllocationID), types.EmptyTSK)
			if err != nil {
				log.Errorw("failed to get allocation", "deal", deal, "error", err)
				continue
			}
			if alloc == nil {
				log.Errorw("allocation not found", "deal", deal, "error", err)
				continue
			}
			if alloc.Expiration < start {
				log.Errorw("allocation expired", "deal", deal, "error", err)
				continue
			}
			end = start + alloc.TermMin
			vak = &miner.VerifiedAllocationKey{
				Client: abi.ActorID(clientId),
				ID:     verifreg13.AllocationId(*deal.AllocationID),
			}
		}

		// TODO: Attach notifications
		pdi := lpiece.PieceDealInfo{
			DealSchedule: lpiece.DealSchedule{
				StartEpoch: start,
				EndEpoch:   end,
			},
			PieceActivationManifest: &miner.PieceActivationManifest{
				CID:                   pcid,
				Size:                  abi.PaddedPieceSize(deal.PieceSize),
				VerifiedAllocationKey: vak,
			},
		}

		maddr, err := address.NewIDAddress(uint64(deal.SPID))
		if err != nil {
			log.Errorw("failed to parse miner address", "deal", deal, "error", err)
			continue
		}

		comm, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			sector, sp, err := d.pin.AllocatePieceToSector(ctx, tx, maddr, pdi, deal.RawSize, *aurl, nil)
			if err != nil {
				return false, xerrors.Errorf("failed to allocate piece to sector: %w", err)
			}
			n, err := tx.Exec(`UPDATE market_mk20_pipeline SET SET sector = $1, reg_seal_proof = $2 WHERE id = $3`, *sector, *sp, deal.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update deal: %w", err)
			}
			return n == 1, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			log.Errorf("failed to commit transaction: %w", err)
			continue
		}
		if comm {
			log.Infow("deal ingested successfully", "deal", deal)
		} else {
			log.Infow("deal not ingested", "deal", deal)
		}
	}
}
