package pdp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/curiostorage/harmonyquery"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// pullAllowInsecure relaxes security validations for development/testing environments.
// Set CURIO_PULL_ALLOW_INSECURE=1 to:
//   - Allow HTTP (not just HTTPS) for pull source URLs
//   - Allow localhost and private IP addresses
//
// WARNING: Never enable this in production!
var pullAllowInsecure = os.Getenv("CURIO_PULL_ALLOW_INSECURE") == "1"

func PullAllowInsecure() bool {
	return pullAllowInsecure
}

// PullStatus represents the status of a pull operation or piece
type PullStatus string

const (
	PullStatusPending    PullStatus = "pending"
	PullStatusInProgress PullStatus = "inProgress"
	PullStatusRetrying   PullStatus = "retrying"
	PullStatusComplete   PullStatus = "complete"
	PullStatusFailed     PullStatus = "failed"
)

// piecePathPattern matches URLs ending with /piece/{cid}
var piecePathPattern = regexp.MustCompile(`/piece/([^/]+)$`)

// ValidatePullSourceURL validates that a source URL is safe and properly formatted
// for pulling a piece from another SP.
//
// Validation rules:
//   - Must be HTTPS
//   - Path must end with /piece/{pieceCid}
//   - The pieceCid in the URL must match the expected pieceCid
//   - Host must not be localhost, private IP, or link-local address
func ValidatePullSourceURL(sourceURL string, expectedPieceCid string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS (or HTTP if explicitly allowed for development)
	if parsed.Scheme != "https" && (!pullAllowInsecure || parsed.Scheme != "http") {
		return fmt.Errorf("URL must use HTTPS scheme, got %q", parsed.Scheme)
	}

	// Validate path matches /piece/{cid} pattern
	matches := piecePathPattern.FindStringSubmatch(parsed.Path)
	if matches == nil {
		return fmt.Errorf("URL path must end with /piece/{pieceCid}, got %q", parsed.Path)
	}

	// Extract pieceCid from URL and compare with expected
	urlPieceCid := matches[1]
	if urlPieceCid != expectedPieceCid {
		return fmt.Errorf("pieceCid in URL %q does not match expected %q", urlPieceCid, expectedPieceCid)
	}

	// Validate host is not a private/local address (skip in devnet mode)
	if !pullAllowInsecure {
		if err := validatePublicHost(parsed.Host); err != nil {
			return err
		}
	}

	return nil
}

// validatePublicHost ensures the host is not localhost, a private IP, or link-local address
func validatePublicHost(host string) error {
	// Strip port if present
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	// Strip brackets from IPv6 addresses (e.g., [::1] -> ::1)
	hostname = strings.TrimPrefix(hostname, "[")
	hostname = strings.TrimSuffix(hostname, "]")

	// Block localhost and common aliases
	lower := strings.ToLower(hostname)
	if strings.HasPrefix(lower, "localhost") ||
		lower == "ip6-localhost" ||
		lower == "ip6-loopback" {
		return fmt.Errorf("localhost addresses are not allowed")
	}

	// Try to parse as IP address and validate
	ip := net.ParseIP(hostname)
	if ip != nil {
		if err := validatePublicIP(ip); err != nil {
			return err
		}
	}

	// For hostnames, we can't fully validate without DNS lookup
	// The actual connection will fail if it resolves to a private IP
	// Additional protection could be added at the HTTP client level

	return nil
}

// validatePublicIP checks that an IP address is not private, loopback, or link-local
func validatePublicIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("loopback addresses are not allowed")
	}
	if ip.IsPrivate() {
		return fmt.Errorf("private IP addresses are not allowed")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local addresses are not allowed")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified addresses (0.0.0.0, ::) are not allowed")
	}

	return nil
}

// PullPieceRequest represents a single piece in a pull request
type PullPieceRequest struct {
	PieceCid  string `json:"pieceCid"`
	SourceURL string `json:"sourceUrl"`
}

// PullRequest represents the incoming pull request body
type PullRequest struct {
	ExtraData    string             `json:"extraData"`
	DataSetId    *uint64            `json:"dataSetId,omitempty"`    // nil or 0 = create new dataset
	RecordKeeper *string            `json:"recordKeeper,omitempty"` // required when dataSetId is nil/0
	Pieces       []PullPieceRequest `json:"pieces"`
}

