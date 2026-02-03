package pdp

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/dealdata"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/pdp"
)

// PullPiecePollInterval is how often to poll for new pull items
var PullPiecePollInterval = 10 * time.Second

// PDPPullPieceTask downloads pieces from external SPs, verifies CommP,
// and stores them in custore:// for StorePiece to pick up.
//
// Flow:
//  1. Handler creates pdp_piece_pull_items with source_url
//  2. This task polls for items with task_id IS NULL AND failed = FALSE
//  3. Task downloads piece, computes CommP, verifies it matches expected
//  4. On success: stores in StashStore, creates parked_pieces entry with custore:// URL
//  5. StorePiece task picks up the parked_piece and moves to long-term storage
//
// Future enhancement: implement verified piece retrieval per FRC-XXXX
// for streaming verification with intermediate tree proofs.
type PDPPullPieceTask struct {
	db      *harmonydb.DB
	storage paths.StashStore

	TF promise.Promise[harmonytask.AddTaskFunc]

	max int
}

// NewPDPPullPieceTask creates a new PDPPullPieceTask
func NewPDPPullPieceTask(ctx context.Context, db *harmonydb.DB, storage paths.StashStore, max int) *PDPPullPieceTask {
	t := &PDPPullPieceTask{
		db:      db,
		storage: storage,
		max:     max,
	}

	go t.pollPullItems(ctx)
	return t
}

// pollPullItems polls for pull items that need processing
func (t *PDPPullPieceTask) pollPullItems(ctx context.Context) {
	ticker := time.NewTicker(PullPiecePollInterval)
	defer ticker.Stop()

	for {
		var items []struct {
			FetchID      int64  `db:"fetch_id"`
			PieceCid     string `db:"piece_cid"`
			PieceRawSize int64  `db:"piece_raw_size"`
			SourceURL    string `db:"source_url"`
		}

		// Select items that:
		// 1. Have no task assigned (task_id IS NULL)
		// 2. Have not permanently failed
		// 3. Do NOT already have a parked_pieces entry (pull already completed)
		err := t.db.Select(ctx, &items, `
			SELECT fi.fetch_id, fi.piece_cid, fi.piece_raw_size, fi.source_url
			FROM pdp_piece_pull_items fi
			WHERE fi.task_id IS NULL AND fi.failed = FALSE
			AND NOT EXISTS (
				SELECT 1 FROM parked_pieces pp
				WHERE pp.piece_cid = fi.piece_cid
				AND pp.long_term = TRUE
				AND pp.cleanup_task_id IS NULL
			)
		`)
		if err != nil {
			log.Errorf("failed to query pull items: %s", err)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}

		if len(items) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				continue
			}
		}

		for _, item := range items {
			fetchID := item.FetchID
			pieceCid := item.PieceCid

			t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// Atomically assign task_id, with same checks as the SELECT query
				// to prevent race with concurrent parked_pieces creation
				n, err := tx.Exec(`
					UPDATE pdp_piece_pull_items fi
					SET task_id = $1
					WHERE fi.fetch_id = $2 AND fi.piece_cid = $3
					AND fi.task_id IS NULL AND fi.failed = FALSE
					AND NOT EXISTS (
						SELECT 1 FROM parked_pieces pp
						WHERE pp.piece_cid = fi.piece_cid
						AND pp.long_term = TRUE
						AND pp.cleanup_task_id IS NULL
					)
				`, id, fetchID, pieceCid)
				if err != nil {
					return false, xerrors.Errorf("updating pull item task_id: %w", err)
				}

				return n > 0, nil
			})
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// continue to next iteration
		}
	}
}

// pullItemData holds the data for a pull item task
type pullItemData struct {
	FetchID      int64  `db:"fetch_id"`
	PieceCid     string `db:"piece_cid"`
	PieceRawSize int64  `db:"piece_raw_size"`
	SourceURL    string `db:"source_url"`
}

