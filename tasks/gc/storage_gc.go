package gc

import (
	"context"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/lotus/storage/paths"
	"golang.org/x/xerrors"
	"time"
)

const StorageGCInterval = 11 * time.Minute

type StorageGC struct {
	si     paths.SectorIndex
	remote *paths.Remote
	db     *harmonydb.DB
}

func NewStorageGC(si paths.SectorIndex, remote *paths.Remote, db *harmonydb.DB) *StorageGC {
	return &StorageGC{
		si:     si,
		remote: remote,
		db:     db,
	}
}

func (s *StorageGC) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	/*
		CREATE TABLE storage_removal_marks (
		    sp_id BIGINT NOT NULL,
		    sector_num BIGINT NOT NULL,
		    sector_filetype TEXT NOT NULL,
		    storage_id TEXT NOT NULL,

		    was_onchain BOOLEAN NOT NULL,
		    was_seal_failed BOOLEAN NOT NULL,
		    was_owned_sp_id BOOLEAN NOT NULL,

		    primary key (sp_id, sector_num, sector_filetype, storage_id)
		);

		CREATE TABLE storage_gc_pins (
		    sp_id BIGINT NOT NULL,
		    sector_num BIGINT NOT NULL,
		    sector_filetype TEXT, -- null = all file types
		    storage_id TEXT, -- null = all storage ids

		    primary key (sp_id, sector_num, sector_filetype, storage_id)
		);
	*/

	// First get a list of all the sectors in all the paths
	storageSectors, err := s.si.StorageList(ctx)
	if err != nil {
		return false, xerrors.Errorf("StorageList: %w", err)
	}

	_, err = s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Then get a list of all the sectors that are pinned
		var pinnedSectors []struct {
			SpID           int64  `db:"sp_id"`
			SectorNum      int64  `db:"sector_num"`
			SectorFiletype string `db:"sector_filetype"`
			StorageID      string `db:"storage_id"`
		}

		err = tx.Select(&pinnedSectors, `SELECT sp_id, sector_num, sector_filetype, storage_id FROM storage_gc_pins`)
		if err != nil {
			return false, xerrors.Errorf("select gc pins: %w", err)
		}

		// Now we need to figure out what can be removed. We need:
		// - A list of sectors that are pinned
		// - A list of all sectors in the sealing pipeline
		// - A list of all sectors not-terminated on-chain
		//   - Precommits
		//   - Sectors in the sectors list
	})
	if err != nil {
		return false, xerrors.Errorf("BeginTransaction: %w", err)
	}

	return true, nil
}

func (s *StorageGC) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *StorageGC) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  1,
		Name: "StorageGC",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(StorageEndpointGCInterval, s),
	}
}

func (s *StorageGC) Adder(taskFunc harmonytask.AddTaskFunc) {
	return
}

var _ harmonytask.TaskInterface = &StorageGC{}
