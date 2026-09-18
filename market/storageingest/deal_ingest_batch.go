package storageingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yugabyte/pgx/v5/pgconn"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/tasks/seal"
)

// sealBatchSize bounds a ready-sector transfer transaction. It is an
// internal work quantum, not a limit on sealing work in progress.
const sealBatchSize = 64

const selectSealCandidatesSQL = `SELECT sector_number
	FROM open_sector_pieces
	WHERE sp_id = $1
	  AND NOT (sector_number = ANY($3::bigint[]))
	GROUP BY sector_number
	HAVING BOOL_AND(is_snap = $2)
	   AND (SUM(piece_size) = $4
	        OR MIN(created_at) < $5
	        OR MIN(COALESCE(direct_start_epoch, f05_deal_start_epoch, 0)) < $6)
	ORDER BY sector_number
	LIMIT $7`

type sealBatchParams struct {
	spID                 int64
	proof                int64
	sectorSize           int64
	isSnap               bool
	maxWaitBefore        time.Time
	sealBeforeChainEpoch abi.ChainEpoch
}

type sealBatchAttempt struct {
	candidates []abi.SectorNumber
	committed  int
}

type sealProviderBatchResult struct {
	candidates int
	committed  int
	resolved   int
	hasMore    bool
	warnings   []error
}

type sealProviderDrainState struct {
	params sealBatchParams
	failed map[abi.SectorNumber]struct{}
	done   bool
}

type sealBatchFunc func(failed map[abi.SectorNumber]struct{}, limit int) (sealBatchAttempt, error)
type sealSectorFunc func(sector abi.SectorNumber) (sealSectorResult, error)
type sealProviderBatchFunc func(state *sealProviderDrainState) (sealProviderBatchResult, error)

type sealSectorDisposition int

const (
	sealSectorReady sealSectorDisposition = iota
	sealSectorMoved
	sealSectorAlreadyTransferred
	sealSectorDisappeared
	sealSectorNotEligible
	sealSectorInconsistent
)

type sealSectorResult struct {
	disposition sealSectorDisposition
}

type sealCandidateError struct {
	err error
}

func (e *sealCandidateError) Error() string { return e.err.Error() }
func (e *sealCandidateError) Unwrap() error { return e.err }

func newSealCandidateError(format string, args ...any) error {
	return &sealCandidateError{err: fmt.Errorf(format, args...)}
}

// isDeterministicSealCandidateError is deliberately conservative. Only
// explicit candidate-state errors, PL/pgSQL candidate exceptions, and unique
// conflicts from pipeline/initial-piece insertion are isolated per sector.
// Connection, cancellation, serialization, generic data/integrity, and
// unknown errors stop the pass instead of fanning out into more transactions.
func isDeterministicSealCandidateError(err error) bool {
	var candidateErr *sealCandidateError
	if errors.As(err, &candidateErr) {
		return true
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "P0001" || pgErr.Code == "23505"
}

// drainSealProviders gives every provider at most one bounded transaction per
// round, then immediately starts another round while work remains.
func drainSealProviders(ctx context.Context, db *harmonydb.DB, params []sealBatchParams) error {
	return drainSealProvidersWith(ctx, params, func(state *sealProviderDrainState) (sealProviderBatchResult, error) {
		return drainSealProviderBatch(ctx, db, state)
	})
}

func drainSealProvidersWith(ctx context.Context, params []sealBatchParams, run sealProviderBatchFunc) error {
	states := make([]sealProviderDrainState, 0, len(params))
	for _, provider := range params {
		states = append(states, sealProviderDrainState{
			params: provider,
			failed: make(map[abi.SectorNumber]struct{}),
		})
	}

	var warnings []error
	for {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(warnings, err)...)
		}

		active := 0
		resolved := 0
		for i := range states {
			state := &states[i]
			if state.done {
				continue
			}
			active++

			result, err := run(state)
			warnings = append(warnings, result.warnings...)
			if err != nil {
				return errors.Join(append(warnings, err)...)
			}
			resolved += result.resolved
			if !result.hasMore {
				state.done = true
			}
		}

		remaining := 0
		for i := range states {
			if !states[i].done {
				remaining++
			}
		}
		if active == 0 || remaining == 0 {
			return errors.Join(warnings...)
		}
		if resolved == 0 {
			return errors.Join(append(warnings, xerrors.New("storage ingest seal drain made no progress"))...)
		}
	}
}

