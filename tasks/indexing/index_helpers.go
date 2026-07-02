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
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-data-segment/fr32"
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
		return datasegment.IndexData{}, fmt.Errorf("unpadded data length %d is not a multiple of 127", len(unpaddedData))
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

func IndexCAR(r io.Reader, buffSize int, recs chan<- indexstore.Record, addFail <-chan struct{}) (int64, bool, error) {
	blockReader, err := carv2.NewBlockReader(bufio.NewReaderSize(r, buffSize), carv2.ZeroLengthSectionAsEOF(true))
	if err != nil {
		return 0, false, fmt.Errorf("getting block reader over piece: %w", err)
	}

	var blocks int64
	var interrupted bool

	for {
		blockMetadata, err := blockReader.SkipNext()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return blocks, interrupted, fmt.Errorf("generating index for piece: %w", err)
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

	return blocks, interrupted, nil
}

type IndexReader interface {
	io.ReaderAt
	io.Seeker
	io.Reader
}

func IndexAggregate(pieceCid cid.Cid,
	reader IndexReader,
	size abi.PaddedPieceSize,
	subPieces []mk20.DataSource,
	recs chan<- indexstore.Record,
	addFail <-chan struct{},
) (int64, map[cid.Cid][]indexstore.Record, bool, error) {
	dsis := datasegment.DataSegmentIndexStartOffset(size)
	if _, err := reader.Seek(int64(dsis), io.SeekStart); err != nil {
		return 0, nil, false, xerrors.Errorf("seeking to data segment index start offset: %w", err)
	}

	idata, err := parseDataSegmentIndex(reader)
	if err != nil {
		return 0, nil, false, xerrors.Errorf("parsing data segment index: %w", err)
	}
	if len(idata.Entries) == 0 {
		return 0, nil, false, xerrors.New("no data segment index entries")
	}

	valid := validateSegments(idata.Entries)
	if len(valid) == 0 {
		return 0, nil, false, xerrors.New("no valid data segment index entries")
	}

	aggidx := make(map[cid.Cid][]indexstore.Record)

	log.Infow("Indexing aggregate", "piece_size", size, "num_chunks", len(valid), "num_sub_pieces", len(subPieces))

	if len(subPieces) > 1 {
		if len(valid) != len(subPieces) {
			return 0, nil, false, xerrors.Errorf("expected %d data segment index entries, got %d", len(subPieces), len(idata.Entries))
		}
	} else {
		return 0, nil, false, xerrors.Errorf("expected at least 2 sub pieces, got 0")
	}

	var totalBlocks int64
	for j, entry := range valid {
		bufferSize := 4 << 20
		if entry.Size < uint64(bufferSize) {
			bufferSize = int(entry.Size)
		}
		strt := entry.UnpaddedOffest()
		leng := entry.UnpaddedLength()
		sectionReader := io.NewSectionReader(reader, int64(strt), int64(leng))
		sp := subPieces[j]

		if sp.Format.Car != nil {
			b, inter, err := IndexCAR(sectionReader, bufferSize, recs, addFail)
			if err != nil {
				//// Allow one more layer of aggregation to be indexed
				//if strings.Contains(err.Error(), "invalid car version") {
				//	if haveSubPieces {
				//		if subPieces[j].Car != nil {
				//			return 0, aggidx, false, xerrors.Errorf("invalid car version for subPiece %d: %w", j, err)
				//		}
				//		if subPieces[j].Raw != nil {
				//			continue
				//		}
				//		if subPieces[j].Aggregate != nil {
				//			b, idx, inter, err = IndexAggregate(commp.PCidV2(), sectionReader, abi.PaddedPieceSize(entry.Size), nil, recs, addFail)
				//			if err != nil {
				//				return totalBlocks, aggidx, inter, xerrors.Errorf("invalid aggregate for subPiece %d: %w", j, err)
				//			}
				//			totalBlocks += b
				//			for k, v := range idx {
				//				aggidx[k] = append(aggidx[k], v...)
				//			}
				//		}
				//	} else {
				//		continue
				//	}
				//}
				return totalBlocks, aggidx, false, xerrors.Errorf("indexing subPiece %d: %w", j, err)
			}

			if inter {
				return totalBlocks, aggidx, true, nil
			}
			totalBlocks += b
		}

		aggidx[pieceCid] = append(aggidx[pieceCid], indexstore.Record{
			Cid:    sp.PieceCID,
			Offset: strt,
			Size:   leng,
		})
	}

	return totalBlocks, aggidx, false, nil
}
