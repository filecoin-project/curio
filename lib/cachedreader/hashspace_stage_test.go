package cachedreader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
)

type fakeFile struct {
	*bytes.Reader
	closed bool
}

func (f *fakeFile) Close() error { f.closed = true; return nil }

type fakeOpen struct {
	mu    sync.Mutex
	files map[cid.Cid][]byte
	err   error
	calls []cid.Cid
	sizes map[cid.Cid]int64
	opens []*fakeFile
}

func (f *fakeOpen) ReadPiece(_ context.Context, pc cid.Cid, rawSize int64) (storiface.Reader, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, pc)
	if f.sizes == nil {
		f.sizes = map[cid.Cid]int64{}
	}
	f.sizes[pc] = rawSize
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.files[pc]
	if !ok {
		return nil, nil
	}
	ff := &fakeFile{Reader: bytes.NewReader(data)}
	f.opens = append(f.opens, ff)
	return ff, nil
}

type fakeAggs struct {
	mu      sync.Mutex
	records []indexstore.Record
	err     error
	calls   []cid.Cid
	block   bool
	ctxDone chan struct{}
}

func (f *fakeAggs) FindPieceInAggregate(ctx context.Context, pc cid.Cid) ([]indexstore.Record, error) {
	f.mu.Lock()
	f.calls = append(f.calls, pc)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		if f.ctxDone != nil {
			close(f.ctxDone)
		}
		return nil, ctx.Err()
	}
	return f.records, f.err
}

func testV2(t *testing.T, rawSize uint64) cid.Cid {
	t.Helper()
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, rawSize)
	require.NoError(t, err)
	return v2
}

func newStageReader(open OpenReader, aggs aggregateFinder) *CachedPieceReader {
	cpr := &CachedPieceReader{}
	if aggs != nil {
		cpr.aggs = aggs
	}
	cpr.SetOpenPieceReader(open)
	return cpr
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return b
}

func TestReadFromHashspace(t *testing.T) {
	ctx := context.Background()
	sub := testV2(t, 127)
	parentA := testV2(t, 1016)
	parentB := testV2(t, 2032)

	parentBytes := bytes.Repeat([]byte{0}, 300)
	copy(parentBytes[100:], []byte("subpiece-bytes"))

	t.Run("no open reader is a miss", func(t *testing.T) {
		cpr := newStageReader(nil, &fakeAggs{})
		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.Nil(t, res.reader)
		require.Nil(t, res.parents)
	})

	t.Run("direct hit is served as-is and the finder is cancelled", func(t *testing.T) {
		open := &fakeOpen{files: map[cid.Cid][]byte{sub: []byte("direct")}}
		done := make(chan struct{})
		aggs := &fakeAggs{block: true, ctxDone: done}
		cpr := newStageReader(open, aggs)

		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.NotNil(t, res.reader)
		require.Equal(t, uint64(127), res.rawSize)
		require.Equal(t, []byte("direct"), readAll(t, res.reader))
		require.Equal(t, int64(127), open.sizes[sub])
		require.Nil(t, res.parents)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("aggregate lookup was not cancelled by the direct hit")
		}
	})

	t.Run("miss with no parents reports the empty lookup", func(t *testing.T) {
		open := &fakeOpen{}
		aggs := &fakeAggs{}
		cpr := newStageReader(open, aggs)

		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.Nil(t, res.reader)
		require.NotNil(t, res.parents)
		require.Empty(t, res.parents.records)
		require.Equal(t, []cid.Cid{sub}, aggs.calls)
	})

	t.Run("parents are probed in order and the subpiece is cut out", func(t *testing.T) {
		open := &fakeOpen{files: map[cid.Cid][]byte{parentB: parentBytes}}
		recs := []indexstore.Record{
			{Cid: parentA, Offset: 10, Size: 20},
			{Cid: parentB, Offset: 100, Size: 14},
		}
		aggs := &fakeAggs{records: recs}
		cpr := newStageReader(open, aggs)

		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.NotNil(t, res.reader)
		require.Equal(t, uint64(127), res.rawSize)
		require.Equal(t, []byte("subpiece-bytes"), readAll(t, res.reader))
		require.Equal(t, recs, res.parents.records)
		require.Equal(t, []cid.Cid{sub, parentA, parentB}, open.calls)
		require.Len(t, aggs.calls, 1)

		require.NoError(t, res.reader.Close())
		require.True(t, open.opens[0].closed)
	})

	t.Run("no parent on disk leaves the parents for the legacy path", func(t *testing.T) {
		open := &fakeOpen{}
		recs := []indexstore.Record{{Cid: parentA, Offset: 0, Size: 5}}
		cpr := newStageReader(open, &fakeAggs{records: recs})

		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.Nil(t, res.reader)
		require.Equal(t, recs, res.parents.records)
	})

	t.Run("finder error is a miss without parents", func(t *testing.T) {
		cpr := newStageReader(&fakeOpen{}, &fakeAggs{err: errors.New("cql down")})
		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.Nil(t, res.reader)
		require.Nil(t, res.parents)
	})

	t.Run("probe error is a miss", func(t *testing.T) {
		cpr := newStageReader(&fakeOpen{err: errors.New("disk read")}, &fakeAggs{})
		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.Nil(t, res.reader)
	})

	t.Run("no finder skips the aggregate lookup", func(t *testing.T) {
		cpr := newStageReader(&fakeOpen{}, nil)
		res, err := cpr.readFromHashspace(ctx, sub)
		require.NoError(t, err)
		require.Nil(t, res.reader)
		require.Nil(t, res.parents)
	})

	t.Run("cancelled ctx is returned", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		cpr := newStageReader(&fakeOpen{}, &fakeAggs{block: true})
		_, err := cpr.readFromHashspace(cctx, sub)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("a piece that is not a piece cid is a miss", func(t *testing.T) {
		open := &fakeOpen{}
		cpr := newStageReader(open, &fakeAggs{})
		other, err := cid.Parse("bafybeihkqv2ukwgpgzkwsuz7whmvneztvxglkljbs3zosewgku2cfluvba")
		require.NoError(t, err)
		res, err := cpr.readFromHashspace(ctx, other)
		require.NoError(t, err)
		require.Nil(t, res.reader)
		require.Empty(t, open.calls)
	})
}

func TestBoundedReader(t *testing.T) {
	f := &fakeFile{Reader: bytes.NewReader([]byte("0123456789"))}
	r := boundedReader(f, 4)
	require.Equal(t, []byte("0123"), readAll(t, r))

	buf := make([]byte, 3)
	n, err := r.ReadAt(buf, 2)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, []byte("234"), buf)

	_, err = r.Seek(1, io.SeekStart)
	require.NoError(t, err)
	require.Equal(t, []byte("123"), readAll(t, r))

	require.NoError(t, r.Close())
	require.True(t, f.closed)
}