func drainSealProviderBatch(ctx context.Context, db *harmonydb.DB, state *sealProviderDrainState) (sealProviderBatchResult, error) {
	return drainSealProviderBatchWith(ctx, state,
		func(failed map[abi.SectorNumber]struct{}, limit int) (sealBatchAttempt, error) {
			return sealReadyBatch(ctx, db, state.params, failed, limit)
		},
		func(sector abi.SectorNumber) (sealSectorResult, error) {
			return sealReadySector(ctx, db, state.params, sector)
		},
	)
}

func drainSealProviderBatchWith(ctx context.Context, state *sealProviderDrainState, sealBatch sealBatchFunc, sealSector sealSectorFunc) (sealProviderBatchResult, error) {
	started := time.Now()
	attempt, err := sealBatch(state.failed, sealBatchSize)
	result := sealProviderBatchResult{
		candidates: len(attempt.candidates),
		hasMore:    len(attempt.candidates) == sealBatchSize,
	}
	if err == nil {
		result.committed = attempt.committed
		result.resolved = len(attempt.candidates)
		if len(attempt.candidates) > 0 {
			log.Infow("committed storage ingest seal batch",
				"sp_id", state.params.spID,
				"candidate_count", len(attempt.candidates),
				"committed_count", attempt.committed,
				"failed_count", 0,
				"elapsed", time.Since(started),
				"another_batch", result.hasMore,
			)
		}
		return result, nil
	}

	if len(attempt.candidates) == 0 || !isDeterministicSealCandidateError(err) {
		return result, err
	}

	log.Warnw("storage ingest seal batch has a candidate-specific failure; isolating its bounded candidates",
		"sp_id", state.params.spID,
		"candidate_count", len(attempt.candidates),
		"error", err,
	)

	failedCount := 0
	for _, sector := range attempt.candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		sectorResult, sectorErr := sealSector(sector)
		if sectorErr != nil {
			if !isDeterministicSealCandidateError(sectorErr) {
				return result, sectorErr
			}
			state.failed[sector] = struct{}{}
			failedCount++
			result.resolved++
			wrapped := xerrors.Errorf("sealing provider %d sector %d: %w", state.params.spID, sector, sectorErr)
			result.warnings = append(result.warnings, wrapped)
			log.Errorw("failed to move open sector to sealing pipeline",
				"sp_id", state.params.spID,
				"sector_number", sector,
				"error", sectorErr,
			)
			continue
		}

		result.resolved++
		if sectorResult.disposition == sealSectorMoved {
			result.committed++
		}
	}

	log.Infow("completed bounded storage ingest candidate isolation",
		"sp_id", state.params.spID,
		"candidate_count", len(attempt.candidates),
		"committed_count", result.committed,
		"failed_count", failedCount,
		"elapsed", time.Since(started),
		"another_batch", result.hasMore,
	)
	return result, nil
}

func sealReadyBatch(ctx context.Context, db *harmonydb.DB, params sealBatchParams, failed map[abi.SectorNumber]struct{}, limit int) (sealBatchAttempt, error) {
	var result sealBatchAttempt
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// The callback can be rerun after a serialization failure. Never expose
		// candidates or counts from an attempt which rolled back.
		result = sealBatchAttempt{}
		if err := seal.LockSectorState(ctx, tx, params.spID); err != nil {
			return false, err
		}

		candidates, err := selectSealCandidates(tx, params, failed, limit)
		if err != nil {
			return false, err
		}
		result.candidates = candidates
		if len(candidates) == 0 {
			return true, nil
		}

		result.committed, err = transferSealCandidates(tx, params, candidates)
		if err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return result, err
	}
	if !committed {
		return result, xerrors.Errorf("provider %d seal batch transaction did not commit", params.spID)
	}
	return result, nil
}

func selectSealCandidates(tx *harmonydb.Tx, params sealBatchParams, failed map[abi.SectorNumber]struct{}, limit int) ([]abi.SectorNumber, error) {
	failedNumbers := make([]int64, 0, len(failed))
	for sector := range failed {
		failedNumbers = append(failedNumbers, int64(sector))
	}

	var rows []struct {
		Sector abi.SectorNumber `db:"sector_number"`
	}
	err := tx.Select(&rows, selectSealCandidatesSQL,
		params.spID,
		params.isSnap,
		failedNumbers,
		params.sectorSize,
		params.maxWaitBefore,
		params.sealBeforeChainEpoch,
		limit,
	)
	if err != nil {
		return nil, xerrors.Errorf("selecting seal candidates for provider %d: %w", params.spID, err)
	}

	candidates := make([]abi.SectorNumber, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, row.Sector)
	}
	return candidates, nil
}

