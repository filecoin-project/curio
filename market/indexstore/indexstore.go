package indexstore

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multihash"
	"github.com/yugabyte/gocql"
	"golang.org/x/sync/errgroup"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
)

const keyspace = "curio"

//go:embed cql/*.cql
var cqlFiles embed.FS

var log = logging.Logger("indexstore")

type settings struct {
	// Number of records per insert batch
	InsertBatchSize int // default 15000
	// Number of concurrent inserts to split AddIndex/DeleteIndex calls to
	InsertConcurrency int // default 8
}

type IndexStore struct {
	settings settings
	cluster  *gocql.ClusterConfig
	session  *gocql.Session
	ctx      context.Context
}

type Record struct {
	Cid    cid.Cid `json:"cid"`
	Offset uint64  `json:"offset"`
	Size   uint64  `json:"size"`
}

var ErrNotFound = errors.New("not found")

func normalizeMultihashError(m multihash.Multihash, err error) error {
	if err == nil {
		return nil
	}
	if isNotFoundErr(err) {
		return fmt.Errorf("multihash %s: %w", m, ErrNotFound)
	}
	return err
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, gocql.ErrNotFound) {
		return true
	}

	// Unfortunately it seems like the Cassandra driver doesn't always return
	// a specific not found error type, so we need to rely on string parsing
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func NewIndexStore(hosts []string, port int, cfg *config.CurioConfig) *IndexStore {
	cluster := gocql.NewCluster(hosts...)
	cluster.Timeout = 5 * time.Minute
	cluster.Consistency = gocql.One
	cluster.NumConns = cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency * 8
	cluster.Port = port

	return &IndexStore{
		cluster: cluster,
		settings: settings{
			InsertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
			InsertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		},
	}
}

type ITestID string

// ItestNewID see ITestWithID doc
func ITestNewID() ITestID {
	return ITestID(strconv.Itoa(rand.Intn(99999)))
}

