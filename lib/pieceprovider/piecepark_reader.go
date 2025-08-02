package pieceprovider

import (
	"context"
	"io"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

type PieceParkReader struct {
	storage *paths.Remote
	index   paths.SectorIndex
}

func NewPieceParkReader(storage *paths.Remote, index paths.SectorIndex) *PieceParkReader {
	return &PieceParkReader{
		storage: storage,
		index:   index,
	}
}

func (p *PieceParkReader) ReadPiece(ctx context.Context, pieceParkID storiface.PieceNumber, pieceSize int64, pc cid.Cid) (storiface.Reader, error) {
	ctx, cancel := context.WithCancel(ctx)

	// Reader returns a reader getter for an unsealed piece at the given offset in the given sector.
	// The returned reader will be nil if none of the workers has an unsealed sector file containing
	// the unsealed piece.
	readerGetter, err := p.storage.ReaderPiece(ctx, pieceParkID.Ref(), storiface.FTPiece, 0, pieceSize)
	if err != nil {
		cancel()
		log.Debugw("failed to get reader for piece", "pieceParkID", pieceParkID, "error", err)
		return nil, err
	}
	if readerGetter == nil {
		cancel()
		return nil, nil
	}

	pr, err := (&pieceReader{
		getReader: func(startOffset, readSize uint64) (io.ReadCloser, error) {
			r, err := readerGetter(int64(startOffset), int64(startOffset+readSize))
			if err != nil {
				return nil, xerrors.Errorf("getting reader at +%d: %w", startOffset, err)
			}
			return r, nil
		},
		len:      abi.UnpaddedPieceSize(pieceSize),
		onClose:  cancel,
		pieceCid: pc,
	}).init(ctx)
	if err != nil || pr == nil { // pr == nil to make sure we don't return typed nil
		cancel()
		return nil, err
	}

	log.Debugw("got reader for piece", "pieceParkID", pieceParkID, "pieceSize", pieceSize, "pieceCID", pc)
	return pr, err
}
