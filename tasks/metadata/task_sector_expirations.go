package metadata

import (
	"context"
	"time"

	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("metadata")

const SectorMetadataRefreshInterval = 191 * time.Minute

type SectorMetadataNodeAPI interface {
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	StateSectorPartition(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tok types.TipSetKey) (*miner.SectorLocation, error)
}

type SectorMetadata struct {
	db     *harmonydb.DB
	bstore curiochain.CurioBlockstore

	api SectorMetadataNodeAPI
}

func NewSectorMetadataTask(db *harmonydb.DB, bstore curiochain.CurioBlockstore, api SectorMetadataNodeAPI) *SectorMetadata {
	return &SectorMetadata{
		db:     db,
		bstore: bstore,
		api:    api,
	}
}

func (s *SectorMetadata) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var sectors []struct {
		SpID      uint64 `db:"sp_id"`
		SectorNum uint64 `db:"sector_num"`

		Expiration *uint64 `db:"expiration_epoch"`

		Partition *uint64 `db:"partition"`
		Deadline  *uint64 `db:"deadline"`
	}

	if err := s.db.Select(ctx, &sectors, "select sp_id, sector_num, expiration_epoch, partition, deadline from sectors_meta ORDER BY sp_id, sector_num"); err != nil {
		return false, xerrors.Errorf("get sector list: %w", err)
	}

	astor := adt.WrapStore(ctx, cbor.NewCborStore(s.bstore))
	minerStates := map[abi.ActorID]miner.State{}

	type partitionUpdate struct {
		SpID      uint64
		SectorNum uint64
		Partition uint64
		Deadline  uint64
	}

	const batchSize = 1000
	updateBatch := make([]partitionUpdate, 0, batchSize)
	total := 0

	flushBatch := func() error {
		if len(updateBatch) == 0 {
			return nil
		}

		total += len(updateBatch)
		log.Infow("updating sector partitions", "total", total)

		_, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			batch := &pgx.Batch{}
			for _, update := range updateBatch {
				batch.Queue("UPDATE sectors_meta SET partition = $1, deadline = $2 WHERE sp_id = $3 AND sector_num = $4",
					update.Partition, update.Deadline, update.SpID, update.SectorNum)
			}

			br := tx.SendBatch(ctx, batch)
			defer br.Close()

			for i := 0; i < batch.Len(); i++ {
				_, err := br.Exec()
				if err != nil {
					return false, xerrors.Errorf("executing batch update %d: %w", i, err)
				}
			}

			return true, nil
		}, harmonydb.OptionRetry())
		return err
	}

	for _, sector := range sectors {
		maddr, err := address.NewIDAddress(sector.SpID)
		if err != nil {
			return false, xerrors.Errorf("creating miner address: %w", err)
		}

		mstate, ok := minerStates[abi.ActorID(sector.SpID)]
		if !ok {
			act, err := s.api.StateGetActor(ctx, maddr, types.EmptyTSK)
			if err != nil {
				return false, xerrors.Errorf("getting miner actor: %w", err)
			}

			mstate, err = miner.Load(astor, act)
			if err != nil {
				return false, xerrors.Errorf("loading miner state: %w", err)
			}

			minerStates[abi.ActorID(sector.SpID)] = mstate
		}

		si, err := mstate.GetSector(abi.SectorNumber(sector.SectorNum))
		if err != nil {
			return false, xerrors.Errorf("getting sector info: %w", err)
		}
		if si == nil {
			continue
		}

		if sector.Expiration == nil || si.Expiration != abi.ChainEpoch(*sector.Expiration) {
			_, err := s.db.Exec(ctx, "update sectors_meta set expiration_epoch = $1 where sp_id = $2 and sector_num = $3", si.Expiration, sector.SpID, sector.SectorNum)
			if err != nil {
				return false, xerrors.Errorf("updating sector expiration: %w", err)
			}
		}

		if sector.Partition == nil || sector.Deadline == nil {
			loc, err := s.api.StateSectorPartition(ctx, maddr, abi.SectorNumber(sector.SectorNum), types.EmptyTSK)
			if err != nil {
				return false, xerrors.Errorf("getting sector partition: %w", err)
			}

			if loc != nil {
				updateBatch = append(updateBatch, partitionUpdate{
					SpID:      sector.SpID,
					SectorNum: sector.SectorNum,
					Partition: loc.Partition,
					Deadline:  loc.Deadline,
				})

				if len(updateBatch) >= batchSize {
					if err := flushBatch(); err != nil {
						return false, xerrors.Errorf("flushing batch: %w", err)
					}
					updateBatch = updateBatch[:0]
				}
			}
		}
	}

	// Flush any remaining updates
	if err := flushBatch(); err != nil {
		return false, xerrors.Errorf("flushing final batch: %w", err)
	}

	return true, nil
}

func (s *SectorMetadata) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SectorMetadata) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  harmonytask.Max(1),
		Name: "SectorMetadata",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(SectorMetadataRefreshInterval, s),
	}
}

func (s *SectorMetadata) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ = harmonytask.Reg(&SectorMetadata{})
var _ harmonytask.TaskInterface = &SectorMetadata{}
