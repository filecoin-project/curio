package pieceprovider

import (
	"bufio"
	"context"
	"io"
	"sync"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	pool "github.com/libp2p/go-buffer-pool"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fr32"
)

var log = logging.Logger("piece-provider")

type SectorReader struct {
	storage *paths.Remote
	index   paths.SectorIndex
}

func NewSectorReader(storage *paths.Remote, index paths.SectorIndex) *SectorReader {
	return &SectorReader{
		storage: storage,
		index:   index,
	}
}

// ReadPiece will try to read the unsealed piece from an existing unsealed sector file for the given sector from any worker that has it.
// It will NOT try to schedule an Unseal of a sealed sector file for the read.
//
// Returns a nil reader if the piece does NOT exist in any unsealed file or there is no unsealed file for the given sector on any of the workers.
func (p *SectorReader) ReadPiece(ctx context.Context, sector storiface.SectorRef, pieceOffset storiface.UnpaddedByteIndex, pieceSize abi.UnpaddedPieceSize, pc cid.Cid) (storiface.Reader, error) {
	if err := pieceOffset.Valid(); err != nil {
		return nil, xerrors.Errorf("pieceOffset is not valid: %w", err)
	}
	if err := pieceSize.Validate(); err != nil {
		return nil, xerrors.Errorf("size is not a valid piece size: %w", err)
	}

	// acquire a lock purely for reading unsealed sectors
	ctx, cancel := context.WithCancel(ctx)
	if err := p.index.StorageLock(ctx, sector.ID, storiface.FTUnsealed, storiface.FTNone); err != nil {
		cancel()
		return nil, xerrors.Errorf("acquiring read sector lock: %w", err)
	}

	// Reader returns a reader getter for an unsealed piece at the given offset in the given sector.
	// The returned reader will be nil if none of the workers has an unsealed sector file containing
	// the unsealed piece.
	readerGetter, err := p.storage.Reader(ctx, sector, abi.PaddedPieceSize(pieceOffset.Padded()), pieceSize.Padded())
	if err != nil {
		cancel()
		log.Debugf("did not get storage reader;sector=%+v, err:%s", sector.ID, err)
		return nil, err
	}
	if readerGetter == nil {
		cancel()
		return nil, nil
	}

	pr, err := (&pieceReader{
		getReader: func(startOffset, readSize uint64) (io.ReadCloser, error) {
			// The request is for unpadded bytes, at any offset.
			// storage.Reader readers give us fr32-padded bytes, so we need to
			// do the unpadding here.

			startOffsetAligned := storiface.UnpaddedFloor(startOffset)
			startOffsetDiff := int(startOffset - uint64(startOffsetAligned))

			endOffset := startOffset + readSize
			endOffsetAligned := storiface.UnpaddedCeil(endOffset)

			r, err := readerGetter(startOffsetAligned.Padded(), endOffsetAligned.Padded())
			if err != nil {
				return nil, xerrors.Errorf("getting reader at +%d: %w", startOffsetAligned, err)
			}

			buf := pool.Get(fr32.BufSize(pieceSize.Padded()))

			upr, err := fr32.NewUnpadReaderBuf(r, pieceSize.Padded(), buf)
			if err != nil {
				r.Close() // nolint
				return nil, xerrors.Errorf("creating unpadded reader: %w", err)
			}

			bir := bufio.NewReaderSize(upr, 127)
			if startOffset > uint64(startOffsetAligned) {
				if _, err := bir.Discard(startOffsetDiff); err != nil {
					r.Close() // nolint
					return nil, xerrors.Errorf("discarding bytes for startOffset: %w", err)
				}
			}

			var closeOnce sync.Once

			return struct {
				io.Reader
				io.Closer
			}{
				Reader: bir,
				Closer: funcCloser(func() error {
					closeOnce.Do(func() {
						pool.Put(buf)
					})
					return r.Close()
				}),
			}, nil
		},
		len:      pieceSize,
		onClose:  cancel,
		pieceCid: pc,
	}).init(ctx)
	if err != nil || pr == nil { // pr == nil to make sure we don't return typed nil
		cancel()
		return nil, err
	}

	log.Debugf("returning reader to read unsealed piece, sector=%+v, pieceOffset=%d, size=%d", sector, pieceOffset, pieceSize)

	return pr, err
}

// IsUnsealed checks if we have the unsealed piece at the given offset in an already
// existing unsealed file either locally or on any of the workers.
func (p *SectorReader) IsUnsealed(ctx context.Context, sector storiface.SectorRef, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	if err := offset.Valid(); err != nil {
		return false, xerrors.Errorf("offset is not valid: %w", err)
	}
	if err := size.Validate(); err != nil {
		return false, xerrors.Errorf("size is not a valid piece size: %w", err)
	}

	ctxLock, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := p.index.StorageLock(ctxLock, sector.ID, storiface.FTUnsealed, storiface.FTNone); err != nil {
		return false, xerrors.Errorf("acquiring read sector lock: %w", err)
	}

	return p.storage.CheckIsUnsealed(ctxLock, sector, abi.PaddedPieceSize(offset.Padded()), size.Padded())
}

type funcCloser func() error

func (f funcCloser) Close() error {
	return f()
}

var _ io.Closer = funcCloser(nil)
