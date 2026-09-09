package pdp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/curiostorage/harmonyquery"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// pullAllowInsecure relaxes security validations for development/testing environments.
// Set CURIO_PULL_ALLOW_INSECURE=1 to allow HTTP (not just HTTPS) for pull source
// URLs, and to let the pull task's SSRF policy reach localhost and private IPs.
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

// ValidatePullSourceURL validates that a source URL is safe for pulling a piece
// from another SP.
//
// The only rule is that the URL must be HTTPS. The path shape is unconstrained,
// since the piece is verified by recomputing its CommP from the downloaded
// bytes, and the host is checked against the SSRF policy at fetch time, which
// resolves DNS rather than matching on the URL string.
func ValidatePullSourceURL(sourceURL string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS (or HTTP if explicitly allowed for development)
	if parsed.Scheme != "https" && (!pullAllowInsecure || parsed.Scheme != "http") {
		return fmt.Errorf("URL must use HTTPS scheme, got %q", parsed.Scheme)
	}

	return nil
}

// PullProvider describes a remote SP from which piece URLs are assembled.
// Host is a hostname (optionally with port or https:// scheme). Each CID is
// turned into https://{host}/piece/{cid} on this provider.
type PullProvider struct {
	Host string   `json:"host"`
	CIDs []string `json:"cids,omitempty"`
}

// PullPieceRequest is the current per-piece form: one piece CID and one source URL.
type PullPieceRequest struct {
	PieceCid  string `json:"pieceCid"`
	SourceURL string `json:"sourceUrl"`
}

type pullSource struct {
	PieceCid  string
	SourceURL string
}

func assembleProviderPieceURL(host, pieceCID string) (string, error) {
	raw := host
	if !strings.Contains(host, "://") {
		raw = "https://" + host
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid provider host %q: %w", host, err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid provider host %q: missing hostname", host)
	}

	return parsed.JoinPath("piece", pieceCID).String(), nil
}

func pieceCIDFromSourceURL(sourceURL string) (string, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	path := strings.TrimSuffix(parsed.Path, "/")
	const marker = "/piece/"
	idx := strings.LastIndex(path, marker)
	if idx < 0 {
		return "", fmt.Errorf("URL path must contain /piece/{cid}")
	}
	cidPart := path[idx+len(marker):]
	if cidPart == "" || strings.Contains(cidPart, "/") {
		return "", fmt.Errorf("URL path must end with /piece/{cid}")
	}
	return cidPart, nil
}

// PullRequest represents the incoming pull request body.
// pieces, urls, and provider are optional and are assembled into the same
// per-URL pull item list used today.
type PullRequest struct {
	ExtraData    string             `json:"extraData"`
	DataSetId    *uint64            `json:"dataSetId,omitempty"`    // nil or 0 = create new dataset
	RecordKeeper *string            `json:"recordKeeper,omitempty"` // required when dataSetId is nil/0
	Pieces       []PullPieceRequest `json:"pieces,omitempty"`
	URLs         []string           `json:"urls,omitempty"`
	Provider     *PullProvider      `json:"provider,omitempty"`
}

