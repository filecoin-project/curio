package mk20

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/oklog/ulid"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/miner"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"
	verifreg9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/paths"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("mk20")

type MK20API interface {
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifreg9.AllocationId, tsk types.TipSetKey) (*verifreg9.Allocation, error)
}

type MK20 struct {
	miners             []address.Address
	DB                 *harmonydb.DB
	api                MK20API
	ethClient          *ethclient.Client
	si                 paths.SectorIndex
	cfg                *config.CurioConfig
	sm                 map[address.Address]abi.SectorSize
	as                 *multictladdr.MultiAddressSelector
	sc                 *ffi.SealCalls
	maxParallelUploads *atomic.Int64
	unknowClient       bool
}

func NewMK20Handler(miners []address.Address, db *harmonydb.DB, si paths.SectorIndex, mapi MK20API, ethClient *ethclient.Client, cfg *config.CurioConfig, as *multictladdr.MultiAddressSelector, sc *ffi.SealCalls) (*MK20, error) {
	ctx := context.Background()

	// Ensure MinChunk size and max chunkSize is a power of 2
	if cfg.Market.StorageMarketConfig.MK20.MinimumChunkSize&(cfg.Market.StorageMarketConfig.MK20.MinimumChunkSize-1) != 0 {
		return nil, xerrors.Errorf("MinimumChunkSize must be a power of 2")
	}

	if cfg.Market.StorageMarketConfig.MK20.MaximumChunkSize&(cfg.Market.StorageMarketConfig.MK20.MaximumChunkSize-1) != 0 {
		return nil, xerrors.Errorf("MaximumChunkSize must be a power of 2")
	}

	sm := make(map[address.Address]abi.SectorSize)

	for _, m := range miners {
		info, err := mapi.StateMinerInfo(ctx, m, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting miner info: %w", err)
		}
		if _, ok := sm[m]; !ok {
			sm[m] = info.SectorSize
		}
	}

	go markDownloaded(ctx, db)

	return &MK20{
		miners:             miners,
		DB:                 db,
		api:                mapi,
		ethClient:          ethClient,
		si:                 si,
		cfg:                cfg,
		sm:                 sm,
		as:                 as,
		sc:                 sc,
		maxParallelUploads: new(atomic.Int64),
		unknowClient:       !cfg.Market.StorageMarketConfig.MK20.DenyUnknownClients,
	}, nil
}

// ExecuteDeal take a *Deal  and returns ProviderDealRejectionInfo which has ErrorCode and Reason
// @param deal *Deal
// @Return DealCode
// @Return Reason string

func (m *MK20) ExecuteDeal(ctx context.Context, deal *Deal) *ProviderDealRejectionInfo {
	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1<<16)
			n := runtime.Stack(trace, false)
			log.Errorf("panic occurred: %v\n%s", r, trace[:n])
			debug.PrintStack()
		}
	}()

	// Validate the DataSource
	code, err := deal.Validate(m.DB, &m.cfg.Market.StorageMarketConfig.MK20)
	if err != nil {
		log.Errorw("deal rejected", "deal", deal, "error", err)
		ret := &ProviderDealRejectionInfo{
			HTTPCode: code,
		}
		if code == ErrServerInternalError {
			ret.Reason = "Internal server error"
		} else {
			ret.Reason = err.Error()
		}
		return ret
	}

	log.Debugw("deal validated", "deal", deal.Identifier.String())

	if deal.Products.DDOV1 != nil {
		return m.processDDODeal(ctx, deal)
	}

	return m.processPDPDeal(ctx, deal)
}

