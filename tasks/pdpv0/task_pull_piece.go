package pdpv0

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/parkpiece"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/robusthttp"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

const pullMinPieceSizeForCache = abi.PaddedPieceSize(uint64(32 * 1024 * 1024))

const pullItemMaxAttempts = 3

var (
	// PullPiecePollInterval is how often to poll for new pull items.
	PullPiecePollInterval = 5 * time.Second

	// PullAttemptTimeout is the maximum duration for a single download attempt.
	PullAttemptTimeout = 10 * time.Minute

	// PullItemBudget is the wall-clock budget for a pull item. It is checked
	// only at scheduling boundaries so an in-flight successful write can still
	// complete the item.
	PullItemBudget = 30 * time.Minute

	// PullIdleReadTimeout cancels a download if no bytes are received for this
	// duration, detecting stalled connections that hold TCP sockets open
	// without transferring data.
	PullIdleReadTimeout = 2 * time.Minute

	// PullRetryDelayAfterFirstFailure and PullRetryDelayAfterLaterFailure bound
	// transient source retries below the total PullItemBudget.
	PullRetryDelayAfterFirstFailure = 30 * time.Second
	PullRetryDelayAfterLaterFailure = 2 * time.Minute
)

// PDPPullPieceTask processes PDPv0 pull items after the handler has accepted a
// pull request.
//
// # Overview
//
// The handler only writes pdp_piece_pulls and pdp_piece_pull_items. This task is
// responsible for scheduling pull work, attaching source URLs to parked_pieces,
// downloading pieces when PullPiece owns the parked row, and setting terminal
// pull-item state.
//
// Pull work is grouped by (piece_cid, piece_raw_size). A group represents one
// unique piece, even if multiple pull requests supplied different source URLs
// for it. Successful completion of one group marks every active pull item for
// that same piece complete, with each item receiving its own parked_piece_ref
// and pdp_piecerefs row.
//
// # Scheduler Loop
//
// Each poll does three things:
//
//  1. completeAlreadyParkedItems: if a matching long-term parked_pieces row is
//     already complete, create missing refs and mark pull items complete.
//
//  2. expireStalePullItems: fail unscheduled items older than PullItemBudget and
//     clean up unused pull-created refs/parked rows.
//
//  3. schedulePullItems: assign one Harmony task per pending piece group whose
//     next_attempt_at has passed. A group with any active task_id is skipped so
//     the same piece is not worked twice at the same time.
//
// # Task Workflow
//
// Do first loads every active pull item for the assigned piece group. That includes
// late arrivals for the same piece, not just rows that originally held this
// task_id.
//
// It then claims the active long-term parked_pieces row for that piece key:
//
//   - If an ordinary row from another flow (push/upload, market) already exists,
//     PullPiece attaches one parked_piece_ref per pull item and exits.
//     ParkPiece/StorePiece continues normal processing, or a later scheduler
//     pass marks the pull items complete if the row is already complete.
//
//   - If the existing row was created by a previous PullPiece run and is still
//     incomplete, PullPiece deletes only refs recorded on pull items for that
//     row, deletes the row when no refs remain, and otherwise flips skip=FALSE
//     so the normal park task can process remaining refs.
//
//   - If no row exists, PullPiece creates a pull-owned parked_pieces row with
//     skip=TRUE, attaches refs for the pull items, and downloads directly to
//     long-term storage.
//
// Pull-owned rows are marked through pdp_piece_pull_items.pull_parked_piece_id.
// That marker is what lets retry cleanup distinguish PullPiece-created skip=TRUE
// rows from other skip=TRUE rows in the system.
//
// # Download and Cleanup
//
// For pull-owned rows, Do tries each unique source URL until one download
// succeeds. The downloaded bytes must match the declared raw size, must not have
// extra trailing bytes, and must compute to the expected CommP.
//
// If no URL succeeds in a task pass, per-URL failures are recorded. Permanent
// failures, such as bad source URLs, 410 responses, size mismatches, and CommP
// mismatches, fail the affected pull items immediately. Transient failures,
// including Curio-side 404s, update attempt_count and next_attempt_at; after
// pullItemMaxAttempts they become failed pull items instead of cycling until the
// wall-clock budget expires.
//
// On success, Do marks parked_pieces.complete=TRUE, re-reads active pull items
// for the piece so refs created during the task are visible, creates pdp_piecerefs,
// and marks those pull items complete. Rows that arrive after that completion
// transaction are handled by the next completeAlreadyParkedItems pass.
//
// On failure or lost ownership, deferred cleanup removes unused pull-created refs,
// deletes the incomplete pull-owned parked row when it is unreferenced, and removes
// the local piece file best-effort. Cleanup errors are logged and do not replace
// the main task error.
type PDPPullPieceTask struct {
	db *harmonydb.DB
	sc *ffi2.SealCalls

	TF promise.Promise[harmonytask.AddTaskFunc]

	max int
}

func NewPDPPullPieceTask(ctx context.Context, db *harmonydb.DB, sc *ffi2.SealCalls, max int) *PDPPullPieceTask {
	t := &PDPPullPieceTask{
		db:  db,
		sc:  sc,
		max: max,
	}

	go t.pollPullItems(ctx)
	return t
}

