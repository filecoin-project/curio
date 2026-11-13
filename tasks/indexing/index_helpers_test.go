package indexing

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"math/bits"
	"os"
	"testing"
	"testing/synctest"

	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/go-cid"
	ipldformat "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"github.com/ipld/go-car/util"
	carv2 "github.com/ipld/go-car/v2"
	carblockstore "github.com/ipld/go-car/v2/blockstore"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-data-segment/datasegment"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/pdp/pieceacl"
)

func TestAggregateWriterFailureUnblocksIndexing(t *testing.T) {
	subPiece := makeCARPiece(t, 384)
	aggregate := makeAggregatePiece(t, subPiece, subPiece, subPiece, subPiece)
	subPieces := make([]mk20.DataSource, 4)
	for i := range subPieces {
		subPieces[i] = mk20.DataSource{
			PieceCID: subPiece.pieceCIDV2,
			Format:   mk20.PieceDataFormat{Car: &mk20.FormatCar{}},
		}
	}

	tests := []struct {
		name  string
		pdpv0 bool
	}{
		{name: "MK20 and PDPv1", pdpv0: false},
		{name: "PDPv0", pdpv0: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				recs := make(chan indexstore.Record, 1)
				aggRecs := make(chan indexstore.Record, 1)
				abort := make(chan struct{})
				writeErr := errors.New("aggregate index write failed")
				payloadRecords := 0
				eg, addFail := startIndexWriters(func() error {
					for {
						select {
						case _, ok := <-recs:
							if !ok {
								return nil
							}
							payloadRecords++
						case <-abort:
							return nil
						}
					}
				}, func() error {
					<-aggRecs
					return writeErr
				})

				done := make(chan struct{})
				var interrupted bool
				var indexErr, waitErr error
				go func() {
					reader := bytes.NewReader(aggregate.raw)
					if tt.pdpv0 {
						_, interrupted, indexErr = IndexPDPv0(aggregate.pieceCIDV2, reader, aggregate.pieceSize, recs, aggRecs, addFail)
					} else {
						_, interrupted, indexErr = IndexAggregate(aggregate.pieceCIDV2, reader, aggregate.pieceSize, subPieces, recs, aggRecs, addFail)
					}
					close(recs)
					close(aggRecs)
					waitErr = eg.Wait()
					close(done)
				}()

				synctest.Wait()
				select {
				case <-done:
				default:
					// Release and join the blocked producer before failing the test.
					close(abort)
					<-done
					t.Fatal("indexing remained blocked after the aggregate writer failed")
				}
				require.NoError(t, indexErr)
				require.True(t, interrupted)
				require.ErrorIs(t, waitErr, writeErr)
				require.Positive(t, payloadRecords)
			})
		})
	}
}

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