func (m *MK20) processDDODeal(ctx context.Context, deal *Deal) *ProviderDealRejectionInfo {
	rejection, err := m.sanitizeDDODeal(ctx, deal)
	if err != nil {
		log.Errorw("deal rejected", "deal", deal, "error", err)
		return rejection
	}

	log.Debugw("deal sanitized", "deal", deal.Identifier.String())

	if rejection != nil {
		return rejection
	}

	id, code, err := deal.Products.DDOV1.GetDealID(ctx, m.DB, m.ethClient)
	if err != nil {
		log.Errorw("error getting deal ID", "deal", deal, "error", err)
		ret := &ProviderDealRejectionInfo{
			HTTPCode: code,
		}
		if code == ErrServerInternalError {
			ret.Reason = "Internal server error"
		} else {
			ret.Reason = err.Error()
		}
		return ret
	}

	log.Debugw("deal ID found", "deal", deal.Identifier.String(), "id", id)

	// TODO: Backpressure, client filter

	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = deal.SaveToDB(tx)
		if err != nil {
			return false, err
		}
		n, err := tx.Exec(`UPDATE market_mk20_deal
								SET ddo_v1 = jsonb_set(ddo_v1, '{deal_id}', to_jsonb($1::text))
								WHERE id = $2;`, id, deal.Identifier.String())
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, fmt.Errorf("expected 1 row to be updated, got %d", n)
		}

		if deal.Data.SourceHttpPut == nil {
			_, err = tx.Exec(`INSERT INTO market_mk20_pipeline_waiting (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, deal.Identifier.String())
		}

		if err != nil {
			return false, xerrors.Errorf("adding deal to waiting pipeline: %w", err)
		}
		return true, nil
	})

	if err != nil {
		log.Errorw("error inserting deal into DB", "deal", deal, "error", err)
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrServerInternalError,
		}
	}

	if !comm {
		log.Errorw("error committing deal into DB", "deal", deal)
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrServerInternalError,
		}
	}

	log.Debugw("deal inserted in DB", "deal", deal.Identifier.String())

	return &ProviderDealRejectionInfo{
		HTTPCode: http.StatusOK,
	}
}

func (m *MK20) sanitizeDDODeal(ctx context.Context, deal *Deal) (*ProviderDealRejectionInfo, error) {
	if !lo.Contains(m.miners, deal.Products.DDOV1.Provider) {
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrBadProposal,
			Reason:   "Provider not available in Curio cluster",
		}, nil
	}

	if deal.Data != nil {
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrBadProposal,
			Reason:   "Data Source must be defined for a DDO deal",
		}, nil
	}

	size, err := deal.Size()
	if err != nil {
		log.Errorw("error getting deal size", "deal", deal, "error", err)
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrBadProposal,
			Reason:   "Error getting deal size from PieceCID",
		}, nil
	}

	if size > abi.PaddedPieceSize(m.sm[deal.Products.DDOV1.Provider]) {
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrBadProposal,
			Reason:   "Deal size is larger than the miner's sector size",
		}, nil
	}

	if deal.Data.Format.Raw != nil {
		if deal.Products.RetrievalV1 != nil {
			if deal.Products.RetrievalV1.Indexing {
				return &ProviderDealRejectionInfo{
					HTTPCode: ErrBadProposal,
					Reason:   "Raw bytes deal cannot be indexed",
				}, nil
			}
		}
	}

	if deal.Products.DDOV1.AllocationId != nil {
		if size < abi.PaddedPieceSize(verifreg.MinimumVerifiedAllocationSize) {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Verified piece size must be at least 1MB",
			}, nil
		}

		alloc, err := m.api.StateGetAllocation(ctx, deal.Client, verifreg9.AllocationId(*deal.Products.DDOV1.AllocationId), types.EmptyTSK)
		if err != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrServerInternalError,
			}, xerrors.Errorf("getting allocation: %w", err)
		}

		if alloc == nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Verified piece must have a valid allocation ID",
			}, nil
		}

		clientID, err := address.IDFromAddress(deal.Client)
		if err != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Invalid client address",
			}, nil
		}

		if alloc.Client != abi.ActorID(clientID) {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "client address does not match the allocation client address",
			}, nil
		}

		prov, err := address.NewIDAddress(uint64(alloc.Provider))
		if err != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrServerInternalError,
			}, xerrors.Errorf("getting provider address: %w", err)
		}

		if !lo.Contains(m.miners, prov) {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Allocation provider does not belong to the list of miners in Curio cluster",
			}, nil
		}

		if !deal.Data.PieceCID.Equals(alloc.Data) {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Allocation data CID does not match the piece CID",
			}, nil
		}

		if size != alloc.Size {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Allocation size does not match the piece size",
			}, nil
		}

		if alloc.TermMin > miner.MaxSectorExpirationExtension-policy.SealRandomnessLookback {
			return &ProviderDealRejectionInfo{
				HTTPCode: ErrBadProposal,
				Reason:   "Allocation term min is greater than the maximum sector expiration extension",
			}, nil
		}
	}

	return nil, nil
}

func (m *MK20) processPDPDeal(ctx context.Context, deal *Deal) *ProviderDealRejectionInfo {
	rejection, err := m.sanitizePDPDeal(ctx, deal)
	if err != nil {
		log.Errorw("PDP deal rejected", "deal", deal, "error", err)
		return rejection
	}

	log.Debugw("PDP deal sanitized", "deal", deal.Identifier.String())

	if rejection != nil {
		return rejection
	}

	// Save deal to DB and start pipeline if required
	comm, err := m.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Save deal
		err = deal.SaveToDB(tx)
		if err != nil {
			return false, xerrors.Errorf("saving deal to DB: %w", err)
		}

		// If we have data source other that PUT then start the pipeline
		if deal.Data != nil {
			if deal.Data.SourceHttpPut != nil || deal.Data.SourceAggregate != nil {
				err = insertPDPPipeline(ctx, tx, deal)
				if err != nil {
					return false, xerrors.Errorf("inserting pipeline: %w", err)
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		log.Errorw("error inserting PDP deal into DB", "deal", deal, "error", err)
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrServerInternalError,
		}
	}
	if !comm {
		log.Errorw("error committing PDP deal into DB", "deal", deal)
		return &ProviderDealRejectionInfo{
			HTTPCode: ErrServerInternalError,
		}
	}
	log.Debugw("PDP deal inserted in DB", "deal", deal.Identifier.String())
	return nil
}

func (m *MK20) sanitizePDPDeal(ctx context.Context, deal *Deal) (*ProviderDealRejectionInfo, error) {
	if deal.Data != nil {
		if deal.Data.SourceOffline != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Offline data source is not supported for pdp_v1",
			}, nil
		}
	}
	return nil, nil
}

func insertPDPPipeline(ctx context.Context, tx *harmonydb.Tx, deal *Deal) error {
	pdp := deal.Products.PDPV1
	data := deal.Data
	dealID := deal.Identifier.String()
	pi, err := deal.PieceInfo()
	if err != nil {
		return fmt.Errorf("getting piece info: %w", err)
	}

	aggregation := 0
	if data.Format.Aggregate != nil {
		aggregation = int(data.Format.Aggregate.Type)
	}

	// Insert pipeline when Data source is HTTP
	if data.SourceHTTP != nil {
		var pieceID int64
		// Attempt to select the piece ID first
		err = tx.QueryRow(`SELECT id FROM parked_pieces WHERE piece_cid = $1 AND piece_padded_size = $2`, pi.PieceCIDV1.String(), pi.Size).Scan(&pieceID)

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Piece does not exist, attempt to insert
				err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
							VALUES ($1, $2, $3, TRUE)
							ON CONFLICT (piece_cid, piece_padded_size, long_term, cleanup_task_id) DO NOTHING
							RETURNING id`, pi.PieceCIDV1.String(), pi.Size, pi.RawSize).Scan(&pieceID)
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

		n, err := tx.Exec(`INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, product, ref_ids) VALUES ($1, $2, $3, $4, $5)`,
			dealID, pi.PieceCIDV1.String(), pi.Size, ProductNamePDPV1, refIds)
		if err != nil {
			return xerrors.Errorf("inserting PDP download pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("inserting PDP download pipeline: %d rows affected", n)
		}

		n, err = tx.Exec(`INSERT INTO pdp_pipeline (
            id, client, piece_cid_v2, piece_cid, piece_size, raw_size, proof_set_id,
            extra_data, deal_aggregation) 
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			dealID, deal.Client.String(), data.PieceCID.String(), pi.PieceCIDV1.String(), pi.Size, pi.RawSize, *pdp.ProofSetID,
			pdp.ExtraData, aggregation)
		if err != nil {
			return xerrors.Errorf("inserting PDP pipeline: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("inserting PDP pipeline: %d rows affected", n)
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
			RawSize  uint64
		}
		toDownload := make(map[downloadkey][]HttpUrl)

		for _, piece := range deal.Data.SourceAggregate.Pieces {
			spi, err := GetPieceInfo(piece.PieceCID)
			if err != nil {
				return xerrors.Errorf("getting piece info: %w", err)
			}
			if piece.SourceHTTP != nil {
				urls, ok := toDownload[downloadkey{ID: dealID, PieceCID: spi.PieceCIDV1, Size: spi.Size, RawSize: spi.RawSize}]
				if ok {
					toDownload[downloadkey{ID: dealID, PieceCID: spi.PieceCIDV1, Size: spi.Size}] = append(urls, piece.SourceHTTP.URLs...)
				} else {
					toDownload[downloadkey{ID: dealID, PieceCID: spi.PieceCIDV1, Size: spi.Size, RawSize: spi.RawSize}] = piece.SourceHTTP.URLs
				}
			}
		}

		batch := &pgx.Batch{}
		batchSize := 5000

		for k, v := range toDownload {
			for _, src := range v {
				headers, err := json.Marshal(src.Headers)
				if err != nil {
					return xerrors.Errorf("marshal headers: %w", err)
				}
				batch.Queue(`WITH inserted_piece AS (
									  INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
									  VALUES ($1, $2, $3, FALSE)
									  ON CONFLICT (piece_cid, piece_padded_size, long_term, cleanup_task_id) DO NOTHING
									  RETURNING id
									),
									selected_piece AS (
									  SELECT COALESCE(
										(SELECT id FROM inserted_piece),
										(SELECT id FROM parked_pieces
										 WHERE piece_cid = $1 AND piece_padded_size = $2 AND long_term = FALSE AND cleanup_task_id IS NULL)
									  ) AS id
									),
									inserted_ref AS (
									  INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
									  SELECT id, $4, $5, FALSE FROM selected_piece
									  RETURNING ref_id
									)
									INSERT INTO market_mk20_download_pipeline (id, piece_cid, piece_size, product, ref_ids)
									VALUES ($6, $1, $2, $7, ARRAY[(SELECT ref_id FROM inserted_ref)])
									ON CONFLICT (id, piece_cid, piece_size, product) DO UPDATE
									SET ref_ids = array_append(
									  market_mk20_download_pipeline.ref_ids,
									  (SELECT ref_id FROM inserted_ref)
									)
									WHERE NOT market_mk20_download_pipeline.ref_ids @> ARRAY[(SELECT ref_id FROM inserted_ref)];`,
					k.PieceCID.String(), k.Size, k.RawSize, src.URL, headers, k.ID, ProductNamePDPV1)
			}

			if batch.Len() > batchSize {
				res := tx.SendBatch(ctx, batch)
				if err := res.Close(); err != nil {
					return xerrors.Errorf("closing parked piece query batch: %w", err)
				}
				batch = &pgx.Batch{}
			}
		}

		if batch.Len() > 0 {
			res := tx.SendBatch(ctx, batch)
			if err := res.Close(); err != nil {
				return xerrors.Errorf("closing parked piece query batch: %w", err)
			}
		}

		pBatch := &pgx.Batch{}
		pBatchSize := 4000
		for i, piece := range deal.Data.SourceAggregate.Pieces {
			spi, err := GetPieceInfo(piece.PieceCID)
			if err != nil {
				return xerrors.Errorf("getting piece info: %w", err)
			}
			pBatch.Queue(`INSERT INTO pdp_pipeline (
                          id, client, piece_cid_v2, piece_cid, piece_size, raw_size, 
                          proof_set_id, extra_data, piece_ref, deal_aggregation, aggr_index) 
        	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				dealID, deal.Client.String(), piece.PieceCID.String(), spi.PieceCIDV1.String(), spi.Size, spi.RawSize,
				pdp.ExtraData, *pdp.ProofSetID, aggregation, i)
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

func markDownloaded(ctx context.Context, db *harmonydb.DB) {
	md := func(ctx context.Context, db *harmonydb.DB) {
		var deals []struct {
			ID        string `db:"id"`
			PieceCID  string `db:"piece_cid"`
			PieceSize int64  `db:"piece_size"`
		}

		err := db.Select(ctx, &deals, `SELECT id, piece_cid, piece_size FROM pdp_pipeline WHERE piece_ref IS NULL`)
		if err != nil {
			log.Errorw("error getting PDP deals", "error", err)
		}

		for _, deal := range deals {
			_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
				var refid int64
				err = tx.QueryRow(`SELECT u.ref_id FROM (
									  SELECT unnest(dp.ref_ids) AS ref_id
									  FROM market_mk20_download_pipeline dp
									  WHERE dp.id = $1 AND dp.piece_cid = $2 AND dp.piece_size = $3 AND dp.product = $4
									) u
									JOIN parked_piece_refs pr ON pr.ref_id = u.ref_id
									JOIN parked_pieces pp ON pp.id = pr.piece_id
									WHERE pp.complete = TRUE
									LIMIT 1;`, deal.ID, deal.PieceCID, deal.PieceSize, ProductNamePDPV1).Scan(&refid)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return false, nil
					}
					return false, xerrors.Errorf("failed to check if the piece is downloaded: %w", err)
				}

				// Remove other ref_ids from piece_park_refs
				_, err = tx.Exec(`DELETE FROM parked_piece_refs
								WHERE ref_id IN (
								  SELECT unnest(dp.ref_ids)
								  FROM market_mk20_download_pipeline dp
								  WHERE dp.id = $1 AND dp.piece_cid = $2 AND dp.piece_size = $3 AND dp.product = $4
								)
								AND ref_id != $5;`, deal.ID, deal.PieceCID, deal.PieceSize, ProductNamePDPV1, refid)
				if err != nil {
					return false, xerrors.Errorf("failed to remove other ref_ids from piece_park_refs: %w", err)
				}

				_, err = tx.Exec(`DELETE FROM market_mk20_download_pipeline WHERE id = $1 AND piece_cid = $2 AND piece_size = $3 AND product = $4;`,
					deal.ID, deal.PieceCID, deal.PieceSize, ProductNamePDPV1)
				if err != nil {
					return false, xerrors.Errorf("failed to delete piece from download table: %w", err)
				}

				_, err = tx.Exec(`UPDATE pdp_pipeline SET downloaded = TRUE, piece_ref = $1 
                                   WHERE id = $2
                                   AND piece_cid = $3
                                   AND piece_size = $4`,
					refid, deal.ID, deal.PieceCID, deal.PieceSize)
				if err != nil {
					return false, xerrors.Errorf("failed to update download statos for PDP pipeline: %w", err)
				}
				return true, nil
			}, harmonydb.OptionRetry())
			if err != nil {
				log.Errorw("error updating PDP deal", "deal", deal, "error", err)
			}
		}
	}

	ticker := time.NewTicker(time.Second * 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			md(ctx, db)
		case <-ctx.Done():
			return
		}
	}
}

