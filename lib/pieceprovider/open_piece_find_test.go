package pieceprovider

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/lib/hashspace"
)

type fakeHolders struct {
	locs    []hashspace.Location
	candErr error
	has     map[string]bool
	errs    map[string]error
	slow    map[string]chan struct{}

	mu      sync.Mutex
	statted []string
}

func (f *fakeHolders) Candidates(cid.Cid) ([]hashspace.Location, error) { return f.locs, f.candErr }

func (f *fakeHolders) StatAt(ctx context.Context, id string, _ cid.Cid) (bool, error) {
	f.mu.Lock()
	f.statted = append(f.statted, id)
	f.mu.Unlock()
	if ch, ok := f.slow[id]; ok {
		select {
		case <-ch:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return f.has[id], f.errs[id]
}

func locs(ids ...string) []hashspace.Location {
	out := make([]hashspace.Location, len(ids))
	for i, id := range ids {
		out[i] = hashspace.Location{StorageID: id}
	}
	return out
}

func TestFindHolder(t *testing.T) {
	ctx := context.Background()

	t.Run("no candidates probes nothing", func(t *testing.T) {
		f := &fakeHolders{}
		id, err := findHolder(ctx, f, cid.Undef)
		require.NoError(t, err)
		require.Empty(t, id)
		require.Empty(t, f.statted)
	})

	t.Run("candidate error is returned", func(t *testing.T) {
		f := &fakeHolders{candErr: errors.New("bad cid")}
		_, err := findHolder(ctx, f, cid.Undef)
		require.Error(t, err)
	})

	t.Run("single candidate hit and miss", func(t *testing.T) {
		f := &fakeHolders{locs: locs("a"), has: map[string]bool{"a": true}}
		id, err := findHolder(ctx, f, cid.Undef)
		require.NoError(t, err)
		require.Equal(t, "a", id)

		f.has["a"] = false
		id, err = findHolder(ctx, f, cid.Undef)
		require.NoError(t, err)
		require.Empty(t, id)
	})

	t.Run("every candidate is probed and the hit wins over a slow miss", func(t *testing.T) {
		never := make(chan struct{})
		f := &fakeHolders{
			locs: locs("owner", "dest", "misplaced"),
			has:  map[string]bool{"dest": true},
			slow: map[string]chan struct{}{"owner": never},
		}
		done := make(chan struct{})
		var id string
		var err error
		go func() { id, err = findHolder(ctx, f, cid.Undef); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("find waited for the slow candidate")
		}
		require.NoError(t, err)
		require.Equal(t, "dest", id)
		require.ElementsMatch(t, []string{"owner", "dest", "misplaced"}, f.statted)
	})

	t.Run("all miss is empty without an error", func(t *testing.T) {
		f := &fakeHolders{locs: locs("a", "b")}
		id, err := findHolder(ctx, f, cid.Undef)
		require.NoError(t, err)
		require.Empty(t, id)
	})

	t.Run("errors are misses when another candidate has it", func(t *testing.T) {
		f := &fakeHolders{
			locs: locs("a", "b"),
			has:  map[string]bool{"b": true},
			errs: map[string]error{"a": errors.New("node down")},
		}
		id, err := findHolder(ctx, f, cid.Undef)
		require.NoError(t, err)
		require.Equal(t, "b", id)
	})

	t.Run("errors surface when nothing has it", func(t *testing.T) {
		f := &fakeHolders{
			locs: locs("a", "b"),
			errs: map[string]error{"a": errors.New("node down")},
		}
		_, err := findHolder(ctx, f, cid.Undef)
		require.Error(t, err)
	})

	t.Run("cancelled ctx stops the wait", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		f := &fakeHolders{locs: locs("a", "b"), slow: map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{})}}
		cancel()
		_, err := findHolder(cctx, f, cid.Undef)
		require.ErrorIs(t, err, context.Canceled)
	})
}
