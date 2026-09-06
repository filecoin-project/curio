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

const lockSectorStateSQL = `INSERT INTO sectors_allocated_numbers (sp_id, allocated)
	VALUES ($1, '[0]'::jsonb)
	ON CONFLICT (sp_id) DO UPDATE
	SET allocated = sectors_allocated_numbers.allocated
	RETURNING sp_id`

// LockSectorState serializes transactions which allocate sector numbers or
// mutate open-sector state for one storage provider. The self-assignment in
// the conflict branch preserves the allocation bitfield while establishing a
// transaction-scoped write-conflict point, including when the row is created
// concurrently for the first time.
func LockSectorState(ctx context.Context, tx *harmonydb.Tx, spID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var lockedSPID int64
	err := tx.QueryRow(lockSectorStateSQL, spID).Scan(&lockedSPID)
	if err != nil {
		return xerrors.Errorf("locking sector state for provider %d: %w", spID, err)
	}
	if lockedSPID != spID {
		return xerrors.Errorf("locked sector state for provider %d, expected %d", lockedSPID, spID)
	}

	return nil
}

func AllocateSectorNumbers(ctx context.Context, a AllocAPI, tx *harmonydb.Tx, maddr address.Address, count int) ([]abi.SectorNumber, error) {
	chainAlloc, err := a.StateMinerAllocated(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("getting on-chain allocated sector numbers: %w", err)
	}

	return AllocateSectorNumbersFromChain(ctx, tx, maddr, chainAlloc, count)
}

// AllocateSectorNumbersFromChain allocates sector numbers using an already
// fetched view of the provider's on-chain allocations. Callers which allocate
// for multiple providers can fetch chain state before acquiring provider
// locks, then acquire those locks in a stable order.
func AllocateSectorNumbersFromChain(ctx context.Context, tx *harmonydb.Tx, maddr address.Address, chainAlloc *bitfield.BitField, count int) ([]abi.SectorNumber, error) {
	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner id: %w", err)
	}
	spID := int64(mid)

	if err := LockSectorState(ctx, tx, spID); err != nil {
		return nil, err
	}

	var res []abi.SectorNumber

	// query from db, if exists unmarsal to bitfield
	var dbAllocated bitfield.BitField
	var rawJson []byte

	err = tx.QueryRow("SELECT allocated FROM sectors_allocated_numbers WHERE sp_id = $1", spID).Scan(&rawJson)
	if err != nil {
		return res, xerrors.Errorf("querying allocated sector numbers: %w", err)
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

	n, err := tx.Exec("UPDATE sectors_allocated_numbers SET allocated = $2 WHERE sp_id = $1", spID, rawJson)
	if err != nil {
		return res, xerrors.Errorf("persisting allocated sector numbers: %w", err)
	}
	if n != 1 {
		return res, xerrors.Errorf("persisting allocated sector numbers: updated %d rows", n)
	}

	return res, nil
}
