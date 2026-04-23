package denylist

import (
	"context"
	"fmt"

	blockstore "github.com/ipfs/boxo/blockstore"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
)

// ErrBlockDenied is returned when a block is on the denylist.
var ErrBlockDenied = fmt.Errorf("block denied by denylist")

// FilteredBlockstore wraps a blockstore and checks every CID access
// against the denylist. If a CID is denylisted, Get/Has/GetSize return
// an error as if the block does not exist.
type FilteredBlockstore struct {
	inner  blockstore.Blockstore
	filter *Filter
}

// NewFilteredBlockstore wraps an existing blockstore with denylist filtering.
// Every Get, Has, and GetSize call will check the CID against the denylist.
func NewFilteredBlockstore(inner blockstore.Blockstore, f *Filter) *FilteredBlockstore {
	return &FilteredBlockstore{
		inner:  inner,
		filter: f,
	}
}

func (fb *FilteredBlockstore) checkCID(c cid.Cid) error {
	denied, ready := fb.filter.IsDenied(c)
	if !ready {
		return fmt.Errorf("denylist not yet loaded")
	}
	if denied {
		return ErrBlockDenied
	}
	return nil
}

func (fb *FilteredBlockstore) Get(ctx context.Context, c cid.Cid) (blocks.Block, error) {
	if err := fb.checkCID(c); err != nil {
		return nil, err
	}
	return fb.inner.Get(ctx, c)
}

func (fb *FilteredBlockstore) Has(ctx context.Context, c cid.Cid) (bool, error) {
	if err := fb.checkCID(c); err != nil {
		return false, err
	}
	return fb.inner.Has(ctx, c)
}

func (fb *FilteredBlockstore) GetSize(ctx context.Context, c cid.Cid) (int, error) {
	if err := fb.checkCID(c); err != nil {
		return 0, err
	}
	return fb.inner.GetSize(ctx, c)
}

// Pass-through methods for the full Blockstore interface

func (fb *FilteredBlockstore) DeleteBlock(ctx context.Context, c cid.Cid) error {
	return fb.inner.DeleteBlock(ctx, c)
}

func (fb *FilteredBlockstore) Put(ctx context.Context, block blocks.Block) error {
	return fb.inner.Put(ctx, block)
}

func (fb *FilteredBlockstore) PutMany(ctx context.Context, blocks []blocks.Block) error {
	return fb.inner.PutMany(ctx, blocks)
}

func (fb *FilteredBlockstore) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {
	return fb.inner.AllKeysChan(ctx)
}

var _ blockstore.Blockstore = (*FilteredBlockstore)(nil)
