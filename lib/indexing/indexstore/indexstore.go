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
)

//go:embed create.cql
var createCQL string

var log = logging.Logger("cassandra")

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

// Probability of a collision in two 20 byte hashes (birthday problem):
// 2^(20*8/2) = 1.4 x 10^24
const multihashLimitBytes = 20

// trimMultihash trims the multihash to the last multihashLimitBytes bytes
func trimMultihash(mh multihash.Multihash) []byte {
	var idx int
	if len(mh) > multihashLimitBytes {
		idx = len(mh) - multihashLimitBytes
	}
	return mh[idx:]
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

func NewIndexStore(hosts []string) (*IndexStore, error) {
	if len(hosts) == 0 {
		return nil, xerrors.Errorf("no hosts provided for cassandra")
	}

	cluster := gocql.NewCluster(hosts...)
	cluster.Timeout = time.Minute
	cluster.Keyspace = "curio"

	store := &IndexStore{
		cluster: cluster,
	}

	return store, store.Start(context.Background())
}

func (i *IndexStore) Start(ctx context.Context) error {
	log.Info("Starting cassandra DB")
	// Create cassandra keyspace
	session, err := i.cluster.CreateSession()
	if err != nil {
		return xerrors.Errorf("creating cassandra session: %w", err)

	}
	query := `CREATE KEYSPACE IF NOT EXISTS ` + i.cluster.Keyspace +
		` WITH REPLICATION = { 'class' : 'SimpleStrategy', 'replication_factor' : 3 }`
	err = session.Query(query).WithContext(ctx).Exec()
	if err != nil {
		return xerrors.Errorf("creating cassandra keyspace: %w", err)
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

	log.Infow("Successfully started Cassandra DB")
	return nil
}

// AddIndex adds multihash -> piece cid mappings, along with offset / size information for the piece.
// It takes a context, the piece cid, and a slice of Record structs as arguments.
// It returns an error if any error occurs during the execution.
func (i *IndexStore) AddIndex(ctx context.Context, pieceCid cid.Cid, records []Record) error {

	Qry := `INSERT INTO PayloadToPiece (PieceCid, PayloadMultihash, BlockOffset, BlockSize) VALUES (?, ?, ?, ?)`
	pieceCidBytes := pieceCid.Bytes()

	batchSize := len(records) / i.settings.InsertConcurrency // split the slice into go-routine batches

	if batchSize == 0 {
		batchSize = len(records)
	}

	log.Debugw("addIndex call", "BatchSize", batchSize, "Total Records", len(records))

	var eg errgroup.Group

	// Batch to allow multiple goroutines concurrency to do batch inserts
	// These batches will be further batched based on InsertBatchSize in the
	// goroutines for each BatchInsert operation
	for start := 0; start < len(records); start += batchSize {
		start := start
		end := start + batchSize
		if end >= len(records) {
			end = len(records)
		}

		eg.Go(func() error {
			var batch *gocql.Batch
			recsb := records[start:end]
			// Running a loop on all instead of creating batches require less memory
			for allIdx, rec := range recsb {
				if batch == nil {
					batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batch.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}

				batch.Entries = append(batch.Entries, gocql.BatchEntry{
					Stmt:       Qry,
					Args:       []any{pieceCidBytes, rec.Cid.Hash(), rec.Offset, rec.Size},
					Idempotent: true,
				})

				if allIdx == len(recsb)-1 || len(batch.Entries) == i.settings.InsertBatchSize {
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
			return nil
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
	Qry := `DELETE FROM PayloadToPiece WHERE PayloadMultihash = ? AND PieceCid = ?`
	pieceCidBytes := pieceCid.Bytes()

	// Get multihashes for piece
	recs, err := i.GetIndex(ctx, pieceCid)
	if err != nil {
		return fmt.Errorf("removing indexes for piece %s: getting recs: %w", pieceCid, err)
	}

	// Batch delete with concurrency
	var eg errgroup.Group
	for j := 0; j < i.settings.InsertConcurrency; j++ {
		eg.Go(func() error {
			var batch *gocql.Batch
			var num int
			select {
			case <-ctx.Done():
				return ctx.Err()
			case rec, ok := <-recs:
				if !ok {
					// Finished adding all the queued items, exit the thread
					err := i.session.ExecuteBatch(batch)
					if err != nil {
						return fmt.Errorf("executing batch delete for piece %s: %w", pieceCid, err)
					}
					return nil
				}

				if batch == nil {
					batch = i.session.NewBatch(gocql.UnloggedBatch).WithContext(ctx)
					batch.Entries = make([]gocql.BatchEntry, 0, i.settings.InsertBatchSize)
				}

				batch.Entries = append(batch.Entries, gocql.BatchEntry{
					Stmt:       Qry,
					Args:       []any{pieceCidBytes, rec.Cid.Hash(), rec.Offset, rec.Size},
					Idempotent: true,
				})

				num++

				if num == i.settings.InsertBatchSize {
					err := i.session.ExecuteBatch(batch)
					if err != nil {
						return fmt.Errorf("executing batch delete for piece %s: %w", pieceCid, err)
					}
				}

			}
			return nil
		})
	}
	err = eg.Wait()
	if err != nil {
		return err
	}

	return nil
}

type IndexRecord struct {
	Record
	Error error `json:"error,omitempty"`
}

// GetIndex retrieves the multihashes and offset/size information for a given piece CID.
// It returns a channel of `IndexRecord` structs, which include the CID and the offset/size information.
// If no multihashes are found for the piece, it returns an error.
func (i *IndexStore) GetIndex(ctx context.Context, pieceCid cid.Cid) (<-chan IndexRecord, error) {
	qry := `SELECT PayloadMultihash, BlockOffset, BlockSize FROM PayloadToPiece WHERE PieceCid = ?`
	iter := i.session.Query(qry, pieceCid.Bytes()).WithContext(ctx).Iter()

	records := make(chan IndexRecord)

	parseRecord := func(payload []byte, off, s uint64) {
		_, pmh, err := multihash.MHFromBytes(payload)
		if err != nil {
			records <- IndexRecord{Error: err}
			return
		}

		records <- IndexRecord{
			Record: Record{
				Cid: cid.NewCidV1(cid.Raw, pmh),
				OffsetSize: OffsetSize{
					Offset: off,
					Size:   s,
				},
			},
		}
	}

	var payloadMHBz []byte
	var offset, size uint64

	// Try to find first record. If not found then we don't have index
	// for this piece
	found := iter.Scan(&payloadMHBz, &offset, &size)
	if !found {
		return nil, fmt.Errorf("no multihashed found for piece %s", pieceCid.String())
	}
	parseRecord(payloadMHBz, offset, size)

	go func() {
		defer close(records)

		for iter.Scan(&payloadMHBz, &offset, &size) {
			parseRecord(payloadMHBz, offset, size)
		}

		if err := iter.Close(); err != nil {
			err = fmt.Errorf("getting piece index for piece %s: %w", pieceCid, err)
			records <- IndexRecord{Error: err}
		}
	}()

	return records, nil
}

// PiecesContainingMultihash gets all pieces that contain a multihash (used when retrieving by payload CID)
func (i *IndexStore) PiecesContainingMultihash(ctx context.Context, m multihash.Multihash) ([]cid.Cid, error) {
	var pcids []cid.Cid
	var bz []byte
	qry := `SELECT PieceCid FROM PayloadToPiece WHERE PayloadMultihash = ?`
	iter := i.session.Query(qry, trimMultihash(m)).WithContext(ctx).Iter()
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
