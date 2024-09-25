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
	InsertBatchSize int
	// Number of concurrent inserts to split AddIndex/DeleteIndex calls to
	InsertConcurrency int
}

type IndexStore struct {
	settings settings
	cluster  *gocql.ClusterConfig
	session  *gocql.Session
	ctx      context.Context
}

type OffsetSize struct {
	// Offset is the offset into the CAR file of the section, where a section
	// is <section size><cid><block data>
	Offset uint64 `json:"offset"`
	// Size is the size of the block data (not the whole section)
	Size uint64 `json:"size"`
}

type Record struct {
	Cid        cid.Cid `json:"cid"`
	OffsetSize `json:"offsetsize"`
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
	cluster.Timeout = time.Minute

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
	// Create cassandra keyspace
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

// AddIndex adds multihash -> piece cid mappings, along with offset / size information for the piece.
// It takes a context, the piece cid, and a slice of Record structs as arguments.
// It returns an error if any error occurs during the execution.
func (i *IndexStore) AddIndex(ctx context.Context, pieceCid cid.Cid, recordsChan chan Record) error {
	Qry := `INSERT INTO PayloadToPiece (PieceCid, PayloadMultihash, BlockOffset, BlockSize) VALUES (?, ?, ?, ?)`
	pieceCidBytes := pieceCid.Bytes()

	var eg errgroup.Group

	// Start worker threads based on InsertConcurrency value
	// These workers will be further batch based on InsertBatchSize in the
	// goroutines for each BatchInsert operation
	for worker := 0; worker < i.settings.InsertConcurrency; worker++ {
		eg.Go(func() error {
			var batch *gocql.Batch
			// Running a loop on all instead of creating batches require less memory
			for {
				if batch == nil {
					batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batch.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}

				rec, ok := <-recordsChan

				if !ok {
					if len(batch.Entries) > 0 {
						err := i.session.ExecuteBatch(batch)
						if err != nil {
							return fmt.Errorf("executing batch insert for piece %s: %w", pieceCid, err)
						}
					}
					return nil
				}

				batch.Entries = append(batch.Entries, gocql.BatchEntry{
					Stmt:       Qry,
					Args:       []any{pieceCidBytes, []byte(rec.Cid.Hash()), rec.Offset, rec.Size},
					Idempotent: true,
				})

				if len(batch.Entries) == i.settings.InsertBatchSize {
					err := func() error {
						defer func(start time.Time) {
							log.Debugw("addIndex Batch Insert", "took", time.Since(start), "entries", len(batch.Entries))
						}(time.Now())

						err := i.session.ExecuteBatch(batch)
						if err != nil {
							return fmt.Errorf("executing batch insert for piece %s: %w", pieceCid, err)
						}
						return nil
					}()
					if err != nil {
						return err
					}
					batch = nil
				}
			}
		})
	}

	err := eg.Wait()
	if err != nil {
		return err
	}

	return nil
}

// RemoveIndexes removes all multihash -> piece cid mappings, and all
// offset / size information for the piece.
func (i *IndexStore) RemoveIndexes(ctx context.Context, pieceCid cid.Cid) error {
	delQry := `DELETE FROM PayloadToPiece WHERE PayloadMultihash = ? AND PieceCid = ?`
	pieceCidBytes := pieceCid.Bytes()

	// Get multihashes for piece
	getQry := `SELECT PayloadMultihash FROM PayloadToPiece WHERE PieceCid = ?`
	iter := i.session.Query(getQry, pieceCidBytes).WithContext(ctx).Iter()

	// Create batch for deletion
	batch := i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
	batch.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)

	var payloadMHBz []byte
	for iter.Scan(&payloadMHBz) {
		// Add each delete operation to batch
		batch.Entries = append(batch.Entries, gocql.BatchEntry{
			Stmt:       delQry,
			Args:       []any{payloadMHBz, pieceCidBytes},
			Idempotent: true,
		})

		// Execute batch
		if len(batch.Entries) >= i.settings.InsertBatchSize {
			err := i.session.ExecuteBatch(batch)
			if err != nil {
				return xerrors.Errorf("executing batch delete for piece %s: %w", pieceCid, err)
			}
			// Create a new batch after executing
			batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
			batch.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
		}
	}

	// Execute remaining operations in the batch
	if len(batch.Entries) > 0 {
		err := i.session.ExecuteBatch(batch)
		if err != nil {
			return xerrors.Errorf("executing batch delete for piece %s: %w", pieceCid, err)
		}
	}

	if err := iter.Close(); err != nil {
		return xerrors.Errorf("Getting piece index for piece %s: %w", pieceCid, err)
	}

	return nil
}

// PiecesContainingMultihash gets all pieces that contain a multihash (used when retrieving by payload CID)
func (i *IndexStore) PiecesContainingMultihash(ctx context.Context, m multihash.Multihash) ([]cid.Cid, error) {
	var pcids []cid.Cid
	var bz []byte
	qry := `SELECT PieceCid FROM PayloadToPiece WHERE PayloadMultihash = ?`
	iter := i.session.Query(qry, []byte(m)).WithContext(ctx).Iter()
	for iter.Scan(&bz) {
		pcid, err := cid.Parse(bz)
		if err != nil {
			return nil, fmt.Errorf("parsing piece cid: %w", err)
		}
		pcids = append(pcids, pcid)
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("getting pieces containing multihash %s: %w", m, err)
	}

	// No pieces found for multihash, return a "not found" error
	if len(pcids) == 0 {
		return nil, normalizeMultihashError(m, ErrNotFound)
	}
	return pcids, nil
}

func (i *IndexStore) GetOffsetSize(ctx context.Context, pieceCid cid.Cid, hash multihash.Multihash) (*OffsetSize, error) {
	var offset, size uint64
	qry := `SELECT BlockOffset, BlockSize FROM PayloadToPiece WHERE PieceCid = ? AND PayloadMultihash = ?`
	err := i.session.Query(qry, pieceCid.Bytes(), []byte(hash)).WithContext(ctx).Scan(&offset, &size)
	if err != nil {
		return nil, fmt.Errorf("getting offset / size: %w", err)
	}

	return &OffsetSize{Offset: offset, Size: size}, nil
}

func (i *IndexStore) GetPieceHashRange(ctx context.Context, piece cid.Cid, start multihash.Multihash, num int64) ([]multihash.Multihash, error) {
	qry := "SELECT PayloadMultihash FROM PayloadToPiece WHERE PieceCid = ? AND PayloadMultihash >= ? LIMIT ?"
	iter := i.session.Query(qry, piece.Bytes(), []byte(start), num).WithContext(ctx).Iter()

	var hashes []multihash.Multihash
	var r []byte
	for iter.Scan(&r) {
		m := multihash.Multihash(r)
		hashes = append(hashes, m)
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("iterating piece hash range: %w", err)
	}
	return hashes, nil
}
