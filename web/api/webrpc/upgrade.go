package webrpc

import (
	"context"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/tasks/snap"
)

type UpgradeSector struct {
	SpID      uint64 `db:"sp_id"`
	SectorNum uint64 `db:"sector_number"`

	TaskIDEncode *uint64 `db:"task_id_encode"`
	AfterEncode  bool    `db:"after_encode"`

	TaskIDProve *uint64 `db:"task_id_prove"`
	AfterProve  bool    `db:"after_prove"`

	TaskIDSubmit *uint64 `db:"task_id_submit"`
	AfterSubmit  bool    `db:"after_submit"`

	AfterProveSuccess bool `db:"after_prove_msg_success"`

	TaskIDMoveStorage *uint64 `db:"task_id_move_storage"`
	AfterMoveStorage  bool    `db:"after_move_storage"`

	Failed       bool   `db:"failed"`
	FailedReason string `db:"failed_reason"`
	FailedMsg    string `db:"failed_reason_msg"`
}

func (a *WebRPC) UpgradeSectors(ctx context.Context) ([]UpgradeSector, error) {
	sectors := []UpgradeSector{}
	err := a.deps.DB.Select(ctx, &sectors, `SELECT sp_id, sector_number, task_id_encode, after_encode, task_id_prove, after_prove, task_id_submit, after_submit, after_prove_msg_success, task_id_move_storage, after_move_storage, failed, failed_reason, failed_reason_msg FROM sectors_snap_pipeline`)
	if err != nil {
		return nil, err
	}
	return sectors, nil
}

func (a *WebRPC) UpgradeResetTaskIDs(ctx context.Context, spid, sectorNum uint64) error {
	_, err := a.deps.DB.Exec(ctx, `SELECT unset_task_id_snap($1, $2)`, spid, sectorNum)
	return err
}

func (a *WebRPC) UpgradeDelete(ctx context.Context, spid, sectorNum uint64) error {
	if err := snap.DropSectorPieceRefsSnap(ctx, a.deps.DB, abi.SectorID{Miner: abi.ActorID(spid), Number: abi.SectorNumber(sectorNum)}); err != nil {
		// bad, but still do best we can and continue
		log.Errorw("failed to drop sector piece refs", "error", err)
	}

	_, err := a.deps.DB.Exec(ctx, `DELETE FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, sectorNum)
	return err
}