func transferSealCandidates(tx *harmonydb.Tx, params sealBatchParams, candidates []abi.SectorNumber) (int, error) {
	sectorNumbers := make([]int64, 0, len(candidates))
	for _, sector := range candidates {
		sectorNumbers = append(sectorNumbers, int64(sector))
	}

	var inserted int
	var err error
	if params.isSnap {
		inserted, err = tx.Exec(`INSERT INTO sectors_snap_pipeline (sp_id, sector_number, upgrade_proof)
			SELECT $2, sector_number, $3
			FROM unnest($1::bigint[]) AS candidate(sector_number)`, sectorNumbers, params.spID, params.proof)
	} else {
		inserted, err = tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof)
			SELECT $2, sector_number, $3
			FROM unnest($1::bigint[]) AS candidate(sector_number)`, sectorNumbers, params.spID, params.proof)
	}
	if err != nil {
		return 0, xerrors.Errorf("adding seal candidates for provider %d: %w", params.spID, err)
	}
	if inserted != len(candidates) {
		return 0, newSealCandidateError("adding seal candidates for provider %d: inserted %d of %d candidates", params.spID, inserted, len(candidates))
	}

	var transferRows *harmonydb.Query
	if params.isSnap {
		transferRows, err = tx.Query(`SELECT transfer_and_delete_sorted_open_piece_snap($2, sector_number)
			FROM unnest($1::bigint[]) AS inserted(sector_number)
			ORDER BY sector_number`, sectorNumbers, params.spID)
	} else {
		transferRows, err = tx.Query(`SELECT transfer_and_delete_sorted_open_piece($2, sector_number)
			FROM unnest($1::bigint[]) AS inserted(sector_number)
			ORDER BY sector_number`, sectorNumbers, params.spID)
	}
	if err != nil {
		return 0, xerrors.Errorf("starting transfer for provider %d: %w", params.spID, err)
	}
	defer transferRows.Close()

	moved := 0
	for transferRows.Next() {
		moved++
	}
	if err := transferRows.Err(); err != nil {
		return 0, xerrors.Errorf("transferring open sectors for provider %d: %w", params.spID, err)
	}
	if moved != len(candidates) {
		return moved, newSealCandidateError("transferring open sectors for provider %d: moved %d of %d candidates", params.spID, moved, len(candidates))
	}
	return moved, nil
}

type sealSectorState struct {
	openPieces      int64
	modeMatches     bool
	eligible        bool
	pipelineExists  bool
	pipelineMatches bool
}

func classifySealSectorState(params sealBatchParams, sector abi.SectorNumber, state sealSectorState) (sealSectorDisposition, error) {
	if state.openPieces == 0 {
		if state.pipelineMatches {
			return sealSectorAlreadyTransferred, nil
		}
		if state.pipelineExists {
			return sealSectorInconsistent, newSealCandidateError("provider %d sector %d has a non-matching pipeline row after its open pieces disappeared", params.spID, sector)
		}
		return sealSectorDisappeared, newSealCandidateError("provider %d sector %d disappeared without a matching pipeline row", params.spID, sector)
	}
	if !state.modeMatches {
		return sealSectorInconsistent, newSealCandidateError("provider %d sector %d contains mixed normal and snap open pieces", params.spID, sector)
	}
	if state.pipelineExists {
		return sealSectorInconsistent, newSealCandidateError("provider %d sector %d has an existing pipeline row while %d open pieces remain", params.spID, sector, state.openPieces)
	}
	if !state.eligible {
		return sealSectorNotEligible, nil
	}
	return sealSectorReady, nil
}

func loadSealSectorState(tx *harmonydb.Tx, params sealBatchParams, sector abi.SectorNumber) (sealSectorState, error) {
	var state sealSectorState
	var err error
	if params.isSnap {
		err = tx.QueryRow(`WITH open_state AS (
			SELECT COUNT(*) AS open_pieces,
			       COALESCE(BOOL_AND(is_snap = TRUE), FALSE) AS mode_matches,
			       COALESCE(
			           SUM(piece_size) = $4
			           OR MIN(created_at) < $5
			           OR MIN(COALESCE(direct_start_epoch, f05_deal_start_epoch, 0)) < $6,
			           FALSE
			       ) AS eligible
			FROM open_sector_pieces
			WHERE sp_id = $1 AND sector_number = $2
		)
		SELECT open_pieces,
		       mode_matches,
		       eligible,
		       EXISTS (SELECT 1 FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2),
		       EXISTS (SELECT 1 FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2 AND upgrade_proof = $3)
		FROM open_state`, params.spID, sector, params.proof, params.sectorSize, params.maxWaitBefore, params.sealBeforeChainEpoch).
			Scan(&state.openPieces, &state.modeMatches, &state.eligible, &state.pipelineExists, &state.pipelineMatches)
	} else {
		err = tx.QueryRow(`WITH open_state AS (
			SELECT COUNT(*) AS open_pieces,
			       COALESCE(BOOL_AND(is_snap = FALSE), FALSE) AS mode_matches,
			       COALESCE(
			           SUM(piece_size) = $4
			           OR MIN(created_at) < $5
			           OR MIN(COALESCE(direct_start_epoch, f05_deal_start_epoch, 0)) < $6,
			           FALSE
			       ) AS eligible
			FROM open_sector_pieces
			WHERE sp_id = $1 AND sector_number = $2
		)
		SELECT open_pieces,
		       mode_matches,
		       eligible,
		       EXISTS (SELECT 1 FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2),
		       EXISTS (SELECT 1 FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2 AND reg_seal_proof = $3)
		FROM open_state`, params.spID, sector, params.proof, params.sectorSize, params.maxWaitBefore, params.sealBeforeChainEpoch).
			Scan(&state.openPieces, &state.modeMatches, &state.eligible, &state.pipelineExists, &state.pipelineMatches)
	}
	if err != nil {
		return sealSectorState{}, xerrors.Errorf("revalidating provider %d sector %d: %w", params.spID, sector, err)
	}
	return state, nil
}

func sealReadySector(ctx context.Context, db *harmonydb.DB, params sealBatchParams, sector abi.SectorNumber) (sealSectorResult, error) {
	var result sealSectorResult
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		result = sealSectorResult{}
		if err := seal.LockSectorState(ctx, tx, params.spID); err != nil {
			return false, err
		}

		state, err := loadSealSectorState(tx, params, sector)
		if err != nil {
			return false, err
		}
		disposition, err := classifySealSectorState(params, sector, state)
		if err != nil {
			return false, err
		}
		result.disposition = disposition
		if disposition != sealSectorReady {
			return true, nil
		}

		if params.isSnap {
			_, err = tx.Exec(`INSERT INTO sectors_snap_pipeline (sp_id, sector_number, upgrade_proof)
				VALUES ($1, $2, $3)`, params.spID, sector, params.proof)
		} else {
			_, err = tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof)
				VALUES ($1, $2, $3)`, params.spID, sector, params.proof)
		}
		if err != nil {
			return false, xerrors.Errorf("adding provider %d sector %d to pipeline: %w", params.spID, sector, err)
		}

		if params.isSnap {
			_, err = tx.Exec(`SELECT transfer_and_delete_sorted_open_piece_snap($1, $2)`, params.spID, sector)
		} else {
			_, err = tx.Exec(`SELECT transfer_and_delete_sorted_open_piece($1, $2)`, params.spID, sector)
		}
		if err != nil {
			return false, xerrors.Errorf("transferring provider %d sector %d: %w", params.spID, sector, err)
		}

		var stillOpen bool
		if err := tx.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM open_sector_pieces
			WHERE sp_id = $1 AND sector_number = $2
		)`, params.spID, sector).Scan(&stillOpen); err != nil {
			return false, xerrors.Errorf("checking provider %d sector %d transfer postcondition: %w", params.spID, sector, err)
		}
		if stillOpen {
			return false, newSealCandidateError("provider %d sector %d still has open pieces after transfer", params.spID, sector)
		}

		result.disposition = sealSectorMoved
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return result, err
	}
	if !committed {
		return result, xerrors.Errorf("provider %d sector %d transaction did not commit", params.spID, sector)
	}
	return result, nil
}
