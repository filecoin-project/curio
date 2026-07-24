package indexing

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"sync"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/multiformats/go-varint"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-data-segment/fr32"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
)

func parseDataSegmentIndex(unpaddedReader io.Reader) (datasegment.IndexData, error) {
	const (
		unpaddedChunk = 127
		paddedChunk   = 128
	)

	// Read all unpadded data (up to 32 MiB Max as per FRC for 64 GiB sector)
	unpaddedData, err := io.ReadAll(unpaddedReader)
	if err != nil {
		return datasegment.IndexData{}, xerrors.Errorf("reading unpadded data: %w", err)
	}

	// Make sure it's aligned to 127
	if len(unpaddedData)%unpaddedChunk != 0 {
		return datasegment.IndexData{}, xerrors.Errorf("%w: unpadded data length %d is not a multiple of 127", errNoDataSegmentIndex, len(unpaddedData))
	}
	numChunks := len(unpaddedData) / unpaddedChunk

	// Prepare padded output buffer
	paddedData := make([]byte, numChunks*paddedChunk)

	// Parallel pad
	var wg sync.WaitGroup
	concurrency := runtime.NumCPU()
	chunkPerWorker := (numChunks + concurrency - 1) / concurrency

	for w := range concurrency {
		start := w * chunkPerWorker
		end := min((w+1)*chunkPerWorker, numChunks)
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				in := unpaddedData[i*unpaddedChunk : (i+1)*unpaddedChunk]
				out := paddedData[i*paddedChunk : (i+1)*paddedChunk]
				fr32.Pad(in, out)
			}
		}(start, end)
	}
	wg.Wait()

	// Decode entries
	allEntries := make([]datasegment.SegmentDesc, numChunks*2)
	for i := range numChunks {
		p := paddedData[i*paddedChunk : (i+1)*paddedChunk]

		if err := allEntries[i*2+0].UnmarshalBinary(p[:datasegment.EntrySize]); err != nil {
			return datasegment.IndexData{}, xerrors.Errorf("unmarshal entry 1 at chunk %d: %w", i, err)
		}
		if err := allEntries[i*2+1].UnmarshalBinary(p[datasegment.EntrySize:]); err != nil {
			return datasegment.IndexData{}, xerrors.Errorf("unmarshal entry 2 at chunk %d: %w", i, err)
		}
	}

	return datasegment.IndexData{Entries: allEntries}, nil
}

func validateSegments(segments []datasegment.SegmentDesc) []datasegment.SegmentDesc {
	entryCount := len(segments)

	validCh := make(chan datasegment.SegmentDesc, entryCount)
	var wg sync.WaitGroup

	workers := runtime.NumCPU()
	chunkSize := (entryCount + workers - 1) / workers

	for w := range workers {
		start := w * chunkSize
		end := min((w+1)*chunkSize, entryCount)
		if start >= end {
			break
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				entry := segments[i]
				if err := entry.Validate(); err == nil {
					validCh <- entry
				}
				log.Debugw("data segment invalid", "segment", entry)
			}
		}(start, end)
	}

	go func() {
		wg.Wait()
		close(validCh)
	}()

	var validEntries []datasegment.SegmentDesc
	for entry := range validCh {
		validEntries = append(validEntries, entry)
	}
	sort.Slice(validEntries, func(i, j int) bool {
		return validEntries[i].Offset < validEntries[j].Offset
	})
	return validEntries
}

