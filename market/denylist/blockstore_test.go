package denylist

import (
	"context"
	"errors"
	"testing"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
)

// memBlockstore is a trivial in-memory blockstore for testing.
type memBlockstore struct {
	data map[cid.Cid]blocks.Block
}

func newMemBlockstore() *memBlockstore {
	return &memBlockstore{data: make(map[cid.Cid]blocks.Block)}
}

func (m *memBlockstore) Has(_ context.Context, c cid.Cid) (bool, error) {
	_, ok := m.data[c]
	return ok, nil
}

func (m *memBlockstore) Get(_ context.Context, c cid.Cid) (blocks.Block, error) {
	b, ok := m.data[c]
	if !ok {
		return nil, errors.New("not found")
	}
	return b, nil
}

func (m *memBlockstore) GetSize(_ context.Context, c cid.Cid) (int, error) {
	b, ok := m.data[c]
	if !ok {
		return 0, errors.New("not found")
	}
	return len(b.RawData()), nil
}

func (m *memBlockstore) Put(_ context.Context, b blocks.Block) error {
	m.data[b.Cid()] = b
	return nil
}

func (m *memBlockstore) PutMany(_ context.Context, bs []blocks.Block) error {
	for _, b := range bs {
		m.data[b.Cid()] = b
	}
	return nil
}

func (m *memBlockstore) DeleteBlock(_ context.Context, c cid.Cid) error {
	delete(m.data, c)
	return nil
}

func (m *memBlockstore) AllKeysChan(_ context.Context) (<-chan cid.Cid, error) {
	ch := make(chan cid.Cid)
	close(ch)
	return ch, nil
}

func makeBlock(data string) blocks.Block {
	hash, _ := mh.Sum([]byte(data), mh.SHA2_256, -1)
	c := cid.NewCidV1(cid.Raw, hash)
	b, _ := blocks.NewBlockWithCid([]byte(data), c)
	return b
}

func TestFilteredBlockstore_AllowedBlock(t *testing.T) {
	inner := newMemBlockstore()
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
	inner := newMemBlockstore()
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
	inner := newMemBlockstore()
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
	inner := newMemBlockstore()
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
