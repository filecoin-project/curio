package indexing_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-data-segment/datasegmentv2"

	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/indexing"
)

// buildV2Piece constructs a minimal V2 aggregate piece from raw content blobs,
// returning the full piece bytes, rawSize, the content CIDs, and the entries used.
func buildV2Piece(t *testing.T, contents [][]byte) ([]byte, int64, []cid.Cid) {
	t.Helper()

	var pieceData []byte
	var entries []*datasegmentv2.SegmentDesc
	contentCIDs := make([]cid.Cid, len(contents))

	offset := uint64(0)
	for i, content := range contents {
		h := sha256.Sum256(content)
		cmh, err := mh.Encode(h[:], mh.SHA2_256)
		require.NoError(t, err)
		c := cid.NewCidV1(cid.Raw, cmh)
		contentCIDs[i] = c

		entry, err := datasegmentv2.NewDataSegmentIndexEntryFromCID(c, offset, uint64(len(content)))
		require.NoError(t, err)
		entry.WithUpdatedChecksum()
		entries = append(entries, entry)

		pieceData = append(pieceData, content...)
		offset += uint64(len(content))
	}

	idx := &datasegmentv2.IndexDataV2{
		Entries: entries,
		Offset:  int64(offset),
	}
	indexBlob, err := idx.MarshalBinary()
	require.NoError(t, err)
	pieceData = append(pieceData, indexBlob...)

	return pieceData, int64(len(pieceData)), contentCIDs
}

func TestIndexAggregateV2_HappyPath(t *testing.T) {
	content1 := []byte("hello world from sub-piece one")
	content2 := []byte("second sub-piece content block here")
	pieceData, rawSize, contentCIDs := buildV2Piece(t, [][]byte{content1, content2})

	// Use a synthetic aggregate piece CID (sha256/raw of the whole piece).
	h := sha256.Sum256(pieceData)
	cmh, err := mh.Encode(h[:], mh.SHA2_256)
	require.NoError(t, err)
	pieceCID := cid.NewCidV1(cid.Raw, cmh)

	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})

	blocks, aggidx, interrupted, err := indexing.IndexAggregateV2(
		pieceCID,
		bytes.NewReader(pieceData),
		rawSize,
		recs,
		addFail,
	)
	close(recs)

	require.NoError(t, err)
	require.False(t, interrupted)

	// Drain the channel to collect emitted records.
	var records []indexstore.Record
	for rec := range recs {
		records = append(records, rec)
	}

	// blocks return should equal len(records) from aggidx.
	require.Equal(t, int64(len(aggidx[pieceCID])), blocks)

	// aggidx should have: synthetic root + 2 content entries = 3 records.
	require.Contains(t, aggidx, pieceCID)
	aggRecs := aggidx[pieceCID]
	require.Len(t, aggRecs, 3)

	// First record: synthetic root for aggregate CID resolution.
	require.True(t, aggRecs[0].Cid.Equals(pieceCID))
	require.Equal(t, uint64(0), aggRecs[0].Offset)
	require.Equal(t, uint64(rawSize), aggRecs[0].Size)

	// Content records at positions 1 and 2.
	require.True(t, aggRecs[1].Cid.Equals(contentCIDs[0]))
	require.Equal(t, uint64(0), aggRecs[1].Offset)
	require.Equal(t, uint64(len(content1)), aggRecs[1].Size)

	require.True(t, aggRecs[2].Cid.Equals(contentCIDs[1]))
	require.Equal(t, uint64(len(content1)), aggRecs[2].Offset)
	require.Equal(t, uint64(len(content2)), aggRecs[2].Size)
}

func TestIndexAggregateV2_ACLSkipped(t *testing.T) {
	content1 := []byte("content with ACL sibling")
	h := sha256.Sum256(content1)
	cmh, err := mh.Encode(h[:], mh.SHA2_256)
	require.NoError(t, err)
	contentCID := cid.NewCidV1(cid.Raw, cmh)

	// Build entries: one content + one ACL.
	entry1, err := datasegmentv2.NewDataSegmentIndexEntryFromCID(contentCID, 0, uint64(len(content1)))
	require.NoError(t, err)
	entry1.WithUpdatedChecksum()

	aclEntry := datasegmentv2.NewDataSegmentIndexEntryFromMultihash(0x1e, []byte{0xAA}, uint64(len(content1)), 64)
	aclEntry.ACLType = 1
	aclEntry.WithUpdatedChecksum()

	idx := &datasegmentv2.IndexDataV2{
		Entries: []*datasegmentv2.SegmentDesc{entry1, aclEntry},
		Offset:  int64(len(content1)),
	}
	indexBlob, err := idx.MarshalBinary()
	require.NoError(t, err)

	pieceData := append(append([]byte{}, content1...), indexBlob...)
	rawSize := int64(len(pieceData))

	pieceCID := cid.NewCidV1(cid.Raw, cmh) // reuse for simplicity

	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})
	blocks, aggidx, interrupted, err := indexing.IndexAggregateV2(pieceCID, bytes.NewReader(pieceData), rawSize, recs, addFail)
	close(recs)

	require.NoError(t, err)
	require.False(t, interrupted)

	// Root + 1 content = 2 records (ACL entry skipped).
	aggRecs := aggidx[pieceCID]
	require.Len(t, aggRecs, 2)
	require.Equal(t, int64(2), blocks)
	require.True(t, aggRecs[1].Cid.Equals(contentCID))
}

func TestIndexAggregateV2_EmptyIndex(t *testing.T) {
	// Build an index with zero content entries — just the sentinel.
	idx := &datasegmentv2.IndexDataV2{
		Entries: []*datasegmentv2.SegmentDesc{},
		Offset:  0,
	}
	indexBlob, err := idx.MarshalBinary()
	require.NoError(t, err)

	h := sha256.Sum256(indexBlob)
	cmh, err := mh.Encode(h[:], mh.SHA2_256)
	require.NoError(t, err)
	pieceCID := cid.NewCidV1(cid.Raw, cmh)

	recs := make(chan indexstore.Record, 64)
	addFail := make(chan struct{})
	_, _, _, err = indexing.IndexAggregateV2(pieceCID, bytes.NewReader(indexBlob), int64(len(indexBlob)), recs, addFail)
	close(recs)

	require.Error(t, err)
	require.Contains(t, err.Error(), "no entries")
}

func TestIndexAggregateV2_Interrupted(t *testing.T) {
	content1 := []byte("first piece data")
	content2 := []byte("second piece data")
	pieceData, rawSize, _ := buildV2Piece(t, [][]byte{content1, content2})

	h := sha256.Sum256(pieceData)
	cmh, err := mh.Encode(h[:], mh.SHA2_256)
	require.NoError(t, err)
	pieceCID := cid.NewCidV1(cid.Raw, cmh)

	// Use an unbuffered channel so sends block, and close addFail immediately.
	recs := make(chan indexstore.Record)
	addFail := make(chan struct{})
	close(addFail)

	_, _, interrupted, err := indexing.IndexAggregateV2(pieceCID, bytes.NewReader(pieceData), rawSize, recs, addFail)

	require.NoError(t, err)
	require.True(t, interrupted)
}
