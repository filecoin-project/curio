package indexing

import (
	"bytes"
	"errors"
	"io"
	"math/bits"
	"os"
	"testing"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	carblockstore "github.com/ipld/go-car/v2/blockstore"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-data-segment/datasegment"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
)

func TestIndexCARReturnsRawLengthAndFirstBlockRecord(t *testing.T) {
	piece := makeCARPiece(t, 512)
	wantRecords := carRecords(t, piece.raw)

	recs := make(chan indexstore.Record, 128)
	rawSize, blocks, interrupted, err := IndexCAR(bytes.NewReader(piece.raw), 4<<20, recs, make(chan struct{}))
	close(recs)
	gotRecords := collectRecords(recs)

	require.NoError(t, err)
	require.False(t, interrupted)
	require.Equal(t, uint64(len(piece.raw)), rawSize)
	require.Equal(t, int64(len(wantRecords)), blocks)
	require.Len(t, gotRecords, len(wantRecords))

	require.True(t, wantRecords[0].Cid.Equals(gotRecords[0].Cid))
	require.Equal(t, wantRecords[0].Offset, gotRecords[0].Offset)
	require.Equal(t, wantRecords[0].Size, gotRecords[0].Size)
}

func TestIndexAggregateUsesSuppliedSubPieceCIDs(t *testing.T) {
	subA := makeCARPiece(t, 384)
	subB := makeCARPiece(t, 768)
	aggregate := makeAggregatePiece(t, subA, subB)

	subPieces := []mk20.DataSource{
		{
			PieceCID: subA.pieceCIDV2,
			Format:   mk20.PieceDataFormat{Car: &mk20.FormatCar{}},
		},
		{
			PieceCID: subB.pieceCIDV2,
			Format:   mk20.PieceDataFormat{Car: &mk20.FormatCar{}},
		},
	}

	recs := make(chan indexstore.Record, 128)
	aggRecs := make(chan indexstore.Record, 128)
	blocks, interrupted, err := IndexAggregate(aggregate.pieceCIDV2, bytes.NewReader(aggregate.raw), aggregate.pieceSize, subPieces, recs, aggRecs, make(chan struct{}))
	close(recs)
	close(aggRecs)
	gotRecords := collectRecords(recs)
	children := collectRecords(aggRecs)

	require.NoError(t, err)
	require.False(t, interrupted)
	require.NotZero(t, blocks)
	require.Len(t, gotRecords, int(blocks))

	require.Len(t, children, 2)
	require.True(t, subA.pieceCIDV2.Equals(children[0].Cid))
	require.True(t, subB.pieceCIDV2.Equals(children[1].Cid))
}

func TestIndexPDPv0AggregateUsesSegmentCARRawSizesForChildPieceCIDV2(t *testing.T) {
	subA := makeCARPiece(t, 384)
	subB := makeCARPiece(t, 768)
	aggregate := makeAggregatePiece(t, subA, subB)

	recs := make(chan indexstore.Record, 128)
	aggRecs := make(chan indexstore.Record, 128)
	blocks, interrupted, err := IndexPDPv0(aggregate.pieceCIDV2, bytes.NewReader(aggregate.raw), aggregate.pieceSize, recs, aggRecs, make(chan struct{}))
	close(recs)
	close(aggRecs)
	gotRecords := collectRecords(recs)
	children := collectRecords(aggRecs)

	require.NoError(t, err)
	require.False(t, interrupted)
	require.NotZero(t, blocks)
	require.Len(t, gotRecords, int(blocks))

	require.Len(t, children, 2)

	assertPDPv0ChildCID(t, subA, children[0])
	assertPDPv0ChildCID(t, subB, children[1])
}

func TestIndexPDPv0AggregateFailsWhenSegmentIsNotCAR(t *testing.T) {
	noCAR := makePiece(t, bytes.Repeat([]byte("this segment is not a CAR"), 6))
	carPiece := makeCARPiece(t, 384)
	aggregate := makeAggregatePiece(t, noCAR, carPiece)

	recs := make(chan indexstore.Record, 128)
	aggRecs := make(chan indexstore.Record, 128)
	blocks, interrupted, err := IndexPDPv0(aggregate.pieceCIDV2, bytes.NewReader(aggregate.raw), aggregate.pieceSize, recs, aggRecs, make(chan struct{}))
	close(recs)
	close(aggRecs)
	gotRecords := collectRecords(recs)
	gotAggRecords := collectRecords(aggRecs)

	require.Error(t, err)
	require.Contains(t, err.Error(), "indexing PDPv0 aggregate segment 0")
	require.NotContains(t, err.Error(), "fallback CAR indexing failed")
	require.False(t, interrupted)
	require.Zero(t, blocks)
	require.Empty(t, gotAggRecords)
	require.Empty(t, gotRecords)
}

