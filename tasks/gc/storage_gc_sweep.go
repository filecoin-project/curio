package gc

import (
	"context"
	"time"

	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/paths"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type StorageGCSweep struct {
	db      *harmonydb.DB
	storage *paths.Remote
	index   paths.SectorIndex
}

func NewStorageGCSweep(db *harmonydb.DB, storage *paths.Remote, index paths.SectorIndex) *StorageGCSweep {
	return &StorageGCSweep{
		db:      db,
		storage: storage,
		index:   index,
	}
}

func (s *StorageGCSweep) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	for {
		// go file by file
		if !stillOwned() {
			return false, nil
		}

		// get the next file
		var marks []struct {
			Actor     int64  `db:"sp_id"`
			SectorNum int64  `db:"sector_num"`
			FileType  int64  `db:"sector_filetype"`
			StorageID string `db:"storage_id"`

			CreatedAt  time.Time  `db:"created_at"`
			ApprovedAt *time.Time `db:"approved_at"`
		}

		err := s.db.Select(ctx, &marks, `SELECT sp_id, sector_num, sector_filetype, storage_id, created_at, approved_at FROM storage_removal_marks WHERE approved = true ORDER BY created_at LIMIT 1`)
		if err != nil {
			return false, xerrors.Errorf("failed to get next file: %w", err)
		}
		if len(marks) == 0 {
			return true, nil
		}

		mark := marks[0]
		if mark.ApprovedAt == nil {
			return false, xerrors.Errorf("approved file approved_at was nil")
		}

		// get ID
		sid := abi.SectorID{
			Miner:  abi.ActorID(mark.Actor),
			Number: abi.SectorNumber(mark.SectorNum),
		}
		typ := storiface.SectorFileType(mark.FileType)

		// compute the set of paths where we want to keep the sector
		si, err := s.index.StorageFindSector(ctx, sid, typ, 0, false)
		if err != nil {
			return false, xerrors.Errorf("finding existing sector %d(t:%d) failed: %w", sid, typ, err)
		}

		keepIn := lo.Map(si, func(item storiface.SectorStorageInfo, index int) storiface.ID {
			return item.ID
		})
		keepIn = lo.Filter(keepIn, func(item storiface.ID, index int) bool {
			return item != storiface.ID(mark.StorageID)
		})

		// Log in detail what we are doing
		log.Infow("removing sector", "id", sid, "type", typ, "keepIn", keepIn, "approvedAt", mark.ApprovedAt, "marked", mark.CreatedAt, "task", taskID)

		// Delete the mark entry
		// We want to delete the mark first because even if we fail here, the mark will be re-added on the next run of gc-mark task

		_, err = s.db.Exec(ctx, `DELETE FROM storage_removal_marks WHERE sp_id = $1 AND sector_num = $2 AND sector_filetype = $3 AND storage_id = $4`, mark.Actor, mark.SectorNum, mark.FileType, mark.StorageID)
		if err != nil {
			return false, xerrors.Errorf("gc removing mark for sector %d(t:%d) failed: %w", sid, typ, err)
		}

		// Remove the file!
		err = s.storage.Remove(ctx, sid, typ, true, keepIn)
		if err != nil {
			return false, xerrors.Errorf("gc removing sector %d(t:%d) failed: %w", sid, typ, err)
		}
	}
}

func (s *StorageGCSweep) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *StorageGCSweep) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  1,
		Name: "StorageGCSweep",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(StorageGCInterval, s),
	}
}

func (s *StorageGCSweep) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ harmonytask.TaskInterface = &StorageGCSweep{}