func (i *IndexStore) Start(ctx context.Context, test bool) error {
	if len(i.cluster.Hosts) == 0 {
		return xerrors.Errorf("no hosts provided for cassandra")
	}

	keyspaceName := keyspace
	if test {
		id := ITestNewID()
		keyspaceName = fmt.Sprintf("test%s", id)
		fmt.Printf("Using test keyspace: %s\n", keyspaceName)
	}

	// Create Cassandra keyspace
	session, err := i.cluster.CreateSession()
	if err != nil {
		return xerrors.Errorf("creating cassandra session: %w", err)
	}
	query := `CREATE KEYSPACE IF NOT EXISTS ` + keyspaceName +
		` WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : 3 }`
	err = session.Query(query).WithContext(ctx).Exec()
	if err != nil {
		return xerrors.Errorf("creating cassandra keyspace: %w", err)
	}

	session.Close()

	// Recreate session with the keyspace
	i.cluster.Keyspace = keyspaceName
	session, err = i.cluster.CreateSession()
	if err != nil {
		return xerrors.Errorf("creating cassandra session: %w", err)
	}

	entries, err := cqlFiles.ReadDir("cql")
	if err != nil {
		log.Fatalf("failed to read embedded directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := cqlFiles.ReadFile("cql/" + entry.Name())
		if err != nil {
			log.Fatalf("failed to read file %s: %v", entry.Name(), err)
		}

		lines := strings.Split(string(data), ";")
		for _, line := range lines {
			line = strings.Trim(line, "\n \t")
			if line == "" {
				continue
			}
			log.Debug(line)
			err := session.Query(line).WithContext(ctx).Exec()
			if err != nil {
				return xerrors.Errorf("creating tables: executing\n%s\n%w", line, err)
			}
		}
	}

	i.session = session
	i.ctx = ctx

	return nil
}

// AddIndex adds multihash -> piece cid (v2) mappings, along with offset and size information for the piece.
func (i *IndexStore) AddIndex(ctx context.Context, pieceCidv2 cid.Cid, recordsChan chan Record) error {
	insertPieceBlockOffsetSize := `INSERT INTO PieceBlockOffsetSize (PieceCid, PayloadMultihash, BlockOffset) VALUES (?, ?, ?)`
	insertPayloadToPieces := `INSERT INTO PayloadToPieces (PayloadMultihash, PieceCid, BlockSize) VALUES (?, ?, ?)`
	pieceCidBytes := pieceCidv2.Bytes()

	var eg errgroup.Group

	// Start worker threads based on InsertConcurrency value
	for worker := 0; worker < i.settings.InsertConcurrency; worker++ {
		eg.Go(func() error {
			var batchPieceBlockOffsetSize *gocql.Batch
			var batchPayloadToPieces *gocql.Batch
			for {
				if batchPieceBlockOffsetSize == nil {
					batchPieceBlockOffsetSize = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batchPieceBlockOffsetSize.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}
				if batchPayloadToPieces == nil {
					batchPayloadToPieces = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batchPayloadToPieces.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}

				rec, ok := <-recordsChan

				if !ok {
					if len(batchPieceBlockOffsetSize.Entries) > 0 {
						if err := i.executeBatchWithRetry(ctx, batchPieceBlockOffsetSize, pieceCidv2); err != nil {
							return err
						}
					}
					if len(batchPayloadToPieces.Entries) > 0 {
						if err := i.executeBatchWithRetry(ctx, batchPayloadToPieces, pieceCidv2); err != nil {
							return err
						}
					}
					return nil
				}

				payloadMultihashBytes := []byte(rec.Cid.Hash())

				batchPieceBlockOffsetSize.Entries = append(batchPieceBlockOffsetSize.Entries, gocql.BatchEntry{
					Stmt:       insertPieceBlockOffsetSize,
					Args:       []interface{}{pieceCidBytes, payloadMultihashBytes, rec.Offset},
					Idempotent: true,
				})

				batchPayloadToPieces.Entries = append(batchPayloadToPieces.Entries, gocql.BatchEntry{
					Stmt:       insertPayloadToPieces,
					Args:       []interface{}{payloadMultihashBytes, pieceCidBytes, rec.Size},
					Idempotent: true,
				})

				if len(batchPieceBlockOffsetSize.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchPieceBlockOffsetSize, pieceCidv2); err != nil {
						return err
					}
					batchPieceBlockOffsetSize = nil
				}
				if len(batchPayloadToPieces.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchPayloadToPieces, pieceCidv2); err != nil {
						return err
					}
					batchPayloadToPieces = nil
				}
			}
		})
	}

	err := eg.Wait()
	if err != nil {
		return xerrors.Errorf("addindex wait: %w", err)
	}

	return nil
}

// executeBatchWithRetry executes a batch with retry logic and exponential backoff
func (i *IndexStore) executeBatchWithRetry(ctx context.Context, batch *gocql.Batch, pieceCidv2 cid.Cid) error {
	var err error
	maxRetries := 20
	backoff := 20 * time.Second
	maxBackoff := 180 * time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		start := time.Now()
		err = i.session.ExecuteBatch(batch)
		if time.Since(start) > 30*time.Second {
			log.Warnw("Batch Insert", "took", time.Since(start), "entries", len(batch.Entries))
		} else {
			log.Debugw("Batch Insert", "took", time.Since(start), "entries", len(batch.Entries))
		}

		if err == nil {
			return nil
		}

		// If context is done, exit immediately
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Warnf("Batch insert attempt %d failed for piece %s: %v", attempt+1, pieceCidv2, err)

		// If max retries reached, return error
		if attempt == maxRetries {
			return xerrors.Errorf("execute batch: executing batch insert for piece %s: %w", pieceCidv2, err)
		}

		// Sleep for backoff duration before retrying
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}

		// Exponential backoff
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return nil
}

