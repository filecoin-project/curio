package remoteblockstore

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/hashicorp/go-multierror"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	format "github.com/ipfs/go-ipld-format"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-car/util"
	"github.com/multiformats/go-multihash"
	"go.opencensus.io/stats"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

const MaxCachedReaders = 128

const MaxCarBlockPrefixSize = 128 // car entry len varint + cid len

var log = logging.Logger("remote-blockstore")

type idxAPI interface {
	PiecesContainingMultihash(ctx context.Context, m multihash.Multihash) ([]indexstore.PieceInfo, error)
	GetOffset(ctx context.Context, pieceCid cid.Cid, hash multihash.Multihash) (uint64, error)
}

// RemoteBlockstore is a read-only blockstore over all cids across all pieces on a provider.
type RemoteBlockstore struct {
	idxApi       idxAPI
	blockMetrics *BlockMetrics
	db           *harmonydb.DB
	cpr          *cachedreader.CachedPieceReader
}

type BlockMetrics struct {
	GetRequestCount             *stats.Int64Measure
	GetFailResponseCount        *stats.Int64Measure
	GetSuccessResponseCount     *stats.Int64Measure
	BytesSentCount              *stats.Int64Measure
	HasRequestCount             *stats.Int64Measure
	HasFailResponseCount        *stats.Int64Measure
	HasSuccessResponseCount     *stats.Int64Measure
	GetSizeRequestCount         *stats.Int64Measure
	GetSizeFailResponseCount    *stats.Int64Measure
	GetSizeSuccessResponseCount *stats.Int64Measure
}

func NewRemoteBlockstore(api idxAPI, db *harmonydb.DB, cpr *cachedreader.CachedPieceReader) *RemoteBlockstore {
	httpBlockMetrics := &BlockMetrics{
		GetRequestCount:             HttpRblsGetRequestCount,
		GetFailResponseCount:        HttpRblsGetFailResponseCount,
		GetSuccessResponseCount:     HttpRblsGetSuccessResponseCount,
		BytesSentCount:              HttpRblsBytesSentCount,
		HasRequestCount:             HttpRblsHasRequestCount,
		HasFailResponseCount:        HttpRblsHasFailResponseCount,
		HasSuccessResponseCount:     HttpRblsHasSuccessResponseCount,
		GetSizeRequestCount:         HttpRblsGetSizeRequestCount,
		GetSizeFailResponseCount:    HttpRblsGetSizeFailResponseCount,
		GetSizeSuccessResponseCount: HttpRblsGetSizeSuccessResponseCount,
	}

	return &RemoteBlockstore{
		idxApi:       api,
		blockMetrics: httpBlockMetrics,
		db:           db,
		cpr:          cpr,
	}
}

func (ro *RemoteBlockstore) Get(ctx context.Context, c cid.Cid) (b blocks.Block, err error) {
	if ro.blockMetrics != nil {
		stats.Record(ctx, ro.blockMetrics.GetRequestCount.M(1))
	}

	defer func() {
		var nb int
		if b != nil {
			nb = len(b.RawData())
		}
		log.Debugw("Get", "cid", c, "err", err, "bytes", nb, "bnil", b == nil)
	}()

	// Get the pieces that contain the cid
	pieces, err := ro.idxApi.PiecesContainingMultihash(ctx, c.Hash())

	// Check if it's an identity cid, if it is, return its digest
	if err != nil {
		digest, ok, iderr := isIdentity(c)
		if iderr == nil && ok {
			if ro.blockMetrics != nil {
				stats.Record(ctx, ro.blockMetrics.GetSuccessResponseCount.M(1))
			}
			return blocks.NewBlockWithCid(digest, c)
		}
		if ro.blockMetrics != nil {
			stats.Record(ctx, ro.blockMetrics.GetFailResponseCount.M(1))
		}
		return nil, fmt.Errorf("getting pieces containing cid %s: %w", c, err)
	}
	if len(pieces) == 0 {
		return nil, fmt.Errorf("no pieces with cid %s found", c)
	}

	// Get a reader over one of the pieces and extract the block data
	var merr error
	for _, piece := range pieces {
		data, err := func() ([]byte, error) {
			// Get a reader over the piece data
			reader, _, err := ro.cpr.GetSharedPieceReader(ctx, piece.PieceCid)
			if err != nil {
				return nil, fmt.Errorf("getting piece reader: %w", err)
			}
			defer func(reader storiface.Reader) {
				_ = reader.Close()
			}(reader)

			// Get the offset of the block within the piece (CAR file)
			offset, err := ro.idxApi.GetOffset(ctx, piece.PieceCid, c.Hash())
			if err != nil {
				return nil, fmt.Errorf("getting offset/size for cid %s in piece %s: %w", c, piece.PieceCid, err)
			}

			// Seek to the section offset
			readerAt := io.NewSectionReader(reader, int64(offset), int64(piece.BlockSize+MaxCarBlockPrefixSize))
			readCid, data, err := util.ReadNode(bufio.NewReader(readerAt))
			if err != nil {
				return nil, fmt.Errorf("reading data for block %s from reader for piece %s: %w", c, piece.PieceCid, err)
			}
			if !bytes.Equal(readCid.Hash(), c.Hash()) {
				return nil, fmt.Errorf("read block %s from reader for piece %s, but expected block %s", readCid, piece.PieceCid, c)
			}
			return data, nil
		}()
		if err != nil {
			merr = multierror.Append(merr, err)
			continue
		}
		return blocks.NewBlockWithCid(data, c)
	}

	if merr == nil {
		merr = fmt.Errorf("no block with cid %s found", c)
	}

	return nil, merr
}

