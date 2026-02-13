// Package indexstore provides a Cassandra-backed index for locating blocks
// within Filecoin pieces.
//
// It maintains two core lookup tables:
//
//   - PayloadToPieces: multihash → piece CID (find which pieces contain a block)
//   - PieceBlockOffsetSize: piece CID + multihash → byte offset (locate a block within a piece)
//
// It also provides a PDP (Provable Data Possession) cache layer for storing
// pre-computed Merkle tree nodes used during proof challenges.
//
// All methods that accept a pieceCid are CID-version-agnostic: callers may
// supply either a v1 (commP) or v2 (commP + size) piece CID. The store
// records whichever version the caller provides. The only method that is
// version-aware is UpdatePieceCidV1ToV2, which explicitly migrates rows
// keyed under a v1 CID to a v2 CID.
//
// The package is organised across several files:
//
//   - indexstore.go   – types, construction, session bootstrap
//   - index_write.go  – AddIndex, RemoveIndexes
//   - index_query.go  – PiecesContainingMultihash, GetOffset, GetPieceHashRange, CheckHasPiece
//   - migration.go    – UpdatePieceCidV1ToV2
//   - pdp.go          – PDP Merkle-tree cache layer CRUD
//   - batch.go        – shared batch-execution helpers with retry logic
package indexstore

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/gocql"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
)

// ---------------------------------------------------------------------------
// Package-level constants
// ---------------------------------------------------------------------------

const (
	// keyspace is the default Cassandra keyspace name used in production.
	keyspace = "curio"

	// defaultBatchSize is the fallback batch size when the configured value
	// is zero or negative.
	defaultBatchSize = 15_000

	// maxRetries is the maximum number of retry attempts for a failed CQL batch.
	maxRetries = 20

	// initialBackoff is the starting delay between retries.
	initialBackoff = 20 * time.Second

	// maxBackoff caps the exponential backoff duration.
	maxBackoff = 180 * time.Second

	// slowBatchThreshold logs a warning when a batch takes longer than this.
	slowBatchThreshold = 30 * time.Second

	// hashDigestSize is the byte length of a PDP Merkle-tree node hash.
	hashDigestSize = 32

	// typicalMultihashSize is a pre-allocation hint for multihash byte slices.
	typicalMultihashSize = 36

	// pdpPageSize is the Cassandra page size used when scanning PDP layer rows.
	pdpPageSize = 2000
)

//go:embed cql/*.cql
var cqlFiles embed.FS

var log = logging.Logger("indexstore")

// ErrNotFound is returned when a queried item does not exist in the store.
var ErrNotFound = errors.New("not found")

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// settings holds tuning parameters for batch CQL operations.
type settings struct {
	InsertBatchSize   int // records per CQL batch (default 15 000)
	InsertConcurrency int // parallel workers for AddIndex / RemoveIndexes (default 8)
}

// IndexStore wraps a Cassandra session and provides block-level indexing
// for Filecoin pieces.
type IndexStore struct {
	settings settings
	cluster  *gocql.ClusterConfig
	session  *gocql.Session
	ctx      context.Context
}

// Record represents a single block inside a piece: its CID, byte offset, and
// size. It is used both when inserting and when reading index entries.
type Record struct {
	Cid    cid.Cid `json:"cid"`
	Offset uint64  `json:"offset"`
	Size   uint64  `json:"size"`
}

// PieceInfo is a (pieceCid, blockSize) pair returned by PiecesContainingMultihash.
// The PieceCid may be either v1 or v2, depending on what was stored.
type PieceInfo struct {
	PieceCid  cid.Cid
	BlockSize uint64
}

// NodeDigest represents a single node in a cached PDP Merkle-tree layer.
type NodeDigest struct {
	Layer int      // layer index in the Merkle tree
	Index int64    // node position within the layer
	Hash  [32]byte // 32-byte hash digest
}

// ---------------------------------------------------------------------------
// Construction & initialisation
// ---------------------------------------------------------------------------

// NewIndexStore creates an IndexStore configured for the given Cassandra
// hosts. Call Start before performing any queries.
func NewIndexStore(hosts []string, port int, cfg *config.CurioConfig) (*IndexStore, error) {
	if len(hosts) == 0 {
		return nil, xerrors.Errorf("no hosts provided for cassandra")
	}

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
	}, nil
}

// ITestID is an opaque identifier used to isolate test keyspaces.
type ITestID string

// ITestNewID returns a random test keyspace suffix.
func ITestNewID() ITestID {
	return ITestID(strconv.Itoa(rand.Intn(99999)))
}

// Start opens a Cassandra session, creates the keyspace (if absent), and
// applies every embedded CQL migration file in sorted order.
//
// When test is true a randomly-named keyspace is created so that concurrent
// test runs do not interfere with each other.
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

	// Create Cassandra keyspace if it doesn't exist yet.
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

	// Reconnect with the keyspace selected so all subsequent queries use it.
	i.cluster.Keyspace = keyspaceName
	session, err = i.cluster.CreateSession()
	if err != nil {
		return xerrors.Errorf("creating cassandra session: %w", err)
	}

	// Apply embedded CQL migrations in alphabetical order.
	if err := i.applyMigrations(ctx, session); err != nil {
		return err
	}

	i.session = session
	i.ctx = ctx

	return nil
}

// applyMigrations reads every *.cql file embedded under cql/ and executes
// each semicolon-delimited statement against the given session.
func (i *IndexStore) applyMigrations(ctx context.Context, session *gocql.Session) error {
	entries, err := cqlFiles.ReadDir("cql")
	if err != nil {
		return xerrors.Errorf("reading embedded cql directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := cqlFiles.ReadFile("cql/" + entry.Name())
		if err != nil {
			return xerrors.Errorf("reading cql file %s: %w", entry.Name(), err)
		}

		// Each CQL file may contain multiple statements separated by ";".
		for _, stmt := range strings.Split(string(data), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			log.Debug(stmt)
			if err := session.Query(stmt).WithContext(ctx).Exec(); err != nil {
				return xerrors.Errorf("creating tables: executing\n%s\n%w", stmt, err)
			}
		}
	}
	return nil
}
