package seal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	ffi2 "github.com/filecoin-project/curio/lib/ffi"
)

const sdrCandidateSQL = `SELECT task_id_sdr FROM sectors_sdr_pipeline
WHERE task_id_sdr = ANY($1::bigint[])
GROUP BY task_id_sdr
HAVING COUNT(*) = 1 AND bool_and(NOT after_sdr AND NOT failed)`

const sdrSectorReferenceSQL = `SELECT sp_id, sector_number, reg_seal_proof, after_sdr, failed
FROM sectors_sdr_pipeline WHERE task_id_sdr = $1`

var errSDRTaskNotReady = errors.New("SDR task has no unique unfinished sector reference")

type sdrCandidateSelect func(context.Context, interface{}, ...interface{}) error

func (s *SDRTask) FilterCandidates(ctx context.Context, ids []harmonytask.TaskID) ([]harmonytask.TaskID, error) {
	return filterSDRCandidates(ctx, func(ctx context.Context, out interface{}, args ...interface{}) error {
		return s.db.Select(ctx, out, sdrCandidateSQL, args...)
	}, ids)
}

func filterSDRCandidates(ctx context.Context, selectRows sdrCandidateSelect, ids []harmonytask.TaskID) ([]harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var eligible []harmonytask.TaskID
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := selectRows(ctx, &eligible, ids); err != nil {
		return nil, fmt.Errorf("checking SDR candidate references: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return eligible, nil
}

type sdrSectorReference struct {
	SpID         int64                   `db:"sp_id"`
	SectorNumber int64                   `db:"sector_number"`
	RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`
	AfterSDR     bool                    `db:"after_sdr"`
	Failed       bool                    `db:"failed"`
}

// Storage claims re-read the reference after advisory discovery/ownership
// claim. Missing, ambiguous, completed or failed work is rejected before
// storage allocation or native work.
// Query errors retain their cause and are not classified as missing work.
func loadSDRSectorReference(ctx context.Context, selectRows sdrCandidateSelect, id harmonytask.TaskID) (ffi2.SectorRef, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var refs []sdrSectorReference
	if err := ctx.Err(); err != nil {
		return ffi2.SectorRef{}, err
	}
	if err := selectRows(ctx, &refs, id); err != nil {
		return ffi2.SectorRef{}, fmt.Errorf("getting SDR sector reference for task %d: %w", id, err)
	}
	if err := ctx.Err(); err != nil {
		return ffi2.SectorRef{}, err
	}
	if len(refs) != 1 {
		return ffi2.SectorRef{}, fmt.Errorf("%w: task %d, got %d references", errSDRTaskNotReady, id, len(refs))
	}
	r := refs[0]
	if r.AfterSDR || r.Failed {
		return ffi2.SectorRef{}, fmt.Errorf("%w: task %d, after_sdr=%v failed=%v", errSDRTaskNotReady, id, r.AfterSDR, r.Failed)
	}
	return ffi2.SectorRef{SpID: r.SpID, SectorNumber: r.SectorNumber, RegSealProof: r.RegSealProof}, nil
}

func (s *SDRTask) sectorReference(ctx context.Context, id harmonytask.TaskID) (ffi2.SectorRef, error) {
	return loadSDRSectorReference(ctx, func(ctx context.Context, out interface{}, args ...interface{}) error {
		return s.db.Select(ctx, out, sdrSectorReferenceSQL, args...)
	}, id)
}
