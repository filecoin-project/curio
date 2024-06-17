package webrpc

import "context"

type UpgradeSector struct {
	SpID      uint64 `db:"sp_id"`
	SectorNum uint64 `db:"sector_number"`

	TaskIDEncode *uint64 `db:"task_id_encode"`
	AfterEncode  bool    `db:"after_encode"`

	TaskIDProve *uint64 `db:"task_id_prove"`
	AfterProve  bool    `db:"after_prove"`

	TaskIDSubmit *uint64 `db:"task_id_submit"`
	AfterSubmit  bool    `db:"after_submit"`

	TaskIDMoveStorage *uint64 `db:"task_id_move_storage"`
	AfterMoveStorage  bool    `db:"after_move_storage"`
}

func (a *WebRPC) UpgradeSectors(ctx context.Context) ([]UpgradeSector, error) {
	var sectors []UpgradeSector
	//err := a.deps.DB.Select(ctx, &sectors, `SELECT sp_id, sector_number FROM sectors_snap_pipeline`)
	err := a.deps.DB.Select(ctx, &sectors, `SELECT sp_id, sector_number, task_id_encode, after_encode, task_id_prove, after_prove, task_id_submit, after_submit, task_id_move_storage, after_move_storage FROM sectors_snap_pipeline`)
	if err != nil {
		return nil, err
	}
	return sectors, nil
}