// RemoveIndexes removes all multihash -> piece cid mappings, and all
// offset information for the piece.
func (i *IndexStore) RemoveIndexes(ctx context.Context, pieceCidv2 cid.Cid) error {
	pieceCidBytes := pieceCidv2.Bytes()

	// First, select all PayloadMultihash for the given PieceCid from PieceBlockOffsetSize
	selectQry := `SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ?`
	iter := i.session.Query(selectQry, pieceCidBytes).WithContext(ctx).Iter()

	var payloadMultihashBytes []byte
	var payloadMultihashes [][]byte
	for iter.Scan(&payloadMultihashBytes) {
		// Copy the bytes since the slice will be overwritten
		mhCopy := make([]byte, len(payloadMultihashBytes))
		copy(mhCopy, payloadMultihashBytes)
		payloadMultihashes = append(payloadMultihashes, mhCopy)
	}
	if err := iter.Close(); err != nil {
		return xerrors.Errorf("scanning PayloadMultihash for piece %s: %w", pieceCidv2, err)
	}

	// Prepare batch deletes for PayloadToPieces
	delPayloadToPiecesQry := `DELETE FROM PayloadToPieces WHERE PayloadMultihash = ? AND PieceCid = ?`
	batch := i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batchSize := i.settings.InsertBatchSize

	for idx, payloadMH := range payloadMultihashes {
		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt:       delPayloadToPiecesQry,
			Args:       []interface{}{payloadMH, pieceCidBytes},
			Idempotent: true,
		})

		if len(batch.Entries) >= batchSize || idx == len(payloadMultihashes)-1 {
			if err := i.executeBatchWithRetry(ctx, batch, pieceCidv2); err != nil {
				return xerrors.Errorf("executing batch delete for PayloadToPieces for piece %s: %w", pieceCidv2, err)
			}
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}
	}

	if len(batch.Entries) >= 0 {
		if err := i.executeBatchWithRetry(ctx, batch, pieceCidv2); err != nil {
			return xerrors.Errorf("executing batch delete for PayloadToPieces for piece %s: %w", pieceCidv2, err)
		}
	}

	// Delete from PieceBlockOffsetSize
	delPieceBlockOffsetSizeQry := `DELETE FROM PieceBlockOffsetSize WHERE PieceCid = ?`
	err := i.session.Query(delPieceBlockOffsetSizeQry, pieceCidBytes).WithContext(ctx).Exec()
	if err != nil {
		return xerrors.Errorf("deleting PieceBlockOffsetSize for piece %s: %w", pieceCidv2, err)
	}

	return nil
}

// PieceInfo contains PieceCidV2 and BlockSize
type PieceInfo struct {
	PieceCidV2 cid.Cid
	BlockSize  uint64
}

// PiecesContainingMultihash gets all pieces that contain a multihash along with their BlockSize
func (i *IndexStore) PiecesContainingMultihash(ctx context.Context, m multihash.Multihash) ([]PieceInfo, error) {
	var pieces []PieceInfo
	var pieceCidBytes []byte
	var blockSize uint64

	qry := `SELECT PieceCid, BlockSize FROM PayloadToPieces WHERE PayloadMultihash = ?`
	iter := i.session.Query(qry, []byte(m)).WithContext(ctx).Iter()
	for iter.Scan(&pieceCidBytes, &blockSize) {
		pcid, err := cid.Parse(pieceCidBytes)
		if err != nil {
			return nil, fmt.Errorf("parsing piece cid: %w", err)
		}
		pieces = append(pieces, PieceInfo{
			PieceCidV2: pcid,
			BlockSize:  blockSize,
		})
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("getting pieces containing multihash %s: %w", m, err)
	}

	// No pieces found for multihash, return a "not found" error
	if len(pieces) == 0 {
		return nil, normalizeMultihashError(m, ErrNotFound)
	}
	return pieces, nil
}

