package storage_market

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/market/mk20release"
)

const (
	mk20ReleaseCandidateQuantum = 64
	mk20ReleasePassTimeout      = 30 * time.Second
)

type mk20ReleasePolicy struct {
	batch     int
	maxActive int64
}

func (p mk20ReleasePolicy) controlled() bool {
	return p.batch > 0 || p.maxActive > 0
}

func snapshotMK20ReleasePolicy(cfg *config.CurioIngestConfig) (mk20ReleasePolicy, error) {
	if cfg == nil {
		return mk20ReleasePolicy{}, fmt.Errorf("MK20 release config is nil")
	}

	var batch int
	if cfg.MK20PipelineInsertBatch != nil {
		batch = cfg.MK20PipelineInsertBatch.Get()
	}
	if batch < 0 {
		return mk20ReleasePolicy{}, fmt.Errorf("Ingest.MK20PipelineInsertBatch must be non-negative, got %d", batch)
	}

	var maxActive int
	if cfg.MK20PipelineInsertMaxActive != nil {
		maxActive = cfg.MK20PipelineInsertMaxActive.Get()
	}
	if maxActive < 0 {
		return mk20ReleasePolicy{}, fmt.Errorf("Ingest.MK20PipelineInsertMaxActive must be non-negative, got %d", maxActive)
	}

	return mk20ReleasePolicy{batch: batch, maxActive: int64(maxActive)}, nil
}

type mk20ReleasePassDeps struct {
	waiting        func(context.Context) (bool, error)
	pressure       func(context.Context) (bool, error)
	active         func(context.Context) (int64, error)
	candidates     func(context.Context, string, int) ([]string, error)
	release        func(context.Context, string, int64) (mk20release.Outcome, error)
	wakeDealPoller func()
}

type mk20ReleasePassResult struct {
	cursor          string
	scanned         int
	released        int
	noLongerWaiting int
	deferred        int
	tooLarge        int
}

