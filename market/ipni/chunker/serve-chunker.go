package chunker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/multiformats/go-multihash"
	"github.com/snadrus/must"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"

	"github.com/filecoin-project/lotus/lib/result"
)

var (
	ErrNotFound = errors.New("not found")
)

const NoSkipCacheTTL = 3 * time.Minute

type ipniEntry struct {
	Data []byte
	Prev cid.Cid
}

type ServeChunker struct {
	db            *harmonydb.DB
	pieceProvider *pieceprovider.SectorReader
	indexStore    *indexstore.IndexStore
	cpr           *cachedreader.CachedPieceReader

	entryCache *lru.Cache[cid.Cid, *promise.Promise[result.Result[ipniEntry]]]

	// small cache keeping track of which piece CIDs (v2) shouldn't be skipped. Entries expire after NoSkipCacheTTL
	noSkipCache *lru.Cache[cid.Cid, time.Time]
}

// Entries are 0.5MiB in size, so we do ~10MiB of caching here
// This cache is only useful in the edge case when entry reads are very slow and time out - this makes retried reads faster
const EntryCacheSize = 20

func NewServeChunker(db *harmonydb.DB, pieceProvider *pieceprovider.SectorReader, indexStore *indexstore.IndexStore, cpr *cachedreader.CachedPieceReader) *ServeChunker {
	return &ServeChunker{
		db:            db,
		pieceProvider: pieceProvider,
		indexStore:    indexStore,
		cpr:           cpr,

		entryCache:  must.One(lru.New[cid.Cid, *promise.Promise[result.Result[ipniEntry]]](EntryCacheSize)),
		noSkipCache: must.One(lru.New[cid.Cid, time.Time](EntryCacheSize)),
	}
}

// validate is a boolean variable that determines whether to validate the reconstructed chunk node against the expected chunk CID.
// If validate is true, the chunk node is validated against the expected chunk CID.
// If the chunk node does not match the expected chunk CID, an error is returned.
// If validate is false, the chunk node is not validated.
var validate = true

// GetEntry retrieves an entry from the provider's database based on the given block CID and provider ID.
// It returns the entry data as a byte slice, or an error if the entry is not found or an error occurs during retrieval.
// If the entry is stored as a CAR file, it reconstructs the chunk from the CAR file.
func (p *ServeChunker) GetEntry(ctx context.Context, block cid.Cid) (b []byte, err error) {
	return p.getEntry(ctx, block, false)
}

