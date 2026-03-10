package cachedreader

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/jellydator/ttlcache/v2"
	"github.com/oklog/ulid"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

var ErrNoDeal = errors.New("no deals found")

var log = logging.Logger("cached-reader")

const (
	MaxCachedReaders    = 512
	PieceReaderCacheTTL = 10 * time.Minute
	PieceErrorCacheTTL  = 5 * time.Second
)

type CachedPieceReader struct {
	db *harmonydb.DB

	sectorReader    *pieceprovider.SectorReader
	pieceParkReader *pieceprovider.PieceParkReader

	idxStor *indexstore.IndexStore

	pieceReaderCacheMu sync.Mutex
	pieceReaderCache   *pieceCidKeyCache // Cache for successful readers (10 minutes with TTL extension)
	pieceErrorCacheMu  sync.Mutex
	pieceErrorCache    *pieceCidKeyCache // Cache for errors (5 seconds without TTL extension)
}

func NewCachedPieceReader(db *harmonydb.DB, sectorReader *pieceprovider.SectorReader, pieceParkReader *pieceprovider.PieceParkReader, idxStor *indexstore.IndexStore) *CachedPieceReader {
	prCache := newPieceCidKeyCache(PieceReaderCacheTTL, MaxCachedReaders, false)  // Enable TTL extension for successful readers
	errorCache := newPieceCidKeyCache(PieceErrorCacheTTL, MaxCachedReaders, true) // Disable TTL extension for errors

	cpr := &CachedPieceReader{
		db:               db,
		sectorReader:     sectorReader,
		pieceParkReader:  pieceParkReader,
		pieceReaderCache: prCache,
		pieceErrorCache:  errorCache,
		idxStor:          idxStor,
	}

	expireCallback := func(key string, reason ttlcache.EvictionReason, value any) {
		log.Debugw("expire callback", "piececid", key, "reason", reason)

		// Record eviction metric
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(cacheTypeKey, "piece_reader"),
			tag.Upsert(reasonKey, reason.String()),
		}, CachedReaderMeasures.CacheEvictions.M(1))

		r := value.(*cachedSectionReader)

		cpr.pieceReaderCacheMu.Lock()
		defer cpr.pieceReaderCacheMu.Unlock()

		r.expired = true

		if r.refs <= 0 {
			r.cancel()
			if r.reader != nil {
				_ = r.reader.Close()
			}
			return
		}

		log.Debugw("expire callback with refs > 0", "refs", r.refs, "piececid", key, "reason", reason)
	}

	errorExpireCallback := func(key string, reason ttlcache.EvictionReason, value any) {
		log.Debugw("error cache expire callback", "piececid", key, "reason", reason)

		// Record eviction metric
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(cacheTypeKey, "piece_error"),
			tag.Upsert(reasonKey, reason.String()),
		}, CachedReaderMeasures.CacheEvictions.M(1))
	}

	prCache.SetExpirationReasonCallback(expireCallback)
	errorCache.SetExpirationReasonCallback(errorExpireCallback)

	return cpr
}

type cachedSectionReader struct {
	reader   storiface.Reader
	cpr      *CachedPieceReader
	pieceCid cid.Cid
	rawSize  uint64
	// Signals when the underlying piece reader is ready
	ready chan struct{}
	// err is non-nil if there's an error getting the underlying piece reader
	err error
	// cancel for underlying GetPieceReader call
	cancel  func()
	refs    int
	expired bool
}

// cachedError represents a cached error for piece reading
type cachedError struct {
	err      error
	pieceCid cid.Cid
}

func (r *cachedSectionReader) Close() error {
	r.cpr.pieceReaderCacheMu.Lock()
	defer r.cpr.pieceReaderCacheMu.Unlock()

	r.refs--

	// Record reference count metric
	stats.Record(context.Background(), CachedReaderMeasures.CacheRefs.M(int64(r.refs)))

	if r.refs == 0 && r.expired {
		log.Debugw("canceling underlying section reader context as cache entry doesn't exist", "piececid", r.pieceCid)

		r.cancel()
		if r.reader != nil {
			_ = r.reader.Close()
		}
	}

	return nil
}

