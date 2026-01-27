package chunker

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipld/go-ipld-prime"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

const longChainThreshold = 500_000_000

// maxCarChunkSize is maximum entry size for car-order ingested entries.
const maxCarChunkSize = 512 << 20

// InitialChunker is used for initial entry chain creation.
// It employs a dual strategy, where it tracks the number of entries, and:
// For chains with less than longChainThreshold entries, it accumulates entries in memory.
// which will then form a sorted chain of schema.EntryChunk nodes. This allows creation of
// ad chains which are read from the index database.
// For chains with more than longChainThreshold entries, it creates a chain of schema.EntryChunk nodes
// in car order, noting where each chunk starts and ends in the car file.
type InitialChunker struct {
	chunkSize int

	ingestedSoFar int64

	// db-order ingest, up to longChainThreshold
	dbMultihashes []multihash.Multihash

	// car-order ingest, after longChainThreshold
	carPending      []multihash.Multihash
	carPendingBytes uint64
	carChunkStart   *int64

	prevChunks []carChunkMeta

	start time.Time
}

type carChunkMeta struct {
	link  ipld.Link
	start int64
	nodes int64
}

func NewInitialChunker() *InitialChunker {
	return &InitialChunker{
		chunkSize: entriesChunkSize,

		start: time.Now(),
	}
}

func (c *InitialChunker) Accept(mh multihash.Multihash, startOff int64, entryLen uint64) error {
	if c.ingestedSoFar < longChainThreshold {
		// db-order ingest
		c.dbMultihashes = append(c.dbMultihashes, mh)
	}
	// note: we always run car-order ingest, even if we're still in db-order ingest

	// free db-order ingest
	if c.ingestedSoFar >= longChainThreshold {
		c.dbMultihashes = nil
	}
	c.ingestedSoFar++

	// car-order ingest

	// append to car-order ingest
	c.carPending = append(c.carPending, mh)
	c.carPendingBytes += entryLen
	if c.carChunkStart == nil {
		c.carChunkStart = &startOff
	}

	if len(c.carPending) >= c.chunkSize || c.carPendingBytes >= maxCarChunkSize {
		if err := c.processCarPending(); err != nil {
			return xerrors.Errorf("process car pending: %w", err)
		}
	}

	return nil
}

func (c *InitialChunker) processCarPending() error {
	// create a chunk
	var next ipld.Link
	if len(c.prevChunks) > 0 {
		next = c.prevChunks[len(c.prevChunks)-1].link
	}

	cNode, err := NewEntriesChunkNode(c.carPending, next)
	if err != nil {
		return err
	}

	link, err := ipniculib.NodeToLink(cNode, ipniculib.EntryLinkproto)
	if err != nil {
		return err
	}

	c.prevChunks = append(c.prevChunks, carChunkMeta{
		link:  link,
		start: *c.carChunkStart,
		nodes: int64(len(c.carPending)),
	})

	c.carPending = c.carPending[:0]
	c.carPendingBytes = 0
	c.carChunkStart = nil

	return nil
}

func (c *InitialChunker) Finish(ctx context.Context, db *harmonydb.DB, pieceCid cid.Cid, isPDP bool) (ipld.Link, error) {
	defer func() {
		took := time.Since(c.start)
		ingestedPerSec := float64(c.ingestedSoFar) / took.Seconds()
		log.Infow("Finished initial chunking", "ingested", c.ingestedSoFar, "took", took, "ingestedPerSec", ingestedPerSec)
	}()

	// note: <= because we're not inserting anything here
	if c.ingestedSoFar <= longChainThreshold {
		// db-order ingest
		return c.finishDB(ctx, db, pieceCid, isPDP)
	}

	// car-order ingest
	return c.finishCAR(ctx, db, pieceCid, isPDP)
}