// IsCreateNew returns true if this pull will create a new dataset (dataSetId is nil or 0)
func (r *PullRequest) IsCreateNew() bool {
	return r.DataSetId == nil || *r.DataSetId == 0
}

// Validate performs validation on the entire pull request
func (r *PullRequest) Validate() error {
	if r.ExtraData == "" {
		return fmt.Errorf("extraData is required")
	}

	// Validate dataSetId/recordKeeper combination
	if r.IsCreateNew() {
		if r.RecordKeeper == nil || *r.RecordKeeper == "" {
			return fmt.Errorf("recordKeeper is required when dataSetId is not provided or is 0")
		}
	}

	if len(r.Pieces) == 0 {
		return fmt.Errorf("at least one piece is required")
	}

	// Validate each piece (CID format validation is done later by ParsePieceCidV2).
	// The same piece may appear more than once with different source URLs so
	// PullPiece can try all supplied sources. An exact duplicate is not useful
	// and would collide with the pull item primary key.
	seenPieceSources := make(map[string]struct{}, len(r.Pieces))
	for i, piece := range r.Pieces {
		if piece.PieceCid == "" {
			return fmt.Errorf("piece[%d]: pieceCid is required", i)
		}
		if piece.SourceURL == "" {
			return fmt.Errorf("piece[%d]: sourceUrl is required", i)
		}
		key := piece.PieceCid + "\x00" + piece.SourceURL
		if _, ok := seenPieceSources[key]; ok {
			return fmt.Errorf("piece[%d]: duplicate pieceCid/sourceUrl", i)
		}
		seenPieceSources[key] = struct{}{}
		if err := ValidatePullSourceURL(piece.SourceURL, piece.PieceCid); err != nil {
			return fmt.Errorf("piece[%d]: %w", i, err)
		}
	}

	return nil
}

// PullPieceStatus represents the status of a single piece
type PullPieceStatus struct {
	PieceCid string     `json:"pieceCid"`
	Status   PullStatus `json:"status"`
}

// PullResponse represents the response from a pull request
type PullResponse struct {
	Status PullStatus        `json:"status"`
	Pieces []PullPieceStatus `json:"pieces"`
}

// PullRecord represents a pull request record from the database
type PullRecord struct {
	ID            int64
	Service       string
	ExtraDataHash []byte
	DataSetId     uint64 // 0 = create new
	RecordKeeper  string // address, required when DataSetId is 0
	ClientAddress string // FWSS payer address from extraData
}

// PullPiece represents a piece stored in a pull request (v1 CID + raw size for v2 reconstruction)
type PullPiece struct {
	CidV1     cid.Cid
	RawSize   uint64
	SourceURL string // external SP URL to pull from
}

const (
	// Admission counts active unique (piece_cid, piece_raw_size) keys plus
	// the incoming request's unique keys before inserting new pull rows.
	pullGlobalPendingLimit    = 128
	pullPerClientPendingLimit = 10
)

// PullStore abstracts database operations for the pull handler
type PullStore interface {
	// GetPullByKey retrieves a pull record by its idempotency key
	GetPullByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*PullRecord, error)

	// CreatePullWithPieces creates a pull record and its associated piece items in a transaction.
	// It returns the created pull ID and whether admission was rejected due to pull backpressure.
	CreatePullWithPieces(ctx context.Context, pull *PullRecord, pieces []PullPiece) (int64, bool, error)

	// GetPullStatus retrieves all piece statuses associated with a pull record.
	GetPullStatus(ctx context.Context, pullID int64) ([]PullPieceStatus, error)
}

// ComputeOverallStatus derives the overall status from individual piece statuses.
// Priority: failed > retrying > inProgress > pending > complete
func (r *PullResponse) ComputeOverallStatus() {
	if len(r.Pieces) == 0 {
		r.Status = PullStatusPending
		return
	}

	allComplete := true
	anyFailed := false
	anyRetrying := false
	anyInProgress := false

	for _, p := range r.Pieces {
		if p.Status != PullStatusComplete {
			allComplete = false
		}
		switch p.Status {
		case PullStatusFailed:
			anyFailed = true
		case PullStatusRetrying:
			anyRetrying = true
		case PullStatusInProgress:
			anyInProgress = true
		}
	}

	if allComplete {
		r.Status = PullStatusComplete
	} else if anyFailed {
		r.Status = PullStatusFailed
	} else if anyRetrying {
		r.Status = PullStatusRetrying
	} else if anyInProgress {
		r.Status = PullStatusInProgress
	} else {
		r.Status = PullStatusPending
	}
}

