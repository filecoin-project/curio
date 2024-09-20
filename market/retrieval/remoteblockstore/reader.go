package remoteblockstore

import (
	"context"
	"fmt"
	"io"

	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/storiface"
)

type SectionReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

type cachedSectionReader struct {
	SectionReader
	ro       *RemoteBlockstore
	pieceCid cid.Cid
	// Signals when the underlying piece reader is ready
	ready chan struct{}
	// err is non-nil if there's an error getting the underlying piece reader
	err error
	// cancel for underlying GetPieceReader call
	cancel  func()
	refs    int
	expired bool
}

func (r *cachedSectionReader) Close() error {
	r.ro.pieceReaderCacheMu.Lock()
	defer r.ro.pieceReaderCacheMu.Unlock()

	r.refs--

	if r.refs == 0 && r.expired {
		log.Debugw("canceling underlying section reader context as cache entry doesn't exist", "piececid", r.pieceCid)

		r.cancel()
	}

	return nil
}

func (ro *RemoteBlockstore) getPieceReader(ctx context.Context, pieceCid cid.Cid) (SectionReader, error) {
	// Get all deals containing this piece

	var deals []struct {
		SpID   abi.ActorID             `db:"sp_id"`
		Sector abi.SectorNumber        `db:"sector_num"`
		Offset abi.UnpaddedPieceSize   `db:"piece_offset"`
		Length abi.UnpaddedPieceSize   `db:"piece_length"`
		Proof  abi.RegisteredSealProof `db:"reg_seal_proof"`
	}

	err := ro.db.Select(ctx, &deals, `SELECT 
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
												mpd.piece_cid = $1;`, pieceCid.String())
	if err != nil {
		return nil, fmt.Errorf("getting piece deals: %w", err)
	}

	if len(deals) == 0 {
		return nil, fmt.Errorf("no deals found for piece cid %s: %w", pieceCid, err)
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
		unsealed, err := ro.pp.IsUnsealed(ctx, sr, storiface.UnpaddedByteIndex(dl.Offset), dl.Length)
		if err != nil {
			merr = multierror.Append(merr, err)
			continue
		}

		if !unsealed {
			continue
		}

		reader, err := ro.pp.ReadPiece(ctx, sr, storiface.UnpaddedByteIndex(dl.Offset), dl.Length, pieceCid)
		if err != nil {
			merr = multierror.Append(merr, err)
			continue
		}

		return reader, nil
	}

	return nil, merr
}

func (ro *RemoteBlockstore) GetSharedPieceReader(ctx context.Context, pieceCid cid.Cid) (SectionReader, error) {

	var r *cachedSectionReader

	// Check if there is already a piece reader in the cache
	ro.pieceReaderCacheMu.Lock()
	rr, err := ro.pieceReaderCache.Get(pieceCid.String())
	if err != nil {
		// There is not yet a cached piece reader, create a new one and add it
		// to the cache
		r = &cachedSectionReader{
			ro:       ro,
			pieceCid: pieceCid,
			ready:    make(chan struct{}),
			refs:     1,
		}
		_ = ro.pieceReaderCache.Set(pieceCid.String(), r)
		ro.pieceReaderCacheMu.Unlock()

		// We just added a cached reader, so get its underlying piece reader
		readerCtx, readerCtxCancel := context.WithCancel(context.Background())
		sr, err := ro.getPieceReader(readerCtx, pieceCid)

		r.SectionReader = sr
		r.err = err
		r.cancel = readerCtxCancel

		// Inform any waiting threads that the cached reader is ready
		close(r.ready)
	} else {

		r = rr.(*cachedSectionReader)
		r.refs++

		ro.pieceReaderCacheMu.Unlock()

		// We already had a cached reader, wait for it to be ready
		select {
		case <-ctx.Done():
			// The context timed out. Deference the cached piece reader and
			// return an error.
			_ = r.Close()
			return nil, ctx.Err()
		case <-r.ready:
		}
	}

	// If there was an error getting the underlying piece reader, make sure
	// that the cached reader gets cleaned up
	if r.err != nil {
		_ = r.Close()
		return nil, r.err
	}

	return r, nil
}
