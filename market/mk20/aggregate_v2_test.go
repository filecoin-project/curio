package mk20

import (
	"io"
	"testing"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	_ "github.com/multiformats/go-multihash/register/blake3"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-data-segment/datasegmentv2"
)

func TestAssembleAggregateV2(t *testing.T) {
	// Create two raw content blobs.
	contentA := []byte("hello world content A for V2 aggregate test")
	contentB := []byte("goodbye world content B for V2 aggregate test - slightly longer")

	readers := []io.Reader{
		io.LimitReader(bytesReader(contentA), int64(len(contentA))),
		io.LimitReader(bytesReader(contentB), int64(len(contentB))),
	}
	rawSizes := []int64{int64(len(contentA)), int64(len(contentB))}

	r, totalSize, err := AssembleAggregateV2(readers, rawSizes)
	require.NoError(t, err)
	require.Greater(t, totalSize, int64(len(contentA)+len(contentB)),
		"total size must exceed content size due to tail index")

	// Read the assembled data.
	assembled, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Len(t, assembled, int(totalSize))

	// Verify the raw content is at the beginning.
	require.Equal(t, contentA, assembled[:len(contentA)])
	require.Equal(t, contentB, assembled[len(contentA):len(contentA)+len(contentB)])

	// Parse the tail index from the assembled data and verify entries.
	idx := &datasegmentv2.IndexDataV2{}
	err = idx.ParseIndexSection(bytesReaderAt(assembled), int64(len(assembled)))
	require.NoError(t, err)
	require.Len(t, idx.Entries, 2, "expected 2 index entries")

	// Verify entry 0 (contentA).
	cidA := contentCID(t, contentA)
	entryCID0, err := idx.Entries[0].DataCID()
	require.NoError(t, err)
	require.True(t, cidA.Equals(entryCID0), "entry 0 CID mismatch: want %s got %s", cidA, entryCID0)
	require.EqualValues(t, 0, idx.Entries[0].Offset)
	require.EqualValues(t, len(contentA), idx.Entries[0].RawSize)

	// Verify entry 1 (contentB).
	cidB := contentCID(t, contentB)
	entryCID1, err := idx.Entries[1].DataCID()
	require.NoError(t, err)
	require.True(t, cidB.Equals(entryCID1), "entry 1 CID mismatch: want %s got %s", cidB, entryCID1)
	require.EqualValues(t, len(contentA), idx.Entries[1].Offset)
	require.EqualValues(t, len(contentB), idx.Entries[1].RawSize)
}

func TestAssembleAggregateV2_SinglePiece(t *testing.T) {
	content := []byte("single piece V2 aggregate content")
	readers := []io.Reader{io.LimitReader(bytesReader(content), int64(len(content)))}
	rawSizes := []int64{int64(len(content))}

	r, totalSize, err := AssembleAggregateV2(readers, rawSizes)
	require.NoError(t, err)

	assembled, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Len(t, assembled, int(totalSize))

	// Content should be at the start.
	require.Equal(t, content, assembled[:len(content)])

	// Should be parseable.
	idx := &datasegmentv2.IndexDataV2{}
	err = idx.ParseIndexSection(bytesReaderAt(assembled), int64(len(assembled)))
	require.NoError(t, err)
	require.Len(t, idx.Entries, 1)

	c := contentCID(t, content)
	ec, err := idx.Entries[0].DataCID()
	require.NoError(t, err)
	require.True(t, c.Equals(ec))
}

func TestAssembleAggregateV2_Errors(t *testing.T) {
	// Mismatched lengths.
	_, _, err := AssembleAggregateV2([]io.Reader{bytesReader([]byte("x"))}, []int64{1, 2})
	require.ErrorContains(t, err, "mismatch")

	// Empty.
	_, _, err = AssembleAggregateV2(nil, nil)
	require.ErrorContains(t, err, "no sub-pieces")

	// Short read.
	_, _, err = AssembleAggregateV2(
		[]io.Reader{io.LimitReader(bytesReader([]byte("ab")), 2)},
		[]int64{100},
	)
	require.Error(t, err)
}

// helpers

func contentCID(t *testing.T, data []byte) cid.Cid {
	t.Helper()
	cmh, err := mh.Sum(data, mh.BLAKE3, 32)
	require.NoError(t, err)
	return cid.NewCidV1(cid.Raw, cmh)
}

type bytesReaderImpl struct {
	data []byte
	pos  int
}

func (b *bytesReaderImpl) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
func bytesReader(data []byte) io.Reader { return &bytesReaderImpl{data: data} }

type bytesReaderAtImpl struct{ data []byte }

func (b *bytesReaderAtImpl) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func bytesReaderAt(data []byte) io.ReaderAt { return &bytesReaderAtImpl{data: data} }
