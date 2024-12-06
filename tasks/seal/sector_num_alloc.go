package seal

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	rlepluslazy "github.com/filecoin-project/go-bitfield/rle"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/chain/types"
)

type AllocAPI interface {
	StateMinerAllocated(context.Context, address.Address, types.TipSetKey) (*bitfield.BitField, error)
}

func AllocateSectorNumbers(ctx context.Context, a AllocAPI, tx *harmonydb.Tx, maddr address.Address, count int) ([]abi.SectorNumber, error) {
	chainAlloc, err := a.StateMinerAllocated(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting on-chain allocated sector numbers: %w", err)
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner id: %w", err)
	}

	var res []abi.SectorNumber

	// query from db, if exists unmarsal to bitfield
	var dbAllocated bitfield.BitField
	var rawJson []byte

	err = tx.QueryRow("SELECT COALESCE(allocated, '[0]') from sectors_allocated_numbers sa FULL OUTER JOIN (SELECT 1) AS d ON FALSE WHERE sp_id = $1 OR sp_id IS NULL ORDER BY sp_id LIMIT 1", mid).Scan(&rawJson)
	if err != nil {
		return res, xerrors.Errorf("querying allocated sector numbers: %w", err)
	}

	if rawJson != nil {
		err = dbAllocated.UnmarshalJSON(rawJson)
		if err != nil {
			return res, xerrors.Errorf("unmarshaling allocated sector numbers: %w", err)
		}
	}

	if err := dbAllocated.UnmarshalJSON(rawJson); err != nil {
		return res, xerrors.Errorf("unmarshaling allocated sector numbers: %w", err)
	}

	merged, err := bitfield.MergeBitFields(*chainAlloc, dbAllocated)
	if err != nil {
		return res, xerrors.Errorf("merging allocated sector numbers: %w", err)
	}

	allAssignable, err := bitfield.NewFromIter(&rlepluslazy.RunSliceIterator{Runs: []rlepluslazy.Run{
		{
			Val: true,
			Len: abi.MaxSectorNumber,
		},
	}})
	if err != nil {
		return res, xerrors.Errorf("creating assignable sector numbers: %w", err)
	}

	inverted, err := bitfield.SubtractBitField(allAssignable, merged)
	if err != nil {
		return res, xerrors.Errorf("subtracting allocated sector numbers: %w", err)
	}

	toAlloc, err := inverted.Slice(0, uint64(count))
	if err != nil {
		return res, xerrors.Errorf("getting slice of allocated sector numbers: %w", err)
	}

	err = toAlloc.ForEach(func(u uint64) error {
		res = append(res, abi.SectorNumber(u))
		return nil
	})
	if err != nil {
		return res, xerrors.Errorf("iterating allocated sector numbers: %w", err)
	}

	toPersist, err := bitfield.MergeBitFields(merged, toAlloc)
	if err != nil {
		return res, xerrors.Errorf("merging allocated sector numbers: %w", err)
	}

	rawJson, err = toPersist.MarshalJSON()
	if err != nil {
		return res, xerrors.Errorf("marshaling allocated sector numbers: %w", err)
	}

	_, err = tx.Exec("INSERT INTO sectors_allocated_numbers(sp_id, allocated) VALUES($1, $2) ON CONFLICT(sp_id) DO UPDATE SET allocated = $2", mid, rawJson)
	if err != nil {
		return res, xerrors.Errorf("persisting allocated sector numbers: %w", err)
	}

	return res, nil
}