// AssembledSources combines the current pieces form, top-level urls, and
// provider {host, cids} into de-duplicated (pieceCid, sourceUrl) pairs.
func (r *PullRequest) AssembledSources() ([]pullSource, error) {
	sources := make([]pullSource, 0, len(r.Pieces)+len(r.URLs))
	seen := make(map[string]struct{}, len(r.Pieces)+len(r.URLs))
	var knownCids []string
	seenCid := make(map[string]struct{}, len(r.Pieces)+len(r.URLs))

	add := func(pieceCid, sourceURL string) {
		key := pieceCid + "\x00" + sourceURL
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		sources = append(sources, pullSource{PieceCid: pieceCid, SourceURL: sourceURL})
		if _, ok := seenCid[pieceCid]; !ok {
			seenCid[pieceCid] = struct{}{}
			knownCids = append(knownCids, pieceCid)
		}
	}

	for i, piece := range r.Pieces {
		if piece.PieceCid == "" {
			return nil, fmt.Errorf("piece[%d]: pieceCid is required", i)
		}
		if piece.SourceURL == "" {
			return nil, fmt.Errorf("piece[%d]: sourceUrl is required", i)
		}
		key := piece.PieceCid + "\x00" + piece.SourceURL
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("piece[%d]: duplicate pieceCid/sourceUrl", i)
		}
		add(piece.PieceCid, piece.SourceURL)
	}

	for i, sourceURL := range r.URLs {
		if sourceURL == "" {
			return nil, fmt.Errorf("urls[%d] is empty", i)
		}
		pieceCid, err := pieceCIDFromSourceURL(sourceURL)
		if err != nil {
			return nil, fmt.Errorf("urls[%d]: %w", i, err)
		}
		add(pieceCid, sourceURL)
	}

	if r.Provider != nil {
		host := strings.TrimSpace(r.Provider.Host)
		if len(r.Provider.CIDs) > 0 && host == "" {
			return nil, fmt.Errorf("provider.cids requires provider.host")
		}
		if host != "" {
			cids := r.Provider.CIDs
			if len(cids) == 0 {
				cids = knownCids
			}
			for i, pieceCid := range cids {
				if pieceCid == "" {
					return nil, fmt.Errorf("provider.cids[%d] is empty", i)
				}
				assembled, err := assembleProviderPieceURL(host, pieceCid)
				if err != nil {
					return nil, err
				}
				add(pieceCid, assembled)
			}
		}
	}

	return sources, nil
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

	sources, err := r.AssembledSources()
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("at least one source URL is required")
	}

	// Unique piece CIDs are what addPieces will see. CID format is checked
	// later by ParsePieceCidV2. The same piece may have several source URLs
	// so the server can try all of them. An exact duplicate is dropped during
	// assembly except for repeated pieces[] entries, which are rejected.
	uniqueCids := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		uniqueCids[source.PieceCid] = struct{}{}
		if err := ValidatePullSourceURL(source.SourceURL); err != nil {
			return fmt.Errorf("piece %s: %w", source.PieceCid, err)
		}
	}
	if len(uniqueCids) > MaxAddPiecesBatchSize {
		return fmt.Errorf("piece count (%d) exceeds the maximum allowed per pull (%d)", len(uniqueCids), MaxAddPiecesBatchSize)
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
	ClientAddress string // FWSS payer address
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
	pullGlobalPendingLimit          = 120
	pullPerClientPendingLimit       = 10
	pullSoloClientPendingPercentage = 90
	pullRetryAfterMin               = time.Minute
	pullRetryAfterMax               = 5 * time.Minute
	pullRetryAfterStepPieces        = 10
)

type PullBackpressure struct {
	RetryAfter time.Duration
}

// PullStore abstracts database operations for the pull handler
type PullStore interface {
	// GetPullByKey retrieves a pull record by its idempotency key
	GetPullByKey(ctx context.Context, service string, hash []byte, dataSetId uint64, recordKeeper string) (*PullRecord, error)

	// CreatePullWithPieces creates a pull record and its associated piece items in a transaction.
	// It returns the created pull ID and pull backpressure details when admission is rejected.
	CreatePullWithPieces(ctx context.Context, pull *PullRecord, pieces []PullPiece) (int64, *PullBackpressure, error)

	// GetPullStatus retrieves all piece statuses associated with a pull record.
	GetPullStatus(ctx context.Context, pullID int64) ([]PullPieceStatus, error)
}

