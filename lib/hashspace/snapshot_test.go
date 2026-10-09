package hashspace

import (
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
)

func ids(ls []Location) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.StorageID
	}
	return out
}

func TestPlacesIn(t *testing.T) {
	c := &Cluster{roots: map[string]string{"b": "/b"}}
	rs := []rangeRow{{EndHash: h(0x40), StorageID: "a"}, {EndHash: h(0xc0), StorageID: "b"}, {EndHash: h(0x00), StorageID: "c"}}

	t.Run("owner only", func(t *testing.T) {
		ls, err := c.placesIn(rs, nil, nil, h(0x80))
		require.NoError(t, err)
		require.Equal(t, []string{"b"}, ids(ls))
		require.True(t, ls[0].Local)
	})

	t.Run("move covers both ends, local first", func(t *testing.T) {
		moves := []moveSourceRow{{StartHash: h(0x60), EndHash: h(0xa0), FromStorage: "a", ToStorage: "b"}}
		ls, err := c.placesIn(rs, moves, nil, h(0x80))
		require.NoError(t, err)
		require.Equal(t, []string{"b", "a"}, ids(ls))
	})

	t.Run("misplaced disks are added once", func(t *testing.T) {
		ls, err := c.placesIn(rs, nil, []string{"a", "b"}, h(0x80))
		require.NoError(t, err)
		require.Equal(t, []string{"b", "a"}, ids(ls))
	})

	t.Run("no range is an error", func(t *testing.T) {
		_, err := c.placesIn(nil, nil, nil, h(0x80))
		require.Error(t, err)
	})
}

func TestCandidates(t *testing.T) {
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, 127)
	require.NoError(t, err)

	c := &Cluster{roots: map[string]string{"local": "/l"}}

	// No snapshot yet.
	ls, err := c.Candidates(v2)
	require.NoError(t, err)
	require.Empty(t, ls)

	// A snapshot without ranges.
	c.snap.Store(&mapSnapshot{})
	ls, err = c.Candidates(v2)
	require.NoError(t, err)
	require.Empty(t, ls)

	// One range owns the whole circle.
	c.snap.Store(&mapSnapshot{
		ranges:    []rangeRow{{EndHash: h(0x10), StorageID: "remote"}},
		misplaced: []string{"local"},
	})
	ls, err = c.Candidates(v2)
	require.NoError(t, err)
	require.Equal(t, []string{"local", "remote"}, ids(ls))

	// Only piece CID v2 names a hash.
	_, err = c.Candidates(v1)
	require.Error(t, err)
}

func TestHTTPBase(t *testing.T) {
	base, ok := httpBase("10.0.0.5:12310")
	require.True(t, ok)
	require.Equal(t, "http://10.0.0.5:12310", base)

	_, ok = httpBase("127.0.0.1:skiff")
	require.False(t, ok)
	_, ok = httpBase("")
	require.False(t, ok)
}

func TestStorageURLsFromSnapshot(t *testing.T) {
	c := &Cluster{}
	c.snap.Store(&mapSnapshot{urls: map[string]string{"s": "http://n1:1234"}})
	u, err := c.storageURLs(t.Context(), "s")
	require.NoError(t, err)
	require.Equal(t, "http://n1:1234", u)
}
