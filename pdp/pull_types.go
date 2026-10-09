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

// PullProvider describes remote SPs from which piece URLs are assembled.
// Each host is a hostname (optionally with port or https:// scheme). Each CID
// is turned into https://{host}/piece/{cid}, and hosts are tried in order.
type PullProvider struct {
	Hosts []string `json:"hosts"`
	CIDs  []string `json:"cids,omitempty"`
}

// PullPieceRequest is one piece and the source URLs to try, in order.
// sourceUrl is the legacy single URL. When both fields are set, sourceUrl is
// tried before sourceUrls.
type PullPieceRequest struct {
	PieceCid   string   `json:"pieceCid"`
	SourceURL  string   `json:"sourceUrl,omitempty"`
	SourceURLs []string `json:"sourceUrls,omitempty"`
}

func (p PullPieceRequest) orderedSourceURLs() []string {
	if p.SourceURL == "" {
		return p.SourceURLs
	}
	urls := make([]string, 0, 1+len(p.SourceURLs))
	urls = append(urls, p.SourceURL)
	urls = append(urls, p.SourceURLs...)
	return urls
}

type pullSource struct {
	PieceCid    string
	SourceURL   string
	SourceOrder int
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
// pieces, urls, and provider are optional and are assembled into an ordered
// per-URL pull item list. URLs for one piece are tried in that order.
type PullRequest struct {
	ExtraData    string             `json:"extraData"`
	DataSetId    *uint64            `json:"dataSetId,omitempty"`    // nil or 0 = create new dataset
	RecordKeeper *string            `json:"recordKeeper,omitempty"` // required when dataSetId is nil/0
	Pieces       []PullPieceRequest `json:"pieces,omitempty"`
	URLs         [][]string         `json:"urls,omitempty"`
	Provider     *PullProvider      `json:"provider,omitempty"`
}

// AssembledSources combines pieces.sourceUrls, top-level urls, and
// provider {hosts, cids} into de-duplicated (pieceCid, sourceUrl) pairs.
// urls is an array of arrays: each inner array is the ordered URL list for
// one piece. Later duplicates of the same (pieceCid, URL) are dropped so the
// first position is the one ingest tries first.
func (r *PullRequest) AssembledSources() ([]pullSource, error) {
	sources := make([]pullSource, 0, len(r.Pieces)+len(r.URLs))
	seen := make(map[string]struct{}, len(r.Pieces)+len(r.URLs))
	order := make(map[string]int, len(r.Pieces)+len(r.URLs))
	var knownCids []string
	seenCid := make(map[string]struct{}, len(r.Pieces)+len(r.URLs))

	add := func(pieceCid, sourceURL string) {
		key := pieceCid + "\x00" + sourceURL
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		sources = append(sources, pullSource{PieceCid: pieceCid, SourceURL: sourceURL, SourceOrder: order[pieceCid]})
		order[pieceCid]++
		if _, ok := seenCid[pieceCid]; !ok {
			seenCid[pieceCid] = struct{}{}
			knownCids = append(knownCids, pieceCid)
		}
	}

	seenPiece := make(map[string]struct{}, len(r.Pieces))
	for i, piece := range r.Pieces {
		if piece.PieceCid == "" {
			return nil, fmt.Errorf("piece[%d]: pieceCid is required", i)
		}
		if _, ok := seenPiece[piece.PieceCid]; ok {
			return nil, fmt.Errorf("piece[%d]: duplicate pieceCid", i)
		}
		seenPiece[piece.PieceCid] = struct{}{}
		sourceURLs := piece.orderedSourceURLs()
		if len(sourceURLs) == 0 {
			return nil, fmt.Errorf("piece[%d]: sourceUrl or sourceUrls is required", i)
		}
		seenURL := make(map[string]struct{}, len(sourceURLs))
		for j, sourceURL := range sourceURLs {
			urlIndex := j
			if piece.SourceURL != "" {
				urlIndex--
			}
			if sourceURL == "" {
				return nil, fmt.Errorf("piece[%d].sourceUrls[%d] is empty", i, urlIndex)
			}
			if _, ok := seenURL[sourceURL]; ok {
				return nil, fmt.Errorf("piece[%d]: duplicate sourceUrls", i)
			}
			seenURL[sourceURL] = struct{}{}
			add(piece.PieceCid, sourceURL)
		}
	}

	for i, group := range r.URLs {
		if len(group) == 0 {
			return nil, fmt.Errorf("urls[%d] is empty", i)
		}
		var pieceCid string
		seenURL := make(map[string]struct{}, len(group))
		for j, sourceURL := range group {
			if sourceURL == "" {
				return nil, fmt.Errorf("urls[%d][%d] is empty", i, j)
			}
			cid, err := pieceCIDFromSourceURL(sourceURL)
			if err != nil {
				return nil, fmt.Errorf("urls[%d][%d]: %w", i, j, err)
			}
			if j == 0 {
				pieceCid = cid
			} else if cid != pieceCid {
				return nil, fmt.Errorf("urls[%d]: URLs must refer to the same piece", i)
			}
			if _, ok := seenURL[sourceURL]; ok {
				return nil, fmt.Errorf("urls[%d]: duplicate URL", i)
			}
			seenURL[sourceURL] = struct{}{}
			add(pieceCid, sourceURL)
		}
	}

	if r.Provider != nil {
		hosts := make([]string, 0, len(r.Provider.Hosts))
		seenHost := make(map[string]struct{}, len(r.Provider.Hosts))
		for i, host := range r.Provider.Hosts {
			host = strings.TrimSpace(host)
			if host == "" {
				return nil, fmt.Errorf("provider.hosts[%d] is empty", i)
			}
			if _, ok := seenHost[host]; ok {
				return nil, fmt.Errorf("provider.hosts[%d]: duplicate host", i)
			}
			seenHost[host] = struct{}{}
			hosts = append(hosts, host)
		}
		if len(r.Provider.CIDs) > 0 && len(hosts) == 0 {
			return nil, fmt.Errorf("provider.cids requires provider.hosts")
		}
		if len(hosts) > 0 {
			cids := r.Provider.CIDs
			if len(cids) == 0 {
				cids = knownCids
			}
			for i, pieceCid := range cids {
				if pieceCid == "" {
					return nil, fmt.Errorf("provider.cids[%d] is empty", i)
				}
				// Hosts are appended in array order so ingest tries them in that order.
				for _, host := range hosts {
					assembled, err := assembleProviderPieceURL(host, pieceCid)
					if err != nil {
						return nil, err
					}
					add(pieceCid, assembled)
				}
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

	// CID format is checked later by ParsePieceCidV2. The same piece may have
	// several source URLs so the server can try all of them. An exact duplicate
	// is dropped during assembly except for repeated pieces[] entries, which
	// are rejected. Piece count is not capped here; the Filecoin message size
	// is enforced later when packing addPieces.
	for _, source := range sources {
		if err := ValidatePullSourceURL(source.SourceURL); err != nil {
			return fmt.Errorf("piece %s: %w", source.PieceCid, err)
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
	ClientAddress string // FWSS payer address
}

// PullPiece represents one source URL stored for a pull request.
// SourceOrder is the client try order for that piece; lower values are first.
type PullPiece struct {
	CidV1       cid.Cid
	RawSize     uint64
	SourceURL   string
	SourceOrder int
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
				INSERT INTO pdp_piece_pull_items (fetch_id, piece_cid, piece_raw_size, source_url, source_ord)
				VALUES ($1, $2, $3, $4, $5)
			`, pullID, piece.CidV1.String(), piece.RawSize, piece.SourceURL, piece.SourceOrder)
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
		ORDER BY fi.piece_cid, fi.piece_raw_size, fi.source_ord, fi.source_url
	`, pullID)
	if err != nil {
		return nil, fmt.Errorf("query pull items: %w", err)
	}

	// One status per piece. Multiple source URLs are fallbacks for that piece,
	// so their rows are folded together. Rows are ordered by piece, then try order.
	result := make([]PullPieceStatus, 0, len(items))
	var currentCID string
	var current []PullStatus
	flush := func() {
		if currentCID == "" {
			return
		}
		result = append(result, PullPieceStatus{
			PieceCid: currentCID,
			Status:   aggregatePullStatuses(current),
		})
	}
	for _, item := range items {
		c, err := cid.Parse(item.PieceCid)
		if err != nil {
			return nil, fmt.Errorf("parse CID %q: %w", item.PieceCid, err)
		}
		cidV2, err := commcid.PieceCidV2FromV1(c, item.PieceRawSize)
		if err != nil {
			return nil, fmt.Errorf("reconstruct piece CIDv2 for %q/%d: %w", item.PieceCid, item.PieceRawSize, err)
		}
		cidV2Str := cidV2.String()
		status := pullStatusFromItem(item.Complete, item.Failed, item.TaskID, item.TaskExists, item.Retries)
		if cidV2Str != currentCID {
			flush()
			currentCID = cidV2Str
			current = nil
		}
		current = append(current, status)
	}
	flush()

	return result, nil
}

// aggregatePullStatuses folds per-URL rows into one piece status. A non-terminal
// URL keeps the piece non-terminal. After every URL is terminal, any success
// makes the piece complete.
func aggregatePullStatuses(statuses []PullStatus) PullStatus {
	if len(statuses) == 0 {
		return PullStatusPending
	}

	completeCount := 0
	failedCount := 0
	hasPending := false
	hasInProgress := false
	hasRetrying := false
	for _, status := range statuses {
		switch status {
		case PullStatusComplete:
			completeCount++
		case PullStatusFailed:
			failedCount++
		case PullStatusRetrying:
			hasRetrying = true
		case PullStatusInProgress:
			hasInProgress = true
		default:
			hasPending = true
		}
	}

	switch {
	case hasRetrying:
		return PullStatusRetrying
	case hasInProgress:
		return PullStatusInProgress
	case hasPending:
		return PullStatusPending
	case failedCount == len(statuses):
		return PullStatusFailed
	case completeCount > 0:
		return PullStatusComplete
	default:
		return PullStatusPending
	}
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