// GetOffset retrieves the offset of a payload in a piece(v2)
func (i *IndexStore) GetOffset(ctx context.Context, pieceCidv2 cid.Cid, hash multihash.Multihash) (uint64, error) {
	var offset uint64
	qryOffset := `SELECT BlockOffset FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash = ?`
	err := i.session.Query(qryOffset, pieceCidv2.Bytes(), []byte(hash)).WithContext(ctx).Scan(&offset)
	if err != nil {
		return 0, fmt.Errorf("getting offset: %w", err)
	}

	return offset, nil
}

func (i *IndexStore) GetPieceHashRange(ctx context.Context, piecev2 cid.Cid, start multihash.Multihash, num int64) ([]multihash.Multihash, error) {
	qry := "SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash >= ? ORDER BY PayloadMultihash ASC LIMIT ?"
	iter := i.session.Query(qry, piecev2.Bytes(), []byte(start), num).WithContext(ctx).Iter()

	var hashes []multihash.Multihash
	var r []byte
	for iter.Scan(&r) {
		m := multihash.Multihash(r)
		hashes = append(hashes, m)

		// Allocate new r, preallocating the typical size of a multihash (36 bytes)
		r = make([]byte, 0, 36)
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("iterating piece hash range (P:0x%02x, H:0x%02x, n:%d): %w", piecev2.Bytes(), []byte(start), num, err)
	}
	if len(hashes) != int(num) {
		return nil, xerrors.Errorf("expected %d hashes, got %d (possibly missing indexes)", num, len(hashes))
	}

	return hashes, nil
}

func (i *IndexStore) CheckHasPiece(ctx context.Context, piecev2 cid.Cid) (bool, error) {
	qry := "SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash >= ? ORDER BY PayloadMultihash ASC LIMIT ?"
	iter := i.session.Query(qry, piecev2.Bytes(), []byte{0}, 1).WithContext(ctx).Iter()

	var hashes []multihash.Multihash
	var r []byte
	for iter.Scan(&r) {
		m := multihash.Multihash(r)
		hashes = append(hashes, m)

		// Allocate new r, preallocating the typical size of a multihash (36 bytes)
		r = make([]byte, 0, 36)
	}
	if err := iter.Close(); err != nil {
		return false, xerrors.Errorf("iterating piece hash range (P:0x%02x, n:%d): %w", piecev2.Bytes(), len(hashes), err)
	}

	return len(hashes) > 0, nil
}

func (i *IndexStore) InsertAggregateIndex(ctx context.Context, aggregatePieceCid cid.Cid, records []Record) error {
	aggregatePieceCidBytes := aggregatePieceCid.Bytes()

	chanSize := i.settings.InsertConcurrency * i.settings.InsertBatchSize

	recordsChan := make(chan Record, chanSize)

	go func(f []Record) {
		for _, r := range f {
			recordsChan <- r
		}
		close(recordsChan)
	}(records)

	insertPieceByAggregate := `INSERT INTO piece_by_aggregate (AggregatePieceCid, PieceCid, UnpaddedOffset, UnpaddedLength) VALUES (?, ?, ?, ?)`
	insertAggregateByPiece := `INSERT INTO aggregate_by_piece (PieceCid, AggregatePieceCid, UnpaddedOffset, UnpaddedLength) VALUES (?, ?, ?, ?)`

	var eg errgroup.Group

	// Start worker threads based on InsertConcurrency value
	for worker := 0; worker < i.settings.InsertConcurrency; worker++ {
		eg.Go(func() error {
			var batchPieceByAggregate *gocql.Batch
			var batchAggregateByPiece *gocql.Batch
			for {
				if batchPieceByAggregate == nil {
					batchPieceByAggregate = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batchPieceByAggregate.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}
				if batchAggregateByPiece == nil {
					batchAggregateByPiece = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batchAggregateByPiece.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}

				rec, ok := <-recordsChan

				if !ok {
					if len(batchPieceByAggregate.Entries) > 0 {
						if err := i.executeBatchWithRetry(ctx, batchPieceByAggregate, aggregatePieceCid); err != nil {
							return err
						}
					}
					if len(batchAggregateByPiece.Entries) > 0 {
						if err := i.executeBatchWithRetry(ctx, batchAggregateByPiece, aggregatePieceCid); err != nil {
							return err
						}
					}
					return nil
				}

				batchPieceByAggregate.Entries = append(batchPieceByAggregate.Entries, gocql.BatchEntry{
					Stmt:       insertPieceByAggregate,
					Args:       []interface{}{aggregatePieceCidBytes, rec.Cid.Bytes(), rec.Offset, rec.Size},
					Idempotent: true,
				})

				batchAggregateByPiece.Entries = append(batchAggregateByPiece.Entries, gocql.BatchEntry{
					Stmt:       insertAggregateByPiece,
					Args:       []interface{}{rec.Cid.Bytes(), aggregatePieceCidBytes, rec.Offset, rec.Size},
					Idempotent: true,
				})

				if len(batchPieceByAggregate.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchPieceByAggregate, aggregatePieceCid); err != nil {
						return err
					}
					batchPieceByAggregate = nil
				}
				if len(batchAggregateByPiece.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchAggregateByPiece, aggregatePieceCid); err != nil {
						return err
					}
					batchAggregateByPiece = nil
				}
			}
		})
	}

	err := eg.Wait()
	if err != nil {
		return xerrors.Errorf("add aggregate index: %w", err)
	}

	return nil
}

