package chunker

import (
	"errors"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/multiformats/go-multihash"
	"io"

	"github.com/filecoin-project/curio/market/ipni/ipniculib"
)

var log = logging.Logger("chunker")

const entriesChunkSize = 16384

// Chunker chunks advertisement entries as a chained series of schema.EntryChunk nodes.
// See: NewChunker
type Chunker struct {
	chunkSize int
}

// NewChunker instantiates a new chain chunker that given a provider.MultihashIterator it drains
// all its mulithashes and stores them in the given link system represented as a chain of
// schema.EntryChunk nodes where each chunk contains no more than chunkSize number of multihashes.
//
// See: schema.EntryChunk.
func NewChunker() *Chunker {
	return &Chunker{
		chunkSize: entriesChunkSize,
	}
}

type HashIterator interface {
	Next() (multihash.Multihash, error)
}

// Chunk chunks all the mulithashes returned by the given iterator into a chain of schema.EntryChunk
// nodes where each chunk contains no more than chunkSize number of multihashes and returns the link
// the root chunk node.
//
// See: schema.EntryChunk.
func (ls *Chunker) Chunk(mhi HashIterator) (ipld.Link, error) {
	mhs := make([]multihash.Multihash, 0, ls.chunkSize)
	var next ipld.Link
	var mhCount, chunkCount int
	for {
		mh, err := mhi.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		mhs = append(mhs, mh)
		if len(mhs) >= ls.chunkSize {
			cNode, err := newEntriesChunkNode(mhs, next)
			if err != nil {
				return nil, err
			}
			next, err = ipniculib.NodeToLink(cNode, schema.Linkproto)
			if err != nil {
				return nil, err
			}
			chunkCount++
			mhCount += len(mhs)
			// NewLinkedListOfMhs makes it own copy, so safe to reuse mhs
			mhs = mhs[:0]
		}
	}
	if len(mhs) != 0 {
		cNode, err := newEntriesChunkNode(mhs, next)
		if err != nil {
			return nil, err
		}
		next, err = ipniculib.NodeToLink(cNode, schema.Linkproto)
		if err != nil {
			return nil, err
		}
		chunkCount++
		mhCount += len(mhs)
	}

	log.Infow("Generated linked chunks of multihashes", "totalMhCount", mhCount, "chunkCount", chunkCount)
	return next, nil
}

func newEntriesChunkNode(mhs []multihash.Multihash, next ipld.Link) (datamodel.Node, error) {
	chunk := schema.EntryChunk{
		Entries: mhs,
	}
	if next != nil {
		chunk.Next = next
	}
	return chunk.ToNode()
}