func TestIndexPDPv0PlainNonCARFailsAfterFallback(t *testing.T) {
	piece := makePiece(t, bytes.Repeat([]byte("this whole piece is not a CAR"), 6))

	recs := make(chan indexstore.Record, 128)
	aggRecs := make(chan indexstore.Record, 128)
	blocks, interrupted, err := IndexPDPv0(piece.pieceCIDV2, bytes.NewReader(piece.raw), piece.pieceSize, recs, aggRecs, make(chan struct{}))
	close(recs)
	close(aggRecs)
	gotRecords := collectRecords(recs)
	gotAggRecords := collectRecords(aggRecs)

	require.Error(t, err)
	require.Contains(t, err.Error(), "fallback CAR indexing failed")
	require.False(t, interrupted)
	require.Zero(t, blocks)
	require.Empty(t, gotAggRecords)
	require.Empty(t, gotRecords)
}

func TestIndexPDPv0FallsBackOnlyForMissingDataSegmentIndex(t *testing.T) {
	t.Run("missing index falls back to whole CAR indexing", func(t *testing.T) {
		piece := makeCARPiece(t, 512)

		recs := make(chan indexstore.Record, 128)
		aggRecs := make(chan indexstore.Record, 128)
		blocks, interrupted, err := IndexPDPv0(piece.pieceCIDV2, bytes.NewReader(piece.raw), piece.pieceSize, recs, aggRecs, make(chan struct{}))
		close(recs)
		close(aggRecs)
		gotRecords := collectRecords(recs)
		gotAggRecords := collectRecords(aggRecs)

		require.NoError(t, err)
		require.False(t, interrupted)
		require.Empty(t, gotAggRecords)
		require.NotZero(t, blocks)
		require.Len(t, gotRecords, int(blocks))
	})

	t.Run("seek failure does not fall back", func(t *testing.T) {
		piece := makeCARPiece(t, 512)
		seekErr := errors.New("seek index tail")
		reader := &seekErrorReader{
			Reader:     bytes.NewReader(piece.raw),
			failOffset: int64(datasegment.DataSegmentIndexStartOffset(piece.pieceSize)),
			err:        seekErr,
		}

		recs := make(chan indexstore.Record, 128)
		aggRecs := make(chan indexstore.Record, 128)
		blocks, interrupted, err := IndexPDPv0(piece.pieceCIDV2, reader, piece.pieceSize, recs, aggRecs, make(chan struct{}))
		close(recs)
		close(aggRecs)
		gotRecords := collectRecords(recs)
		gotAggRecords := collectRecords(aggRecs)

		require.ErrorIs(t, err, seekErr)
		require.False(t, interrupted)
		require.Zero(t, blocks)
		require.Empty(t, gotAggRecords)
		require.Empty(t, gotRecords)
	})

	t.Run("read failure does not fall back", func(t *testing.T) {
		piece := makeCARPiece(t, 512)
		readErr := errors.New("read index tail")
		reader := &readErrorAtOffsetReader{
			data:       piece.raw,
			failOffset: int64(datasegment.DataSegmentIndexStartOffset(piece.pieceSize)),
			err:        readErr,
		}

		recs := make(chan indexstore.Record, 128)
		aggRecs := make(chan indexstore.Record, 128)
		blocks, interrupted, err := IndexPDPv0(piece.pieceCIDV2, reader, piece.pieceSize, recs, aggRecs, make(chan struct{}))
		close(recs)
		close(aggRecs)
		gotRecords := collectRecords(recs)
		gotAggRecords := collectRecords(aggRecs)

		require.ErrorIs(t, err, readErr)
		require.False(t, interrupted)
		require.Zero(t, blocks)
		require.Empty(t, gotAggRecords)
		require.Empty(t, gotRecords)
		require.Zero(t, reader.seekStartCount)
	})
}

type testPiece struct {
	raw        []byte
	pieceCIDV1 cid.Cid
	pieceCIDV2 cid.Cid
	pieceSize  abi.PaddedPieceSize
	rawSize    uint64
}

func makeCARPiece(t *testing.T, sourceSize int64) testPiece {
	t.Helper()

	dir := t.TempDir()
	srcPath, err := testutils.CreateRandomTmpFile(dir, sourceSize)
	require.NoError(t, err)

	_, carPath, err := testutils.CreateDenseCARWith(dir, srcPath, 64, 8, []carv2.Option{
		carblockstore.WriteAsCarV1(true),
	})
	require.NoError(t, err)

	carBytes, err := os.ReadFile(carPath)
	require.NoError(t, err)

	return makePiece(t, carBytes)
}

