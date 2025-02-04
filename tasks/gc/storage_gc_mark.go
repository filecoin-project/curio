package gc

import (
	"context"
	"time"

	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

const StorageGCInterval = 9 * time.Minute

type StorageGCMarkNodeAPI interface {
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	ChainHead(ctx context.Context) (*types.TipSet, error)
	ChainGetTipSetByHeight(ctx context.Context, height abi.ChainEpoch, tsk types.TipSetKey) (*types.TipSet, error)
}

type StorageGCMark struct {
	si     paths.SectorIndex
	remote *paths.Remote
	db     *harmonydb.DB
	bstore curiochain.CurioBlockstore
	api    StorageGCMarkNodeAPI
}

func NewStorageGCMark(si paths.SectorIndex, remote *paths.Remote, db *harmonydb.DB, bstore curiochain.CurioBlockstore, api StorageGCMarkNodeAPI) *StorageGCMark {
	return &StorageGCMark{
		si:     si,
		remote: remote,
		db:     db,
		bstore: bstore,
		api:    api,
	}
}

func (s *StorageGCMark) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	/*
		CREATE TABLE storage_removal_marks (
		    sp_id BIGINT NOT NULL,
		    sector_num BIGINT NOT NULL,
		    sector_filetype TEXT NOT NULL,
		    storage_id TEXT NOT NULL,

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

	// ToRemove += InStorage - Precommits - Live - Unproven - Pinned - InPorepPipeline
	toRemove := map[abi.ActorID]*bitfield.BitField{}
	minerStates := map[abi.ActorID]miner.State{}

	astor := adt.WrapStore(ctx, cbor.NewCborStore(s.bstore))

	for _, decls := range storageSectors {
		for _, decl := range decls {
			if decl.SectorFileType == storiface.FTPiece {
				continue
			}

			if toRemove[decl.Miner] == nil {
				bf := bitfield.New()
				toRemove[decl.Miner] = &bf

				maddr, err := address.NewIDAddress(uint64(decl.Miner))
				if err != nil {
					return false, xerrors.Errorf("NewIDAddress: %w", err)
				}

				mact, err := s.api.StateGetActor(ctx, maddr, types.EmptyTSK)
				if err != nil {
					return false, xerrors.Errorf("get miner actor %s: %w", maddr, err)
				}

				mas, err := miner.Load(astor, mact)
				if err != nil {
					return false, xerrors.Errorf("load miner actor state %s: %w", maddr, err)
				}

				minerStates[decl.Miner] = mas
			}

			toRemove[decl.Miner].Set(uint64(decl.Number))
		}
	}

	_, err = s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Now we need to figure out what can be removed. We need:
		// - A list of sectors that are pinned
		// - A list of all sectors in the sealing pipeline
		// - A list of all sectors not-terminated on-chain
		//   - Precommits
		//   - Live + Unproven

		if len(toRemove) > 0 { // pins
			var pinnedSectors []struct {
				SpID      int64 `db:"sp_id"`
				SectorNum int64 `db:"sector_num"`
			}

			err = tx.Select(&pinnedSectors, `SELECT sp_id, sector_num FROM storage_gc_pins`)
			if err != nil {
				return false, xerrors.Errorf("select gc pins: %w", err)
			}

			for _, sector := range pinnedSectors {
				if toRemove[abi.ActorID(sector.SpID)] == nil {
					continue
				}

				toRemove[abi.ActorID(sector.SpID)].Unset(uint64(sector.SectorNum))
			}
		}

		if len(toRemove) > 0 { // sealing pipeline
			var pipelineSectors []struct {
				SpID      int64 `db:"sp_id"`
				SectorNum int64 `db:"sector_number"`
			}

			err = tx.Select(&pipelineSectors, `SELECT sp_id, sector_number FROM sectors_sdr_pipeline`)
			if err != nil {
				return false, xerrors.Errorf("select sd pipeline: %w", err)
			}

			for _, sector := range pipelineSectors {
				if toRemove[abi.ActorID(sector.SpID)] == nil {
					continue
				}

				toRemove[abi.ActorID(sector.SpID)].Unset(uint64(sector.SectorNum))
			}
		}

		if len(toRemove) > 0 { // precommits
			for mid, state := range minerStates {
				err := state.ForEachPrecommittedSector(func(info miner.SectorPreCommitOnChainInfo) error {
					toRemove[mid].Unset(uint64(info.Info.SectorNumber))
					return nil
				})
				if err != nil {
					return false, xerrors.Errorf("iterating precommits for miner %d: %w", mid, err)
				}
			}
		}

		if len(toRemove) > 0 { // live + unproven
			for mid, state := range minerStates {
				err := state.ForEachDeadline(func(idx uint64, dl miner.Deadline) error {
					return dl.ForEachPartition(func(idx uint64, part miner.Partition) error {
						live, err := part.LiveSectors()
						if err != nil {
							return xerrors.Errorf("getting live sectors: %w", err)
						}

						unproven, err := part.UnprovenSectors()
						if err != nil {
							return xerrors.Errorf("getting unproven sectors: %w", err)
						}

						toRm, err := bitfield.SubtractBitField(*toRemove[mid], live)
						if err != nil {
							return xerrors.Errorf("subtracting live: %w", err)
						}

						toRm, err = bitfield.SubtractBitField(toRm, unproven)
						if err != nil {
							return xerrors.Errorf("subtracting unproven: %w", err)
						}

						toRemove[mid] = &toRm
						return nil
					})
				})
				if err != nil {
					return false, xerrors.Errorf("iterating deadlines for miner %d: %w", mid, err)
				}
			}
		}

		if len(toRemove) > 0 { // persist new removal candidates
			for storageId, decls := range storageSectors {
				for _, decl := range decls {
					for _, filetype := range decl.SectorFileType.AllSet() {
						if filetype == storiface.FTPiece {
							continue
						}

						if toRemove[decl.Miner] == nil {
							continue
						}

						set, err := toRemove[decl.Miner].IsSet(uint64(decl.Number))
						if err != nil {
							return false, xerrors.Errorf("checking if sector is set: %w", err)
						}
						if set {
							n, err := tx.Exec(`INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id)
							VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, decl.Miner, decl.Number, filetype, storageId)
							if err != nil {
								return false, xerrors.Errorf("insert storage_removal_marks: %w", err)
							}
							if n > 0 {
								log.Infow("file marked for GC", "miner", decl.Miner, "sector", decl.Number, "filetype", filetype, "storage_id", storageId, "reason", "not-in-use")
							}
						}
					}

				}
			}
		}

		return len(toRemove) > 0, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("stage 1 mark transaction: %w", err)
	}

	/*
		STAGE 2: Mark unsealed sectors which we don't want for removal
	*/
	_, err = s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		/*
				SELECT m.sector_num, m.sp_id, sl.storage_id FROM sectors_meta m
			         INNER JOIN sector_location sl ON m.sp_id = sl.miner_id AND m.sector_num = sl.sector_num
			         WHERE m.target_unseal_state = false AND sl.sector_filetype= 1
		*/

		var unsealedSectors []struct {
			SpID      int64  `db:"sp_id"`
			SectorNum int64  `db:"sector_num"`
			StorageID string `db:"storage_id"`
		}

		err = tx.Select(&unsealedSectors, `SELECT m.sector_num, m.sp_id, sl.storage_id FROM sectors_meta m
			INNER JOIN sector_location sl ON m.sp_id = sl.miner_id AND m.sector_num = sl.sector_num
			LEFT JOIN sectors_unseal_pipeline sup ON m.sp_id = sup.sp_id AND m.sector_num = sup.sector_number
			WHERE m.target_unseal_state = false AND sl.sector_filetype= 1 AND sup.sector_number IS NULL`) // FTUnsealed = 1
		if err != nil {
			return false, xerrors.Errorf("select unsealed sectors: %w", err)
		}

		for _, sector := range unsealedSectors {
			n, err := tx.Exec(`INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id)
				VALUES ($1, $2, 1, $3) ON CONFLICT DO NOTHING`, sector.SpID, sector.SectorNum, sector.StorageID)
			if err != nil {
				return false, xerrors.Errorf("insert storage_removal_marks: %w", err)
			}
			if n > 0 {
				log.Infow("file marked for GC", "miner", sector.SpID, "sector", sector.SectorNum, "filetype", 1, "storage_id", sector.StorageID, "reason", "unseal-target-state")
			}
		}

		return len(unsealedSectors) > 0, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("unseal stage transaction: %w", err)
	}

	/*
		STAGE 3: Mark "sealed" files which are sector-keys in snap sectors
	*/

	// get a tipset 1.5 finality-ago; we only want to take sectors which were snapped for a while
	head, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("get chain head: %w", err)
	}

	finalityHeight := head.Height() - (900 + 450)

	finalityTipset, err := s.api.ChainGetTipSetByHeight(ctx, finalityHeight, head.Key())
	if err != nil {
		return false, xerrors.Errorf("get finality tipset: %w", err)
	}

	var minerIDs []int64
	if err = s.db.Select(ctx, &minerIDs, `SELECT DISTINCT sp_id FROM sectors_meta WHERE orig_unsealed_cid != cur_sealed_cid`); err != nil {
		return false, xerrors.Errorf("distinct miners from snap sectors: %w", err)
	}

	finalityMinerStates := make(map[abi.ActorID]miner.State)
	for _, mID := range minerIDs {
		maddr, err := address.NewIDAddress(uint64(mID))
		if err != nil {
			return false, xerrors.Errorf("creating miner address for %d: %w", mID, err)
		}

		mact, err := s.api.StateGetActor(ctx, maddr, finalityTipset.Key())
		if err != nil {
			return false, xerrors.Errorf("get miner actor %s at finality: %w", maddr, err)
		}

		mState, err := miner.Load(astor, mact)
		if err != nil {
			return false, xerrors.Errorf("load miner actor state %s at finality: %w", maddr, err)
		}

		finalityMinerStates[abi.ActorID(mID)] = mState
	}

	// SELECT sp_id, sector_num FROM sectors_meta WHERE orig_unsealed_cid != cur_sealed_cid
	var snapSectors []struct {
		SpID      int64 `db:"sp_id"`
		SectorNum int64 `db:"sector_number"`
	}
	err = s.db.Select(ctx, &snapSectors, `SELECT sp_id, sector_num FROM sectors_meta WHERE orig_unsealed_cid != cur_sealed_cid ORDER BY sp_id, sector_num`)
	if err != nil {
		return false, xerrors.Errorf("select snap sectors: %w", err)
	}
	marks := map[abi.SectorID]struct{}{}

	for _, sector := range snapSectors {
		mstate, ok := finalityMinerStates[abi.ActorID(sector.SpID)]
		if !ok {
			continue
		}

		s, err := mstate.GetSector(abi.SectorNumber(sector.SectorNum))
		if err != nil {
			return false, xerrors.Errorf("get sector %d: %w", sector.SectorNum, err)
		}

		if s.SectorKeyCID != nil {
			marks[abi.SectorID{
				Miner:  abi.ActorID(sector.SpID),
				Number: abi.SectorNumber(sector.SectorNum),
			}] = struct{}{}
		}
	}

	_, err = s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {

		for storageId, decls := range storageSectors {
			for _, decl := range decls {
				if _, ok := marks[decl.SectorID]; !ok {
					continue
				}

				n, err := tx.Exec(`INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id)
				VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, decl.Miner, decl.Number, storiface.FTSealed, string(storageId))
				if err != nil {
					return false, xerrors.Errorf("insert storage_removal_marks: %w", err)
				}
				if n > 0 {
					log.Infow("file marked for GC", "miner", decl.Miner, "sector", decl.Number, "filetype", storiface.FTSealed, "storage_id", string(storageId), "reason", "snap-sector-key")
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, err
	}

	return true, nil
}

func (s *StorageGCMark) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *StorageGCMark) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "StorageGCMark",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(StorageGCInterval, s),
	}
}

func (s *StorageGCMark) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ harmonytask.TaskInterface = &StorageGCMark{}
var _ = harmonytask.Reg(&StorageGCMark{})
