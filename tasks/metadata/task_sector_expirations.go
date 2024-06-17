package metadata

import (
	"context"
	"github.com/filecoin-project/curio/lib/curiochain"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	cbor "github.com/ipfs/go-ipld-cbor"
	"golang.org/x/xerrors"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
)

const SectorMetadataRefreshInterval = 191 * time.Minute

type SectorMetadataNodeAPI interface {
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
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
	}

	if err := s.db.Select(ctx, &sectors, "select sp_id, sector_num, expiration_epoch from sectors_meta ORDER BY sp_id, sector_num"); err != nil {
		return false, xerrors.Errorf("get sector list: %w", err)
	}

	astor := adt.WrapStore(ctx, cbor.NewCborStore(s.bstore))
	minerStates := map[abi.ActorID]miner.State{}

	for _, sector := range sectors {
		mstate, ok := minerStates[abi.ActorID(sector.SpID)]
		if !ok {
			maddr, err := address.NewIDAddress(sector.SpID)
			if err != nil {
				return false, xerrors.Errorf("creating miner address: %w", err)
			}

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
	}

	return true, nil
}

func (s *SectorMetadata) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SectorMetadata) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  1,
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

var _ harmonytask.TaskInterface = &SectorMetadata{}
