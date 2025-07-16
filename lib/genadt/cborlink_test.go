package genadt

import (
	"bytes"
	"context"
	"testing"

	"github.com/ipfs/go-cid"
	ipldcbor "github.com/ipfs/go-ipld-cbor"
	"github.com/stretchr/testify/require"

	bstore "github.com/filecoin-project/lotus/blockstore"
)

// ------------------------------------------------------------------
//  2. A minimal in-memory Store that implements `genadt.Store`.
//     We use go-ipfs-blockstore + ipfs/go-ipld-cbor for the store logic.
//
// ------------------------------------------------------------------
type testStore struct {
	ipldcbor.IpldStore
	ctx context.Context
}

func (ts *testStore) Context() context.Context {
	return ts.ctx
}

// newTestStore creates a memory-backed IPLD store.
func newTestStore() Store {
	ctx := context.Background()
	bs := bstore.NewMemory()
	return &testStore{
		IpldStore: ipldcbor.NewCborStore(bs),
		ctx:       ctx,
	}
}

// ------------------------------------------------------------------
// 3) Unit Tests
// ------------------------------------------------------------------

func TestCborLink_StoreLoad(t *testing.T) {
	st := newTestStore()

	// Create a link
	var link CborLink[*testValue]

	// Attempt to load with cid.Undef => error
	_, err := link.Load(st)
	require.Error(t, err, "Expected error for undefined CID")

	// Store a value
	val := &testValue{"HelloWorld"}
	require.NoError(t, link.Store(st, val), "Store should succeed")
	require.True(t, link.Cid().Defined(), "CID should be defined after store")

	// Load it back
	out, err := link.Load(st)
	require.NoError(t, err, "Load should succeed after store")
	require.Equal(t, "HelloWorld", out.Data, "value mismatch after load")

	// Load again (should use cached val)
	out2, err := link.Load(st)
	require.NoError(t, err)
	require.Same(t, out, out2, "should have returned the cached pointer")
}

func TestCborLink_MarshalUnmarshal(t *testing.T) {
	var link CborLink[*testValue]

	// Let’s define some dummy Cid for the link
	dummyCid, _ := cid.Parse("bafy2bzaceaa6q7u6zrjlt3lsyqn7mwj4ovyadfa4mnjrq4z2jvpf3xmpqqoue") // valid example
	link.cid = dummyCid

	// Round-trip
	var buf bytes.Buffer
	require.NoError(t, link.MarshalCBOR(&buf), "marshal cbor link")

	var link2 CborLink[*testValue]
	require.NoError(t, link2.UnmarshalCBOR(&buf), "unmarshal cbor link")

	require.Equal(t, dummyCid, link2.Cid(), "CIDs should match after round trip")
}