func (cpr *CachedPieceReader) getPieceReaderFromMarketPieceDeal(ctx context.Context, piece cid.Cid, retrieval bool) (storiface.Reader, uint64, error) {
	/*
		Check if the requested CID is PieceCidV2 or not.
			- YES
				1. V2 is requested by the following:
					a. Mk20 retrievals
					b. MK20 indexing
					c. PDP v1 indexing
					d. PDP v1 retrieval
					e. PDP v0 retrieval
			- NO
				1. V1 is requested by the following:
					a. PDP v0 indexing (Needs to be served from piece park)
					b. MK12 retrieval (Needs to be served from sector)
	*/

	// Get all deals containing this piece
	pieceCid := piece
	var rawSize uint64
	var pieceSize abi.PaddedPieceSize

	if commcidv2.IsPieceCidV2(pieceCid) {
		var err error
		pieceCid, rawSize, err = commcid.PieceCidV1FromV2(pieceCid)
		if err != nil {
			return nil, 0, xerrors.Errorf("getting piece CID v1 from piece CID v2: %w", err)
		}
		pieceSize = padreader.PaddedSize(rawSize).Padded()
	} else {
		var pieceSizeRaw int64
		err := cpr.db.QueryRow(ctx, `SELECT COALESCE(
												(SELECT piece_size FROM market_piece_metadata WHERE piece_cid = $1 ORDER BY piece_size DESC LIMIT 1),
												(SELECT piece_padded_size FROM parked_pieces WHERE piece_cid = $1 ORDER BY piece_padded_size DESC LIMIT 1),
												0
											  )`, pieceCid.String()).Scan(&pieceSizeRaw)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get piece size for piece cid %s: %w", pieceCid, err)
		}
		if pieceSizeRaw <= 0 {
			return nil, 0, fmt.Errorf("failed to determine piece size for piece cid %s", pieceCid)
		}
		pieceSize = abi.PaddedPieceSize(pieceSizeRaw)
		rawSize = uint64(pieceSize.Unpadded())
	}

	var deals []struct {
		ID       string                  `db:"id"`
		SpID     int64                   `db:"sp_id"`
		Sector   int64                   `db:"sector_num"`
		Offset   sql.NullInt64           `db:"piece_offset"`
		Length   abi.PaddedPieceSize     `db:"piece_length"`
		RawSize  int64                   `db:"raw_size"`
		Proof    abi.RegisteredSealProof `db:"reg_seal_proof"`
		PieceRef sql.NullInt64           `db:"piece_ref"`
	}

	err := cpr.db.Select(ctx, &deals, `SELECT 
											  mpd.id,
											  mpd.sp_id,
											  mpd.sector_num,
											  mpd.piece_offset,
											  mpd.piece_length,
											  mpd.raw_size,
											  mpd.piece_ref,
											  COALESCE(sm.reg_seal_proof, 0::bigint) AS reg_seal_proof
											FROM market_piece_deal mpd
											LEFT JOIN sectors_meta sm
											  ON sm.sp_id = mpd.sp_id
											 AND sm.sector_num = mpd.sector_num
											WHERE mpd.piece_cid = $1
											  AND mpd.piece_length = $2;`, pieceCid.String(), pieceSize)
	if err != nil {
		return nil, 0, fmt.Errorf("getting piece deals: %w", err)
	}

	if len(deals) == 0 {
		if retrieval {
			var isPDP bool
			err = cpr.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pdp_piecerefs WHERE piece_cid = $1);`, pieceCid.String()).Scan(&isPDP)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to query pdp_piecerefs for piece cid %s: %w", pieceCid, err)
			}
			if !isPDP {
				return nil, 0, fmt.Errorf("piece cid %s: %w", pieceCid, ErrNoDeal)
			}
		}
		reader, rawSize, err := cpr.getPieceReaderFromPiecePark(ctx, nil, &pieceCid, &pieceSize)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read piece from piece park: %w", err)
		}
		return reader, rawSize, nil
	}

	// For each deal, try to read an unsealed copy of the data from the sector
	// it is stored in
	var merr error
	for _, dl := range deals {
		_, err := ulid.Parse(dl.ID)
		if err != nil {
			// This is likely a MK12 deal, get from sector
			sr := storiface.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(dl.SpID),
					Number: abi.SectorNumber(dl.Sector),
				},
				ProofType: dl.Proof,
			}

			reader, err := cpr.sectorReader.ReadPiece(ctx, sr, storiface.UnpaddedByteIndex(abi.PaddedPieceSize(dl.Offset.Int64).Unpadded()), dl.Length.Unpadded(), pieceCid)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("failed to read piece from sector: %w", err))
				continue
			}

			dealRawSize := uint64(dl.RawSize)
			if dealRawSize == 0 {
				if rawSize > 0 {
					dealRawSize = rawSize
				} else {
					dealRawSize = uint64(dl.Length.Unpadded())
				}
			}

			return reader, dealRawSize, nil
		}

		// MK20 deal: for piece_ref 0 or invalid (e.g. AggregateTypeV2 single piece in sector),
		// try sector read first; otherwise try piece park then sector.
		if (!dl.PieceRef.Valid || dl.PieceRef.Int64 == 0) && dl.Offset.Valid {
			// No piece park ref or ref=0: read directly from sector (e.g. V2 with tail index section).
			sr := storiface.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(dl.SpID),
					Number: abi.SectorNumber(dl.Sector),
				},
				ProofType: dl.Proof,
			}
			reader, err := cpr.sectorReader.ReadPiece(ctx, sr, storiface.UnpaddedByteIndex(abi.PaddedPieceSize(dl.Offset.Int64).Unpadded()), dl.Length.Unpadded(), pieceCid)
			if err == nil {
				return reader, uint64(dl.RawSize), nil
			}
			merr = multierror.Append(merr, xerrors.Errorf("failed to read piece from sector: %w", err))
		}

		// MK20 deal: try piece park when piece_ref is set and non-zero
		if dl.PieceRef.Valid && dl.PieceRef.Int64 != 0 {
			ref := dl.PieceRef.Int64
			reader, rawSize, err := cpr.getPieceReaderFromPiecePark(ctx, &ref, nil, nil)
			if err == nil {
				return reader, rawSize, nil
			}
			merr = multierror.Append(merr, xerrors.Errorf("failed to read piece from piece park: %w", err))
		}

		// MK20 deal: fallback to sector read (e.g. when piece park failed or piece_ref was 0 but Offset was missing earlier)
		if !dl.Offset.Valid {
			merr = multierror.Append(merr, xerrors.Errorf("MK20 deal has no piece_offset for sector read"))
			continue
		}
		sr := storiface.SectorRef{
			ID: abi.SectorID{
				Miner:  abi.ActorID(dl.SpID),
				Number: abi.SectorNumber(dl.Sector),
			},
			ProofType: dl.Proof,
		}
		reader, err := cpr.sectorReader.ReadPiece(ctx, sr, storiface.UnpaddedByteIndex(abi.PaddedPieceSize(dl.Offset.Int64).Unpadded()), dl.Length.Unpadded(), pieceCid)
		if err != nil {
			merr = multierror.Append(merr, xerrors.Errorf("failed to read piece from sector: %w", err))
			continue
		}
		return reader, uint64(dl.RawSize), nil
	}

	return nil, 0, merr
}

// GetPieceReaderByRef returns a reader for the piece stored in the piece park under the given ref_id (pieceref:N).
// Used by indexing when the task has url=pieceref:N so we can read the piece before market_piece_deal is populated.
func (cpr *CachedPieceReader) GetPieceReaderByRef(ctx context.Context, pieceRef int64) (storiface.Reader, uint64, error) {
	ref := pieceRef
	return cpr.getPieceReaderFromPiecePark(ctx, &ref, nil, nil)
}

func (cpr *CachedPieceReader) getPieceReaderFromPiecePark(ctx context.Context, pieceRef *int64, pieceCid *cid.Cid, pieceSize *abi.PaddedPieceSize) (storiface.Reader, uint64, error) {
	type pieceData struct {
		ID           int64  `db:"id"`
		PieceCid     string `db:"piece_cid"`
		PieceRawSize int64  `db:"piece_raw_size"`
	}

	var pd []pieceData

	if pieceRef != nil {
		var pdr []pieceData
		// Do not require long_term = TRUE: Mk20 SourceAggregate (e.g. exa-gateway) inserts with long_term = FALSE.
		err := cpr.db.Select(ctx, &pdr, `
										SELECT
										  pp.id,
										  pp.piece_cid,
										  pp.piece_raw_size
										FROM parked_piece_refs pr
										JOIN parked_pieces     pp ON pp.id = pr.piece_id
										WHERE pr.ref_id = $1 AND pp.complete = TRUE;
    `, *pieceRef)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to query parked_pieces and parked_piece_refs for piece_ref %d: %w", *pieceRef, err)
		}
		if len(pdr) > 0 {
			pd = append(pd, pdr...)
		} else {
			// Diagnostic: see why ref wasn't found (ref missing vs piece not complete)
			var diag []struct {
				RefID    int64  `db:"ref_id"`
				PieceID  int64  `db:"piece_id"`
				Complete bool   `db:"complete"`
				PieceCID string `db:"piece_cid"`
			}
			_ = cpr.db.Select(ctx, &diag, `
				SELECT pr.ref_id, pr.piece_id, pp.complete, pp.piece_cid
				FROM parked_piece_refs pr
				LEFT JOIN parked_pieces pp ON pp.id = pr.piece_id
				WHERE pr.ref_id = $1`, *pieceRef)
			if len(diag) > 0 {
				d := diag[0]
				if !d.Complete {
					return nil, 0, fmt.Errorf("piece_ref %d exists but piece id=%d is not complete (ParkPiece may still be running or failed); piece_cid=%s",
						*pieceRef, d.PieceID, d.PieceCID)
				}
				// complete=true but we didn't get it above — shouldn't happen
			}
		}
	}

	if pieceCid != nil && pieceSize != nil {
		pcid := *pieceCid
		var pdc []pieceData
		err := cpr.db.Select(ctx, &pdc, `
										SELECT
										  id,
										  piece_cid,
										  piece_raw_size
										FROM parked_pieces
										WHERE piece_cid = $1 AND piece_padded_size = $2;`, pcid.String(), *pieceSize)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to query parked_pieces by piece_cid: %w", err)
		}
		if len(pdc) > 0 {
			pd = append(pd, pdc...)
		}
	}

	if len(pd) == 0 {
		if pieceRef != nil {
			return nil, 0, fmt.Errorf("failed to find piece in parked_pieces for piece_ref %d (ref_id not in parked_piece_refs or piece was removed; check DB and that indexing runs after ParkPiece completes)", *pieceRef)
		}
		if pieceCid != nil {
			return nil, 0, fmt.Errorf("failed to find piece in parked_pieces for piece_cid %s", pieceCid.String())
		}
		return nil, 0, fmt.Errorf("failed to find piece in parked_pieces")
	}

	pcid, err := cid.Parse(pd[0].PieceCid)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse piece cid: %w", err)
	}

	reader, err := cpr.pieceParkReader.ReadPiece(ctx, storiface.PieceNumber(pd[0].ID), pd[0].PieceRawSize, pcid)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read piece from piece park: %w", err)
	}

	return reader, uint64(pd[0].PieceRawSize), nil
}

type SubPieceReader struct {
	sr *io.SectionReader
	r  io.Closer
}

func (s SubPieceReader) Read(p []byte) (n int, err error) {
	return s.sr.Read(p)
}

func (s SubPieceReader) Close() error {
	return s.r.Close()
}

func (s SubPieceReader) Seek(offset int64, whence int) (int64, error) {
	return s.sr.Seek(offset, whence)
}

func (s SubPieceReader) ReadAt(p []byte, off int64) (n int, err error) {
	return s.sr.ReadAt(p, off)
}

func (cpr *CachedPieceReader) getPieceReaderFromAggregate(ctx context.Context, pieceCidV2 cid.Cid, retrieval bool) (storiface.Reader, uint64, error) {
	// Aggregate is a MK20 exclusive concept. The requesting pieceCID must be v2

	pieces, err := cpr.idxStor.FindPieceInAggregate(ctx, pieceCidV2)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find piece in aggregate: %w", err)
	}

	if len(pieces) == 0 {
		return nil, 0, fmt.Errorf("subpiece %s not found in any aggregate piece", pieceCidV2.String())
	}

	_, rawSize, err := commcid.PieceCidV1FromV2(pieceCidV2)
	if err != nil {
		return nil, 0, xerrors.Errorf("getting piece commitment from piece CID v2: %w", err)
	}

	var merr error

	for _, p := range pieces {
		reader, _, err := cpr.getPieceReaderFromMarketPieceDeal(ctx, p.Cid, retrieval)
		if err != nil {
			merr = multierror.Append(merr, err)
			continue
		}

		sr := io.NewSectionReader(reader, int64(p.Offset), int64(p.Size))
		return SubPieceReader{r: reader, sr: sr}, rawSize, nil
	}

	return nil, 0, fmt.Errorf("failed to find piece in aggregate: %w", merr)
}

func (cpr *CachedPieceReader) GetSharedPieceReader(ctx context.Context, pieceCidV2 cid.Cid, retrieval bool) (storiface.Reader, uint64, error) {
	yes := commcidv2.IsPieceCidV2(pieceCidV2)
	if !yes {
		// When input is v1: try index first (e.g. CidMapping v1 or piece v1 if ever indexed). Only fall back to market_piece_metadata when index has no match.
		pieces, err := cpr.idxStor.FindPieceInAggregate(ctx, pieceCidV2)
		if err == nil && len(pieces) > 0 {
			for _, p := range pieces {
				reader, _, err := cpr.getPieceReaderFromMarketPieceDeal(ctx, p.Cid, retrieval)
				if err != nil {
					continue
				}
				sr := io.NewSectionReader(reader, int64(p.Offset), int64(p.Size))
				return SubPieceReader{r: reader, sr: sr}, p.Size, nil
			}
		}
		// Not found in index: resolve v1 via market_piece_metadata and convert to v2
		var rawSize int64
		var singlePiece bool
		err = cpr.db.QueryRow(ctx, `WITH meta AS (
											  SELECT piece_size
											  FROM market_piece_metadata
											  WHERE piece_cid = $1
											),
											exact AS (
											  SELECT COUNT(*) AS n, MIN(piece_size) AS piece_size
											  FROM meta
											),
											raw AS (
											  SELECT MAX(mpd.raw_size) AS raw_size
											  FROM market_piece_deal mpd
											  WHERE mpd.piece_cid   = $1
												AND mpd.piece_length = (SELECT piece_size FROM exact)
												AND (SELECT n FROM exact) = 1
											)
											SELECT
											  COALESCE((SELECT raw_size FROM raw), 0)        AS raw_size,
											  ((SELECT n FROM exact) = 1)                    AS has_single_metadata;`, pieceCidV2.String()).Scan(&rawSize, &singlePiece)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get piece metadata: %w", err)
		}
		if !singlePiece {
			return nil, 0, fmt.Errorf("more than 1 piece metadata found for piece cid %s, please use piece cid v2", pieceCidV2.String())
		}
		pcid2, err := commcid.PieceCidV2FromV1(pieceCidV2, uint64(rawSize))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to convert piece cid v1 to v2: %w", err)
		}
		pieceCidV2 = pcid2
	}

	pieceCid, _, err := commcid.PieceCidV1FromV2(pieceCidV2)
	if err != nil {
		return nil, 0, xerrors.Errorf("getting piece CID v1 from piece CID v2: %w", err)
	}

	// First check if we have a cached error for this piece
	cpr.pieceErrorCacheMu.Lock()
	if errorItem, found := cpr.pieceErrorCache.Get(pieceCid); found {
		cachedErr := errorItem.(*cachedError)
		cpr.pieceErrorCacheMu.Unlock()
		log.Debugw("returning cached error", "piececid", pieceCid, "err", cachedErr.err)

		// Record cache hit for error cache
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(cacheTypeKey, "piece_error"),
		}, CachedReaderMeasures.CacheHits.M(1))

		return nil, 0, cachedErr.err
	}
	cpr.pieceErrorCacheMu.Unlock()

	var r *cachedSectionReader

	// Check if there is already a piece reader in the cache
	cpr.pieceReaderCacheMu.Lock()
	rr, found := cpr.pieceReaderCache.Get(pieceCid)
	if !found {
		// Cache miss - there is not yet a cached piece reader
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(cacheTypeKey, "piece_reader"),
		}, CachedReaderMeasures.CacheMisses.M(1))

		// Create a new one and add it to the cache
		r = &cachedSectionReader{
			cpr:      cpr,
			pieceCid: pieceCid,
			ready:    make(chan struct{}),
			refs:     1,
		}
		cpr.pieceReaderCache.Set(pieceCid, r)

		// Record cache size
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(cacheTypeKey, "piece_reader"),
		}, CachedReaderMeasures.CacheSize.M(int64(cpr.pieceReaderCache.Count())))

		cpr.pieceReaderCacheMu.Unlock()

		// We just added a cached reader, so get its underlying piece reader
		readerCtx, readerCtxCancel := context.WithCancel(context.Background())
		defer close(r.ready)

		reader, size, err := cpr.getPieceReaderFromAggregate(readerCtx, pieceCid, retrieval)
		if err != nil {
			log.Debugw("failed to get piece reader from aggregate", "piececid", pieceCid.String(), "err", err)

			aerr := err

			reader, size, err = cpr.getPieceReaderFromMarketPieceDeal(readerCtx, pieceCid, retrieval)
			if err != nil {
				log.Debugw("failed to get piece reader", "piececid", pieceCid, "err", err)
				finalErr := fmt.Errorf("failed to get piece reader from aggregate, sector or piece park: %w, %w", aerr, err)

				// Record error metric
				_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
					tag.Upsert(reasonKey, "piece_not_found"),
				}, CachedReaderMeasures.ReaderErrors.M(1))

				// Cache the error in the error cache
				cpr.pieceErrorCacheMu.Lock()
				cpr.pieceErrorCache.Set(pieceCid, &cachedError{err: finalErr, pieceCid: pieceCid})
				// Record error cache size
				_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
					tag.Upsert(cacheTypeKey, "piece_error"),
				}, CachedReaderMeasures.CacheSize.M(int64(cpr.pieceErrorCache.Count())))
				cpr.pieceErrorCacheMu.Unlock()

				// Remove the failed reader from the main cache
				cpr.pieceReaderCacheMu.Lock()
				cpr.pieceReaderCache.Remove(pieceCid)
				// Record updated cache size
				_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
					tag.Upsert(cacheTypeKey, "piece_reader"),
				}, CachedReaderMeasures.CacheSize.M(int64(cpr.pieceReaderCache.Count())))
				cpr.pieceReaderCacheMu.Unlock()

				r.err = finalErr
				readerCtxCancel()

				return nil, 0, finalErr
			}

		}

		// Record successful reader creation
		stats.Record(context.Background(), CachedReaderMeasures.ReaderSuccesses.M(1))

		r.reader = reader
		r.err = nil
		r.cancel = readerCtxCancel
		r.rawSize = size
	} else {
		// Cache hit - we already have a cached reader
		_ = stats.RecordWithTags(context.Background(), []tag.Mutator{
			tag.Upsert(cacheTypeKey, "piece_reader"),
		}, CachedReaderMeasures.CacheHits.M(1))

		r = rr.(*cachedSectionReader)
		r.refs++

		// Record reference count metric
		stats.Record(context.Background(), CachedReaderMeasures.CacheRefs.M(int64(r.refs)))

		cpr.pieceReaderCacheMu.Unlock()

		// We already had a cached reader, wait for it to be ready
		select {
		case <-ctx.Done():
			// The context timed out. Dereference the cached piece reader and
			// return an error.
			_ = r.Close()
			return nil, 0, ctx.Err()
		case <-r.ready:
		}
	}

	// If there was an error getting the underlying piece reader, make sure
	// that the cached reader gets cleaned up
	if r.err != nil {
		_ = r.Close()
		return nil, 0, r.err
	}

	rs := io.NewSectionReader(r.reader, 0, int64(r.rawSize))

	return struct {
		io.Closer
		io.Reader
		io.ReaderAt
		io.Seeker
	}{
		Closer:   r,
		Reader:   rs,
		Seeker:   rs,
		ReaderAt: r.reader,
	}, r.rawSize, nil
}
