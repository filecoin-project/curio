package pdpv0

import (
	"context"
	"fmt"
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

var (
	// PullPiecePollInterval is how often to poll for new pull items
	PullPiecePollInterval = 10 * time.Second

	// PullTotalBudget is the maximum wall-clock time from pull request creation
	// before the item is marked as permanently failed. Prevents infinite retry
	// loops when Harmony tasks exhaust MaxFailures and the item re-enters the
	// queue with a fresh task (ON DELETE SET NULL on task_id).
	PullTotalBudget = 1 * time.Hour

	// PullAttemptTimeout is the maximum duration for a single download attempt.
	PullAttemptTimeout = 30 * time.Minute

	// PullIdleReadTimeout cancels a download if no bytes are received for this
	// duration, detecting stalled connections that hold TCP sockets open
	// without transferring data.
	PullIdleReadTimeout = 2 * time.Minute
)

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

	budgetInterval := fmt.Sprintf("%d Seconds", int(PullTotalBudget.Seconds()))

	for {
		// Mark expired items as permanently failed before looking for new work.
		n, err := t.db.Exec(ctx, `
			UPDATE pdp_piece_pull_items fi
			SET failed = TRUE, fail_reason = 'pull budget exceeded'
			FROM pdp_piece_pulls pp
			WHERE pp.id = fi.fetch_id
			AND fi.task_id IS NULL AND fi.failed = FALSE
			AND pp.created_at <= NOW() - $1::interval
		`, budgetInterval)
		if err != nil {
			log.Errorf("failed to expire pull items: %s", err)
		} else if n > 0 {
			log.Infow("PDPv0_PullPiece: expired stale pull items", "count", n)
		}

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
		// 4. Pull request is within the total time budget
		err = t.db.Select(ctx, &items, `
			SELECT fi.fetch_id, fi.piece_cid, fi.piece_raw_size, fi.source_url
			FROM pdp_piece_pull_items fi
			JOIN pdp_piece_pulls pp ON pp.id = fi.fetch_id
			WHERE fi.task_id IS NULL AND fi.failed = FALSE
			AND pp.created_at > NOW() - $1::interval
			AND NOT EXISTS (
				SELECT 1 FROM parked_pieces pp2
				WHERE pp2.piece_cid = fi.piece_cid
				AND pp2.long_term = TRUE
				AND pp2.cleanup_task_id IS NULL
			)
		`, budgetInterval)
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

		log.Debugw("PDPv0_PullPiece: found pending pull items, scheduling tasks", "count", len(items))
		for _, item := range items {
			fetchID := item.FetchID
			pieceCid := item.PieceCid

			t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// Atomically assign task_id, with same checks as the SELECT query
				// to prevent race with concurrent parked_pieces creation
				n, err := tx.Exec(`
					UPDATE pdp_piece_pull_items fi
					SET task_id = $1
					FROM pdp_piece_pulls pp
					WHERE pp.id = fi.fetch_id
					AND fi.fetch_id = $2 AND fi.piece_cid = $3
					AND fi.task_id IS NULL AND fi.failed = FALSE
					AND pp.created_at > NOW() - $4::interval
					AND NOT EXISTS (
						SELECT 1 FROM parked_pieces pp2
						WHERE pp2.piece_cid = fi.piece_cid
						AND pp2.long_term = TRUE
						AND pp2.cleanup_task_id IS NULL
					)
				`, id, fetchID, pieceCid, budgetInterval)
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

func (t *PDPPullPieceTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
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

	log.Debugw("PDPv0_PullPiece starting", "taskID", taskID, "pieceCid", item.PieceCid, "sourceUrl", item.SourceURL, "rawSize", item.PieceRawSize)

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
		// Set needs_save_cache=TRUE for large pieces to enable proactive caching
		needsSaveCache := uint64(item.PieceRawSize) >= MinSizeForCache
		_, err = tx.Exec(`
			INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
			VALUES ($1, $2, $3, NOW(), $4)
		`, service, item.PieceCid, pieceRef, needsSaveCache)
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

	log.Debugw("PDPv0_PullPiece: downloading piece from source", "sourceURL", sourceURL, "expectedSize", expectedSize, "expectedCid", expectedCid)

	ctx, cancel := context.WithTimeout(ctx, PullAttemptTimeout)
	defer cancel()

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

	// Wrap body with idle-read detection: if no bytes arrive within
	// PullIdleReadTimeout the context is cancelled, aborting the download.
	idleReader := newIdleTimeoutReader(resp.Body, PullIdleReadTimeout, cancel)
	defer idleReader.stop()

	// Limit reader to expected size + 1 to detect oversized data
	dataReader := io.LimitReader(idleReader, expectedSize+1)

	// Create commp calculator
	cp := &commp.Calc{}
	defer cp.Reset()
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

	log.Debugw("PDPv0_PullPiece: download complete, verifying CommP", "sourceURL", sourceURL, "readSize", readSize, "stashID", stashID)

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

	log.Debugw("PDPv0_PullPiece: CommP verified, piece stored in stash", "computedCid", computedCid, "stashID", stashID)

	return stashID, nil
}

func (t *PDPPullPieceTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
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

// idleTimeoutReader wraps an io.Reader and cancels a context if no successful
// reads occur within the timeout. Each Read that returns n > 0 resets the
// timer. This detects stalled HTTP transfers where the TCP connection stays
// open but no data flows.
type idleTimeoutReader struct {
	r      io.Reader
	timer  *time.Timer
	cancel context.CancelFunc
	idle   time.Duration
}

func newIdleTimeoutReader(r io.Reader, timeout time.Duration, cancel context.CancelFunc) *idleTimeoutReader {
	return &idleTimeoutReader{
		r:      r,
		timer:  time.AfterFunc(timeout, cancel),
		cancel: cancel,
		idle:   timeout,
	}
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	return n, err
}

func (r *idleTimeoutReader) stop() {
	r.timer.Stop()
}
