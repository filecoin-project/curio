package indexing

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	"github.com/filecoin-project/go-data-segment/fr32"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
)

var log = logging.Logger("indexing")

type IndexingTask struct {
	db                *harmonydb.DB
	indexStore        *indexstore.IndexStore
	pieceProvider     *pieceprovider.SectorReader
	cpr               *cachedreader.CachedPieceReader
	sc                *ffi.SealCalls
	cfg               *config.CurioConfig
	insertConcurrency int
	insertBatchSize   int
	max               taskhelp.Limiter
}

func NewIndexingTask(db *harmonydb.DB, sc *ffi.SealCalls, indexStore *indexstore.IndexStore, pieceProvider *pieceprovider.SectorReader, cpr *cachedreader.CachedPieceReader, cfg *config.CurioConfig, max taskhelp.Limiter) *IndexingTask {

	return &IndexingTask{
		db:                db,
		indexStore:        indexStore,
		pieceProvider:     pieceProvider,
		cpr:               cpr,
		sc:                sc,
		cfg:               cfg,
		insertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		insertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
		max:               max,
	}
}

type itask struct {
	UUID              string                  `db:"uuid"`
	SpID              int64                   `db:"sp_id"`
	Sector            abi.SectorNumber        `db:"sector"`
	Proof             abi.RegisteredSealProof `db:"reg_seal_proof"`
	PieceCid          string                  `db:"piece_cid"`
	Size              abi.PaddedPieceSize     `db:"piece_size"`
	Offset            int64                   `db:"sector_offset"`
	RawSize           int64                   `db:"raw_size"`
	Url               sql.NullString          `db:"url"`
	ShouldIndex       bool                    `db:"should_index"`
	IndexingCreatedAt time.Time               `db:"indexing_created_at"`
	Announce          bool                    `db:"announce"`
	ChainDealId       abi.DealID              `db:"chain_deal_id"`
	IsDDO             bool                    `db:"is_ddo"`
	Mk20              bool                    `db:"mk20"`
	IsRM              bool                    `db:"is_rm"`
	PieceRef          int64
}

