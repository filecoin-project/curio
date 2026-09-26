package pieceprovider

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/hashspace"
	"github.com/filecoin-project/curio/lib/storiface"
)

// OpenPieceReader reads PDP pieces placed in the open-pieces hash space.
type OpenPieceReader struct {
	hs *hashspace.Cluster
}

func NewOpenPieceReader(hs *hashspace.Cluster) *OpenPieceReader {
	return &OpenPieceReader{hs: hs}
}

// ReadPiece opens pc (a piece CID v1) from open-pieces. It returns nil, nil
// when pc has not been placed.
//
// Every range read opens the file at the last location that worked. If the
// file is gone there (moved by a rebalance, or dropped from a finished move source), the
// locations are looked up again and the read moves on to the next one, so a
// long-lived cached reader follows the piece.
func (o *OpenPieceReader) ReadPiece(ctx context.Context, pc cid.Cid, rawSize int64) (storiface.Reader, error) {
	locs, err := o.hs.Locations(ctx, pc.String())
	if err != nil {
		return nil, err
	}
	if len(locs) == 0 {
		return nil, nil
	}

	rctx, cancel := context.WithCancel(ctx)
	fr := &failoverRange{hs: o.hs, pc: pc, ctx: rctx, current: locs[0].StorageID}
	pr, err := (&pieceReader{
		getReader: fr.get,
		len:       abi.UnpaddedPieceSize(rawSize),
		onClose:   cancel,
		pieceCid:  pc,
	}).init(rctx)
	if err != nil || pr == nil {
		cancel()
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return pr, nil
}

type failoverRange struct {
	hs  *hashspace.Cluster
	pc  cid.Cid
	ctx context.Context

	mu      sync.Mutex
	current string
}

func (f *failoverRange) get(offset, size uint64) (io.ReadCloser, error) {
	f.mu.Lock()
	current := f.current
	f.mu.Unlock()

	r, err := f.open(current, offset, size)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return r, err
	}

	locs, lerr := f.hs.Locations(f.ctx, f.pc.String())
	if lerr != nil {
		return nil, xerrors.Errorf("open piece %s missing on %s; relisting locations: %w", f.pc, current, lerr)
	}
	for _, l := range locs {
		if l.StorageID == current {
			continue
		}
		r, err = f.open(l.StorageID, offset, size)
		if err == nil {
			f.mu.Lock()
			f.current = l.StorageID
			f.mu.Unlock()
			return r, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			log.Warnw("reading open piece location", "piece", f.pc, "storage", l.StorageID, "error", err)
		}
	}
	return nil, xerrors.Errorf("open piece %s not found at any location: %w", f.pc, os.ErrNotExist)
}

func (f *failoverRange) open(storageID string, offset, size uint64) (io.ReadCloser, error) {
	if f.hs.HasLocal(storageID) {
		return f.hs.OpenLocalAt(storageID, f.pc, int64(offset), int64(size))
	}
	return f.hs.RemoteAt(f.ctx, storageID, f.pc, int64(offset), int64(offset+size))
}
