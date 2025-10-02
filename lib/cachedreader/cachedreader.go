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
	"golang.org/x/xerrors"

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
	MaxCachedReaders    = 128
	PieceReaderCacheTTL = 10 * time.Minute
	PieceErrorCacheTTL  = 5 * time.Second
)

type CachedPieceReader struct {
	db *harmonydb.DB

	sectorReader    *pieceprovider.SectorReader
	pieceParkReader *pieceprovider.PieceParkReader

	idxStor *indexstore.IndexStore

	pieceReaderCacheMu sync.Mutex
	pieceReaderCache   *ttlcache.Cache // Cache for successful readers (10 minutes with TTL extension)
	pieceErrorCacheMu  sync.Mutex
	pieceErrorCache    *ttlcache.Cache // Cache for errors (5 seconds without TTL extension)
}

func NewCachedPieceReader(db *harmonydb.DB, sectorReader *pieceprovider.SectorReader, pieceParkReader *pieceprovider.PieceParkReader, idxStor *indexstore.IndexStore) *CachedPieceReader {
	prCache := ttlcache.NewCache()
	_ = prCache.SetTTL(PieceReaderCacheTTL)
	prCache.SetCacheSizeLimit(MaxCachedReaders)
	prCache.SkipTTLExtensionOnHit(false) // Enable TTL extension for successful readers

	errorCache := ttlcache.NewCache()
	_ = errorCache.SetTTL(PieceErrorCacheTTL)
	errorCache.SetCacheSizeLimit(MaxCachedReaders)
	errorCache.SkipTTLExtensionOnHit(true) // Disable TTL extension for errors

	cpr := &CachedPieceReader{
		db:               db,
		sectorReader:     sectorReader,
		pieceParkReader:  pieceParkReader,
		pieceReaderCache: prCache,
		pieceErrorCache:  errorCache,
		idxStor:          idxStor,
	}

	expireCallback := func(key string, reason ttlcache.EvictionReason, value interface{}) {
		log.Debugw("expire callback", "piececid", key, "reason", reason)

		r := value.(*cachedSectionReader)

		cpr.pieceReaderCacheMu.Lock()
		defer cpr.pieceReaderCacheMu.Unlock()

		r.expired = true

		if r.refs <= 0 {
			r.cancel()
			return
		}

		log.Debugw("expire callback with refs > 0", "refs", r.refs, "piececid", key, "reason", reason)
	}

	errorExpireCallback := func(key string, reason ttlcache.EvictionReason, value interface{}) {
		log.Debugw("error cache expire callback", "piececid", key, "reason", reason)
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

	if r.refs == 0 && r.expired {
		log.Debugw("canceling underlying section reader context as cache entry doesn't exist", "piececid", r.pieceCid)

		r.cancel()
	}

	return nil
}

func (cpr *CachedPieceReader) getPieceReaderFromMarketPieceDeal(ctx context.Context, pieceCidV2 cid.Cid, retrieval bool) (storiface.Reader, uint64, error) {
	// Get all deals containing this piece

	commp, err := commcidv2.CommPFromPCidV2(pieceCidV2)
	if err != nil {
		return nil, 0, xerrors.Errorf("getting piece commitment from piece CID v2: %w", err)
	}

	pieceCid := commp.PCidV1()
	pieceSize := commp.PieceInfo().Size

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

	err = cpr.db.Select(ctx, &deals, `SELECT 
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
			return nil, 0, fmt.Errorf("piece cid %s: %w", pieceCid, ErrNoDeal)
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

			return reader, uint64(dl.RawSize), nil
		}

		if dl.PieceRef.Valid {
			// This is a MK20 deal, get from piece park
			ref := dl.PieceRef.Int64
			reader, rawSize, err := cpr.getPieceReaderFromPiecePark(ctx, &ref, nil, nil)
			if err != nil {
				merr = multierror.Append(merr, xerrors.Errorf("failed to read piece from piece park: %w", err))
				continue
			}
			return reader, rawSize, nil
		}

	}

	return nil, 0, merr
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
		err := cpr.db.Select(ctx, &pdr, `
										SELECT
										  pp.id,
										  pp.piece_cid,
										  pp.piece_raw_size
										FROM parked_piece_refs pr
										JOIN parked_pieces     pp ON pp.id = pr.piece_id
										WHERE pr.ref_id = $1 AND pp.complete = TRUE and pp.long_term = TRUE;
    `, pieceRef)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to query parked_pieces and parked_piece_refs for piece_ref %d: %w", pieceRef, err)
		}
		if len(pdr) > 0 {
			pd = append(pd, pdr...)
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
			return nil, 0, fmt.Errorf("failed to query parked_pieces and parked_piece_refs for piece_ref %d: %w", pieceRef, err)
		}
		if len(pdc) > 0 {
			pd = append(pd, pdc...)
		}
	}

	if len(pd) == 0 {
		return nil, 0, fmt.Errorf("failed to find piece in parked_pieces for piece_ref %d", pieceRef)
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
	pieces, err := cpr.idxStor.FindPieceInAggregate(ctx, pieceCidV2)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find piece in aggregate: %w", err)
	}

	if len(pieces) == 0 {
		return nil, 0, fmt.Errorf("subpiece not found in any aggregate piece")
	}

	pi, err := commcidv2.CommPFromPCidV2(pieceCidV2)
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
		return SubPieceReader{r: reader, sr: sr}, pi.PayloadSize(), nil
	}

	return nil, 0, fmt.Errorf("failed to find piece in aggregate: %w", merr)
}

func (cpr *CachedPieceReader) GetSharedPieceReader(ctx context.Context, pieceCidV2 cid.Cid, retrieval bool) (storiface.Reader, uint64, error) {
	// Check if this is PieceCidV1 and try to convert to v2 if possible
	yes := commcidv2.IsPieceCidV2(pieceCidV2)
	if !yes {
		var rawSize int64
		var singlePiece bool
		err := cpr.db.QueryRow(ctx, `WITH meta AS (
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
		pcid2, err := commcidv2.PieceCidV2FromV1(pieceCidV2, uint64(rawSize))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to convert piece cid v1 to v2: %w", err)
		}
		pieceCidV2 = pcid2
	}

	cacheKey := pieceCidV2.String()

	commp, err := commcidv2.CommPFromPCidV2(pieceCidV2)
	if err != nil {
		return nil, 0, xerrors.Errorf("getting piece commitment from piece CID v2: %w", err)
	}

	pieceCid := commp.PCidV1()
	pieceSize := commp.PieceInfo().Size

	// First check if we have a cached error for this piece
	cpr.pieceErrorCacheMu.Lock()
	if errorItem, err := cpr.pieceErrorCache.Get(cacheKey); err == nil {
		cachedErr := errorItem.(*cachedError)
		cpr.pieceErrorCacheMu.Unlock()
		log.Debugw("returning cached error", "piececid", pieceCid, "err", cachedErr.err)
		return nil, 0, cachedErr.err
	}
	cpr.pieceErrorCacheMu.Unlock()

	var r *cachedSectionReader

	// Check if there is already a piece reader in the cache
	cpr.pieceReaderCacheMu.Lock()
	rr, err := cpr.pieceReaderCache.Get(cacheKey)
	if err != nil {
		// There is not yet a cached piece reader, create a new one and add it
		// to the cache
		r = &cachedSectionReader{
			cpr:      cpr,
			pieceCid: pieceCidV2,
			ready:    make(chan struct{}),
			refs:     1,
		}
		_ = cpr.pieceReaderCache.Set(cacheKey, r)
		cpr.pieceReaderCacheMu.Unlock()

		// We just added a cached reader, so get its underlying piece reader
		readerCtx, readerCtxCancel := context.WithCancel(context.Background())
		defer close(r.ready)

		reader, size, err := cpr.getPieceReaderFromAggregate(readerCtx, pieceCidV2, retrieval)
		if err != nil {
			log.Debugw("failed to get piece reader from aggregate", "piececid", pieceCidV2.String(), "err", err)

			aerr := err

			reader, size, err = cpr.getPieceReaderFromMarketPieceDeal(readerCtx, pieceCidV2, retrieval)
			if err != nil {
				log.Errorw("failed to get piece reader", "piececid", pieceCid, "piece size", pieceSize, "err", err)
				finalErr := fmt.Errorf("failed to get piece reader from aggregate, sector or piece park: %w, %w", aerr, err)
				// Cache the error in the error cache
				cpr.pieceErrorCacheMu.Lock()
				_ = cpr.pieceErrorCache.Set(cacheKey, &cachedError{err: finalErr, pieceCid: pieceCid})
				cpr.pieceErrorCacheMu.Unlock()

				// Remove the failed reader from the main cache
				cpr.pieceReaderCacheMu.Lock()
				_ = cpr.pieceReaderCache.Remove(cacheKey)
				cpr.pieceReaderCacheMu.Unlock()

				r.err = finalErr
				readerCtxCancel()

				return nil, 0, finalErr
			}

		}

		r.reader = reader
		r.err = nil
		r.cancel = readerCtxCancel
		r.rawSize = size
	} else {
		r = rr.(*cachedSectionReader)
		r.refs++

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
