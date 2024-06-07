package gc

import (
	"context"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/paths"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
	"time"
)

const StorageGCInterval = 11 * time.Minute

type StorageGCMarkNodeAPI interface {
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
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
							_, err := tx.Exec(`INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id)
							VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, decl.Miner, decl.Number, filetype, storageId)
							if err != nil {
								return false, xerrors.Errorf("insert storage_removal_marks: %w", err)
							}
						}
					}

				}
			}
		}

		return len(toRemove) > 0, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("BeginTransaction: %w", err)
	}

	return true, nil
}

func (s *StorageGCMark) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *StorageGCMark) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  1,
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
	return
}

var _ harmonytask.TaskInterface = &StorageGCMark{}