// dbPullStore implements PullStore using harmonydb
type dbPullStore struct {
	db *harmonydb.DB
	mu sync.Mutex
}

// NewDBPullStore creates a PullStore backed by harmonydb
func NewDBPullStore(db *harmonydb.DB) PullStore {
	return &dbPullStore{db: db}
}

func (s *dbPullStore) GetPullByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*PullRecord, error) {
	var records []struct {
		ID            int64  `db:"id"`
		Service       string `db:"service"`
		ExtraDataHash []byte `db:"extra_data_hash"`
		DataSetId     uint64 `db:"data_set_id"`
		RecordKeeper  string `db:"record_keeper"`
		ClientAddress string `db:"client_address"`
	}

	err := s.db.Select(ctx, &records, `
		SELECT id, service, extra_data_hash, data_set_id, record_keeper, client_address
		FROM pdp_piece_pulls
		WHERE service = $1 AND extra_data_hash = $2 AND data_set_id = $3 AND record_keeper = $4
	`, service, hash, dataSetId, recordKeeper)
	if err != nil {
		return nil, fmt.Errorf("query pull by key: %w", err)
	}

	if len(records) == 0 {
		return nil, nil
	}

	return &PullRecord{
		ID:            records[0].ID,
		Service:       records[0].Service,
		ExtraDataHash: records[0].ExtraDataHash,
		DataSetId:     records[0].DataSetId,
		RecordKeeper:  records[0].RecordKeeper,
		ClientAddress: records[0].ClientAddress,
	}, nil
}

