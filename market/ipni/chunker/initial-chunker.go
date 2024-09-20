package chunker

import (
	"bytes"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/multiformats/go-multihash"
	"sort"
)

const longChainThreshold = 500_000

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
	carPending    []multihash.Multihash
	carChunkStart *int64
	carChunkNodes int64

	prevChunks []carChunkMeta
}

type carChunkMeta struct {
	link  ipld.Link
	start int64
	nodes int64
}

func NewInitialChunker() *InitialChunker {
	return &InitialChunker{
		chunkSize: entriesChunkSize,
	}
}

func (c *InitialChunker) Accept(mh multihash.Multihash, startOff int64) error {
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
	if c.carChunkStart == nil {
		c.carChunkStart = &startOff
		c.carChunkNodes = 0
	}
	c.carChunkNodes++

	if len(c.carPending) >= c.chunkSize {
		// create a chunk
		var next ipld.Link
		if len(c.prevChunks) > 0 {
			next = c.prevChunks[len(c.prevChunks)-1].link
		}

		cNode, err := newEntriesChunkNode(c.carPending, next)
		if err != nil {
			return err
		}

		link, err := ipniculib.NodeToLink(cNode, schema.Linkproto)
		if err != nil {
			return err
		}

		c.prevChunks = append(c.prevChunks, carChunkMeta{
			link:  link,
			start: *c.carChunkStart,
			nodes: c.carChunkNodes,
		})

		c.carPending = c.carPending[:0]
	}

	return nil
}

func (c *InitialChunker) Finish() error {
	// note: <= because we're not inserting anything here
	if c.ingestedSoFar <= longChainThreshold {
		// db-order ingest
		return c.finishDB()
	}

	// car-order ingest
	return c.finishCAR()
}

func (c *InitialChunker) finishDB() error {
	if len(c.dbMultihashes) == 0 {
		return nil
	}

	// in db-order ingest, we create a chain of schema.EntryChunk nodes with sorted multihashes

	// sort multihashes
	sort.Slice(c.dbMultihashes, func(i, j int) bool {
		return bytes.Compare(c.dbMultihashes[i], c.dbMultihashes[j]) < 0
	})

	return nil
}

func (c *InitialChunker) finishCAR() error {

}