func (i *IndexStore) FindPieceInAggregate(ctx context.Context, pieceCid cid.Cid) ([]Record, error) {
	var recs []Record
	qry := `SELECT AggregatePieceCid, UnpaddedOffset, UnpaddedLength FROM aggregate_by_piece WHERE PieceCid = ?`
	iter := i.session.Query(qry, pieceCid.Bytes()).WithContext(ctx).Iter()
	var r []byte
	var idx, length int64
	for iter.Scan(&r, &idx, &length) {
		c, err := cid.Cast(r)
		if err != nil {
			return nil, xerrors.Errorf("casting aggregate piece cid: %w", err)
		}
		recs = append(recs, Record{
			Cid:    c,
			Offset: uint64(idx),
			Size:   uint64(length),
		})

		r = make([]byte, 0)
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("iterating aggregate piece cid (P:0x%02x): %w", pieceCid.Bytes(), err)
	}
	return recs, nil
}

func (i *IndexStore) RemoveAggregateIndex(ctx context.Context, aggregatePieceCid cid.Cid) error {
	aggregatePieceCidBytes := aggregatePieceCid.Bytes()

	// 1) iterate children with paging
	iter := i.session.Query(`SELECT PieceCid, UnpaddedOffset FROM piece_by_aggregate WHERE AggregatePieceCid = ?`, aggregatePieceCidBytes).WithContext(ctx).PageSize(1000).Iter()

	var piece []byte
	var off int64

	batch := i.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)

	flush := func() error {
		if len(batch.Entries) == 0 {
			return nil
		}
		if berr := i.session.ExecuteBatch(batch); berr != nil {
			return xerrors.Errorf("executing batch delete for aggregate index for piece cid (P:0x%02x) %s: %w", aggregatePieceCidBytes, aggregatePieceCid.String(), berr)
		}
		batch = i.session.NewBatch(gocql.LoggedBatch).WithContext(ctx)
		return nil
	}

	for iter.Scan(&piece, &off) {
		batch.Query(`DELETE FROM aggregate_by_piece
                     WHERE PieceCid = ? AND AggregatePieceCid = ? AND UnpaddedOffset = ?`,
			append([]byte{}, piece...), aggregatePieceCidBytes, off)

		if len(batch.Entries) >= 1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := iter.Close(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	// 3) delete the forward partition
	err := i.session.Query(`DELETE FROM piece_by_aggregate WHERE AggregatePieceCid = ?`, aggregatePieceCidBytes).WithContext(ctx).Exec()
	if err != nil {
		return xerrors.Errorf("deleting piece_by_aggregate aggregate piece cid (P:0x%02x) %s: %w", aggregatePieceCidBytes, aggregatePieceCid.String(), err)
	}

	return nil
}

func (i *IndexStore) UpdatePieceCidV1ToV2(ctx context.Context, pieceCidV1 cid.Cid, pieceCidV2 cid.Cid) error {
	p1 := pieceCidV1.Bytes()
	p2 := pieceCidV2.Bytes()

	// First, select all PayloadMultihash for the given PieceCid from PieceBlockOffsetSize
	selectQry := `SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ?`
	iter := i.session.Query(selectQry, p1).WithContext(ctx).Iter()

	var payloadMultihashBytes []byte
	var payloadMultihashes [][]byte
	for iter.Scan(&payloadMultihashBytes) {
		// Copy the bytes since the slice will be overwritten
		mhCopy := make([]byte, len(payloadMultihashBytes))
		copy(mhCopy, payloadMultihashBytes)
		payloadMultihashes = append(payloadMultihashes, mhCopy)
	}
	if err := iter.Close(); err != nil {
		return xerrors.Errorf("scanning PayloadMultihash for piece %s: %w", pieceCidV1.String(), err)
	}

	// Prepare batch replace for PayloadToPieces
	updatePiecesQry := `UPDATE PayloadToPieces SET PieceCid = ? WHERE PayloadMultihash = ? AND PieceCid = ?`
	batch := i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batchSize := i.settings.InsertBatchSize

	for idx, payloadMH := range payloadMultihashes {
		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt:       updatePiecesQry,
			Args:       []interface{}{p2, payloadMH, p1},
			Idempotent: true,
		})

		if len(batch.Entries) >= batchSize || idx == len(payloadMultihashes)-1 {
			if err := i.executeBatchWithRetry(ctx, batch, pieceCidV1); err != nil {
				return xerrors.Errorf("executing batch replace for PayloadToPieces for piece %s: %w", pieceCidV1, err)
			}
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}
	}

	if len(batch.Entries) >= 0 {
		if err := i.executeBatchWithRetry(ctx, batch, pieceCidV1); err != nil {
			return xerrors.Errorf("executing batch replace for PayloadToPieces for piece %s: %w", pieceCidV1, err)
		}
	}

	// Prepare batch replace for PieceBlockOffsetSize
	updatePiecesQry = `UPDATE PieceBlockOffsetSize SET PieceCid = ? WHERE PayloadMultihash = ? AND PieceCid = ?`
	batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batchSize = i.settings.InsertBatchSize

	for idx, payloadMH := range payloadMultihashes {
		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt:       updatePiecesQry,
			Args:       []interface{}{p2, payloadMH, p1},
			Idempotent: true,
		})

		if len(batch.Entries) >= batchSize || idx == len(payloadMultihashes)-1 {
			if err := i.executeBatchWithRetry(ctx, batch, pieceCidV1); err != nil {
				return xerrors.Errorf("executing batch replace for PieceBlockOffsetSize for piece %s: %w", pieceCidV1, err)
			}
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}
	}

	if len(batch.Entries) >= 0 {
		if err := i.executeBatchWithRetry(ctx, batch, pieceCidV1); err != nil {
			return xerrors.Errorf("executing batch replace for PieceBlockOffsetSize for piece %s: %w", pieceCidV1, err)
		}
	}

	return nil
}

