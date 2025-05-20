package cachedreader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/jellydator/ttlcache/v2"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
)

var NoDealErr = errors.New("no deals found")

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

	pieceReaderCacheMu sync.Mutex
	pieceReaderCache   *ttlcache.Cache // Cache for successful readers (10 minutes with TTL extension)
	pieceErrorCacheMu  sync.Mutex
	pieceErrorCache    *ttlcache.Cache // Cache for errors (5 seconds without TTL extension)
}

func NewCachedPieceReader(db *harmonydb.DB, sectorReader *pieceprovider.SectorReader, pieceParkReader *pieceprovider.PieceParkReader) *CachedPieceReader {
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
	reader    storiface.Reader
	cpr       *CachedPieceReader
	pieceCid  cid.Cid
	pieceSize abi.UnpaddedPieceSize
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

func (cpr *CachedPieceReader) getPieceReaderFromSector(ctx context.Context, pieceCid cid.Cid, pieceSize abi.PaddedPieceSize) (storiface.Reader, abi.UnpaddedPieceSize, error) {
	// Get all deals containing this piece

	var deals []struct {
		SpID   abi.ActorID             `db:"sp_id"`
		Sector abi.SectorNumber        `db:"sector_num"`
		Offset abi.PaddedPieceSize     `db:"piece_offset"`
		Length abi.PaddedPieceSize     `db:"piece_length"`
		Proof  abi.RegisteredSealProof `db:"reg_seal_proof"`
	}

	err := cpr.db.Select(ctx, &deals, `SELECT 
												mpd.sp_id,
												mpd.sector_num,
												mpd.piece_offset,
												mpd.piece_length,
												sm.reg_seal_proof
											FROM 
												market_piece_deal mpd
											JOIN 
												sectors_meta sm 
											ON 
												mpd.sp_id = sm.sp_id 
												AND mpd.sector_num = sm.sector_num
											WHERE 
												mpd.piece_cid = $1 AND mpd.piece_length = $2`, pieceCid.String(), pieceSize)
	if err != nil {
		return nil, 0, fmt.Errorf("getting piece deals: %w", err)
	}

	if len(deals) == 0 {
		return nil, 0, fmt.Errorf("piece cid %s: %w", pieceCid, NoDealErr)
	}

	// For each deal, try to read an unsealed copy of the data from the sector
	// it is stored in
	var merr error
	for _, dl := range deals {
		sr := storiface.SectorRef{
			ID: abi.SectorID{
				Miner:  dl.SpID,
				Number: dl.Sector,
			},
			ProofType: dl.Proof,
		}

		reader, err := cpr.sectorReader.ReadPiece(ctx, sr, storiface.UnpaddedByteIndex(dl.Offset.Unpadded()), dl.Length.Unpadded(), pieceCid)
		if err != nil {
			merr = multierror.Append(merr, err)
			continue
		}

		return reader, dl.Length.Unpadded(), nil
	}

	return nil, 0, merr
}

func (cpr *CachedPieceReader) getPieceReaderFromPiecePark(ctx context.Context, pieceCid cid.Cid, pieceSize abi.PaddedPieceSize) (storiface.Reader, abi.UnpaddedPieceSize, error) {
	// Query parked_pieces and parked_piece_refs in one go
	var pieceData []struct {
		ID           int64 `db:"id"`
		PieceRawSize int64 `db:"piece_raw_size"`
	}

	err := cpr.db.Select(ctx, &pieceData, `
        SELECT
            pp.id,
            pp.piece_raw_size
        FROM
            parked_pieces pp
        WHERE
            pp.piece_cid = $1 AND pp.piece_padded_size = $2 AND pp.complete = TRUE AND pp.long_term = TRUE
        LIMIT 1;
    `, pieceCid.String(), pieceSize)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query parked_pieces and parked_piece_refs for piece cid %s: %w", pieceCid.String(), err)
	}

	if len(pieceData) == 0 {
		return nil, 0, fmt.Errorf("failed to find piece in parked_pieces for piece cid %s", pieceCid.String())
	}

	reader, err := cpr.pieceParkReader.ReadPiece(ctx, storiface.PieceNumber(pieceData[0].ID), pieceData[0].PieceRawSize, pieceCid)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read piece from piece park: %w", err)
	}

	return reader, abi.UnpaddedPieceSize(pieceData[0].PieceRawSize), nil
}

func (cpr *CachedPieceReader) GetSharedPieceReader(ctx context.Context, pieceCid cid.Cid, pieceSize abi.PaddedPieceSize) (storiface.Reader, abi.UnpaddedPieceSize, error) {
	cacheKey := pieceCid.String()

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
			pieceCid: pieceCid,
			ready:    make(chan struct{}),
			refs:     1,
		}
		_ = cpr.pieceReaderCache.Set(cacheKey, r)
		cpr.pieceReaderCacheMu.Unlock()

		// We just added a cached reader, so get its underlying piece reader
		readerCtx, readerCtxCancel := context.WithCancel(context.Background())
		defer close(r.ready)

		reader, size, err := cpr.getPieceReaderFromSector(readerCtx, pieceCid, pieceSize)
		if err != nil {
			log.Warnw("failed to get piece reader from sector", "piececid", pieceCid, "piece size", pieceSize, "err", err)

			serr := err

			// Try getPieceReaderFromPiecePark
			reader, size, err = cpr.getPieceReaderFromPiecePark(readerCtx, pieceCid, pieceSize)
			if err != nil {
				log.Errorw("failed to get piece reader from piece park", "piececid", pieceCid, "piece size", pieceSize, "err", err)

				finalErr := fmt.Errorf("failed to get piece reader from sector or piece park: %w, %w", err, serr)

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
		r.pieceSize = size
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

	rs := io.NewSectionReader(r.reader, 0, int64(r.pieceSize))

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
	}, r.pieceSize, nil
}