// pollPullItems runs the scheduler maintenance passes in dependency order.
func (t *PDPPullPieceTask) pollPullItems(ctx context.Context) {
	ticker := time.NewTicker(PullPiecePollInterval)
	defer ticker.Stop()

	for {
		if err := t.completeAlreadyParkedItems(ctx); err != nil {
			log.Errorf("failed to mark already parked pull items complete: %s", err)
		}
		if err := t.expireStalePullItems(ctx); err != nil {
			log.Errorf("failed to expire stale pull items: %s", err)
		}
		if err := t.schedulePullItems(ctx); err != nil {
			log.Errorf("failed to schedule pull items: %s", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pullPieceKey is the scheduling key shared by all pull items for one piece.
type pullPieceKey struct {
	PieceCid     string `db:"piece_cid"`
	PieceRawSize int64  `db:"piece_raw_size"`
}

// pullSource is the active pull item shape used by Do after a group is assigned.
type pullSource struct {
	PieceCid     string `db:"piece_cid"`
	PieceRawSize int64  `db:"piece_raw_size"`
	FetchID      int64  `db:"fetch_id"`
	Service      string `db:"service"`
	SourceURL    string `db:"source_url"`
	PieceRef     *int64 `db:"parked_piece_ref"`
}

// pullRetryDelayForAttempt returns the backoff after a transient source failure.
func pullRetryDelayForAttempt(attempt int) time.Duration {
	if attempt <= 1 {
		return PullRetryDelayAfterFirstFailure
	}
	return PullRetryDelayAfterLaterFailure
}

// pullSourceErrorStage records where a source attempt failed.
type pullSourceErrorStage int

const (
	pullSourceValidate pullSourceErrorStage = iota
	pullSourceRequest
	pullSourceStatus
	pullSourceWrite
	pullSourceVerify
)

// pullSourceError keeps the original per-URL error and facts used for retry classification.
type pullSourceError struct {
	sourceURL  string
	err        error
	statusCode int
	stage      pullSourceErrorStage
}

// pullItemToComplete is the row shape needed to finish one accepted pull item.
type pullItemToComplete struct {
	FetchID       int64  `db:"fetch_id"`
	Service       string `db:"service"`
	PieceCid      string `db:"piece_cid"`
	PieceRawSize  int64  `db:"piece_raw_size"`
	SourceURL     string `db:"source_url"`
	ParkedPieceID int64  `db:"parked_piece_id"`
	PieceRef      *int64 `db:"parked_piece_ref"`
}

// completeAlreadyParkedItems completes pull items whose long-term piece already exists.
func (t *PDPPullPieceTask) completeAlreadyParkedItems(ctx context.Context) error {
	_, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var items []pullItemToComplete
		err := tx.Select(&items, `
			SELECT DISTINCT ON (fi.fetch_id, fi.piece_cid, fi.source_url)
				fi.fetch_id,
				pp.service,
				fi.piece_cid,
				fi.piece_raw_size,
				fi.source_url,
				parked.id AS parked_piece_id,
				fi.parked_piece_ref
			FROM pdp_piece_pull_items fi
			JOIN pdp_piece_pulls pp ON pp.id = fi.fetch_id
			JOIN parked_pieces parked
				ON parked.piece_cid = fi.piece_cid
				AND parked.piece_raw_size = fi.piece_raw_size
				AND parked.long_term = TRUE
				AND parked.complete = TRUE
				AND parked.cleanup_task_id IS NULL
			WHERE fi.complete = FALSE
				AND fi.failed = FALSE
			ORDER BY fi.fetch_id, fi.piece_cid, fi.source_url, parked.created_at ASC, parked.id ASC
		`)
		if err != nil {
			return false, xerrors.Errorf("query already parked pull items: %w", err)
		}

		for _, item := range items {
			existingRef := int64(0)
			if item.PieceRef != nil {
				existingRef = *item.PieceRef
			}
			err := completePullItemWithParkedPiece(tx, item.Service, item.FetchID, item.PieceCid, uint64(item.PieceRawSize), item.SourceURL, item.ParkedPieceID, existingRef)
			if err != nil {
				return false, xerrors.Errorf("complete already parked pull item %d/%s: %w", item.FetchID, item.PieceCid, err)
			}
		}

		return len(items) > 0, nil
	}, harmonydb.OptionRetry())
	return err
}

// expireStalePullItems fails unscheduled over-budget rows and cleans pull-created refs.
func (t *PDPPullPieceTask) expireStalePullItems(ctx context.Context) error {
	type stalePullItem struct {
		FetchID           int64  `db:"fetch_id"`
		PieceCid          string `db:"piece_cid"`
		SourceURL         string `db:"source_url"`
		PieceRef          *int64 `db:"parked_piece_ref"`
		PullParkedPieceID *int64 `db:"pull_parked_piece_id"`
	}

	var expiredCount int64
	var removedPieces []struct {
		ID int64 `db:"id"`
	}
	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var staleItems []stalePullItem
		// The wall-clock budget is enforced only between tasks. Do can still
		// complete an item that crosses the budget while bytes are in flight.
		err := tx.Select(&staleItems, `
			SELECT fetch_id,
				piece_cid,
				source_url,
				parked_piece_ref,
				pull_parked_piece_id
			FROM pdp_piece_pull_items
			WHERE complete = FALSE
				AND failed = FALSE
				AND task_id IS NULL
				AND created_at < NOW() - ($1::BIGINT * INTERVAL '1 second')
		`, int64(PullItemBudget.Seconds()))
		if err != nil {
			return false, xerrors.Errorf("query stale pull items: %w", err)
		}

		var failedItems []stalePullItem
		// Terminal failures can still carry pull-created refs if Curio died
		// before deferred cleanup ran.
		err = tx.Select(&failedItems, `
			SELECT fetch_id,
				piece_cid,
				source_url,
				parked_piece_ref,
				pull_parked_piece_id
			FROM pdp_piece_pull_items
			WHERE complete = FALSE
				AND failed = TRUE
				AND task_id IS NULL
				AND (parked_piece_ref IS NOT NULL OR pull_parked_piece_id IS NOT NULL)
		`)
		if err != nil {
			return false, xerrors.Errorf("query failed pull items with refs: %w", err)
		}
		if len(staleItems) == 0 && len(failedItems) == 0 {
			return true, nil
		}

		fetchIDs := make([]int64, 0, len(staleItems))
		pieceCids := make([]string, 0, len(staleItems))
		sourceURLs := make([]string, 0, len(staleItems))
		failedFetchIDs := make([]int64, 0, len(failedItems))
		failedPieceCids := make([]string, 0, len(failedItems))
		failedSourceURLs := make([]string, 0, len(failedItems))
		refIDs := make([]int64, 0, len(staleItems))
		pieceIDs := make([]int64, 0, len(staleItems))
		seenRefs := map[int64]struct{}{}
		seenPieces := map[int64]struct{}{}
		// Ref and parked-piece cleanup is shared by stale rows and rows that
		// were already failed by source-error handling.
		collectRefs := func(item stalePullItem) {
			if item.PieceRef != nil {
				if _, ok := seenRefs[*item.PieceRef]; !ok {
					refIDs = append(refIDs, *item.PieceRef)
					seenRefs[*item.PieceRef] = struct{}{}
				}
			}
			if item.PullParkedPieceID != nil {
				if _, ok := seenPieces[*item.PullParkedPieceID]; !ok {
					pieceIDs = append(pieceIDs, *item.PullParkedPieceID)
					seenPieces[*item.PullParkedPieceID] = struct{}{}
				}
			}
		}
		for _, item := range staleItems {
			fetchIDs = append(fetchIDs, item.FetchID)
			pieceCids = append(pieceCids, item.PieceCid)
			sourceURLs = append(sourceURLs, item.SourceURL)
			collectRefs(item)
		}
		for _, item := range failedItems {
			failedFetchIDs = append(failedFetchIDs, item.FetchID)
			failedPieceCids = append(failedPieceCids, item.PieceCid)
			failedSourceURLs = append(failedSourceURLs, item.SourceURL)
			collectRefs(item)
		}

		if len(staleItems) > 0 {
			// Stale open rows become terminal failures here. Already-failed
			// rows below keep their original fail_reason.
			n, err := tx.Exec(`
				WITH stale(fetch_id, piece_cid, source_url) AS (
					SELECT * FROM unnest($1::BIGINT[], $2::TEXT[], $3::TEXT[])
				)
				UPDATE pdp_piece_pull_items fi
				SET failed = TRUE,
					fail_reason = 'pull budget exceeded',
					task_id = NULL,
					parked_piece_ref = NULL,
					pull_parked_piece_id = NULL
				FROM stale
				WHERE fi.fetch_id = stale.fetch_id
					AND fi.piece_cid = stale.piece_cid
					AND fi.source_url = stale.source_url
					AND fi.complete = FALSE
					AND fi.failed = FALSE
			`, fetchIDs, pieceCids, sourceURLs)
			if err != nil {
				return false, xerrors.Errorf("expire stale pull items: %w", err)
			}
			expiredCount = int64(n)
		}

		if len(failedItems) > 0 {
			// These rows are already terminal. Only detach refs so the cleanup
			// queries can decide whether each ref or parked row is now unused.
			_, err := tx.Exec(`
				WITH failed_refs(fetch_id, piece_cid, source_url) AS (
					SELECT * FROM unnest($1::BIGINT[], $2::TEXT[], $3::TEXT[])
				)
				UPDATE pdp_piece_pull_items fi
				SET parked_piece_ref = NULL,
					pull_parked_piece_id = NULL
				FROM failed_refs
				WHERE fi.fetch_id = failed_refs.fetch_id
					AND fi.piece_cid = failed_refs.piece_cid
					AND fi.source_url = failed_refs.source_url
					AND fi.complete = FALSE
					AND fi.failed = TRUE
					AND fi.task_id IS NULL
			`, failedFetchIDs, failedPieceCids, failedSourceURLs)
			if err != nil {
				return false, xerrors.Errorf("clear failed pull item refs: %w", err)
			}
		}

		if len(refIDs) > 0 {
			// Delete only refs that are not visible to PDP and are not still
			// attached to an active pull item.
			_, err = tx.Exec(`
				DELETE FROM parked_piece_refs pr
				WHERE pr.ref_id = ANY($1::BIGINT[])
					AND NOT EXISTS (
						SELECT 1 FROM pdp_piecerefs ppr WHERE ppr.piece_ref = pr.ref_id
					)
					AND NOT EXISTS (
						SELECT 1
						FROM pdp_piece_pull_items fi
						WHERE fi.parked_piece_ref = pr.ref_id
							AND fi.complete = FALSE
							AND fi.failed = FALSE
					)
			`, refIDs)
			if err != nil {
				return false, xerrors.Errorf("delete expired pull refs: %w", err)
			}
		}

		if len(pieceIDs) > 0 {
			// Pull-owned skip rows are deleted only after their refs are gone.
			err = tx.Select(&removedPieces, `
				DELETE FROM parked_pieces pp
				WHERE pp.id = ANY($1::BIGINT[])
					AND pp.complete = FALSE
					AND pp.skip = TRUE
					AND NOT EXISTS (
						SELECT 1 FROM parked_piece_refs ppr WHERE ppr.piece_id = pp.id
					)
				RETURNING id
			`, pieceIDs)
			if err != nil {
				return false, xerrors.Errorf("delete expired pull parked pieces: %w", err)
			}

			// If refs remain, hand the row back to ParkPiece instead of leaving
			// a skipped parked_pieces row with no pull task able to finish it.
			_, err = tx.Exec(`
				UPDATE parked_pieces pp
				SET skip = FALSE
				WHERE pp.id = ANY($1::BIGINT[])
					AND pp.complete = FALSE
					AND pp.skip = TRUE
					AND EXISTS (
						SELECT 1 FROM parked_piece_refs ppr WHERE ppr.piece_id = pp.id
					)
			`, pieceIDs)
			if err != nil {
				return false, xerrors.Errorf("enable park task for remaining expired pull refs: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	if !comm {
		return xerrors.Errorf("failed to commit DB transaction")
	}

	if expiredCount > 0 {
		log.Infow("PDPv0_PullPiece: expired stale pull items", "count", expiredCount)
	}

	for _, piece := range removedPieces {
		if err := t.sc.RemovePiece(context.Background(), storiface.PieceNumber(piece.ID)); err != nil {
			log.Errorw("failed to remove expired pull piece", "piece_id", piece.ID, "error", err)
		}
	}

	return nil
}

// schedulePullItems creates one Harmony task per due piece group, up to t.max.
func (t *PDPPullPieceTask) schedulePullItems(ctx context.Context) error {
	var activeGroups int
	// The cap is group-based, not row-based, because all rows for the same
	// piece share a single download attempt.
	err := t.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT DISTINCT piece_cid, piece_raw_size
			FROM pdp_piece_pull_items
			WHERE complete = FALSE
				AND failed = FALSE
				AND task_id IS NOT NULL
		) active
	`).Scan(&activeGroups)
	if err != nil {
		return xerrors.Errorf("count active pull item groups: %w", err)
	}
	if activeGroups >= t.max {
		return nil
	}

	limit := t.max - activeGroups
	var groups []pullPieceKey
	// next_attempt_at gates whether a group is due to be scheduled. Once due,
	// assignGroup can attach newer rows in the same group too.
	err = t.db.Select(ctx, &groups, `
		SELECT fi.piece_cid, fi.piece_raw_size
		FROM pdp_piece_pull_items fi
		WHERE fi.complete = FALSE
			AND fi.failed = FALSE
			AND fi.task_id IS NULL
			AND fi.attempt_count < $3
			AND fi.next_attempt_at <= NOW()
			AND fi.created_at >= NOW() - ($2::BIGINT * INTERVAL '1 second')
			AND NOT EXISTS (
				SELECT 1
				FROM pdp_piece_pull_items running
				WHERE running.piece_cid = fi.piece_cid
					AND running.piece_raw_size = fi.piece_raw_size
					AND running.complete = FALSE
					AND running.failed = FALSE
					AND running.task_id IS NOT NULL
		)
		GROUP BY fi.piece_cid, fi.piece_raw_size
		ORDER BY MIN(fi.created_at) ASC, MIN(fi.fetch_id) ASC, fi.piece_cid ASC
		LIMIT $1
	`, limit, int64(PullItemBudget.Seconds()), pullItemMaxAttempts)
	if err != nil {
		return xerrors.Errorf("query pending pull item groups: %w", err)
	}
	if len(groups) == 0 {
		return nil
	}

	log.Debugw("PDPv0_PullPiece: found pending pull item groups", "count", len(groups))

	for _, group := range groups {
		group := group
		t.TF.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			return t.assignGroup(tx, id, group)
		})
	}

	return nil
}

// assignGroup attaches a task to every eligible row for the selected piece key.
func (t *PDPPullPieceTask) assignGroup(tx *harmonydb.Tx, taskID harmonytask.TaskID, group pullPieceKey) (bool, error) {
	// If the piece completed between schedule selection and task insert, do not
	// create another pull task. The next poll will complete the pull items.
	completePieceID, err := findCompleteParkedPiece(tx, group)
	if err != nil {
		return false, err
	}

	if completePieceID != nil {
		return false, nil
	}

	// Do not filter on next_attempt_at here. Once one row makes the group due,
	// all still-eligible rows for that piece can ride on the same attempt.
	n, err := tx.Exec(`
		UPDATE pdp_piece_pull_items fi
		SET task_id = $3
		WHERE fi.piece_cid = $1
			AND fi.piece_raw_size = $2
			AND fi.complete = FALSE
			AND fi.failed = FALSE
			AND fi.task_id IS NULL
			AND fi.attempt_count < $5
			AND fi.created_at >= NOW() - ($4::BIGINT * INTERVAL '1 second')
			AND NOT EXISTS (
				SELECT 1
				FROM pdp_piece_pull_items running
				WHERE running.piece_cid = fi.piece_cid
					AND running.piece_raw_size = fi.piece_raw_size
					AND running.complete = FALSE
					AND running.failed = FALSE
					AND running.task_id IS NOT NULL
			)
	`, group.PieceCid, group.PieceRawSize, taskID, int64(PullItemBudget.Seconds()), pullItemMaxAttempts)
	if err != nil {
		return false, xerrors.Errorf("assign pull item task: %w", err)
	}

	return n > 0, nil
}

func (t *PDPPullPieceTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sources []pullSource
	// Load by assigned group, then expand back to all active rows for that
	// group. This picks up rows that arrived after the scheduler assigned task_id.
	err = t.db.Select(ctx, &sources, `
		WITH assigned_groups AS (
			SELECT piece_cid, piece_raw_size
			FROM pdp_piece_pull_items
			WHERE task_id = $1
				AND complete = FALSE
				AND failed = FALSE
			GROUP BY piece_cid, piece_raw_size
		),
		items AS (
			SELECT
				fi.piece_cid,
				fi.piece_raw_size,
				fi.fetch_id,
				pp.service,
				fi.source_url,
				fi.parked_piece_ref,
				fi.created_at
			FROM assigned_groups ag
			JOIN pdp_piece_pull_items fi
				ON fi.piece_cid = ag.piece_cid
				AND fi.piece_raw_size = ag.piece_raw_size
			JOIN pdp_piece_pulls pp ON pp.id = fi.fetch_id
			WHERE fi.complete = FALSE
				AND fi.failed = FALSE
				AND fi.attempt_count < $2
		)
		SELECT piece_cid, piece_raw_size, fetch_id, service, source_url, parked_piece_ref
		FROM items
		ORDER BY created_at ASC, fetch_id ASC
	`, taskID, pullItemMaxAttempts)
	if err != nil {
		return false, xerrors.Errorf("query pull task sources: %w", err)
	}
	if len(sources) == 0 {
		return true, nil
	}

	group := pullPieceKey{PieceCid: sources[0].PieceCid, PieceRawSize: sources[0].PieceRawSize}
	for _, source := range sources[1:] {
		if source.PieceCid != group.PieceCid || source.PieceRawSize != group.PieceRawSize {
			return false, xerrors.Errorf("expected 1 pull group for task %d", taskID)
		}
	}

	expectedCid, err := cid.Parse(group.PieceCid)
	if err != nil {
		return false, xerrors.Errorf("invalid expected piece CID for pull task %d: %w", taskID, err)
	}

	log.Debugw("PDPv0_PullPiece starting", "taskID", taskID, "pieceCid", group.PieceCid, "rawSize", group.PieceRawSize)

	var parkedPieceID int64
	createdParkedPiece := false
	var stalePullPiecesToRemove []int64
	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var err error
		// Claim or create the active long-term parked row. New pull-owned rows
		// start as skip=TRUE so ParkPiece does not race this direct download.
		parkedPieceID, createdParkedPiece, err = parkpiece.UpsertSkipWithInserted(tx, group.PieceCid, int64(padreader.PaddedSize(uint64(group.PieceRawSize)).Padded()), group.PieceRawSize, true, true)
		if err != nil {
			return false, xerrors.Errorf("upsert parked piece: %w", err)
		}

		var activeComplete bool
		var activeTaskID sql.NullInt64
		var activeTaskExists bool
		var activePullOwned bool
		cleanedPullOwnedPiece := false
		var txRemovePieces []int64

		for {
			// Existing rows split into three cases:
			// complete rows are handled by completion logic, non-pull rows are
			// left for their owner, and previous pull-owned rows are cleaned once.
			err = tx.QueryRow(`
				SELECT pp.complete,
					pp.task_id,
					ht.id IS NOT NULL AS task_exists,
					EXISTS (
						SELECT 1
						FROM pdp_piece_pull_items fi
						WHERE fi.pull_parked_piece_id = pp.id
							AND fi.piece_cid = $2
							AND fi.piece_raw_size = $3
					) AS pull_owned
				FROM parked_pieces pp
				LEFT JOIN harmony_task ht ON ht.id = pp.task_id
				WHERE pp.id = $1
			`, parkedPieceID, group.PieceCid, group.PieceRawSize).Scan(&activeComplete, &activeTaskID, &activeTaskExists, &activePullOwned)
			if err != nil {
				return false, xerrors.Errorf("query claimed parked piece: %w", err)
			}

			if createdParkedPiece || activeComplete || !activePullOwned {
				break
			}
			if cleanedPullOwnedPiece {
				return false, xerrors.Errorf("pull-owned parked piece still active after cleanup: %d", parkedPieceID)
			}

			// A previous PullPiece task created this row but did not finish it.
			// Clear only refs recorded on pull items for that row.
			removed, err := cleanupPullCreatedParkedPieceTx(tx, parkedPieceID)
			if err != nil {
				return false, xerrors.Errorf("cleanup stale pull-owned parked piece: %w", err)
			}
			if removed {
				txRemovePieces = append(txRemovePieces, parkedPieceID)
			}

			parkedPieceID, createdParkedPiece, err = parkpiece.UpsertSkipWithInserted(tx, group.PieceCid, int64(padreader.PaddedSize(uint64(group.PieceRawSize)).Padded()), group.PieceRawSize, true, true)
			if err != nil {
				return false, xerrors.Errorf("upsert parked piece after cleanup: %w", err)
			}
			cleanedPullOwnedPiece = true
		}

		// If ParkPiece's task row is gone, clear the stale task_id so its
		// scheduler can pick this parked row again after the new refs attach.
		if !createdParkedPiece && activeTaskID.Valid && !activeTaskExists && !activeComplete {
			n, err := tx.Exec(`
				UPDATE parked_pieces
				SET task_id = NULL
				WHERE id = $1
					AND task_id = $2
					AND complete = FALSE
			`, parkedPieceID, activeTaskID.Int64)
			if err != nil {
				return false, xerrors.Errorf("clear stale parked piece task: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 parked piece task, got %d", n)
			}
		}

		for _, source := range sources {
			// Each accepted pull item gets its own ref. That stays true even
			// when another URL in the group supplies the bytes.
			if source.PieceRef != nil && !cleanedPullOwnedPiece {
				continue
			}

			var pieceRef int64
			err := tx.QueryRow(`
				INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
				VALUES ($1, $2, TRUE)
				RETURNING ref_id
			`, parkedPieceID, source.SourceURL).Scan(&pieceRef)
			if err != nil {
				return false, xerrors.Errorf("insert parked_piece_ref for pull item: %w", err)
			}

			n, err := tx.Exec(`
				UPDATE pdp_piece_pull_items
				SET parked_piece_ref = $4,
					pull_parked_piece_id = CASE WHEN $5 THEN $6 ELSE pull_parked_piece_id END
				WHERE fetch_id = $1
					AND piece_cid = $2
					AND piece_raw_size = $3
					AND source_url = $8
					AND complete = FALSE
					AND failed = FALSE
					AND (parked_piece_ref IS NULL OR $7)
			`, source.FetchID, source.PieceCid, source.PieceRawSize, pieceRef, createdParkedPiece, parkedPieceID, cleanedPullOwnedPiece, source.SourceURL)
			if err != nil {
				return false, xerrors.Errorf("attach parked_piece_ref to pull item: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 attached piece ref, got %d", n)
			}
		}

		stalePullPiecesToRemove = txRemovePieces
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, err
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit DB transaction")
	}

	for _, pieceID := range stalePullPiecesToRemove {
		if err := t.sc.RemovePiece(context.Background(), storiface.PieceNumber(pieceID)); err != nil {
			log.Errorw("failed to remove stale pull-owned piece", "piece_id", pieceID, "error", err)
		}
	}

	// If the row already existed, this task only contributes refs. ParkPiece will
	// keep running, retry using newly attached refs after the stale task_id path
	// above, or the next completion pass will finish the pull items if the row is
	// already complete.
	if !createdParkedPiece {
		return true, nil
	}

	ssrfPolicy := robusthttp.CurrentSSRFPolicy()
	if pdp.PullAllowInsecure() {
		ssrfPolicy.Disabled = true
	}

	shouldCleanup := true
	defer func(ppid int64, taskname string) {
		if !shouldCleanup {
			return
		}
		var removed bool
		cleanupComm, cleanupErr := t.db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
			var err error
			removed, err = cleanupPullCreatedParkedPieceTx(tx, ppid)
			if err != nil {
				return false, err
			}
			return true, nil
		}, harmonydb.OptionRetry())
		if cleanupErr != nil {
			log.Errorw("failed to cleanup pull parked_piece_refs", "task_id", taskID, "task_type", taskname, "error", cleanupErr)
		}

		if !cleanupComm {
			log.Errorw("failed to cleanup pull parked_piece_refs", "task_id", taskID, "task_type", taskname, "error", "failed to commit DB transaction")
		}

		if removed {
			if err := t.sc.RemovePiece(context.Background(), storiface.PieceNumber(ppid)); err != nil {
				log.Errorw("failed to remove piece", "task_id", taskID, "task_type", taskname, "piece_id", ppid, "error", err)
			}
		}
	}(parkedPieceID, t.TypeDetails().Name)

	attemptedSources := map[string]struct{}{}
	var sourceErrors error
	var sourceFailures []pullSourceError
	for _, source := range sources {
		// Multiple pull items can carry the same URL. Try each distinct URL
		// once; if every URL fails, failures are recorded by source_url.
		if _, ok := attemptedSources[source.SourceURL]; ok {
			continue
		}
		attemptedSources[source.SourceURL] = struct{}{}

		if !stillOwned() {
			return false, nil
		}

		sourceErr := t.tryPullSource(ctx, source.SourceURL, group, expectedCid, parkedPieceID, ssrfPolicy)
		if sourceErr != nil {
			log.Warnw("pull source URL failed", "error", sourceErr.err, "pieceCid", group.PieceCid, "sourceURL", source.SourceURL)
			sourceErrors = multierror.Append(sourceErrors, xerrors.Errorf("%s: %w", source.SourceURL, sourceErr.err))
			sourceFailures = append(sourceFailures, *sourceErr)
			continue
		}

		comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`
				UPDATE parked_pieces
				SET complete = TRUE
				WHERE id = $1
			`, parkedPieceID)
			if err != nil {
				return false, xerrors.Errorf("mark parked piece complete: %w", err)
			}

			if n != 1 {
				return false, xerrors.Errorf("mark parked piece complete: expected 1, got %d", n)
			}

			// Re-read from the DB instead of using sources from task start:
			// refs were attached after sources was loaded, and new pull items
			// for this same piece may have arrived while the download ran.
			// Rows that arrive after this transaction will be completed by the
			// next completeAlreadyParkedItems pass.
			var items []pullItemToComplete
			err = tx.Select(&items, `
				SELECT fi.fetch_id,
					pp.service,
					fi.piece_cid,
					fi.piece_raw_size,
					fi.source_url,
					$3::BIGINT AS parked_piece_id,
					fi.parked_piece_ref
				FROM pdp_piece_pull_items fi
				JOIN pdp_piece_pulls pp ON pp.id = fi.fetch_id
				WHERE fi.piece_cid = $1
					AND fi.piece_raw_size = $2
					AND fi.complete = FALSE
					AND fi.failed = FALSE
				ORDER BY fi.fetch_id ASC, fi.piece_cid ASC
			`, group.PieceCid, group.PieceRawSize, parkedPieceID)
			if err != nil {
				return false, xerrors.Errorf("query pull items to complete: %w", err)
			}

			for _, item := range items {
				existingRef := int64(0)
				if item.PieceRef != nil {
					existingRef = *item.PieceRef
				}

				err := completePullItemWithParkedPiece(tx, item.Service, item.FetchID, item.PieceCid, uint64(item.PieceRawSize), item.SourceURL, item.ParkedPieceID, existingRef)
				if err != nil {
					return false, xerrors.Errorf("complete pull item %d/%s: %w", item.FetchID, item.PieceCid, err)
				}
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			return false, err
		}

		if !comm {
			mainErr := xerrors.Errorf("failed to commit DB transaction")
			return false, mainErr
		}

		log.Infow("pull piece complete", "pieceCid", group.PieceCid, "pieceID", parkedPieceID, "taskID", taskID)
		shouldCleanup = false
		return true, nil
	}

	if len(sourceFailures) > 0 {
		if err := t.recordPullSourceFailures(ctx, group, sourceFailures); err != nil {
			return false, err
		}
		return false, xerrors.Errorf("all pull source URLs failed: %w", sourceErrors)
	}

	return false, xerrors.Errorf("no good URL found to download the piece")
}

// tryPullSource downloads one source URL into the pull-owned parked piece.
func (t *PDPPullPieceTask) tryPullSource(ctx context.Context, sourceURL string, group pullPieceKey, expectedCid cid.Cid, parkedPieceID int64, ssrfPolicy robusthttp.SSRFPolicy) *pullSourceError {
	fail := func(stage pullSourceErrorStage, err error) *pullSourceError {
		return &pullSourceError{sourceURL: sourceURL, err: err, stage: stage}
	}

	// Validation-stage errors are treated as permanent because they are request
	// shape or policy failures, not source availability failures.
	parsedURL, err := url.Parse(sourceURL)
	if err != nil {
		return fail(pullSourceValidate, err)
	}
	if parsedURL.Scheme != "https" && (!pdp.PullAllowInsecure() || parsedURL.Scheme != "http") {
		return fail(pullSourceValidate, xerrors.Errorf("source URL must use HTTPS scheme, got: %s", parsedURL.Scheme))
	}

	downloadPolicy := ssrfPolicy
	downloadPolicy.ResponseHeaderTimeout = 2 * time.Minute
	if _, err := robusthttp.ValidateClientFetchURL(sourceURL, nil, &downloadPolicy); err != nil {
		return fail(pullSourceValidate, err)
	}

	downloadCtx, downloadCancel := context.WithTimeout(ctx, PullAttemptTimeout)
	defer downloadCancel()

	// Request failures are classified from typed network/context errors later.
	client, _ := robusthttp.NewSSRFProtectedHTTPClient(&downloadPolicy, nil)
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fail(pullSourceValidate, err)
	}

	resp, err := client.Do(req)
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}
	if err != nil {
		return fail(pullSourceRequest, err)
	}

	// HTTP status gets its own stage so 404/429/5xx can be retried without
	// string matching, while other statuses fail the matching rows.
	if resp.StatusCode != http.StatusOK {
		return &pullSourceError{
			sourceURL:  sourceURL,
			err:        xerrors.Errorf("HTTP status %d from source", resp.StatusCode),
			statusCode: resp.StatusCode,
			stage:      pullSourceStatus,
		}
	}
	if resp.ContentLength > group.PieceRawSize {
		return fail(pullSourceVerify, xerrors.Errorf("size mismatch: expected %d, got at least %d", group.PieceRawSize, resp.ContentLength))
	}

	idleReader := &idleTimeoutReader{
		r:      resp.Body,
		timer:  time.AfterFunc(PullIdleReadTimeout, downloadCancel),
		cancel: downloadCancel,
		idle:   PullIdleReadTimeout,
	}
	defer idleReader.timer.Stop()

	// Storage/write errors are treated as transient because they can be caused
	// by temporary local storage pressure or IO failures.
	pieceInfo, readSize, err := t.sc.WriteUploadPiece(downloadCtx, storiface.PieceNumber(parkedPieceID), group.PieceRawSize, idleReader, storiface.PathStorage, true)
	if err != nil {
		return fail(pullSourceWrite, xerrors.Errorf("write pulled piece: %w", err))
	}
	// Verification failures mean the source did not serve the requested piece.
	if readSize != uint64(group.PieceRawSize) {
		return fail(pullSourceVerify, xerrors.Errorf("size mismatch: expected %d, got %d", group.PieceRawSize, readSize))
	}
	if !pieceInfo.PieceCID.Equals(expectedCid) {
		return fail(pullSourceVerify, xerrors.Errorf("CommP mismatch: expected %s, got %s", expectedCid, pieceInfo.PieceCID))
	}
	if pieceInfo.Size != abi.PaddedPieceSize(pdp.PadPieceSize(group.PieceRawSize)) {
		return fail(pullSourceVerify, xerrors.Errorf("padded size mismatch: expected %d, got %d", pdp.PadPieceSize(group.PieceRawSize), pieceInfo.Size))
	}

	var extra [1]byte
	n, err := idleReader.Read(extra[:])
	if n > 0 {
		return fail(pullSourceVerify, xerrors.Errorf("size mismatch: expected %d, got more", group.PieceRawSize))
	}
	if err != nil && err != io.EOF {
		return fail(pullSourceRequest, xerrors.Errorf("checking for oversized response: %w", err))
	}

	return nil
}

// recordPullSourceFailures applies all per-URL failures after a full source pass.
func (t *PDPPullPieceTask) recordPullSourceFailures(ctx context.Context, group pullPieceKey, failures []pullSourceError) error {
	var permanentURLs, permanentReasons []string
	var transientURLs, transientReasons []string
	// Split only after all URLs have been tried. One good source wins the group;
	// this function is only for the all-sources-failed path.
	for _, failure := range failures {
		if isTransientPullSourceError(failure) {
			transientURLs = append(transientURLs, failure.sourceURL)
			transientReasons = append(transientReasons, failure.err.Error())
			continue
		}
		permanentURLs = append(permanentURLs, failure.sourceURL)
		permanentReasons = append(permanentReasons, failure.err.Error())
	}

	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		if len(permanentURLs) > 0 {
			// Permanent errors fail rows that used that exact URL. Rows for
			// other URLs in the group are handled by their own failure result.
			_, err := tx.Exec(`
				WITH failed_sources(source_url, reason) AS (
					SELECT * FROM unnest($3::TEXT[], $4::TEXT[])
				)
				UPDATE pdp_piece_pull_items fi
				SET failed = TRUE,
					fail_reason = failed_sources.reason,
					task_id = NULL
				FROM failed_sources
				WHERE fi.source_url = failed_sources.source_url
					AND fi.piece_cid = $1
					AND fi.piece_raw_size = $2
					AND fi.complete = FALSE
					AND fi.failed = FALSE
			`, group.PieceCid, group.PieceRawSize, permanentURLs, permanentReasons)
			if err != nil {
				return false, xerrors.Errorf("mark permanent pull source failures: %w", err)
			}
		}

		if len(transientURLs) > 0 {
			// Transient rows get bounded retries. The final attempt records the
			// real source error instead of leaving them to budget expiry.
			_, err := tx.Exec(`
				WITH transient_sources(source_url, reason) AS (
					SELECT * FROM unnest($3::TEXT[], $4::TEXT[])
				)
				UPDATE pdp_piece_pull_items fi
				SET attempt_count = fi.attempt_count + 1,
					next_attempt_at = CASE
						WHEN fi.attempt_count + 1 >= $5 THEN fi.next_attempt_at
						WHEN fi.attempt_count = 0 THEN NOW() + ($6::BIGINT * INTERVAL '1 second')
						ELSE NOW() + ($7::BIGINT * INTERVAL '1 second')
					END,
					failed = CASE WHEN fi.attempt_count + 1 >= $5 THEN TRUE ELSE fi.failed END,
					fail_reason = CASE WHEN fi.attempt_count + 1 >= $5 THEN transient_sources.reason ELSE fi.fail_reason END,
					task_id = NULL
				FROM transient_sources
				WHERE fi.source_url = transient_sources.source_url
					AND fi.piece_cid = $1
					AND fi.piece_raw_size = $2
					AND fi.complete = FALSE
					AND fi.failed = FALSE
					AND fi.attempt_count < $5
			`, group.PieceCid, group.PieceRawSize, transientURLs, transientReasons, pullItemMaxAttempts, int64(pullRetryDelayForAttempt(1).Seconds()), int64(pullRetryDelayForAttempt(2).Seconds()))
			if err != nil {
				return false, xerrors.Errorf("record transient pull source failures: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return err
	}
	if !comm {
		return xerrors.Errorf("failed to commit DB transaction")
	}

	return nil
}

// isTransientPullSourceError decides whether a failed URL should get another pass.
func isTransientPullSourceError(failure pullSourceError) bool {
	switch failure.stage {
	case pullSourceStatus:
		return failure.statusCode == http.StatusNotFound ||
			failure.statusCode == http.StatusTooManyRequests ||
			(failure.statusCode >= http.StatusInternalServerError && failure.statusCode <= 599)
	case pullSourceRequest:
		return isTransientPullRequestError(failure.err)
	case pullSourceWrite:
		return true
	default:
		return false
	}
}

// isTransientPullRequestError classifies network/request errors without strings.
func isTransientPullRequestError(err error) bool {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var noAddrs *robusthttp.NoResolvedAddressError
	if errors.As(err, &noAddrs) {
		return true
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

// cleanupPullCreatedParkedPieceTx removes pull-owned refs from an incomplete parked row.
func cleanupPullCreatedParkedPieceTx(tx *harmonydb.Tx, parkedPieceID int64) (bool, error) {
	var refIDs []int64
	// Only refs recorded on pull items are candidates. Refs created by other
	// subsystems for the same parked row are left alone.
	err := tx.Select(&refIDs, `
		SELECT DISTINCT fi.parked_piece_ref
		FROM pdp_piece_pull_items fi
		JOIN parked_piece_refs pr ON pr.ref_id = fi.parked_piece_ref
		WHERE fi.pull_parked_piece_id = $1
			AND pr.piece_id = $1
			AND fi.parked_piece_ref IS NOT NULL
	`, parkedPieceID)
	if err != nil {
		return false, xerrors.Errorf("query pull-created parked piece refs: %w", err)
	}

	// Clear item pointers first so terminal rows do not keep stale pull markers
	// if a ref cannot be deleted because it has already been promoted.
	_, err = tx.Exec(`
		UPDATE pdp_piece_pull_items
		SET parked_piece_ref = NULL,
			pull_parked_piece_id = NULL
		WHERE complete = FALSE
			AND pull_parked_piece_id = $1
	`, parkedPieceID)
	if err != nil {
		return false, xerrors.Errorf("clear pull-created parked piece refs: %w", err)
	}

	if len(refIDs) > 0 {
		// Do not delete refs already promoted into pdp_piecerefs.
		_, err = tx.Exec(`
			DELETE FROM parked_piece_refs pr
			WHERE pr.ref_id = ANY($1::BIGINT[])
				AND NOT EXISTS (
					SELECT 1 FROM pdp_piecerefs ppr WHERE ppr.piece_ref = pr.ref_id
				)
		`, refIDs)
		if err != nil {
			return false, xerrors.Errorf("delete pull-created parked piece refs: %w", err)
		}
	}

	// Remove the pull-created parked row only when all refs are gone.
	n, err := tx.Exec(`
		DELETE FROM parked_pieces pp
		WHERE pp.id = $1
			AND pp.complete = FALSE
			AND NOT EXISTS (
				SELECT 1 FROM parked_piece_refs ppr WHERE ppr.piece_id = pp.id
			)
	`, parkedPieceID)
	if err != nil {
		return false, xerrors.Errorf("delete pull-created parked piece: %w", err)
	}
	if n > 0 {
		return true, nil
	}

	// If refs remain, make the normal ParkPiece scheduler responsible for the row.
	_, err = tx.Exec(`
		UPDATE parked_pieces pp
		SET skip = FALSE
		WHERE pp.id = $1
			AND pp.complete = FALSE
			AND pp.skip = TRUE
			AND EXISTS (
				SELECT 1 FROM parked_piece_refs ppr WHERE ppr.piece_id = pp.id
			)
	`, parkedPieceID)
	if err != nil {
		return false, xerrors.Errorf("enable park task for remaining refs: %w", err)
	}

	return false, nil
}

// completePullItemWithParkedPiece creates the ref records and marks one item complete.
func completePullItemWithParkedPiece(tx *harmonydb.Tx, service string, fetchID int64, pieceCID string, rawSize uint64, sourceURL string, parkedPieceID int64, existingPieceRef int64) error {
	n, err := tx.Exec(`
		UPDATE pdp_piece_pull_items
		SET complete = TRUE,
			failed = FALSE,
			fail_reason = NULL,
			task_id = NULL
		WHERE fetch_id = $1
			AND piece_cid = $2
			AND piece_raw_size = $3
			AND source_url = $4
			AND complete = FALSE
			AND failed = FALSE
	`, fetchID, pieceCID, rawSize, sourceURL)
	if err != nil {
		return xerrors.Errorf("mark pull item complete: %w", err)
	}
	if n == 0 {
		return nil
	}
	if n != 1 {
		return xerrors.Errorf("mark pull item complete: expected 1, got %d", n)
	}

	pieceRef := existingPieceRef
	if pieceRef == 0 {
		// Rows that were not attached during Do may not have a ref yet. Create
		// one with their original URL even if another source URL downloaded
		// the bytes.
		err = tx.QueryRow(`
			INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
			VALUES ($1, $2, TRUE)
			RETURNING ref_id
		`, parkedPieceID, sourceURL).Scan(&pieceRef)
		if err != nil {
			return xerrors.Errorf("insert parked_piece_refs: %w", err)
		}

		n, err = tx.Exec(`
			UPDATE pdp_piece_pull_items
			SET parked_piece_ref = $5
			WHERE fetch_id = $1
				AND piece_cid = $2
				AND piece_raw_size = $3
				AND source_url = $4
		`, fetchID, pieceCID, rawSize, sourceURL, pieceRef)
		if err != nil {
			return xerrors.Errorf("attach parked_piece_ref to completed pull item: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("attach parked_piece_ref to completed pull item: expected 1, got %d", n)
		}
	}

	needsSaveCache := padreader.PaddedSize(rawSize).Padded() >= pullMinPieceSizeForCache
	n, err = tx.Exec(`
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		VALUES ($1, $2, $3, NOW(), $4)
	`, service, pieceCID, pieceRef, needsSaveCache)
	if err != nil {
		return xerrors.Errorf("insert pdp_piecerefs: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert pdp_piecerefs: expected 1, got %d", n)
	}

	return nil
}

// findCompleteParkedPiece returns the oldest complete long-term row for a piece key.
func findCompleteParkedPiece(tx *harmonydb.Tx, group pullPieceKey) (*int64, error) {
	var id int64
	err := tx.QueryRow(`
		SELECT id
		FROM parked_pieces
		WHERE piece_cid = $1
			AND piece_padded_size = $2
			AND long_term = TRUE
			AND complete = TRUE
			AND cleanup_task_id IS NULL
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`, group.PieceCid, pdp.PadPieceSize(group.PieceRawSize)).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, xerrors.Errorf("query complete parked piece: %w", err)
	}
	return &id, nil
}

func (t *PDPPullPieceTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *PDPPullPieceTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: tasknames.PDPv0_PullPiece,
		Max:  taskhelp.Max(t.max),
		Cost: resources.Resources{
			Cpu:     0,
			Gpu:     0,
			Ram:     128 << 20,
			Storage: nil,
		},
		MaxFailures: 1,
	}
}

func (t *PDPPullPieceTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	t.TF.Set(taskFunc)
}

var _ harmonytask.TaskInterface = &PDPPullPieceTask{}
var _ = harmonytask.Reg(&PDPPullPieceTask{})

// idleTimeoutReader cancels its context when a response stalls between reads.
type idleTimeoutReader struct {
	r      io.Reader
	timer  *time.Timer
	cancel context.CancelFunc
	idle   time.Duration
}

// Read resets the idle timer only after bytes are received.
func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	return n, err
}