func (i *IndexingTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	var tasks []itask

	ctx := context.Background()

	err = i.db.Select(ctx, &tasks, `SELECT 
										  p.uuid, 
										  p.sp_id, 
										  p.sector,
										  p.piece_cid, 
										  p.piece_size, 
										  p.sector_offset,
										  p.reg_seal_proof,
										  p.raw_size,
										  p.url,
										  p.should_index,
										  p.announce,
										  p.is_ddo,
										  COALESCE(d.chain_deal_id, 0) AS chain_deal_id,
										  FALSE AS mk20
										FROM 
										  market_mk12_deal_pipeline p
										LEFT JOIN 
										  market_mk12_deals d 
										  ON p.uuid = d.uuid AND p.sp_id = d.sp_id
										LEFT JOIN 
										  market_direct_deals md 
										  ON p.uuid = md.uuid AND p.sp_id = md.sp_id
										WHERE 
										  p.indexing_task_id = $1
										
										UNION ALL
										
										SELECT 
										  id AS uuid,
										  sp_id,
										  sector,
										  piece_cid,
										  piece_size,
										  sector_offset,
										  reg_seal_proof,
										  raw_size,
										  url,
										  indexing as should_index,
										  announce,
										  TRUE AS is_ddo,
										  0 AS chain_deal_id,
										  TRUE AS mk20
										FROM 
										  market_mk20_pipeline p
										WHERE 
										  p.indexing_task_id = $1;
										`, taskID)
	if err != nil {
		return false, xerrors.Errorf("getting indexing params: %w", err)
	}

	if len(tasks) != 1 {
		return false, xerrors.Errorf("expected 1 sector params, got %d", len(tasks))
	}

	task := tasks[0]

	// Check if piece is already indexed
	var indexed bool
	err = i.db.QueryRow(ctx, `SELECT indexed FROM market_piece_metadata WHERE piece_cid = $1 and piece_size = $2`, task.PieceCid, task.Size).Scan(&indexed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, xerrors.Errorf("checking if piece %s is already indexed: %w", task.PieceCid, err)
	}

	var byteData bool
	var subPieces []mk20.DataSource

	if task.Mk20 {
		id, err := ulid.Parse(task.UUID)
		if err != nil {
			return false, xerrors.Errorf("parsing id: %w", err)
		}
		deal, err := mk20.DealFromDB(ctx, i.db, id)
		if err != nil {
			return false, xerrors.Errorf("getting mk20 deal from DB: %w", err)
		}
		if deal.Data.Format.Aggregate != nil {
			if deal.Data.Format.Aggregate.Type > 0 {
				var found bool
				if len(deal.Data.Format.Aggregate.Sub) > 0 {
					subPieces = deal.Data.Format.Aggregate.Sub
					found = true
				}
				if len(deal.Data.SourceAggregate.Pieces) > 0 {
					subPieces = deal.Data.SourceAggregate.Pieces
					found = true
				}
				if !found {
					return false, xerrors.Errorf("no sub pieces for aggregate mk20 deal")
				}
			}
		}

		if deal.Data.Format.Raw != nil {
			byteData = true
		}

		if !task.Url.Valid {
			return false, xerrors.Errorf("no url for mk20 deal")
		}

		url, err := url.Parse(task.Url.String)
		if err != nil {
			return false, xerrors.Errorf("parsing url: %w", err)
		}

		if url.Scheme != "pieceref" {
			return false, xerrors.Errorf("invalid url scheme: %s", url.Scheme)
		}

		refNum, err := strconv.ParseInt(url.Opaque, 10, 64)
		if err != nil {
			return false, xerrors.Errorf("parsing piece reference number: %w", err)
		}

		task.PieceRef = refNum
	}

	// Return early if already indexed or should not be indexed
	if indexed || !task.ShouldIndex || byteData {
		err = i.recordCompletion(ctx, task, taskID, false)
		if err != nil {
			return false, err
		}
		log.Infow("Piece already indexed or should not be indexed", "piece_cid", task.PieceCid, "indexed", indexed, "should_index", task.ShouldIndex, "id", task.UUID, "sp_id", task.SpID, "sector", task.Sector)

		return true, nil
	}

	pieceCid, err := cid.Parse(task.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("parsing piece CID: %w", err)
	}

	pc2, err := commcidv2.PieceCidV2FromV1(pieceCid, uint64(task.RawSize))
	if err != nil {
		return false, xerrors.Errorf("getting piece commP: %w", err)
	}

	var reader storiface.Reader

	if task.Mk20 {
		reader, _, err = i.cpr.GetSharedPieceReader(ctx, pc2)

		if err != nil {
			return false, xerrors.Errorf("getting piece reader: %w", err)
		}
	} else {
		reader, err = i.pieceProvider.ReadPiece(ctx, storiface.SectorRef{
			ID: abi.SectorID{
				Miner:  abi.ActorID(task.SpID),
				Number: task.Sector,
			},
			ProofType: task.Proof,
		}, storiface.PaddedByteIndex(task.Offset).Unpadded(), task.Size.Unpadded(), pieceCid)

		if err != nil {
			return false, xerrors.Errorf("getting piece reader: %w", err)
		}
	}

	defer func() {
		_ = reader.Close()
	}()

	startTime := time.Now()

	dealCfg := i.cfg.Market.StorageMarketConfig
	chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize

	recs := make(chan indexstore.Record, chanSize)
	var blocks int64

	var eg errgroup.Group
	addFail := make(chan struct{})
	var interrupted bool

	eg.Go(func() error {
		defer close(addFail)
		return i.indexStore.AddIndex(ctx, pc2, recs)
	})

	var aggidx map[cid.Cid][]indexstore.Record

	if task.Mk20 && len(subPieces) > 0 {
		blocks, aggidx, interrupted, err = IndexAggregate(pc2, reader, task.Size, subPieces, recs, addFail)
	} else {
		blocks, interrupted, err = IndexCAR(reader, 4<<20, recs, addFail)
	}

	if err != nil {
		// Indexing itself failed, stop early
		close(recs) // still safe to close, AddIndex will exit on channel close
		// wait for AddIndex goroutine to finish cleanly
		_ = eg.Wait()
		return false, xerrors.Errorf("indexing failed: %w", err)
	}

	// Close the channel
	close(recs)

	// Wait till AddIndex is finished
	err = eg.Wait()
	if err != nil {
		return false, xerrors.Errorf("adding index to DB (interrupted %t): %w", interrupted, err)
	}

	log.Infof("Indexing deal %s took %0.3f seconds", task.UUID, time.Since(startTime).Seconds())

	// Save aggregate index if present
	for k, v := range aggidx {
		if len(v) > 0 {
			err = i.indexStore.InsertAggregateIndex(ctx, k, v)
			if err != nil {
				return false, xerrors.Errorf("inserting aggregate index: %w", err)
			}
		}
	}

	err = i.recordCompletion(ctx, task, taskID, true)
	if err != nil {
		return false, err
	}

	blocksPerSecond := float64(blocks) / time.Since(startTime).Seconds()
	log.Infow("Piece indexed", "piece_cid", task.PieceCid, "id", task.UUID, "sp_id", task.SpID, "sector", task.Sector, "blocks", blocks, "blocks_per_second", blocksPerSecond)

	return true, nil
}