func (p *ServeChunker) getEntry(rctx context.Context, block cid.Cid, speculated bool) (b []byte, err error) {
	var prevChunk cid.Cid

	defer func() {
		defer func() {
			if r := recover(); r != nil {
				log.Errorw("panic while getting entry", "r", r)
				err = xerrors.Errorf("panic while getting entry: %v", r)
			}
		}()

		if !speculated && err == nil && prevChunk != cid.Undef {
			go func() {
				_, err := p.getEntry(context.Background(), prevChunk, true)
				if err != nil {
					log.Errorw("failed to speculatively get previous entry", "block", block, "prev", prevChunk, "err", err)
				}
			}()
		}
	}()

	if b, ok := p.entryCache.Get(block); ok {
		v := b.Val(rctx)
		if v.Error == nil {
			prevChunk = v.Value.Prev
			return v.Value.Data, nil
		} else if errors.Is(v.Error, ErrNotFound) {
			log.Errorw("Cached promise skip", "block", block, "prev", prevChunk, "err", err)
			return v.Value.Data, v.Error
		}
		log.Errorw("Error in cached promise", "block", block, "error", v.Error)
	}

	prom := &promise.Promise[result.Result[ipniEntry]]{}
	p.entryCache.Add(block, prom)
	defer func() {
		prom.Set(result.Result[ipniEntry]{Value: ipniEntry{
			Data: b,
			Prev: prevChunk,
		}, Error: err})
	}()

	// We should use background context to avoid early exit
	// while chunking as first attempt will always fail
	ctx := context.Background()

	type ipniChunk struct {
		PieceCIDv2 string `db:"piece_cid"`
		FromCar    bool   `db:"from_car"`

		FirstCID    *string `db:"first_cid"`
		StartOffset *int64  `db:"start_offset"`
		NumBlocks   int64   `db:"num_blocks"`

		IsPDP bool `db:"is_pdp"`

		PrevCID *string `db:"prev_cid"`
	}

	var ipniChunks []ipniChunk

	err = p.db.Select(ctx, &ipniChunks, `SELECT 
			current.piece_cid, 
			current.from_car, 
			current.first_cid, 
			current.start_offset, 
			current.num_blocks,
			current.is_pdp,
			prev.cid AS prev_cid
		FROM 
			ipni_chunks current
		LEFT JOIN 
			ipni_chunks prev 
		ON 
			current.piece_cid = prev.piece_cid AND
			current.chunk_num = prev.chunk_num + 1
		WHERE 
			current.cid = $1
		LIMIT 1;`, block.String())
	if err != nil {
		return nil, xerrors.Errorf("querying chunks with entry link %s: %w", block, err)
	}

	if len(ipniChunks) == 0 {
		log.Warnw("No chunk found for entry", "block", block)
		return nil, ErrNotFound
	}

	chunk := ipniChunks[0]
	pieceCidv2, err := cid.Parse(chunk.PieceCIDv2)
	if err != nil {
		return nil, xerrors.Errorf("parsing piece CID: %w", err)
	}

	// Convert to pcid2 if needed
	yes := commcidv2.IsPieceCidV2(pieceCidv2)
	if !yes {
		var rawSize int64
		var singlePiece bool
		err := p.db.QueryRow(ctx, `WITH meta AS (
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
											  ((SELECT n FROM exact) = 1)                    AS has_single_metadata;`, pieceCidv2.String()).Scan(&rawSize, &singlePiece)
		if err != nil {
			return nil, fmt.Errorf("failed to get piece metadata: %w", err)
		}
		if !singlePiece {
			return nil, fmt.Errorf("more than 1 piece metadata found for piece cid %s, please use piece cid v2", pieceCidv2.String())
		}
		pcid2, err := commcid.PieceCidV2FromV1(pieceCidv2, uint64(rawSize))
		if err != nil {
			return nil, fmt.Errorf("failed to convert piece cid v1 to v2: %w", err)
		}
		pieceCidv2 = pcid2
	}

	if leave, ok := p.noSkipCache.Get(pieceCidv2); !ok || time.Now().After(leave) {
		skip, err := p.checkIsEntrySkip(ctx, block)
		if err != nil {
			return nil, xerrors.Errorf("checking entry skipped for block %s: %w", block, err)
		}
		if skip {
			log.Warnw("Skipped entry skipped for block", "block", block)
			return nil, ErrNotFound
		}
	}

	p.noSkipCache.Add(pieceCidv2, time.Now().Add(NoSkipCacheTTL))

	var next ipld.Link
	if chunk.PrevCID != nil {
		prevChunk, err = cid.Parse(*chunk.PrevCID)
		if err != nil {
			return nil, xerrors.Errorf("parsing previous CID: %w", err)
		}

		next = cidlink.Link{Cid: prevChunk}
	}

	if chunk.IsPDP {
		if chunk.NumBlocks != 1 {
			return nil, xerrors.Errorf("Expected 1 block for PDP piece announcement, got %d", chunk.NumBlocks)
		}
		if chunk.PrevCID != nil {
			return nil, xerrors.Errorf("Expected no previous chunk for PDP piece announcement, got %s", *chunk.PrevCID)
		}

		if chunk.FirstCID == nil {
			return nil, xerrors.Errorf("chunk does not have first CID")
		}

		cb, err := hex.DecodeString(*chunk.FirstCID)
		if err != nil {
			return nil, xerrors.Errorf("decoding first CID: %w", err)
		}

		mhs := make([]multihash.Multihash, 0, 1)
		mhs = append(mhs, cb)

		chunkNode, err := NewEntriesChunkNode(mhs, next)
		if err != nil {
			return nil, xerrors.Errorf("creating chunk node: %w", err)
		}

		if validate {
			link, err := ipniculib.NodeToLink(chunkNode, ipniculib.EntryLinkproto)
			if err != nil {
				return nil, err
			}

			if link.String() != block.String() {
				return nil, xerrors.Errorf("car chunk node does not match the expected chunk CID, got %s, expected %s", link.String(), block.String())
			}
		}

		b := new(bytes.Buffer)
		err = dagcbor.Encode(chunkNode, b)
		if err != nil {
			return nil, xerrors.Errorf("encoding chunk node: %w", err)
		}

		log.Infow("Served a PDP chunk", "chunk", chunk, "piece", pieceCidv2, "startOffset", 0, "numBlocks", 1, "speculated", speculated)

		return b.Bytes(), nil
	}

	if !chunk.FromCar {
		if chunk.FirstCID == nil {
			return nil, xerrors.Errorf("chunk does not have first CID")
		}

		cb, err := hex.DecodeString(*chunk.FirstCID)
		if err != nil {
			return nil, xerrors.Errorf("decoding first CID: %w", err)
		}

		firstHash := multihash.Multihash(cb)

		return p.reconstructChunkFromDB(ctx, block, pieceCidv2, firstHash, next, chunk.NumBlocks, speculated)
	}

	return p.reconstructChunkFromCar(ctx, block, pieceCidv2, *chunk.StartOffset, next, chunk.NumBlocks, speculated)
}