func TestIndexCARIndexesPrivatePieceFileCID(t *testing.T) {
	payload := []byte("encrypted-fee-payload")
	sum, err := mh.Sum(payload, mh.SHA2_256, -1)
	require.NoError(t, err)
	leafCID := cid.NewCidV1(cid.Raw, sum)
	leafData := payload
	fsn := unixfs.NewFSNode(unixfs.TFile)
	fsn.AddBlockSize(uint64(len(payload)))
	fsBytes, err := fsn.GetBytes()
	require.NoError(t, err)
	file := new(merkledag.ProtoNode)
	require.NoError(t, file.SetCidBuilder(merkledag.V1CidPrefix()))
	file.SetData(fsBytes)
	require.NoError(t, file.AddRawLink("", &ipldformat.Link{Name: "", Size: uint64(len(payload)), Cid: leafCID}))
	fileCID, fileData := file.Cid(), file.RawData()

	dirNode := unixfs.NewFSNode(unixfs.TDirectory)
	dirBytes, err := dirNode.GetBytes()
	require.NoError(t, err)
	dir := new(merkledag.ProtoNode)
	require.NoError(t, dir.SetCidBuilder(merkledag.V1CidPrefix()))
	dir.SetData(dirBytes)
	require.NoError(t, dir.AddRawLink("encrypted.fee", &ipldformat.Link{Name: "encrypted.fee", Size: uint64(len(fileData) + len(leafData)), Cid: fileCID}))
	dirCID, dirData := dir.Cid(), dir.RawData()
	require.False(t, fileCID.Equals(dirCID))
	require.False(t, fileCID.Equals(leafCID))

	t.Run("directory root", func(t *testing.T) {
		raw := writePrivateCAR(t, dirCID, []privateCARBlock{
			{dirCID, dirData},
			{fileCID, fileData},
			{leafCID, leafData},
		})
		fileRec := recordByCID(t, indexCARRecords(t, raw), fileCID)
		require.Equal(t, uint64(len(fileData)), fileRec.Size)
	})

	t.Run("file root", func(t *testing.T) {
		raw := writePrivateCAR(t, fileCID, []privateCARBlock{
			{fileCID, fileData},
			{leafCID, leafData},
		})
		fileRec := recordByCID(t, indexCARRecords(t, raw), fileCID)
		require.Equal(t, uint64(len(fileData)), fileRec.Size)
	})
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

func TestSkipSubpieceIndex(t *testing.T) {
	require.False(t, skipSubpieceIndex(true, true, true))
	require.False(t, skipSubpieceIndex(true, false, false))
	require.True(t, skipSubpieceIndex(false, true, false))
	require.False(t, skipSubpieceIndex(false, false, false))
	require.True(t, skipSubpieceIndex(false, false, true))
}

func TestEveryACLRestricted(t *testing.T) {
	secured := pieceacl.Link{ID: 1, ACL: sql.NullInt32{Int32: pieceacl.Secured, Valid: true}}
	public0 := pieceacl.Link{ID: 2, ACL: sql.NullInt32{Int32: 0, Valid: true}}
	unresolved := pieceacl.Link{ID: 3, ACL: sql.NullInt32{Valid: false}}

	require.False(t, pieceacl.EverySecured(nil))
	require.True(t, pieceacl.EverySecured([]pieceacl.Link{secured}))
	require.True(t, pieceacl.EverySecured([]pieceacl.Link{secured, secured}))
	require.False(t, pieceacl.EverySecured([]pieceacl.Link{unresolved}))
	require.False(t, pieceacl.EverySecured([]pieceacl.Link{public0}))
	require.False(t, pieceacl.EverySecured([]pieceacl.Link{secured, unresolved}))
}

func TestIndexPDPv0AggregateIndexesBlocksWithoutChildPieceCIDs(t *testing.T) {
	subA := makeCARPiece(t, 384)
	subB := makeCARPiece(t, 768)
	aggregate := makeAggregatePiece(t, subA, subB)

	recs := make(chan indexstore.Record, 128)
	blocks, interrupted, err := IndexPDPv0(aggregate.pieceCIDV2, bytes.NewReader(aggregate.raw), aggregate.pieceSize, recs, nil, make(chan struct{}))
	close(recs)
	gotRecords := collectRecords(recs)

	require.NoError(t, err)
	require.False(t, interrupted)
	require.NotZero(t, blocks)
	require.Len(t, gotRecords, int(blocks))
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

func indexCARRecords(t *testing.T, raw []byte) []indexstore.Record {
	t.Helper()
	recs := make(chan indexstore.Record, 16)
	_, _, interrupted, err := IndexCAR(bytes.NewReader(raw), 4<<20, recs, make(chan struct{}))
	close(recs)
	got := collectRecords(recs)
	require.NoError(t, err)
	require.False(t, interrupted)
	return got
}

func recordByCID(t *testing.T, recs []indexstore.Record, id cid.Cid) indexstore.Record {
	t.Helper()
	for _, rec := range recs {
		if rec.Cid.Equals(id) {
			return rec
		}
	}
	t.Fatalf("index is missing %s", id)
	return indexstore.Record{}
}

type privateCARBlock struct {
	id   cid.Cid
	data []byte
}

func writePrivateCAR(t *testing.T, root cid.Cid, blocks []privateCARBlock) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, car.WriteHeader(&car.CarHeader{Roots: []cid.Cid{root}, Version: 1}, &buf))
	for _, block := range blocks {
		require.NoError(t, util.LdWrite(&buf, block.id.Bytes(), block.data))
	}
	return buf.Bytes()
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
