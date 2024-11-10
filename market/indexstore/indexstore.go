package indexstore

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
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

//go:embed create.cql
var createCQL string

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

func NewIndexStore(hosts []string, cfg *config.CurioConfig) (*IndexStore, error) {
	if len(hosts) == 0 {
		return nil, xerrors.Errorf("no hosts provided for cassandra")
	}

	cluster := gocql.NewCluster(hosts...)
	cluster.Timeout = 5 * time.Minute
	cluster.Consistency = gocql.One
	cluster.NumConns = cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency * 8

	store := &IndexStore{
		cluster: cluster,
		settings: settings{
			InsertBatchSize:   cfg.Market.StorageMarketConfig.Indexing.InsertBatchSize,
			InsertConcurrency: cfg.Market.StorageMarketConfig.Indexing.InsertConcurrency,
		},
	}

	return store, store.Start(context.Background())
}

func (i *IndexStore) Start(ctx context.Context) error {
	// Create Cassandra keyspace
	session, err := i.cluster.CreateSession()
	if err != nil {
		return xerrors.Errorf("creating cassandra session: %w", err)
	}
	query := `CREATE KEYSPACE IF NOT EXISTS ` + keyspace +
		` WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : 3 }`
	err = session.Query(query).WithContext(ctx).Exec()
	if err != nil {
		return xerrors.Errorf("creating cassandra keyspace: %w", err)
	}

	session.Close()

	// Recreate session with the keyspace
	i.cluster.Keyspace = keyspace
	session, err = i.cluster.CreateSession()
	if err != nil {
		return xerrors.Errorf("creating cassandra session: %w", err)
	}

	lines := strings.Split(createCQL, ";")
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

	i.session = session
	i.ctx = ctx

	return nil
}

// AddIndex adds multihash -> piece cid mappings, along with offset and size information for the piece.
func (i *IndexStore) AddIndex(ctx context.Context, pieceCid cid.Cid, recordsChan chan Record) error {
	insertPieceBlockOffsetSize := `INSERT INTO PieceBlockOffsetSize (PieceCid, PayloadMultihash, BlockOffset) VALUES (?, ?, ?)`
	insertPayloadToPieces := `INSERT INTO PayloadToPieces (PayloadMultihash, PieceCid, BlockSize) VALUES (?, ?, ?)`
	pieceCidBytes := pieceCid.Bytes()

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
						if err := i.executeBatchWithRetry(ctx, batchPieceBlockOffsetSize, pieceCid); err != nil {
							return err
						}
					}
					if len(batchPayloadToPieces.Entries) > 0 {
						if err := i.executeBatchWithRetry(ctx, batchPayloadToPieces, pieceCid); err != nil {
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
					if err := i.executeBatchWithRetry(ctx, batchPieceBlockOffsetSize, pieceCid); err != nil {
						return err
					}
					batchPieceBlockOffsetSize = nil
				}
				if len(batchPayloadToPieces.Entries) == i.settings.InsertBatchSize {
					if err := i.executeBatchWithRetry(ctx, batchPayloadToPieces, pieceCid); err != nil {
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
func (i *IndexStore) executeBatchWithRetry(ctx context.Context, batch *gocql.Batch, pieceCid cid.Cid) error {
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

		log.Warnf("Batch insert attempt %d failed for piece %s: %v", attempt+1, pieceCid, err)

		// If max retries reached, return error
		if attempt == maxRetries {
			return xerrors.Errorf("execute batch: executing batch insert for piece %s: %w", pieceCid, err)
		}

		// Sleep for backoff duration before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
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
func (i *IndexStore) RemoveIndexes(ctx context.Context, pieceCid cid.Cid) error {
	pieceCidBytes := pieceCid.Bytes()

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
		return xerrors.Errorf("scanning PayloadMultihash for piece %s: %w", pieceCid, err)
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
			if err := i.executeBatchWithRetry(ctx, batch, pieceCid); err != nil {
				return xerrors.Errorf("executing batch delete for PayloadToPieces for piece %s: %w", pieceCid, err)
			}
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
		}
	}

	if len(batch.Entries) >= 0 {
		if err := i.executeBatchWithRetry(ctx, batch, pieceCid); err != nil {
			return xerrors.Errorf("executing batch delete for PayloadToPieces for piece %s: %w", pieceCid, err)
		}
		batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	}

	// Delete from PieceBlockOffsetSize
	delPieceBlockOffsetSizeQry := `DELETE FROM PieceBlockOffsetSize WHERE PieceCid = ?`
	err := i.session.Query(delPieceBlockOffsetSizeQry, pieceCidBytes).WithContext(ctx).Exec()
	if err != nil {
		return xerrors.Errorf("deleting PieceBlockOffsetSize for piece %s: %w", pieceCid, err)
	}

	return nil
}

// PieceInfo contains PieceCid and BlockSize
type PieceInfo struct {
	PieceCid  cid.Cid
	BlockSize uint64
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
			PieceCid:  pcid,
			BlockSize: blockSize,
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

// GetOffset retrieves the offset of a payload in a piece
func (i *IndexStore) GetOffset(ctx context.Context, pieceCid cid.Cid, hash multihash.Multihash) (uint64, error) {
	var offset uint64
	qryOffset := `SELECT BlockOffset FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash = ?`
	err := i.session.Query(qryOffset, pieceCid.Bytes(), []byte(hash)).WithContext(ctx).Scan(&offset)
	if err != nil {
		return 0, fmt.Errorf("getting offset: %w", err)
	}

	return offset, nil
}

func (i *IndexStore) GetPieceHashRange(ctx context.Context, piece cid.Cid, start multihash.Multihash, num int64) ([]multihash.Multihash, error) {
	qry := "SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash >= ? ORDER BY PayloadMultihash ASC LIMIT ?"
	iter := i.session.Query(qry, piece.Bytes(), []byte(start), num).WithContext(ctx).Iter()

	var hashes []multihash.Multihash
	var r []byte
	for iter.Scan(&r) {
		m := multihash.Multihash(r)
		hashes = append(hashes, m)

		// Allocate new r, preallocating the typical size of a multihash (36 bytes)
		r = make([]byte, 0, 36)
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("iterating piece hash range (P:0x%02x, H:0x%02x, n:%d): %w", piece.Bytes(), []byte(start), num, err)
	}

	return hashes, nil
}