// reconstructChunkFromCar reconstructs a chunk from a car file.
func (p *ServeChunker) reconstructChunkFromCar(ctx context.Context, chunk, piecev2 cid.Cid, startOff int64, next ipld.Link, numBlocks int64, speculate bool) ([]byte, error) {
	start := time.Now()

	pieceCid, rawSize, err := commcid.PieceCidV1FromV2(piecev2)
	if err != nil {
		return nil, xerrors.Errorf("getting piece CID v2 from piece CID v2: %w", err)
	}

	size := padreader.PaddedSize(rawSize).Padded()

	reader, _, err := p.cpr.GetSharedPieceReader(ctx, piecev2, false)
	defer func(reader storiface.Reader) {
		_ = reader.Close()
	}(reader)

	if err != nil {
		return nil, xerrors.Errorf("failed to read piece %s of size %d for ipni chunk %s reconstruction: %w", pieceCid, size, chunk, err)
	}

	_, err = reader.Seek(startOff, io.SeekStart)
	if err != nil {
		return nil, xerrors.Errorf("seeking to start offset: %w", err)
	}

	br := bufio.NewReader(reader)

	mhs := make([]multihash.Multihash, 0, numBlocks)
	for i := int64(0); i < numBlocks; i++ {
		bcid, err := ipniculib.SkipCarNode(br)
		if err != nil {
			return nil, xerrors.Errorf("skipping car node: %w", err)
		}

		mhs = append(mhs, bcid.Hash())
	}

	curOff, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, xerrors.Errorf("getting current offset: %w", err)
	}

	read := time.Now()

	// Create the chunk node
	chunkNode, err := NewEntriesChunkNode(mhs, next)
	if err != nil {
		return nil, xerrors.Errorf("creating chunk node: %w", err)
	}

	if validate {
		link, err := ipniculib.NodeToLink(chunkNode, ipniculib.EntryLinkproto)
		if err != nil {
			return nil, err
		}

		if link.String() != chunk.String() {
			return nil, xerrors.Errorf("car chunk node does not match the expected chunk CID, got %s, expected %s", link.String(), chunk.String())
		}
	}

	b := new(bytes.Buffer)
	err = dagcbor.Encode(chunkNode, b)
	if err != nil {
		return nil, xerrors.Errorf("encoding chunk node: %w", err)
	}

	log.Infow("Reconstructing chunk from car", "chunk", chunk, "piece", pieceCid, "size", size, "startOffset", startOff, "numBlocks", numBlocks, "speculated", speculate, "readMiB", float64(curOff-startOff)/1024/1024, "recomputeTime", time.Since(read), "totalTime", time.Since(start), "ents/s", float64(numBlocks)/time.Since(start).Seconds(), "MiB/s", float64(curOff-startOff)/1024/1024/time.Since(start).Seconds())

	return b.Bytes(), nil
}

// ReconstructChunkFromDB reconstructs a chunk from the database.
func (p *ServeChunker) reconstructChunkFromDB(ctx context.Context, chunk, piecev2 cid.Cid, firstHash multihash.Multihash, next ipld.Link, numBlocks int64, speculate bool) ([]byte, error) {
	start := time.Now()

	pieceCid, rawSize, err := commcid.PieceCidV1FromV2(piecev2)
	if err != nil {
		return nil, xerrors.Errorf("getting piece CID v1 from piece CID v2: %w", err)
	}

	size := padreader.PaddedSize(rawSize).Padded()

	var mhs []multihash.Multihash

	// Handle exception for PDP piece announcement with FilecoinPieceHttp{} metadata
	if numBlocks == 1 {
		mhs = []multihash.Multihash{firstHash}
	} else {
		mhs, err = p.indexStore.GetPieceHashRange(ctx, piecev2, firstHash, numBlocks)
		if err != nil {
			return nil, xerrors.Errorf("getting piece hash range: %w", err)
		}
	}

	// Create the chunk node
	chunkNode, err := NewEntriesChunkNode(mhs, next)
	if err != nil {
		return nil, xerrors.Errorf("creating chunk node: %w", err)
	}

	if validate {
		link, err := ipniculib.NodeToLink(chunkNode, ipniculib.EntryLinkproto)
		if err != nil {
			return nil, err
		}

		if link.String() != chunk.String() {
			for i, mh := range mhs {
				log.Infow("db chunk node mh", "mh", mh, "i", i)
			}

			return nil, xerrors.Errorf("db chunk node does not match the expected chunk CID, got %s, expected %s, mhs %d/%d, first %s, nextL %s", link.String(), chunk.String(), len(mhs), numBlocks, firstHash.HexString(), next)
		}
	}

	b := new(bytes.Buffer)
	err = dagcbor.Encode(chunkNode, b)
	if err != nil {
		return nil, err
	}

	log.Infow("Reconstructing chunk from DB", "chunk", chunk, "piece", pieceCid, "size", size, "firstHash", firstHash, "numBlocks", numBlocks, "speculated", speculate, "totalTime", time.Since(start), "ents/s", float64(numBlocks)/time.Since(start).Seconds())

	return b.Bytes(), nil
}

func (p *ServeChunker) checkIsEntrySkip(ctx context.Context, entry cid.Cid) (bool, error) {
	// CREATE INDEX ipni_entries_skip ON ipni(entries, is_skip, piece_cid);
	var isSkip bool
	err := p.db.QueryRow(ctx, `SELECT is_skip FROM ipni WHERE entries = $1`, entry).Scan(&isSkip)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	return isSkip, nil
}