// ComputeOverallStatus derives the batch status from individual piece statuses.
// The batch only reaches a terminal status after every piece is terminal. At
// that point any successful piece makes the batch complete; failed pieces remain
// visible in per-piece status.
func (r *PullResponse) ComputeOverallStatus() {
	if len(r.Pieces) == 0 {
		r.Status = PullStatusPending
		return
	}

	completeCount := 0
	failedCount := 0
	hasPending := false
	hasInProgress := false
	hasRetrying := false

	for _, p := range r.Pieces {
		switch p.Status {
		case PullStatusComplete:
			completeCount++
		case PullStatusFailed:
			failedCount++
		case PullStatusRetrying:
			hasRetrying = true
		case PullStatusInProgress:
			hasInProgress = true
		case PullStatusPending:
			hasPending = true
		default:
			hasPending = true
		}
	}

	// Non-terminal pieces keep the batch non-terminal. Terminal status is only
	// chosen after every piece is either complete or failed.
	switch {
	case hasRetrying:
		r.Status = PullStatusRetrying
	case hasInProgress:
		r.Status = PullStatusInProgress
	case hasPending:
		r.Status = PullStatusPending
	case failedCount == len(r.Pieces):
		r.Status = PullStatusFailed
	case completeCount > 0:
		r.Status = PullStatusComplete
	default:
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

func (s *dbPullStore) CreatePullWithPieces(ctx context.Context, pull *PullRecord, pieces []PullPiece) (int64, *PullBackpressure, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var pullID int64
	var backpressure *PullBackpressure
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

		if backpressure != nil {
			return false, nil
		}

		// Insert piece items with raw size and source URL for processing.
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
		return pullID, nil, xerrors.Errorf("insert pull items: %w", err)
	}

	if existing {
		return pullID, nil, nil
	}

	if !comm && backpressure == nil {
		return pullID, nil, xerrors.Errorf("failed to commit the transaction")
	}

	if backpressure != nil {
		return pullID, backpressure, nil
	}

	return pullID, nil, nil
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

// enforceBackpressure decides whether a new pull can be admitted without
// exceeding global or per-client pending-piece limits.
func (s *dbPullStore) enforceBackpressure(tx *harmonydb.Tx, pull *PullRecord, pieces []PullPiece) (*PullBackpressure, error) {
	type pieceKey struct {
		cid     string
		rawSize uint64
	}

	// Backpressure is counted by unique piece key, not by URL or pull item.
	// Multiple URLs for the same piece should not consume multiple slots.
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
		return nil, nil
	}

	var globalPending, clientPending, otherPending int
	// Count the post-admission state in one query:
	// - globalPending: all active unique pieces plus this request's unique pieces
	// - clientPending: this client's active unique pieces plus this request
	// - otherPending: active unique pieces owned by all other clients
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
		other_active AS (
			SELECT DISTINCT fi.piece_cid, fi.piece_raw_size
			FROM pdp_piece_pull_items fi
			JOIN pdp_piece_pulls fp ON fp.id = fi.fetch_id
			WHERE fi.complete = FALSE
				AND fi.failed = FALSE
				AND fp.client_address <> $3
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
			(SELECT COUNT(*) FROM client_combined),
			(SELECT COUNT(*) FROM other_active)
	`, pieceCids, rawSizes, pull.ClientAddress).Scan(&globalPending, &clientPending, &otherPending)
	if err != nil {
		return nil, fmt.Errorf("count pull pending pieces: %w", err)
	}
	// The global limit is absolute. A request is rejected if accepting it would
	// push total active unique pieces over the node-wide cap.
	if globalPending > pullGlobalPendingLimit {
		return &PullBackpressure{RetryAfter: pullRetryAfter(globalPending, pullGlobalPendingLimit)}, nil
	}

	// The per-client limit is soft while the node is otherwise empty. A client
	// can borrow unused slots up to the solo-client ceiling, but other clients'
	// pending pieces reduce that allowance.
	clientLimit := pullEffectiveClientPendingLimit(otherPending)
	if clientPending > clientLimit {
		return &PullBackpressure{RetryAfter: pullRetryAfter(clientPending, clientLimit)}, nil
	}

	return nil, nil
}

func pullClientBorrowLimit() int {
	return pullGlobalPendingLimit * pullSoloClientPendingPercentage / 100
}

// pullEffectiveClientPendingLimit returns how many unique pending pieces one
// client may hold after accounting for capacity already used by other clients.
func pullEffectiveClientPendingLimit(otherPending int) int {
	limit := pullClientBorrowLimit() - otherPending
	if limit < pullPerClientPendingLimit {
		return pullPerClientPendingLimit
	}
	return limit
}

// pullRetryAfter suggests how long a rejected pull should wait: 1 minute per 10
// pieces over the limit, clamped to [1, 5] minutes.
func pullRetryAfter(pending, limit int) time.Duration {
	over := pending - limit
	if over <= 0 {
		return pullRetryAfterMin
	}

	minutes := 1 + (over-1)/pullRetryAfterStepPieces
	retryAfter := time.Duration(minutes) * time.Minute
	if retryAfter < pullRetryAfterMin {
		return pullRetryAfterMin
	}
	if retryAfter > pullRetryAfterMax {
		return pullRetryAfterMax
	}
	return retryAfter
}
