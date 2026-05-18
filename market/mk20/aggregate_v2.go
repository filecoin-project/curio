package mk20

import (
	"bytes"
	"crypto/sha256"
	"io"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegmentv2"
)

// AssembleAggregateV2 reads raw content from each sub-piece reader, concatenates
// them, and appends a datasegmentv2 tail index. It returns a reader over the
// assembled V2 aggregate piece data and its total raw size.
//
// Each reader is consumed up to the corresponding rawSize. The content CID for
// each sub-piece is computed as sha256/raw over the raw bytes.
func AssembleAggregateV2(readers []io.Reader, rawSizes []int64) (io.Reader, int64, error) {
	if len(readers) != len(rawSizes) {
		return nil, 0, xerrors.Errorf("readers and rawSizes length mismatch: %d vs %d", len(readers), len(rawSizes))
	}
	if len(readers) == 0 {
		return nil, 0, xerrors.Errorf("no sub-pieces to aggregate")
	}

	var pieceData []byte
	var entries []*datasegmentv2.SegmentDesc
	offset := uint64(0)

	for i, r := range readers {
		content, err := io.ReadAll(io.LimitReader(r, rawSizes[i]))
		if err != nil {
			return nil, 0, xerrors.Errorf("reading sub-piece %d: %w", i, err)
		}
		if int64(len(content)) != rawSizes[i] {
			return nil, 0, xerrors.Errorf("sub-piece %d: expected %d bytes, got %d", i, rawSizes[i], len(content))
		}

		// Compute sha256/raw content CID.
		h := sha256.Sum256(content)
		cmh, err := mh.Encode(h[:], mh.SHA2_256)
		if err != nil {
			return nil, 0, xerrors.Errorf("encoding multihash for sub-piece %d: %w", i, err)
		}
		contentCID := cid.NewCidV1(cid.Raw, cmh)

		entry, err := datasegmentv2.NewDataSegmentIndexEntryFromCID(contentCID, offset, uint64(len(content)))
		if err != nil {
			return nil, 0, xerrors.Errorf("creating V2 index entry for sub-piece %d: %w", i, err)
		}
		entry.WithUpdatedChecksum()
		entries = append(entries, entry)

		pieceData = append(pieceData, content...)
		offset += uint64(len(content))
	}

	// Build and append the tail index.
	idx := &datasegmentv2.IndexDataV2{
		Entries: entries,
		Offset:  int64(offset),
	}
	indexBlob, err := idx.MarshalBinary()
	if err != nil {
		return nil, 0, xerrors.Errorf("marshaling V2 tail index: %w", err)
	}

	pieceData = append(pieceData, indexBlob...)

	return bytes.NewReader(pieceData), int64(len(pieceData)), nil
}
