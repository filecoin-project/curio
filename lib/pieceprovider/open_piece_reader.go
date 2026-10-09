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

// ReadPiece opens pc (a piece CID v2) from open-pieces. It returns nil, nil
// when no disk that may hold pc has it. Finding the disks uses only the
// in-memory cluster map, and they are checked in parallel, so a miss costs no
// SQL.
//
// Every range read opens the file at the last location that worked. If the
// file is gone there (moved by a rebalance, or dropped from a finished move source), the
// places are looked up again and the read moves on to the other one, so a
// long-lived cached reader follows the piece.
func (o *OpenPieceReader) ReadPiece(ctx context.Context, pc cid.Cid, rawSize int64) (storiface.Reader, error) {
	holder, err := o.find(ctx, pc)
	if err != nil {
		return nil, err
	}
	if holder == "" {
		return nil, nil
	}

	rctx, cancel := context.WithCancel(ctx)
	fr := &failoverRange{hs: o.hs, pc: pc, ctx: rctx, current: holder}
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

// find returns the storage id of a disk that has pc, or "" when none does.
// Every candidate is checked at once and the first hit wins, local disks
// being the quickest to answer. A candidate that errors counts as a miss; the
// error is returned only when no candidate has the piece.
func (o *OpenPieceReader) find(ctx context.Context, pc cid.Cid) (string, error) {
	return findHolder(ctx, o.hs, pc)
}

// holderFinder is the part of the cluster that finding a holder uses.
type holderFinder interface {
	Candidates(pc cid.Cid) ([]hashspace.Location, error)
	StatAt(ctx context.Context, storageID string, pc cid.Cid) (bool, error)
}

func findHolder(ctx context.Context, hs holderFinder, pc cid.Cid) (string, error) {
	locs, err := hs.Candidates(pc)
	if err != nil {
		return "", err
	}
	switch len(locs) {
	case 0:
		return "", nil
	case 1:
		ok, err := hs.StatAt(ctx, locs[0].StorageID, pc)
		if err != nil || !ok {
			return "", err
		}
		return locs[0].StorageID, nil
	}

	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type probe struct {
		storageID string
		ok        bool
		err       error
	}
	results := make(chan probe, len(locs))
	for _, l := range locs {
		go func(id string) {
			ok, err := hs.StatAt(sctx, id, pc)
			results <- probe{storageID: id, ok: ok, err: err}
		}(l.StorageID)
	}

	var firstErr error
	for range locs {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case p := <-results:
			if p.ok {
				return p.storageID, nil
			}
			if p.err != nil {
				log.Debugw("probing open piece location", "piece", pc, "storage", p.storageID, "error", p.err)
				if firstErr == nil {
					firstErr = p.err
				}
			}
		}
	}
	return "", firstErr
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
