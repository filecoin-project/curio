package chunker

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"

	lru "github.com/hashicorp/golang-lru/v2"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-car/v2/index"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/multiformats/go-multihash"

	"github.com/filecoin-project/curio/lib/ipni/ipniculib"
)

var log = logging.Logger("chunker")

const EntriesChunkSize = 16384
const EntriesCacheCapacity = 4096

// Chunker chunks advertisement entries as a chained series of schema.EntryChunk nodes.
// See: NewChunker
type Chunker struct {
	chunkSize int
	cache     *lru.Cache[ipld.Link, datamodel.Node]
}

// NewChunker instantiates a new chain chunker that given a provider.MultihashIterator it drains
// all its mulithashes and stores them in the given link system represented as a chain of
// schema.EntryChunk nodes where each chunk contains no more than chunkSize number of multihashes.
//
// See: schema.EntryChunk.
func NewChunker(cache *lru.Cache[ipld.Link, datamodel.Node]) *Chunker {
	chunker := &Chunker{
		chunkSize: EntriesChunkSize,
	}
	if cache != nil {
		chunker.cache = cache
	}
	return chunker
}

// Chunk chunks all the mulithashes returned by the given iterator into a chain of schema.EntryChunk
// nodes where each chunk contains no more than chunkSize number of multihashes and returns the link
// the root chunk node.
//
// See: schema.EntryChunk.
func (ls *Chunker) Chunk(mhi SliceMhIterator) (ipld.Link, error) {
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
		mhCount++
		if len(mhs) >= ls.chunkSize {
			cNode, err := newEntriesChunkNode(mhs, next)
			if err != nil {
				return nil, err
			}
			next, err = ipniculib.NodeToLink(cNode, schema.Linkproto)
			if err != nil {
				return nil, err
			}
			if ls.cache != nil {
				ls.cache.Add(next, cNode)
			}
			chunkCount++
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

// SliceMhIterator is a simple MultihashIterator implementation that
// iterates a slice of multihash.Multihash.
type SliceMhIterator struct {
	mhs []multihash.Multihash
	pos int
}

type iteratorStep struct {
	mh     multihash.Multihash
	offset uint64
}

// CarMultihashIterator constructs a new MultihashIterator from a CAR index.
//
// This iterator supplies multihashes in deterministic order of their
// corresponding CAR offset. The order is maintained consistently regardless of
// the underlying IterableIndex implementation. Returns error if duplicate
// offsets detected.
func CarMultihashIterator(idx index.IterableIndex) (*SliceMhIterator, error) {
	var steps []iteratorStep
	if err := idx.ForEach(func(mh multihash.Multihash, offset uint64) error {
		steps = append(steps, iteratorStep{mh, offset})
		return nil
	}); err != nil {
		return nil, err
	}
	slices.SortFunc(steps, func(a, b iteratorStep) int {
		return cmp.Compare(a.offset, b.offset)
	})

	var lastOffset uint64
	mhs := make([]multihash.Multihash, len(steps))
	for i := range steps {
		if steps[i].offset == lastOffset {
			return nil, fmt.Errorf("car multihash iterator has duplicate offset %d", steps[i].offset)
		}
		mhs[i] = steps[i].mh
	}
	return &SliceMhIterator{mhs: mhs}, nil
}

// Next implements the MultihashIterator interface.
func (it *SliceMhIterator) Next() (multihash.Multihash, error) {
	if it.pos >= len(it.mhs) {
		return nil, io.EOF
	}
	mh := it.mhs[it.pos]
	it.pos++
	return mh, nil
}
