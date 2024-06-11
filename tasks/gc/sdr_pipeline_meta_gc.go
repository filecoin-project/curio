package gc

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
)

const SDRPipelineGCInterval = 19 * time.Minute

type SDRPipelineGC struct {
	db *harmonydb.DB
}

func NewSDRPipelineGC(db *harmonydb.DB) *SDRPipelineGC {
	return &SDRPipelineGC{
		db: db,
	}
}

func (s *SDRPipelineGC) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	if err := s.cleanupSealed(); err != nil {
		return false, xerrors.Errorf("cleanupSealed: %w", err)
	}

	// TODO cleanupFailed

	return true, nil
}

func (s *SDRPipelineGC) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SDRPipelineGC) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  1,
		Name: "SDRPipelineGC",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(SDRPipelineGCInterval, s),
	}
}

func (s *SDRPipelineGC) Adder(taskFunc harmonytask.AddTaskFunc) {
}

func (s *SDRPipelineGC) cleanupSealed() error {
	// Remove sectors_sdr_pipeline entries where:
	// after_commit_msg_success is true
	// after_move_storage is true
	// Related sector entry is present in sectors_meta
	// Related pieces are present in sectors_meta_pieces
	//
	// note: pipeline entries are copied to sectors_meta table when sending the commit message. The pipeline entry
	// is still needed at that point for MoveStorage and for the message poller. This GC is needed to clean up the
	// pipeline entries after the message has landed and the storage is moved.
	ctx := context.Background()

	// Execute the query
	_, err := s.db.Exec(ctx, `WITH unmatched_pieces AS (
									SELECT
										sip.sp_id,
										sip.sector_number
									FROM
										sectors_meta_pieces smp
											FULL JOIN sectors_sdr_initial_pieces sip
													  ON smp.sp_id = sip.sp_id
														  AND smp.sector_num = sip.sector_number
														  AND smp.piece_num = sip.piece_index
									WHERE
										smp.sp_id IS NULL AND sip.sp_id IS NOT NULL
								)
								DELETE FROM sectors_sdr_pipeline
								WHERE after_commit_msg_success = true
								  AND after_move_storage = true
								  AND EXISTS (
									SELECT 1
									FROM sectors_meta
									WHERE sectors_meta.sp_id = sectors_sdr_pipeline.sp_id
									  AND sectors_meta.sector_num = sectors_sdr_pipeline.sector_number
								)
								  AND NOT EXISTS (
									SELECT 1
									FROM unmatched_pieces up
									WHERE up.sp_id = sectors_sdr_pipeline.sp_id
									  AND up.sector_number = sectors_sdr_pipeline.sector_number
								);
`)
	if err != nil {
		return xerrors.Errorf("failed to clean up sealed entries: %w", err)
	}

	return nil
}

var _ harmonytask.TaskInterface = &SDRPipelineGC{}
