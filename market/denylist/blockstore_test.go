package denylist

import (
	"context"
	"errors"
	"testing"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"

	"github.com/filecoin-project/lotus/blockstore"
)

func makeBlock(data string) blocks.Block {
	hash, _ := mh.Sum([]byte(data), mh.SHA2_256, -1)
	c := cid.NewCidV1(cid.Raw, hash)
	b, _ := blocks.NewBlockWithCid([]byte(data), c)
	return b
}

func TestFilteredBlockstore_AllowedBlock(t *testing.T) {
	inner := blockstore.NewMemory()
	b := makeBlock("allowed data")
	_ = inner.Put(context.Background(), b)

	f := &Filter{}
	m := map[string]struct{}{} // empty denylist
	f.hashes.Store(&m)

	fbs := NewFilteredBlockstore(inner, f)

	// Get should succeed
	got, err := fbs.Get(context.Background(), b.Cid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Cid() != b.Cid() {
		t.Fatalf("expected CID %s, got %s", b.Cid(), got.Cid())
	}

	// Has should succeed
	has, err := fbs.Has(context.Background(), b.Cid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected Has to return true")
	}

	// GetSize should succeed
	size, err := fbs.GetSize(context.Background(), b.Cid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != len(b.RawData()) {
		t.Fatalf("expected size %d, got %d", len(b.RawData()), size)
	}
}

func TestFilteredBlockstore_DeniedBlock(t *testing.T) {
	inner := blockstore.NewMemory()
	b := makeBlock("blocked data")
	_ = inner.Put(context.Background(), b)

	h := CIDToHash(b.Cid())
	f := &Filter{}
	m := map[string]struct{}{h: {}}
	f.hashes.Store(&m)

	fbs := NewFilteredBlockstore(inner, f)

	// Get should fail with ErrBlockDenied
	_, err := fbs.Get(context.Background(), b.Cid())
	if !errors.Is(err, ErrBlockDenied) {
		t.Fatalf("expected ErrBlockDenied, got: %v", err)
	}

	// Has should fail with ErrBlockDenied
	_, err = fbs.Has(context.Background(), b.Cid())
	if !errors.Is(err, ErrBlockDenied) {
		t.Fatalf("expected ErrBlockDenied, got: %v", err)
	}

	// GetSize should fail with ErrBlockDenied
	_, err = fbs.GetSize(context.Background(), b.Cid())
	if !errors.Is(err, ErrBlockDenied) {
		t.Fatalf("expected ErrBlockDenied, got: %v", err)
	}
}

func TestFilteredBlockstore_NotReady(t *testing.T) {
	inner := blockstore.NewMemory()
	b := makeBlock("some data")
	_ = inner.Put(context.Background(), b)

	f := &Filter{} // not loaded yet

	fbs := NewFilteredBlockstore(inner, f)

	// Get should fail with not-ready error
	_, err := fbs.Get(context.Background(), b.Cid())
	if err == nil {
		t.Fatal("expected error when denylist not ready")
	}

	// Has should fail with not-ready error
	_, err = fbs.Has(context.Background(), b.Cid())
	if err == nil {
		t.Fatal("expected error when denylist not ready")
	}

	// GetSize should fail with not-ready error
	_, err = fbs.GetSize(context.Background(), b.Cid())
	if err == nil {
		t.Fatal("expected error when denylist not ready")
	}
}

func TestFilteredBlockstore_PassThroughMethods(t *testing.T) {
	inner := blockstore.NewMemory()
	b := makeBlock("pass-through data")

	f := &Filter{}
	m := map[string]struct{}{} // empty denylist
	f.hashes.Store(&m)

	fbs := NewFilteredBlockstore(inner, f)

	// Put should pass through
	err := fbs.Put(context.Background(), b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the block was written to the inner store
	got, err := inner.Get(context.Background(), b.Cid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Cid() != b.Cid() {
		t.Fatalf("expected CID %s, got %s", b.Cid(), got.Cid())
	}

	// DeleteBlock should pass through
	err = fbs.DeleteBlock(context.Background(), b.Cid())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	has, _ := inner.Has(context.Background(), b.Cid())
	if has {
		t.Fatal("expected block to be deleted from inner store")
	}
}