// IndexCAR streams CAR block records into recs.
//
// It returns the raw CAR length observed while walking block sections, the
// number of indexed blocks, and whether indexing was interrupted because the
// record sink failed. The raw length is derived from the CAR block layout, not
// from caller-provided piece metadata.
func IndexCAR(r IndexReader, buffSize int, recs chan<- indexstore.Record, addFail <-chan struct{}) (uint64, int64, bool, error) {
	blockReader, err := carv2.NewBlockReader(bufio.NewReaderSize(r, buffSize), carv2.ZeroLengthSectionAsEOF(true))
	if err != nil {
		return 0, 0, false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	var blocks int64
	var interrupted, head bool
	var combinedSize, length uint64

	for {
		blockMetadata, err := blockReader.SkipNext()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, blocks, interrupted, fmt.Errorf("generating index for piece: %w", err)
		}
		if !head {
			// Read the first block to know the header size
			// CAR sections are [varint (length), CID, blockData]
			combinedSize = blockMetadata.Size + uint64(blockMetadata.ByteLen())
			lenSize := uint64(varint.UvarintSize(combinedSize))
			sectionSize := combinedSize + lenSize
			length = blockMetadata.SourceOffset + sectionSize
			head = true
		} else {
			combinedSize = blockMetadata.Size + uint64(blockMetadata.ByteLen())
			lenSize := uint64(varint.UvarintSize(combinedSize))
			sectionSize := combinedSize + lenSize
			length += sectionSize
		}

		blocks++
		select {
		case recs <- indexstore.Record{
			Cid:    blockMetadata.Cid,
			Offset: blockMetadata.SourceOffset,
			Size:   blockMetadata.Size,
		}:
		case <-addFail:
			interrupted = true
		}

		if interrupted {
			break
		}
	}

	return length, blocks, interrupted, nil
}

type IndexReader interface {
	io.ReaderAt
	io.Seeker
	io.Reader
}

// IndexAggregate indexes an mk20 aggregate using caller-supplied subpiece
// metadata as the authority for child piece CIDs.
//
// The data segment index must parse successfully for this path; mk20 callers
// already know the piece is an aggregate, so parse failures are returned rather
// than falling back to whole-piece CAR indexing.
func IndexAggregate(pieceCid cid.Cid,
	reader IndexReader,
	size abi.PaddedPieceSize,
	subPieces []mk20.DataSource,
	recs chan<- indexstore.Record,
	addFail <-chan struct{},
) (int64, map[cid.Cid][]indexstore.Record, bool, error) {

	valid, err := parseDataSegments(reader, size)
	if err != nil {
		return 0, nil, false, err
	}

	log.Infow("Indexing aggregate", "piece_size", size, "num_chunks", len(valid), "num_sub_pieces", len(subPieces))

	if len(subPieces) > 1 {
		if len(valid) != len(subPieces) {
			return 0, nil, false, xerrors.Errorf("expected %d data segment index entries, got %d", len(subPieces), len(valid))
		}
	} else {
		return 0, nil, false, xerrors.Errorf("expected at least 2 sub pieces, got 0")
	}

	return indexSegments(pieceCid, reader, valid, func(j int, _ datasegment.SegmentDesc, sectionReader *io.SectionReader, bufferSize int) (cid.Cid, int64, bool, error) {
		sp := subPieces[j]
		if sp.Format.Car == nil {
			return sp.PieceCID, 0, false, nil
		}

		_, b, inter, err := IndexCAR(sectionReader, bufferSize, recs, addFail)
		if err != nil {
			return cid.Undef, b, false, xerrors.Errorf("indexing subPiece %d: %w", j, err)
		}

		return sp.PieceCID, b, inter, nil
	})
}