type NodeDigest struct {
	Layer int      // Layer index in the merkle Tree
	Index int64    // logical index at that layer
	Hash  [32]byte // 32 bytes
}

func (i *IndexStore) AddPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layer []NodeDigest) error {
	qry := `INSERT INTO pdp_cache_layer (PieceCid, LayerIndex, Leaf, LeafIndex) VALUES (?, ?, ?, ?)`
	pieceCidBytes := pieceCidV2.Bytes()
	var batch *gocql.Batch
	batchSize := i.settings.InsertBatchSize

	if len(layer) == 0 {
		return xerrors.Errorf("no records to insert")
	}

	for _, r := range layer {
		if batch == nil {
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}

		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt:       qry,
			Args:       []interface{}{pieceCidBytes, r.Layer, r.Hash[:], r.Index},
			Idempotent: true,
		})

		if len(batch.Entries) >= batchSize {
			if err := i.session.ExecuteBatch(batch); err != nil {
				return xerrors.Errorf("executing batch insert for PDP cache layer for piece %s: %w", pieceCidV2.String(), err)
			}
			batch = nil
		}
	}

	if batch != nil {
		if len(batch.Entries) >= 0 {
			if err := i.session.ExecuteBatch(batch); err != nil {
				return xerrors.Errorf("executing batch insert for PDP cache layer for piece %s: %w", pieceCidV2.String(), err)
			}
		}
	}

	return nil
}

