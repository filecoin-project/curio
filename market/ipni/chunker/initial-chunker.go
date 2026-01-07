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

	dbMultihashes []multihash.Multihash

	prevChunks []chunkMeta

	start time.Time
}

type chunkMeta struct {
	link  ipld.Link
	start multihash.Multihash
	nodes int64
}

func NewInitialChunker() *InitialChunker {
	return &InitialChunker{
		chunkSize: EntriesChunkSize,

		start: time.Now(),
	}
}

func (c *InitialChunker) Accept(mhs []multihash.Multihash) error {
	c.dbMultihashes = append(c.dbMultihashes, mhs...)

	// chunk if for loop so if we remove duplicate entries, we don't skip chunk if we still have enough entries
	for len(c.dbMultihashes) >= c.chunkSize {
		err := c.chunk()
		if err != nil {
			return xerrors.Errorf("chunking failed: %w", err)
		}
	}

	c.ingestedSoFar += int64(len(mhs))
	return nil
}

func (c *InitialChunker) chunk() error {
	l := c.chunkSize
	if len(c.dbMultihashes) < l {
		l = len(c.dbMultihashes)
	}
	mhs := make([]multihash.Multihash, l)
	copy(mhs, c.dbMultihashes[:l])

	// Sort multihashes
	sort.Slice(mhs, func(i, j int) bool {
		return bytes.Compare(mhs[i], mhs[j]) < 0
	})

	// Drop duplicates
	var n int
	var prev multihash.Multihash
	for i := range mhs {
		if i > 0 {
			if string(mhs[i]) == string(prev) {
				continue
			}
		}

		mhs[n] = mhs[i]
		prev = mhs[i]
		n++
	}

	if len(mhs) != n {
		mhs = mhs[:n]
		// Shift remaining data to the front of the internal buffer
		extra := c.dbMultihashes[l:]
		copy(c.dbMultihashes, mhs)
		extraLen := copy(c.dbMultihashes[len(mhs):], extra)
		c.dbMultihashes = c.dbMultihashes[:len(mhs)+extraLen]
		return nil
	}

	var next ipld.Link
	if len(c.prevChunks) > 0 {
		next = c.prevChunks[len(c.prevChunks)-1].link
	}

	cNode, err := NewEntriesChunkNode(mhs, next)
	if err != nil {
		return err
	}

	link, err := ipniculib.NodeToLink(cNode, ipniculib.EntryLinkproto)
	if err != nil {
		return err
	}

	c.prevChunks = append(c.prevChunks, chunkMeta{link, mhs[0], int64(len(mhs))})

	count := copy(c.dbMultihashes, c.dbMultihashes[l:])
	c.dbMultihashes = c.dbMultihashes[:count]

	return nil
}

func (c *InitialChunker) Finish(ctx context.Context, db *harmonydb.DB, pieceCid cid.Cid, isPDP bool) (ipld.Link, error) {
	defer func() {
		took := time.Since(c.start)
		ingestedPerSec := float64(c.ingestedSoFar) / took.Seconds()
		log.Infow("Finished initial chunking", "ingested", c.ingestedSoFar, "took", took, "ingestedPerSec", ingestedPerSec)
	}()

	for len(c.dbMultihashes) > 0 {
		err := c.chunk()
		if err != nil {
			return nil, xerrors.Errorf("chunking failed during finish: %w", err)
		}
	}

	return c.finishDB(ctx, db, pieceCid, isPDP)
}

func (c *InitialChunker) finishDB(ctx context.Context, db *harmonydb.DB, pieceCid cid.Cid, isPDP bool) (ipld.Link, error) {
	if db == nil {
		// dry run mode, just log chunk hashes
		for i, chunk := range c.prevChunks {
			fmt.Printf("%d. %s\n", i, chunk.link)
		}

		return nil, nil
	}

	commit, err := db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		batch := &pgx.Batch{}

		// Queue insert statements into the batch
		for i := 0; i < len(c.prevChunks); i++ {
			link := c.prevChunks[i].link
			firstCID := c.prevChunks[i].start
			numBlocks := c.prevChunks[i].nodes
			startOffset := (*int64)(nil)

			// Prepare the insert statement
			batch.Queue(`
                INSERT INTO ipni_chunks (cid, piece_cid, chunk_num, first_cid, start_offset, num_blocks, from_car, is_pdp)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING
            `, link.String(), pieceCid.String(), i, firstCID.HexString(), startOffset, numBlocks, false, isPDP)
		}

		// Send the batch
		br := tx.SendBatch(ctx, batch)
		defer func() {
			_ = br.Close()
		}()

		// Execute the batch and check for errors
		for i := 0; i < len(c.prevChunks); i++ {
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

	lastLink := c.prevChunks[len(c.prevChunks)-1].link

	log.Infow("Generated linked chunks of multihashes for DB ingest", "totalMhCount", c.ingestedSoFar, "chunkCount", len(c.prevChunks), "lastCid", lastLink)
	return lastLink, nil
}