func makeAggregatePiece(t *testing.T, subPieces ...testPiece) testPiece {
	t.Helper()

	deals := make([]abi.PieceInfo, 0, len(subPieces))
	readers := make([]io.Reader, 0, len(subPieces))
	for _, sp := range subPieces {
		deals = append(deals, abi.PieceInfo{
			PieceCID: sp.pieceCIDV1,
			Size:     sp.pieceSize,
		})
		readers = append(readers, io.LimitReader(bytes.NewReader(sp.raw), int64(sp.rawSize)))
	}

	_, aggregateRawSize, err := datasegment.ComputeDealPlacement(deals)
	require.NoError(t, err)

	aggregateSize := abi.PaddedPieceSize(1 << (64 - bits.LeadingZeros64(aggregateRawSize+256)))
	aggregate, err := datasegment.NewAggregate(aggregateSize, deals)
	require.NoError(t, err)

	aggregateReader, err := aggregate.AggregateObjectReader(readers)
	require.NoError(t, err)

	aggregateBytes, err := io.ReadAll(aggregateReader)
	require.NoError(t, err)

	piece := makePiece(t, aggregateBytes)
	require.Equal(t, aggregateSize, piece.pieceSize)

	return piece
}

func makePiece(t *testing.T, raw []byte) testPiece {
	t.Helper()

	wr := new(commp.Calc)
	defer wr.Reset()

	n, err := wr.Write(raw)
	require.NoError(t, err)

	digest, paddedPieceSize, err := wr.Digest()
	require.NoError(t, err)

	pieceCIDV1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)

	pieceCIDV2, err := commcid.PieceCidV2FromV1(pieceCIDV1, uint64(n))
	require.NoError(t, err)

	return testPiece{
		raw:        raw,
		pieceCIDV1: pieceCIDV1,
		pieceCIDV2: pieceCIDV2,
		pieceSize:  abi.PaddedPieceSize(paddedPieceSize),
		rawSize:    uint64(n),
	}
}

func carRecords(t *testing.T, carBytes []byte) []indexstore.Record {
	t.Helper()

	blockReader, err := carv2.NewBlockReader(bytes.NewReader(carBytes), carv2.ZeroLengthSectionAsEOF(true))
	require.NoError(t, err)

	var records []indexstore.Record
	for {
		blockMetadata, err := blockReader.SkipNext()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		records = append(records, indexstore.Record{
			Cid:    blockMetadata.Cid,
			Offset: blockMetadata.SourceOffset,
			Size:   blockMetadata.Size,
		})
	}

	return records
}

func collectRecords(recs <-chan indexstore.Record) []indexstore.Record {
	var records []indexstore.Record
	for rec := range recs {
		records = append(records, rec)
	}
	return records
}

func assertPDPv0ChildCID(t *testing.T, subPiece testPiece, got indexstore.Record) {
	t.Helper()

	want, err := commcid.PieceCidV2FromV1(subPiece.pieceCIDV1, subPiece.rawSize)
	require.NoError(t, err)

	wrongFromSegmentLength, err := commcid.PieceCidV2FromV1(subPiece.pieceCIDV1, got.Size)
	require.NoError(t, err)

	require.True(t, want.Equals(got.Cid))
	require.NotEqual(t, subPiece.rawSize, got.Size)
	require.False(t, wrongFromSegmentLength.Equals(got.Cid))
}

type seekErrorReader struct {
	*bytes.Reader
	failOffset int64
	err        error
}

func (r *seekErrorReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart && offset == r.failOffset {
		return 0, r.err
	}
	return r.Reader.Seek(offset, whence)
}

type readErrorAtOffsetReader struct {
	data           []byte
	offset         int64
	failOffset     int64
	err            error
	seekStartCount int
}

func (r *readErrorAtOffsetReader) Read(p []byte) (int, error) {
	if r.offset == r.failOffset {
		return 0, r.err
	}
	if r.offset >= int64(len(r.data)) {
		return 0, io.EOF
	}

	n := copy(p, r.data[r.offset:])
	r.offset += int64(n)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *readErrorAtOffsetReader) ReadAt(p []byte, off int64) (int, error) {
	if off == r.failOffset {
		return 0, r.err
	}
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}

	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *readErrorAtOffsetReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.offset + offset
	case io.SeekEnd:
		next = int64(len(r.data)) + offset
	default:
		return 0, errors.New("bad whence")
	}

	if whence == io.SeekStart && offset == 0 {
		r.seekStartCount++
	}
	r.offset = next
	return r.offset, nil
}