// UpdateDeal updates the details of a deal specified by its ID and writes the result or error to the provided HTTP response writer.
// @param id ulid.ULID
// @param deal *Deal
// @Return DealCode

func (m *MK20) UpdateDeal(id ulid.ULID, deal *Deal, w http.ResponseWriter) {
	if deal == nil {
		http.Error(w, "deal not defined", int(ErrBadProposal))
		return
	}

	ctx := context.Background()

	allowed, err := AuthenticateClient(m.DB, deal.Identifier.String(), deal.Client.String())
	if err != nil {
		log.Errorw("deal rejected", "deal", deal, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}
	if !allowed {
		log.Errorw("deal rejected as client is not authorized", "deal", deal)
		http.Error(w, "client not authorized", int(ErrUnAuthorized))
		return
	}

	var exists bool
	err = m.DB.QueryRow(ctx, `SELECT EXISTS (
								  SELECT 1
								  FROM market_mk20_deal
								  WHERE id = $1)`, id.String()).Scan(&exists)
	if err != nil {
		log.Errorw("failed to check if deal exists", "deal", id, "error", err)
		http.Error(w, "", int(ErrServerInternalError))
		return
	}

	if !exists {
		http.Error(w, "", int(ErrDealNotFound))
		return
	}

	code, nd, np, err := m.updateDealDetails(id, deal)
	if err != nil {
		log.Errorw("failed to update deal details", "deal", id, "error", err)
		if code == ErrServerInternalError {
			http.Error(w, "", int(ErrServerInternalError))
		} else {
			http.Error(w, err.Error(), int(code))
		}
		return
	}

	// Initiate new pipelines for DDO if required
	for _, p := range np {
		if p == ProductNameDDOV1 {
			m.processDDODeal(ctx, nd)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// To be used later for when data source is minerID
//func validateMinerAddresses(madrs []abi.Multiaddrs, pcid cid.Cid, psize abi.PaddedPieceSize, rawSize int64) bool {
//	var surls []*url.URL
//	for _, adr := range madrs {
//		surl, err := maurl.ToURL(multiaddr.Cast(adr))
//		if err != nil {
//			continue
//		}
//		surls = append(surls, surl)
//	}
//
//	var validUrls []*url.URL
//
//	for _, surl := range surls {
//		if surl.Scheme == "ws" {
//			surl.Scheme = "http"
//		}
//
//		if surl.Scheme == "wss" {
//			surl.Scheme = "https"
//		}
//
//		if surl.Port() == "443" {
//			surl.Host = surl.Hostname()
//		}
//
//		if surl.Port() == "80" {
//			surl.Host = surl.Hostname()
//		}
//
//		resp, err := http.Head(surl.String() + "/piece/" + pcid.String())
//		if err != nil {
//			continue
//		}
//		if resp.StatusCode != 200 {
//			continue
//		}
//
//		if resp.Header.Get("Content-Length") != fmt.Sprint(psize) {
//			continue
//		}
//
//		validUrls = append(validUrls, surl)
//	}
//	return len(validUrls) > 0
//}
