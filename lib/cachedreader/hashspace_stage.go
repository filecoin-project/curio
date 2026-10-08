package cachedreader

import (
	"context"
	"io"

	"github.com/ipfs/go-cid"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

// OpenReader reads pieces placed in the open-pieces hash space. It returns
// nil, nil when no disk that may hold the piece has it.
type OpenReader interface {
	ReadPiece(ctx context.Context, pc cid.Cid, rawSize int64) (storiface.Reader, error)
}

// aggregateFinder lists the aggregates that contain a piece, with the
// subpiece's offset and size inside each.
type aggregateFinder interface {
	FindPieceInAggregate(ctx context.Context, pieceCidV2 cid.Cid) ([]indexstore.Record, error)
}

type openReaderBox struct {
	r OpenReader
}

// aggregateParents is a completed lookup of the aggregates holding a piece.
type aggregateParents struct {
	records []indexstore.Record
}

// hashspaceResult is the outcome of the hashspace stage. reader is set on a
// hit. parents is set when the aggregate lookup ran and succeeded, so the
// legacy path does not repeat it.
type hashspaceResult struct {
	reader  storiface.Reader
	rawSize uint64
	parents *aggregateParents
}

type aggregateLookup struct {
	records []indexstore.Record
	err     error
}

// SetOpenPieceReader makes retrieval look in open-pieces before anything else.
func (cpr *CachedPieceReader) SetOpenPieceReader(r OpenReader) {
	if r == nil {
		cpr.openPieceReader.Store(nil)
		return
	}
	cpr.openPieceReader.Store(&openReaderBox{r: r})
}

func (cpr *CachedPieceReader) hasCachedReader(pieceCid cid.Cid) bool {
	cpr.pieceReaderCacheMu.Lock()
	defer cpr.pieceReaderCacheMu.Unlock()
	_, found := cpr.pieceReaderCache.Get(pieceCid)
	return found
}

// hashspaceKey returns the piece CID v2 and raw size that name pc in
// open-pieces. A v1 CID takes one lookup of its raw size in parked_pieces.
func (cpr *CachedPieceReader) hashspaceKey(ctx context.Context, pc cid.Cid) (cid.Cid, uint64, bool) {
	if commcidv2.IsPieceCidV2(pc) {
		_, rawSize, err := commcid.PieceCidV1FromV2(pc)
		if err != nil {
			log.Debugw("piece cid v2 raw size", "piece", pc, "error", err)
			return cid.Undef, 0, false
		}
		return pc, rawSize, true
	}
	if cpr.db == nil || !commcidv2.IsCidV1PieceCid(pc) {
		return cid.Undef, 0, false
	}
	var sizes []int64
	if err := cpr.db.Select(ctx, &sizes, `SELECT piece_raw_size FROM parked_pieces WHERE piece_cid = $1 LIMIT 1`, pc.String()); err != nil {
		log.Warnw("looking up raw size of a v1 piece for open-pieces", "piece", pc, "error", err)
		return cid.Undef, 0, false
	}
	if len(sizes) == 0 || sizes[0] <= 0 {
		return cid.Undef, 0, false
	}
	v2, err := commcid.PieceCidV2FromV1(pc, uint64(sizes[0]))
	if err != nil {
		log.Debugw("piece cid v2 from v1", "piece", pc, "error", err)
		return cid.Undef, 0, false
	}
	return v2, uint64(sizes[0]), true
}

// readFromHashspace serves pc from open-pieces without SQL (a v1 CID costs one
// raw size lookup). The disk probe runs with the aggregate lookup, and a hit
// on the piece cancels it. When the piece is not there, each parent aggregate
// is probed the same way and the subpiece is read out of it. Only a cancelled
// ctx is an error: any other failure is a miss, left to the legacy path.
func (cpr *CachedPieceReader) readFromHashspace(ctx context.Context, pc cid.Cid) (hashspaceResult, error) {
	var res hashspaceResult
	box := cpr.openPieceReader.Load()
	if box == nil {
		return res, nil
	}
	opr := box.r

	v2, rawSize, ok := cpr.hashspaceKey(ctx, pc)
	if !ok {
		return res, ctx.Err()
	}

	// Aggregates are indexed by piece CID v2.
	var lookup chan aggregateLookup
	lookupCtx, lookupCancel := context.WithCancel(ctx)
	defer lookupCancel()
	if v2.Equals(pc) && cpr.aggs != nil {
		lookup = make(chan aggregateLookup, 1)
		go func() {
			recs, err := cpr.aggs.FindPieceInAggregate(lookupCtx, v2)
			lookup <- aggregateLookup{records: recs, err: err}
		}()
	}

	r, err := opr.ReadPiece(ctx, v2, int64(rawSize))
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		log.Warnw("reading open piece", "piece", pc, "error", err)
	}
	if r != nil {
		res.reader, res.rawSize = r, rawSize
		return res, nil
	}
	if lookup == nil {
		return res, nil
	}

	var found aggregateLookup
	select {
	case <-ctx.Done():
		return res, ctx.Err()
	case found = <-lookup:
	}
	if found.err != nil {
		log.Warnw("looking up aggregates of a piece", "piece", pc, "error", found.err)
		return res, nil
	}
	res.parents = &aggregateParents{records: found.records}

	for _, p := range found.records {
		if !commcidv2.IsPieceCidV2(p.Cid) {
			continue
		}
		_, parentRawSize, err := commcid.PieceCidV1FromV2(p.Cid)
		if err != nil {
			log.Debugw("aggregate raw size", "aggregate", p.Cid, "error", err)
			continue
		}
		parent, err := opr.ReadPiece(ctx, p.Cid, int64(parentRawSize))
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			log.Warnw("reading open piece aggregate", "piece", pc, "aggregate", p.Cid, "error", err)
			continue
		}
		if parent == nil {
			continue
		}
		sr := io.NewSectionReader(parent, int64(p.Offset), int64(p.Size))
		res.reader, res.rawSize = SubPieceReader{r: parent, sr: sr}, rawSize
		return res, nil
	}
	return res, nil
}

// boundedReader limits reads to the first rawSize bytes of r. Close closes r.
func boundedReader(r storiface.Reader, rawSize uint64) storiface.Reader {
	rs := io.NewSectionReader(r, 0, int64(rawSize))
	return struct {
		io.Closer
		io.Reader
		io.ReaderAt
		io.Seeker
	}{
		Closer:   r,
		Reader:   rs,
		Seeker:   rs,
		ReaderAt: r,
	}
}