// runMK20ReleasePass scans at most mk20ReleaseCandidateQuantum waiting rows.
// The cursor is a fairness hint only; release revalidates every authoritative
// decision after taking the database gate. A successful commit is always
// followed by exactly one wake, even when a later candidate fails.
func runMK20ReleasePass(ctx context.Context, cursor string, policy mk20ReleasePolicy, deps mk20ReleasePassDeps) (result mk20ReleasePassResult, retErr error) {
	result.cursor = cursor
	if deps.candidates == nil || deps.release == nil || deps.wakeDealPoller == nil {
		return result, fmt.Errorf("MK20 release pass dependencies are incomplete")
	}
	defer func() {
		if result.released > 0 {
			deps.wakeDealPoller()
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if policy.controlled() {
		if deps.waiting == nil {
			return result, fmt.Errorf("MK20 controlled release requires a waiting-queue check")
		}
		// Candidate selection is already the bounded emptiness check in default
		// mode. Controlled passes use this cheaper global probe to avoid fresh
		// pressure and active-count queries when there is no waiting work.
		hasWaiting, err := deps.waiting(ctx)
		if err != nil {
			return result, fmt.Errorf("checking for MK20 waiting deals: %w", err)
		}
		if !hasWaiting {
			result.cursor = ""
			return result, nil
		}

		if deps.pressure == nil {
			return result, fmt.Errorf("MK20 controlled release requires a fresh pressure check")
		}
		pressure, err := deps.pressure(ctx)
		if err != nil {
			return result, fmt.Errorf("checking fresh MK20 release pressure: %w", err)
		}
		if pressure {
			return result, nil
		}
	}

	// This is only a conservative shortcut. Capacity is authoritatively checked
	// after the singleton gate write in every release transaction.
	if policy.maxActive > 0 {
		if deps.active == nil {
			return result, fmt.Errorf("MK20 capped release requires an active-row query")
		}
		active, err := deps.active(ctx)
		if err != nil {
			return result, fmt.Errorf("counting active MK20 pipeline rows: %w", err)
		}
		if active < 0 {
			return result, fmt.Errorf("negative active MK20 pipeline row count: %d", active)
		}
		if active >= policy.maxActive {
			return result, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	ids, err := deps.candidates(ctx, cursor, mk20ReleaseCandidateQuantum)
	if err != nil {
		return result, fmt.Errorf("selecting MK20 waiting candidates: %w", err)
	}
	if len(ids) == 0 && cursor != "" {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		result.cursor = ""
		ids, err = deps.candidates(ctx, "", mk20ReleaseCandidateQuantum)
		if err != nil {
			return result, fmt.Errorf("wrapping MK20 waiting candidate cursor: %w", err)
		}
	}
	if len(ids) > mk20ReleaseCandidateQuantum {
		return result, fmt.Errorf("MK20 candidate query returned %d rows, maximum is %d", len(ids), mk20ReleaseCandidateQuantum)
	}

	seen := make(map[string]struct{}, len(ids))
	var candidateErrs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(errors.Join(candidateErrs...), err)
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		result.scanned++
		result.cursor = id

		outcome, err := deps.release(ctx, id, policy.maxActive)
		if err != nil {
			if errors.Is(err, mk20release.ErrGateUnavailable) {
				return result, errors.Join(errors.Join(candidateErrs...), err)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, errors.Join(errors.Join(candidateErrs...), err, ctxErr)
			}
			candidateErrs = append(candidateErrs, fmt.Errorf("releasing MK20 deal %s: %w", id, err))
			continue
		}

		switch outcome {
		case mk20release.Released:
			result.released++
			if policy.batch > 0 && result.released >= policy.batch {
				return result, errors.Join(candidateErrs...)
			}
		case mk20release.AtCapacity:
			return result, errors.Join(candidateErrs...)
		case mk20release.DoesNotFit:
			result.deferred++
		case mk20release.TooLarge:
			result.tooLarge++
		case mk20release.NoLongerWaiting:
			result.noLongerWaiting++
		default:
			candidateErrs = append(candidateErrs, fmt.Errorf("MK20 deal %s returned unknown release outcome %q", id, outcome))
		}
	}

	return result, errors.Join(candidateErrs...)
}

func (d *CurioStorageDealMarket) insertDDODealInPipeline(ctx context.Context) {
	if d == nil || d.cfg == nil || d.db == nil {
		log.Error("MK20 waiting release is unavailable: storage market dependencies are nil")
		return
	}
	policy, err := snapshotMK20ReleasePolicy(&d.cfg.Ingest)
	if err != nil {
		log.Errorf("MK20 waiting release disabled by invalid config: %s", err)
		return
	}

	passCtx, cancel := context.WithTimeout(ctx, mk20ReleasePassTimeout)
	defer cancel()

	result, err := runMK20ReleasePass(passCtx, d.mk20WaitingCursor, policy, mk20ReleasePassDeps{
		waiting: func(ctx context.Context) (bool, error) {
			return hasMK20WaitingDeals(ctx, d.db)
		},
		pressure: func(ctx context.Context) (bool, error) {
			if d.bp == nil {
				return false, fmt.Errorf("backpressure module is unavailable")
			}
			bp, err := d.bp.Val()
			if err != nil {
				return false, err
			}
			return bp.MK20ReleasePressure(ctx, &d.cfg.Ingest, d.db)
		},
		active: func(ctx context.Context) (int64, error) {
			return countActiveMK20PipelineRows(ctx, d.db)
		},
		candidates: func(ctx context.Context, after string, limit int) ([]string, error) {
			return selectMK20WaitingCandidates(ctx, d.db, after, limit)
		},
		release: func(ctx context.Context, id string, maxActive int64) (mk20release.Outcome, error) {
			return releaseMK20WaitingDeal(ctx, d.db, id, maxActive)
		},
		wakeDealPoller: d.WakeDealPoller,
	})
	d.mk20WaitingCursor = result.cursor

	if err != nil {
		log.Errorf("MK20 waiting release pass failed after scanning %d and releasing %d deal(s): %s", result.scanned, result.released, err)
		return
	}
	if result.released > 0 || result.deferred > 0 || result.tooLarge > 0 {
		log.Infow("MK20 waiting release pass",
			"scanned", result.scanned,
			"released", result.released,
			"deferred", result.deferred,
			"too_large", result.tooLarge,
			"already_released", result.noLongerWaiting,
			"batch_limit", policy.batch,
			"max_active", policy.maxActive)
	}
}

func hasMK20WaitingDeals(ctx context.Context, db *harmonydb.DB) (bool, error) {
	if db == nil {
		return false, fmt.Errorf("MK20 database is nil")
	}
	var waiting bool
	err := db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1
		FROM market_mk20_pipeline_waiting
	)`).Scan(&waiting)
	return waiting, err
}

func selectMK20WaitingCandidates(ctx context.Context, db *harmonydb.DB, after string, limit int) ([]string, error) {
	if db == nil || limit <= 0 || limit > mk20ReleaseCandidateQuantum {
		return nil, fmt.Errorf("invalid MK20 waiting candidate query")
	}

	var rows *harmonydb.Query
	var err error
	if after == "" {
		rows, err = db.Query(ctx, `SELECT id
			FROM market_mk20_pipeline_waiting
			ORDER BY id
			LIMIT $1`, limit)
	} else {
		rows, err = db.Query(ctx, `SELECT id
			FROM market_mk20_pipeline_waiting
			WHERE id > $1
			ORDER BY id
			LIMIT $2`, after, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func countActiveMK20PipelineRows(ctx context.Context, db *harmonydb.DB) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("MK20 database is nil")
	}
	var active int64
	err := db.QueryRow(ctx, `SELECT COUNT(*)
		FROM market_mk20_pipeline
		WHERE complete = FALSE`).Scan(&active)
	return active, err
}

func releaseMK20WaitingDeal(ctx context.Context, db *harmonydb.DB, id string, maxActive int64) (mk20release.Outcome, error) {
	dealID, err := ulid.Parse(id)
	if err != nil {
		return "", fmt.Errorf("parsing MK20 deal ID %q: %w", id, err)
	}

	return mk20release.Release(ctx, db, id, maxActive, func(tx *harmonydb.Tx) (mk20release.Plan, error) {
		deal, err := mk20.DealFromTX(tx, dealID)
		if err != nil {
			return mk20release.Plan{}, fmt.Errorf("loading MK20 deal: %w", err)
		}
		rows, err := mk20PipelineRowCost(deal)
		if err != nil {
			return mk20release.Plan{}, err
		}
		return mk20release.Plan{
			Rows: rows,
			Insert: func() error {
				return insertPiecesInTransaction(ctx, tx, deal)
			},
		}, nil
	})
}

func mk20PipelineRowCost(deal *mk20.Deal) (int64, error) {
	if deal == nil {
		return 0, fmt.Errorf("MK20 deal is nil")
	}
	if deal.Products.DDOV1 == nil {
		return 0, fmt.Errorf("MK20 deal %s has no DDO product", deal.Identifier)
	}
	if deal.Data == nil {
		return 0, fmt.Errorf("MK20 deal %s has no data source", deal.Identifier)
	}

	sourceCount := 0
	for _, present := range []bool{
		deal.Data.SourceHTTP != nil,
		deal.Data.SourceOffline != nil,
		deal.Data.SourceAggregate != nil,
		deal.Data.SourceHttpPut != nil,
	} {
		if present {
			sourceCount++
		}
	}
	if sourceCount != 1 {
		return 0, fmt.Errorf("MK20 deal %s must have exactly one data source, found %d", deal.Identifier, sourceCount)
	}

	switch {
	case deal.Data.SourceHTTP != nil:
		return 1, nil
	case deal.Data.SourceOffline != nil:
		return 1, nil
	case deal.Data.SourceAggregate != nil:
		rows := len(deal.Data.SourceAggregate.Pieces)
		if rows == 0 {
			return 0, fmt.Errorf("MK20 aggregate deal %s has no pieces", deal.Identifier)
		}
		for i, piece := range deal.Data.SourceAggregate.Pieces {
			pieceSources := 0
			if piece.SourceHTTP != nil {
				pieceSources++
			}
			if piece.SourceOffline != nil {
				pieceSources++
			}
			if piece.SourceAggregate != nil || piece.SourceHttpPut != nil {
				return 0, fmt.Errorf("MK20 aggregate deal %s subpiece %d has an unsupported data source", deal.Identifier, i)
			}
			if pieceSources != 1 {
				return 0, fmt.Errorf("MK20 aggregate deal %s subpiece %d must have exactly one HTTP or offline source, found %d", deal.Identifier, i, pieceSources)
			}
		}
		return int64(rows), nil
	default:
		return 0, fmt.Errorf("MK20 deal %s has an unsupported data source", deal.Identifier)
	}
}