func (ro *RemoteBlockstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	if ro.blockMetrics != nil {
		stats.Record(ctx, ro.blockMetrics.HasRequestCount.M(1))
	}

	log.Debugw("Has", "cid", c)

	pieces, err := ro.idxApi.PiecesContainingMultihash(ctx, c.Hash())
	if err != nil {
		if ro.blockMetrics != nil {
			stats.Record(ctx, ro.blockMetrics.HasFailResponseCount.M(1))
		}
		return false, fmt.Errorf("getting pieces containing cid %s: %w", c, err)
	}
	has := len(pieces) > 0

	log.Debugw("Has response", "cid", c, "has", has, "error", err)
	if ro.blockMetrics != nil {
		stats.Record(ctx, ro.blockMetrics.HasSuccessResponseCount.M(1))
	}
	return has, nil
}

func (ro *RemoteBlockstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	if ro.blockMetrics != nil {
		stats.Record(ctx, ro.blockMetrics.GetSizeRequestCount.M(1))
	}

	log.Debugw("GetSize", "cid", c)
	size, err := ro.blockstoreGetSize(ctx, c)
	log.Debugw("GetSize response", "cid", c, "size", size, "error", err)
	if err != nil && ro.blockMetrics != nil {
		stats.Record(ctx, ro.blockMetrics.GetSizeFailResponseCount.M(1))
	} else if ro.blockMetrics != nil {
		stats.Record(ctx, ro.blockMetrics.GetSizeSuccessResponseCount.M(1))
	}
	return size, err
}

// --- UNSUPPORTED BLOCKSTORE METHODS -------
func (ro *RemoteBlockstore) DeleteBlock(context.Context, cid.Cid) error {
	return errors.New("unsupported operation DeleteBlock")
}
func (ro *RemoteBlockstore) HashOnRead(_ bool) {}
func (ro *RemoteBlockstore) Put(context.Context, blocks.Block) error {
	return errors.New("unsupported operation Put")
}
func (ro *RemoteBlockstore) PutMany(context.Context, []blocks.Block) error {
	return errors.New("unsupported operation PutMany")
}
func (ro *RemoteBlockstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return nil, errors.New("unsupported operation AllKeysChan")
}

func (ro *RemoteBlockstore) blockstoreGetSize(ctx context.Context, c cid.Cid) (int, error) {
	// Get the pieces that contain the cid
	pieces, err := ro.idxApi.PiecesContainingMultihash(ctx, c.Hash())
	if err != nil {
		return 0, fmt.Errorf("getting pieces containing cid %s: %w", c, err)
	}
	if len(pieces) == 0 {
		// We must return ipld ErrNotFound here because that's the only type
		// that bitswap interprets as a not found error. All other error types
		// are treated as general errors.
		return 0, format.ErrNotFound{Cid: c}
	}

	var merr error

	// Iterate over all pieces in case the sector containing the first piece with the Block
	// is not unsealed
	for _, p := range pieces {
		return int(p.BlockSize), nil
	}

	return 0, merr
}

func isIdentity(c cid.Cid) (digest []byte, ok bool, err error) {
	dmh, err := multihash.Decode(c.Hash())
	if err != nil {
		return nil, false, err
	}
	ok = dmh.Code == multihash.IDENTITY
	digest = dmh.Digest
	return digest, ok, nil
}
