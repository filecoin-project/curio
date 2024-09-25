package chunker

import (
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/multiformats/go-multihash"
)

var log = logging.Logger("chunker")

const entriesChunkSize = 16384

func NewEntriesChunkNode(mhs []multihash.Multihash, next ipld.Link) (datamodel.Node, error) {
	chunk := schema.EntryChunk{
		Entries: mhs,
	}
	if next != nil {
		chunk.Next = next
	}
	return chunk.ToNode()
}