func (i *IndexStore) GetPDPLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error) {
	var layerIdx int
	if err := i.session.Query(`SELECT LayerIndex FROM pdp_cache_layer WHERE PieceCid = ? LIMIT 1`, pieceCidV2.Bytes()).WithContext(ctx).Scan(&layerIdx); err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return false, 0, nil
		}
		return false, 0, xerrors.Errorf("scanning highest layer for PDP cache layer (P:0x%02x) %s: %w", pieceCidV2.Bytes(), pieceCidV2.String(), err)
	}

	return true, layerIdx, nil
}

func (i *IndexStore) GetPDPLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]NodeDigest, error) {
	var layer []NodeDigest

	iter := i.session.Query(`SELECT LeafIndex, Leaf FROM pdp_cache_layer WHERE PieceCid = ? AND LayerIndex = ?`, pieceCidV2.Bytes(), layerIdx).WithContext(ctx).PageSize(2000).Iter()

	var leafIdx int64
	var leaf []byte
	for iter.Scan(&leafIdx, &leaf) {
		layer = append(layer, NodeDigest{
			Layer: layerIdx,
			Index: leafIdx,
			Hash:  [32]byte(leaf),
		})
		leaf = make([]byte, 32)
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("iterating PDP cache layer (P:0x%02x) %s: %w", pieceCidV2.Bytes(), pieceCidV2.String(), err)
	}

	sort.Slice(layer, func(i, j int) bool {
		return layer[i].Index < layer[j].Index
	})
	return layer, nil
}

func (i *IndexStore) DeletePDPLayer(ctx context.Context, pieceCidV2 cid.Cid) error {
	for {
		has, layerIdx, err := i.GetPDPLayerIndex(ctx, pieceCidV2)
		if err != nil {
			return err
		}
		if !has {
			return nil
		}

		qry := `DELETE FROM pdp_cache_layer WHERE PieceCid = ? AND LayerIndex = ?`
		if err := i.session.Query(qry, pieceCidV2.Bytes(), layerIdx).WithContext(ctx).Exec(); err != nil {
			return xerrors.Errorf("deleting PDP cache layer with index %d (P:0x%02x): %w", layerIdx, pieceCidV2.Bytes(), err)
		}
	}
}

func (i *IndexStore) GetPDPNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, *NodeDigest, error) {
	var r []byte

	qry := `SELECT Leaf FROM pdp_cache_layer WHERE PieceCid = ? AND LayerIndex = ? AND LeafIndex = ? LIMIT 1`
	err := i.session.Query(qry, pieceCidV2.Bytes(), layerIdx, index).WithContext(ctx).Scan(&r)
	if err != nil {
		if errors.Is(err, gocql.ErrNotFound) {
			return false, nil, nil
		}
		return false, nil, xerrors.Errorf("scanning PDP cache layer (P:0x%02x) %s: %w", pieceCidV2.Bytes(), pieceCidV2.String(), err)
	}

	var hash [32]byte
	copy(hash[:], r)

	return true, &NodeDigest{
		Layer: layerIdx,
		Index: index,
		Hash:  hash,
	}, nil
}