func (c *InitialChunker) finishDB(ctx context.Context, db *harmonydb.DB, pieceCid cid.Cid, isPDP bool) (ipld.Link, error) {
	if len(c.dbMultihashes) == 0 {
		return nil, nil
	}

	c.carPending = nil
	c.prevChunks = nil

	// Sort multihashes
	sort.Slice(c.dbMultihashes, func(i, j int) bool {
		return bytes.Compare(c.dbMultihashes[i], c.dbMultihashes[j]) < 0
	})

	// Drop duplicates
	var n int
	var prev multihash.Multihash
	for i := range c.dbMultihashes {
		if i > 0 {
			if string(c.dbMultihashes[i]) == string(prev) {
				continue
			}
		}

		c.dbMultihashes[n] = c.dbMultihashes[i]
		prev = c.dbMultihashes[i]
		n++
	}

	if len(c.dbMultihashes) != n {
		log.Warnw("duplicates while indexing piece", "piece", pieceCid, "num", len(c.dbMultihashes)-n, "pieceOrigCount", len(c.dbMultihashes))
		c.dbMultihashes = c.dbMultihashes[:n]
	}

	// Partition multihashes into chunks
	totalMhCount := len(c.dbMultihashes)

	var chunks [][]multihash.Multihash
	for i := 0; i < len(c.dbMultihashes); i += c.chunkSize {
		end := i + c.chunkSize
		if end > len(c.dbMultihashes) {
			end = len(c.dbMultihashes)
		}
		chunks = append(chunks, c.dbMultihashes[i:end])
	}

	// Collect links for each chunk
	totalChunks := len(chunks)
	chunkLinks := make([]ipld.Link, totalChunks)

	for i := 0; i < totalChunks; i++ {
		var next ipld.Link
		if i > 0 {
			next = chunkLinks[i-1]
		}

		cNode, err := NewEntriesChunkNode(chunks[i], next)
		if err != nil {
			return nil, err
		}

		link, err := ipniculib.NodeToLink(cNode, ipniculib.EntryLinkproto)
		if err != nil {
			return nil, err
		}

		chunkLinks[i] = link
	}

	if db == nil {
		// dry run mode, just log chunk hashes
		for i, link := range chunkLinks {
			fmt.Printf("%d. %s\n", i, link)
		}

		return nil, nil
	}

	commit, err := db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		batch := &pgx.Batch{}

		// Queue insert statements into the batch
		for i := 0; i < totalChunks; i++ {
			link := chunkLinks[i]
			firstCID := chunks[i][0]
			numBlocks := len(chunks[i])
			startOffset := (*int64)(nil)

			// Prepare the insert statement
			batch.Queue(`
                INSERT INTO ipni_chunks (cid, piece_cid, chunk_num, first_cid, start_offset, num_blocks, from_car, is_pdp)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING
            `, link.String(), pieceCid.String(), i, firstCID.HexString(), startOffset, numBlocks, false, isPDP)
		}

		// Send the batch
		br, err := tx.SendBatch(ctx, batch)
		if err != nil {
			return false, xerrors.Errorf("failed to send batch: %w", err)
		}
		defer func() {
			_ = br.Close()
		}()

		// Execute the batch and check for errors
		for i := 0; i < totalChunks; i++ {
			_, err := br.Exec()
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return nil, err
	}
	if !commit {
		return nil, fmt.Errorf("transaction was rolled back")
	}

	lastLink := chunkLinks[totalChunks-1]

	log.Infow("Generated linked chunks of multihashes for DB ingest", "totalMhCount", totalMhCount, "chunkCount", totalChunks, "lastCid", lastLink)
	return lastLink, nil
}

func (c *InitialChunker) finishCAR(ctx context.Context, db *harmonydb.DB, pieceCid cid.Cid, isPDP bool) (ipld.Link, error) {
	// Process any remaining carPending multihashes
	if len(c.carPending) > 0 {
		if err := c.processCarPending(); err != nil {
			return nil, xerrors.Errorf("process car pending: %w", err)
		}
	}

	totalChunks := len(c.prevChunks)

	if db == nil {
		// dry run mode, just log chunk hashes
		for i, link := range c.prevChunks {
			fmt.Printf("%d. %s\n", i, link.link)
		}

		return nil, nil
	}

	commit, err := db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		batch := &pgx.Batch{}

		// Queue insert statements into the batch
		for i := 0; i < totalChunks; i++ {
			link := c.prevChunks[i].link
			numBlocks := int(c.prevChunks[i].nodes)
			startOffset := c.prevChunks[i].start

			// Prepare the insert statement
			batch.Queue(`
                INSERT INTO ipni_chunks (cid, piece_cid, chunk_num, first_cid, start_offset, num_blocks, from_car, is_pdp)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING
            `, link.String(), pieceCid.String(), i, nil, startOffset, numBlocks, true, isPDP)
		}

		// Send the batch
		br, err := tx.SendBatch(ctx, batch)
		if err != nil {
			return false, xerrors.Errorf("failed to send batch: %w", err)
		}
		defer func() {
			_ = br.Close()
		}()

		// Execute the batch and check for errors
		for i := 0; i < totalChunks; i++ {
			_, err := br.Exec()
			if err != nil {
				return false, err
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return nil, xerrors.Errorf("transaction: %w", err)
	}
	if !commit {
		return nil, fmt.Errorf("transaction was rolled back")
	}

	lastLink := c.prevChunks[totalChunks-1].link

	log.Infow("Generated linked chunks of multihashes for CAR ingest", "chunkCount", len(c.prevChunks), "lastCid", lastLink)
	return lastLink, nil
}
