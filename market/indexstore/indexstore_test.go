package indexstore

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/filecoin-project/go-commp-utils/writer"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/testutils"
)

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

func TestNewIndexStore(t *testing.T) {
	// Set up the indexStore for testing

	ctx := context.Background()
	cfg := config.DefaultCurioConfig()

	idxStore, err := NewIndexStore([]string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, cfg)
	require.NoError(t, err)

	// Create a car file and calculate commP
	dir, err := os.MkdirTemp(os.TempDir(), "curio-indexstore")
	require.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	rf, err := testutils.CreateRandomFile(dir, int(time.Now().Unix()), 8000000)
	require.NoError(t, err)

	caropts := []carv2.Option{
		blockstore.WriteAsCarV1(true),
	}

	_, cn, err := testutils.CreateDenseCARWith(dir, rf, 1024, 1024, caropts)
	require.NoError(t, err)

	f, err := os.Open(cn)
	require.NoError(t, err)

	defer func() {
		_ = f.Close()
	}()

	w := &writer.Writer{}
	_, err = io.CopyBuffer(w, f, make([]byte, writer.CommPBuf))
	require.NoError(t, err)

	commp, err := w.Sum()
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	// Create recods
	dealCfg := cfg.Market.StorageMarketConfig
	chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize

	recs := make(chan Record, chanSize)
	opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
	blockReader, err := carv2.NewBlockReader(f, opts...)
	require.NoError(t, err)

	// Add index to the store
	var eg errgroup.Group
	eg.Go(func() error {
		serr := idxStore.AddIndex(ctx, commp.PieceCID, recs)
		return serr
	})

	var m multihash.Multihash
	i := 0

	blockMetadata, err := blockReader.SkipNext()
	for err == nil {
		if i == 0 {
			m = blockMetadata.Hash()
		}
		recs <- Record{
			Cid:    blockMetadata.Cid,
			Offset: blockMetadata.SourceOffset,
			Size:   blockMetadata.Size,
		}
		i++

		blockMetadata, err = blockReader.SkipNext()
	}
	require.Error(t, io.EOF)
	close(recs)
	err = eg.Wait()
	require.NoError(t, err)

	// Try to find a multihash
	pcids, err := idxStore.PiecesContainingMultihash(ctx, m)
	require.NoError(t, err)
	require.Len(t, pcids, 1)
	require.Equal(t, pcids[0].PieceCid.String(), commp.PieceCID.String())

	// Remove all indexes from the store
	err = idxStore.RemoveIndexes(ctx, pcids[0].PieceCid)
	require.NoError(t, err)

	// Drop the tables
	err = idxStore.session.Query("DROP TABLE PayloadToPieces").Exec()
	require.NoError(t, err)
	err = idxStore.session.Query("DROP TABLE PieceBlockOffsetSize").Exec()
	require.NoError(t, err)
}