func (s *dbPullStore) CreatePullWithPieces(ctx context.Context, pull *PullRecord, pieces []PullPiece) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pullID int64
	var backpressure bool
	var existing bool

	comm, err := s.db.BeginTransaction(ctx, func(tx *harmonyquery.Tx) (commit bool, err error) {
		err = tx.QueryRow(`
			INSERT INTO pdp_piece_pulls (service, extra_data_hash, data_set_id, record_keeper, client_address)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (service, extra_data_hash, data_set_id, record_keeper) DO NOTHING
			RETURNING id
		`, pull.Service, pull.ExtraDataHash, pull.DataSetId, pull.RecordKeeper, pull.ClientAddress).Scan(&pullID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(`
				SELECT id
				FROM pdp_piece_pulls
				WHERE service = $1
					AND extra_data_hash = $2
					AND data_set_id = $3
					AND record_keeper = $4
			`, pull.Service, pull.ExtraDataHash, pull.DataSetId, pull.RecordKeeper).Scan(&pullID)
			if err != nil {
				return false, fmt.Errorf("query existing pull after conflict: %w", err)
			}
			existing = true
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("insert pull: %w", err)
		}

		backpressure, err = s.enforceBackpressure(tx, pull, pieces)
		if err != nil {
			return false, err
		}

		if backpressure {
			return false, nil
		}

		// Insert piece items with raw size and source_url for task to pick up
		for _, piece := range pieces {
			_, err := tx.Exec(`
				INSERT INTO pdp_piece_pull_items (fetch_id, piece_cid, piece_raw_size, source_url)
				VALUES ($1, $2, $3, $4)
			`, pullID, piece.CidV1.String(), piece.RawSize, piece.SourceURL)
			if err != nil {
				return false, fmt.Errorf("insert pull item: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return pullID, false, xerrors.Errorf("insert pull items: %w", err)
	}

	if existing {
		return pullID, false, nil
	}

	if !comm && !backpressure {
		return pullID, false, xerrors.Errorf("failed to commit the transaction")
	}

	if backpressure {
		return pullID, true, nil
	}

	return pullID, false, nil
}

func (s *dbPullStore) GetPullStatus(ctx context.Context, pullID int64) ([]PullPieceStatus, error) {
	var items []struct {
		PieceCid     string `db:"piece_cid"`
		PieceRawSize uint64 `db:"piece_raw_size"`
		Complete     bool   `db:"complete"`
		Failed       bool   `db:"failed"`
		TaskID       *int64 `db:"task_id"`
		TaskExists   bool   `db:"task_exists"`
		Retries      int    `db:"retries"`
	}

	err := s.db.Select(ctx, &items, `
		SELECT fi.piece_cid, fi.piece_raw_size,
		       fi.complete, fi.failed,
		       fi.task_id, (ht.id IS NOT NULL) AS task_exists, COALESCE(ht.retries, 0) AS retries
		FROM pdp_piece_pull_items fi
		LEFT JOIN harmony_task ht ON ht.id = fi.task_id
		WHERE fi.fetch_id = $1
		ORDER BY fi.piece_cid, fi.source_url
	`, pullID)
	if err != nil {
		return nil, fmt.Errorf("query pull items: %w", err)
	}

	result := make([]PullPieceStatus, len(items))
	for i, item := range items {
		c, err := cid.Parse(item.PieceCid)
		if err != nil {
			return nil, fmt.Errorf("parse CID %q: %w", item.PieceCid, err)
		}
		cidV2, err := commcid.PieceCidV2FromV1(c, item.PieceRawSize)
		if err != nil {
			return nil, fmt.Errorf("reconstruct piece CIDv2 for %q/%d: %w", item.PieceCid, item.PieceRawSize, err)
		}
		result[i] = PullPieceStatus{
			PieceCid: cidV2.String(),
			Status:   pullStatusFromItem(item.Complete, item.Failed, item.TaskID, item.TaskExists, item.Retries),
		}
	}

	return result, nil
}

func pullStatusFromItem(complete, failed bool, taskID *int64, taskExists bool, retries int) PullStatus {
	if failed {
		return PullStatusFailed
	}
	if complete {
		return PullStatusComplete
	}
	if taskID == nil {
		return PullStatusPending
	}
	if taskExists {
		if retries > 0 {
			return PullStatusRetrying
		}
		return PullStatusInProgress
	}
	return PullStatusPending
}

func (s *dbPullStore) enforceBackpressure(tx *harmonydb.Tx, pull *PullRecord, pieces []PullPiece) (bool, error) {
	type pieceKey struct {
		cid     string
		rawSize uint64
	}

	seen := make(map[pieceKey]struct{}, len(pieces))
	pieceCids := make([]string, 0, len(pieces))
	rawSizes := make([]int64, 0, len(pieces))
	for _, piece := range pieces {
		key := pieceKey{cid: piece.CidV1.String(), rawSize: piece.RawSize}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		pieceCids = append(pieceCids, key.cid)
		rawSizes = append(rawSizes, int64(key.rawSize))
	}

	if len(pieceCids) == 0 {
		return false, nil
	}

	var globalPending, clientPending int
	err := tx.QueryRow(`
		WITH incoming AS (
			SELECT DISTINCT piece_cid, piece_raw_size
			FROM unnest($1::TEXT[], $2::BIGINT[]) AS t(piece_cid, piece_raw_size)
		),
		global_active AS (
			SELECT DISTINCT fi.piece_cid, fi.piece_raw_size
			FROM pdp_piece_pull_items fi
			WHERE fi.complete = FALSE
				AND fi.failed = FALSE
		),
		client_active AS (
			SELECT DISTINCT fi.piece_cid, fi.piece_raw_size
			FROM pdp_piece_pull_items fi
			JOIN pdp_piece_pulls fp ON fp.id = fi.fetch_id
			WHERE fi.complete = FALSE
				AND fi.failed = FALSE
				AND fp.client_address = $3
		),
		global_combined AS (
			SELECT piece_cid, piece_raw_size FROM global_active
			UNION
			SELECT piece_cid, piece_raw_size FROM incoming
		),
		client_combined AS (
			SELECT piece_cid, piece_raw_size FROM client_active
			UNION
			SELECT piece_cid, piece_raw_size FROM incoming
		)
		SELECT
			(SELECT COUNT(*) FROM global_combined),
			(SELECT COUNT(*) FROM client_combined)
	`, pieceCids, rawSizes, pull.ClientAddress).Scan(&globalPending, &clientPending)
	if err != nil {
		return false, fmt.Errorf("count pull pending pieces: %w", err)
	}
	if globalPending > pullGlobalPendingLimit {
		return true, nil
	}
	if clientPending > pullPerClientPendingLimit {
		return true, nil
	}

	return false, nil
}