// parseDataSegmentIndex is a local more efficient alternative to the method provided by the datasegment library
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

	for w := 0; w < concurrency; w++ {
		start := w * chunkPerWorker
		end := (w + 1) * chunkPerWorker
		if end > numChunks {
			end = numChunks
		}
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
	for i := 0; i < numChunks; i++ {
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

	for w := 0; w < workers; w++ {
		start := w * chunkSize
		end := (w + 1) * chunkSize
		if end > entryCount {
			end = entryCount
		}
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

// recordCompletion add the piece metadata and piece deal to the DB and
// records the completion of an indexing task in the database
func (i *IndexingTask) recordCompletion(ctx context.Context, task itask, taskID harmonytask.TaskID, indexed bool) error {
	if task.Mk20 {
		_, err := i.db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			task.UUID, task.PieceCid, !task.IsDDO, task.SpID, task.Sector, task.Offset, task.Size, task.RawSize, indexed, task.PieceRef, false, task.ChainDealId)
		if err != nil {
			return xerrors.Errorf("failed to update piece metadata and piece deal for deal %s: %w", task.UUID, err)
		}
	} else {
		_, err := i.db.Exec(ctx, `SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			task.UUID, task.PieceCid, !task.IsDDO, task.SpID, task.Sector, task.Offset, task.Size, task.RawSize, indexed, nil, false, task.ChainDealId)
		if err != nil {
			return xerrors.Errorf("failed to update piece metadata and piece deal for deal %s: %w", task.UUID, err)
		}
	}

	// If IPNI is disabled then mark deal as complete otherwise just mark as indexed
	if i.cfg.Market.StorageMarketConfig.IPNI.Disable {
		if task.Mk20 {
			n, err := i.db.Exec(ctx, `UPDATE market_mk20_pipeline SET indexed = TRUE, indexing_task_id = NULL, 
                                     complete = TRUE WHERE id = $1 AND indexing_task_id = $2`, task.UUID, taskID)
			if err != nil {
				return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
			}
			if n != 1 {
				return xerrors.Errorf("store indexing success: updated %d rows", n)
			}
		} else {
			n, err := i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE, indexing_task_id = NULL, 
                                     complete = TRUE WHERE uuid = $1 AND indexing_task_id = $2`, task.UUID, taskID)
			if err != nil {
				return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
			}
			if n != 1 {
				return xerrors.Errorf("store indexing success: updated %d rows", n)
			}
		}
	} else {
		if task.Mk20 {
			n, err := i.db.Exec(ctx, `UPDATE market_mk20_pipeline SET indexed = TRUE, indexing_task_id = NULL 
                                 WHERE id = $1 AND indexing_task_id = $2`, task.UUID, taskID)
			if err != nil {
				return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
			}
			if n != 1 {
				return xerrors.Errorf("store indexing success: updated %d rows", n)
			}
		} else {
			n, err := i.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET indexed = TRUE, indexing_task_id = NULL 
                                 WHERE uuid = $1 AND indexing_task_id = $2`, task.UUID, taskID)
			if err != nil {
				return xerrors.Errorf("store indexing success: updating pipeline: %w", err)
			}
			if n != 1 {
				return xerrors.Errorf("store indexing success: updated %d rows", n)
			}
		}
	}

	return nil
}

func (i *IndexingTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	ctx := context.Background()

	type task struct {
		TaskID       harmonytask.TaskID `db:"indexing_task_id"`
		SpID         int64              `db:"sp_id"`
		SectorNumber int64              `db:"sector"`
		StorageID    string             `db:"storage_id"`
		Url          string             `db:"url"`
		Indexing     bool               `db:"indexing"`
	}

	var tasks []*task

	indIDs := make([]int64, len(ids))
	for x, id := range ids {
		indIDs[x] = int64(id)
	}

	var mk20tasks []*task
	if storiface.FTPiece != 32 {
		panic("storiface.FTPiece != 32")
	}

	err := i.db.Select(ctx, &mk20tasks, `SELECT indexing_task_id, url, indexing FROM market_mk20_pipeline WHERE indexing_task_id = ANY($1)`, indIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting mk20 urls: %w", err)
	}

	for _, t := range mk20tasks {

		if !t.Indexing {
			continue
		}

		goUrl, err := url.Parse(t.Url)
		if err != nil {
			return nil, xerrors.Errorf("parsing data URL: %w", err)
		}
		if goUrl.Scheme == "pieceref" {
			refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
			if err != nil {
				return nil, xerrors.Errorf("parsing piece reference number: %w", err)
			}

			// get pieceID
			var pieceID []struct {
				PieceID storiface.PieceNumber `db:"piece_id"`
			}
			err = i.db.Select(ctx, &pieceID, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, refNum)
			if err != nil {
				return nil, xerrors.Errorf("getting pieceID: %w", err)
			}

			var sLocation string

			err = i.db.QueryRow(ctx, `
					SELECT storage_id FROM sector_location 
						WHERE miner_id = 0 AND sector_num = $1 AND sector_filetype = 32`, pieceID[0].PieceID).Scan(&sLocation)

			if err != nil {
				return nil, xerrors.Errorf("failed to get storage location from DB: %w", err)
			}

			t.StorageID = sLocation

		}
	}

	if storiface.FTUnsealed != 1 {
		panic("storiface.FTUnsealed != 1")
	}

	var mk12tasks []*task

	err = i.db.Select(ctx, &mk12tasks, `SELECT dp.indexing_task_id, dp.should_index AS indexing, dp.sp_id, dp.sector, l.storage_id
										FROM market_mk12_deal_pipeline dp
										INNER JOIN sector_location l ON dp.sp_id = l.miner_id AND dp.sector = l.sector_num
										WHERE dp.indexing_task_id = ANY ($1) AND l.sector_filetype = 1`, indIDs)
	if err != nil {
		return nil, xerrors.Errorf("getting mk12 tasks: %w", err)
	}

	tasks = append(mk20tasks, mk12tasks...)

	ls, err := i.sc.LocalStorage(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting local storage: %w", err)
	}

	localStorageMap := make(map[string]bool, len(ls))
	for _, l := range ls {
		localStorageMap[string(l.ID)] = true
	}

	for _, t := range tasks {
		if !t.Indexing {
			return &t.TaskID, nil
		}
		if found, ok := localStorageMap[t.StorageID]; ok && found {
			return &t.TaskID, nil
		}
	}

	return nil, nil
}

func (i *IndexingTask) TypeDetails() harmonytask.TaskTypeDetails {
	//dealCfg := i.cfg.Market.StorageMarketConfig
	//chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize * 56 // (56 = size of each index.Record)

	return harmonytask.TaskTypeDetails{
		Name: "Indexing",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: uint64(i.insertBatchSize * i.insertConcurrency * 56 * 2),
		},
		Max:         i.max,
		MaxFailures: 3,
		IAmBored: passcall.Every(30*time.Second, func(taskFunc harmonytask.AddTaskFunc) error {
			return i.schedule(context.Background(), taskFunc)
		}),
	}
}

func (i *IndexingTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// schedule submits
	var stop bool
	for !stop {
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			stop = true // assume we're done until we find a task to schedule

			var mk12Pendings []struct {
				UUID string `db:"uuid"`
			}

			// Indexing job must be created for every deal to make sure piece details are inserted in DB
			// even if we don't want to index it. If piece is not supposed to be indexed then it will handled
			// by the Do()
			err := tx.Select(&mk12Pendings, `SELECT uuid FROM market_mk12_deal_pipeline 
            										WHERE sealed = TRUE
            										AND indexing_task_id IS NULL
            										AND indexed = FALSE
													ORDER BY indexing_created_at ASC LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting pending mk12 indexing tasks: %w", err)
			}

			if len(mk12Pendings) > 0 {
				pending := mk12Pendings[0]

				_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET indexing_task_id = $1 
                             WHERE indexing_task_id IS NULL AND uuid = $2`, id, pending.UUID)
				if err != nil {
					return false, xerrors.Errorf("updating mk12 indexing task id: %w", err)
				}

				stop = false // we found a task to schedule, keep going
				return true, nil
			}

			var mk20Pendings []struct {
				UUID string `db:"id"`
			}

			err = tx.Select(&mk20Pendings, `SELECT id FROM market_mk20_pipeline 
            										WHERE sealed = TRUE
            										AND indexing_task_id IS NULL
            										AND indexed = FALSE
													ORDER BY indexing_created_at ASC LIMIT 1;`)
			if err != nil {
				return false, xerrors.Errorf("getting mk20 pending indexing tasks: %w", err)
			}

			if len(mk20Pendings) == 0 {
				return false, nil
			}

			pending := mk20Pendings[0]
			_, err = tx.Exec(`UPDATE market_mk20_pipeline SET indexing_task_id = $1 
                             WHERE indexing_task_id IS NULL AND id = $2`, id, pending.UUID)
			if err != nil {
				return false, xerrors.Errorf("updating mk20 indexing task id: %w", err)
			}

			stop = false
			return true, nil
		})
	}

	return nil
}

func (i *IndexingTask) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (i *IndexingTask) GetSpid(db *harmonydb.DB, taskID int64) string {
	var spid string
	err := db.QueryRow(context.Background(), `SELECT sp_id FROM market_mk12_deal_pipeline WHERE indexing_task_id = $1
													UNION ALL
													SELECT sp_id FROM market_mk20_pipeline WHERE indexing_task_id = $1`, taskID).Scan(&spid)
	if err != nil {
		log.Errorf("getting spid: %s", err)
		return ""
	}
	return spid
}

var _ = harmonytask.Reg(&IndexingTask{})
var _ harmonytask.TaskInterface = &IndexingTask{}