// IndexPDPv0 indexes PDPv0 pieces that may be either data-segment aggregates
// or plain CARs.
//
// For aggregate pieces, child piece CIDv2 values are built from each segment's
// PieceCIDv1 plus the raw CAR length returned by IndexCAR for that segment.
// Whole-piece CAR fallback is only used when the data segment index is absent
// or unusable as an index. Reader/seek failures while trying to read the index
// are treated as real errors and do not fall back.
func IndexPDPv0(pieceCid cid.Cid,
	reader IndexReader,
	size abi.PaddedPieceSize,
	recs chan<- indexstore.Record,
	addFail <-chan struct{},
) (int64, map[cid.Cid][]indexstore.Record, bool, error) {
	valid, err := parseDataSegments(reader, size)
	if err != nil && !errors.Is(err, errNoDataSegmentIndex) {
		return 0, nil, false, err
	}
	if err == nil {
		log.Infow("Indexing PDPv0 aggregate", "piece_cid", pieceCid, "piece_size", size, "num_chunks", len(valid))

		return indexSegments(pieceCid, reader, valid, func(j int, entry datasegment.SegmentDesc, sectionReader *io.SectionReader, bufferSize int) (cid.Cid, int64, bool, error) {
			rawSize, b, inter, err := IndexCAR(sectionReader, bufferSize, recs, addFail)
			if err != nil {
				return cid.Undef, b, false, xerrors.Errorf("indexing PDPv0 aggregate segment %d: %w", j, err)
			}

			if inter {
				return cid.Undef, b, true, nil
			}

			subPieceCIDV2, err := commcid.PieceCidV2FromV1(entry.PieceCID(), rawSize)
			if err != nil {
				return cid.Undef, b, false, xerrors.Errorf("converting PDPv0 aggregate segment %d piece CID to v2: %w", j, err)
			}

			return subPieceCIDV2, b, false, nil
		})
	}

	log.Debugw("PDPv0 aggregate indexing failed, falling back to CAR indexing", "piece_cid", pieceCid, "piece_size", size, "error", err)

	if _, seekErr := reader.Seek(0, io.SeekStart); seekErr != nil {
		return 0, nil, false, xerrors.Errorf("seeking to piece start after PDPv0 aggregate indexing failed: %w", seekErr)
	}

	_, carBlocks, carInterrupted, carErr := IndexCAR(reader, 4<<20, recs, addFail)
	if carErr != nil {
		return carBlocks, nil, carInterrupted, xerrors.Errorf("PDPv0 aggregate indexing failed (%v); fallback CAR indexing failed: %w", err, carErr)
	}

	return carBlocks, nil, carInterrupted, nil
}

var errNoDataSegmentIndex = errors.New("no data segment index")

// segmentIndexer contains the format-specific work for a validated data
// segment. It returns the child piece CID to store in the aggregate index, the
// number of payload blocks indexed from the segment, and whether indexing was
// interrupted.
type segmentIndexer func(
	j int,
	entry datasegment.SegmentDesc,
	sectionReader *io.SectionReader,
	bufferSize int,
) (cid.Cid, int64, bool, error)

// parseDataSegments reads and validates the data segment index from the tail of
// a piece. It returns errNoDataSegmentIndex only when bytes were readable but no
// usable segment index was present.
func parseDataSegments(reader IndexReader, size abi.PaddedPieceSize) ([]datasegment.SegmentDesc, error) {
	dsis := datasegment.DataSegmentIndexStartOffset(size)
	if _, err := reader.Seek(int64(dsis), io.SeekStart); err != nil {
		return nil, xerrors.Errorf("seeking to data segment index start offset: %w", err)
	}

	idata, err := parseDataSegmentIndex(reader)
	if err != nil {
		return nil, xerrors.Errorf("parsing data segment index: %w", err)
	}
	if len(idata.Entries) == 0 {
		return nil, xerrors.Errorf("%w: no data segment index entries", errNoDataSegmentIndex)
	}

	valid := validateSegments(idata.Entries)
	if len(valid) == 0 {
		return nil, xerrors.Errorf("%w: no valid data segment index entries", errNoDataSegmentIndex)
	}

	return valid, nil
}

// indexSegments walks validated data segment entries and records aggregate
// child mappings. The caller-provided segmentIndexer supplies the child CID and
// handles any segment payload indexing needed for the specific aggregate type.
func indexSegments(
	pieceCid cid.Cid,
	reader IndexReader,
	valid []datasegment.SegmentDesc,
	indexSegment segmentIndexer) (int64, map[cid.Cid][]indexstore.Record, bool, error) {
	var totalBlocks int64
	aggidx := make(map[cid.Cid][]indexstore.Record)
	for j, entry := range valid {
		bufferSize := 4 << 20
		if entry.Size < uint64(bufferSize) {
			bufferSize = int(entry.Size)
		}
		strt := entry.UnpaddedOffest()
		leng := entry.UnpaddedLength()
		sectionReader := io.NewSectionReader(reader, int64(strt), int64(leng))

		subPieceCID, b, inter, err := indexSegment(j, entry, sectionReader, bufferSize)
		if err != nil {
			return totalBlocks, aggidx, false, err
		}
		if inter {
			return totalBlocks, aggidx, true, nil
		}
		totalBlocks += b

		aggidx[pieceCid] = append(aggidx[pieceCid], indexstore.Record{
			Cid:    subPieceCID,
			Offset: strt,
			Size:   leng,
		})
	}

	return totalBlocks, aggidx, false, nil
}
