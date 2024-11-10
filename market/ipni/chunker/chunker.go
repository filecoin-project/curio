package chunker

import (
	"bytes"
	"fmt"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/go-ipld-prime"
	"github.com/ipld/go-ipld-prime/codec/dagcbor"
	"github.com/ipld/go-ipld-prime/datamodel"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"
	"os"
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

	{
		n, err := chunk.ToNode()
		if err != nil {
			return nil, err
		}

		link, err := ipniculib.NodeToLink(n, ipniculib.EntryLinkproto)
		if err != nil {
			return nil, err
		}

		cstr := link.String()
		b := new(bytes.Buffer)
		err = dagcbor.Encode(n, b)
		if err != nil {
			return nil, xerrors.Errorf("encoding chunk node: %w", err)
		}

		err = os.WriteFile(fmt.Sprintf("/tmp/cnk/%s", cstr), b.Bytes(), 0666)
		if err != nil {
			log.Errorf("failed to write chunk node to file: %s", err)
		}
	}

	return chunk.ToNode()
}