func (t *PDPPullPieceTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Fetch task data
	var items []pullItemData
	err = t.db.Select(ctx, &items, `
		SELECT fetch_id, piece_cid, piece_raw_size, source_url
		FROM pdp_piece_pull_items
		WHERE task_id = $1
	`, taskID)
	if err != nil {
		return false, xerrors.Errorf("fetching task data: %w", err)
	}

	if len(items) == 0 {
		return false, xerrors.Errorf("no pull item found for task_id: %d", taskID)
	}

	item := items[0]

	// Parse expected CID
	expectedCid, err := cid.Parse(item.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("parsing expected piece CID: %w", err)
	}

	// Download and verify the piece
	stashID, err := t.downloadAndVerify(ctx, item.SourceURL, item.PieceRawSize, expectedCid)
	if err != nil {
		// Mark as failed and return error (task will retry or exhaust retries)
		log.Errorw("pull piece failed", "error", err, "pieceCid", item.PieceCid, "sourceUrl", item.SourceURL)
		return false, xerrors.Errorf("download and verify piece: %w", err)
	}

	// Get stash URL for creating parked_pieces entry
	stashURL, err := t.storage.StashURL(stashID)
	if err != nil {
		// Clean up stash on failure
		_ = t.storage.StashRemove(ctx, stashID)
		return false, xerrors.Errorf("getting stash URL: %w", err)
	}

	// Change scheme to custore://
	stashURL.Scheme = dealdata.CustoreScheme
	custoreURL := stashURL.String()

	// Calculate padded size
	paddedSize := pdp.PadPieceSize(item.PieceRawSize)

	// Create parked_pieces entry in a transaction
	_, err = t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Get the service from pdp_piece_pulls (via fetch_id)
		var service string
		err := tx.QueryRow(`
			SELECT pp.service
			FROM pdp_piece_pulls pp
			JOIN pdp_piece_pull_items ppi ON ppi.fetch_id = pp.id
			WHERE ppi.fetch_id = $1 AND ppi.piece_cid = $2
		`, item.FetchID, item.PieceCid).Scan(&service)
		if err != nil {
			return false, xerrors.Errorf("get service from pull: %w", err)
		}

		// Create parked_pieces entry
		var parkedPieceID int64
		err = tx.QueryRow(`
			INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, long_term)
			VALUES ($1, $2, $3, TRUE)
			RETURNING id
		`, item.PieceCid, paddedSize, item.PieceRawSize).Scan(&parkedPieceID)
		if err != nil {
			return false, xerrors.Errorf("insert parked_pieces: %w", err)
		}

		// Create parked_piece_refs entry with custore:// URL
		var pieceRef int64
		err = tx.QueryRow(`
			INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
			VALUES ($1, $2, TRUE)
			RETURNING ref_id
		`, parkedPieceID, custoreURL).Scan(&pieceRef)
		if err != nil {
			return false, xerrors.Errorf("insert parked_piece_refs: %w", err)
		}

		// Register piece with service (parallels notify_task.go for uploads)
		_, err = tx.Exec(`
			INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at)
			VALUES ($1, $2, $3, NOW())
		`, service, item.PieceCid, pieceRef)
		if err != nil {
			return false, xerrors.Errorf("insert pdp_piecerefs: %w", err)
		}

		// Clear task_id from pull item (task is done)
		_, err = tx.Exec(`
			UPDATE pdp_piece_pull_items
			SET task_id = NULL
			WHERE fetch_id = $1 AND piece_cid = $2
		`, item.FetchID, item.PieceCid)
		if err != nil {
			return false, xerrors.Errorf("clearing pull item task_id: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		// Clean up stash on failure
		_ = t.storage.StashRemove(ctx, stashID)
		return false, xerrors.Errorf("creating parked piece: %w", err)
	}

	log.Infow("pull piece complete", "pieceCid", item.PieceCid, "custoreUrl", custoreURL)
	return true, nil
}

// downloadAndVerify downloads a piece from sourceURL, computes CommP, and verifies it matches expected.
// On success, returns the stash ID where the piece is stored.
func (t *PDPPullPieceTask) downloadAndVerify(ctx context.Context, sourceURL string, expectedSize int64, expectedCid cid.Cid) (uuid.UUID, error) {
	// Validate URL - HTTPS required for security (HTTP allowed only with env var for testing)
	parsedURL, err := url.Parse(sourceURL)
	if err != nil {
		return uuid.UUID{}, xerrors.Errorf("invalid source URL: %w", err)
	}
	allowHTTP := os.Getenv("CURIO_PULL_ALLOW_INSECURE") == "1"
	if parsedURL.Scheme != "https" && (!allowHTTP || parsedURL.Scheme != "http") {
		return uuid.UUID{}, xerrors.Errorf("source URL must use HTTPS scheme, got: %s", parsedURL.Scheme)
	}

	// 1 hour timeout for entire pull operation
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()

	// Create HTTP client with header timeout
	client := &http.Client{
		Transport: &http.Transport{
			ResponseHeaderTimeout: 2 * time.Minute,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return uuid.UUID{}, xerrors.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return uuid.UUID{}, xerrors.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return uuid.UUID{}, xerrors.Errorf("HTTP status %d from source", resp.StatusCode)
	}

	// Limit reader to expected size + 1 to detect oversized data
	dataReader := io.LimitReader(resp.Body, expectedSize+1)

	// Create commp calculator
	cp := &commp.Calc{}
	var readSize int64

	// Write to stash while computing CommP
	writeFunc := func(f *os.File) error {
		multiWriter := io.MultiWriter(cp, f)

		n, err := io.Copy(multiWriter, dataReader)
		if err != nil {
			return xerrors.Errorf("copying piece data: %w", err)
		}

		readSize = n
		return nil
	}

	// Store in StashStore
	stashID, err := t.storage.StashCreate(ctx, expectedSize, writeFunc)
	if err != nil {
		return uuid.UUID{}, xerrors.Errorf("creating stash: %w", err)
	}

	// Check size
	if readSize != expectedSize {
		_ = t.storage.StashRemove(ctx, stashID)
		return uuid.UUID{}, xerrors.Errorf("size mismatch: expected %d, got %d", expectedSize, readSize)
	}

	// Finalize CommP calculation
	digest, _, err := cp.Digest()
	if err != nil {
		_ = t.storage.StashRemove(ctx, stashID)
		return uuid.UUID{}, xerrors.Errorf("computing CommP digest: %w", err)
	}

	// Convert to CID
	computedCid, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		_ = t.storage.StashRemove(ctx, stashID)
		return uuid.UUID{}, xerrors.Errorf("converting digest to CID: %w", err)
	}

	// Verify CommP matches
	if !expectedCid.Equals(computedCid) {
		_ = t.storage.StashRemove(ctx, stashID)
		return uuid.UUID{}, xerrors.Errorf("CommP mismatch: expected %s, got %s", expectedCid, computedCid)
	}

	return stashID, nil
}

func (t *PDPPullPieceTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (t *PDPPullPieceTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPv0_PullPiece",
		Max:  taskhelp.Max(t.max),
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 128 << 20, // 128 MiB for streaming + CommP computation
		},
		MaxFailures: 5,
		RetryWait: func(retries int) time.Duration {
			const baseWait, maxWait, factor = 10 * time.Second, 5 * time.Minute, 2.0
			return min(time.Duration(float64(baseWait)*math.Pow(factor, float64(retries))), maxWait)
		},
	}
}

func (t *PDPPullPieceTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var (
	_ harmonytask.TaskInterface = &PDPPullPieceTask{}
	_                           = harmonytask.Reg(&PDPPullPieceTask{})
)
