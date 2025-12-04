package itests

import (
	"context"
	"io"
	"math/rand"
	"os"
	"testing"

	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/lib/savecache"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
)

func TestNewIndexStore(t *testing.T) {
	// Set up the indexStore for testing

	ctx := context.Background()
	cfg := config.DefaultCurioConfig()

	idxStore := indexstore.NewIndexStore([]string{testutils.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, cfg)
	err := idxStore.Start(ctx, true)
	require.NoError(t, err)

	// Create a car file and calculate commP
	dir, err := os.MkdirTemp(os.TempDir(), "curio-indexstore")
	require.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(dir)
	}()

	rf, err := testutils.CreateRandomTmpFile(dir, 8000000)
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

	stat, err := f.Stat()
	require.NoError(t, err)

	// Calculate commP
	cp := savecache.NewCommPWithSizeForTest(uint64(stat.Size()))
	_, err = io.Copy(cp, f)
	require.NoError(t, err)

	digest, _, layerIdx, _, layer, err := cp.DigestWithSnapShot()
	require.NoError(t, err)

	t.Logf("Layer number: %d", layerIdx)
	t.Logf("Number of nodes in layer: %d", len(layer))

	pcid1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)

	pcid2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(stat.Size()))
	require.NoError(t, err)

	// Rewind the file
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	// Create recods
	dealCfg := cfg.Market.StorageMarketConfig
	chanSize := dealCfg.Indexing.InsertConcurrency * dealCfg.Indexing.InsertBatchSize

	recs := make(chan indexstore.Record, chanSize)
	opts := []carv2.Option{carv2.ZeroLengthSectionAsEOF(true)}
	blockReader, err := carv2.NewBlockReader(f, opts...)
	require.NoError(t, err)

	// Add index to the store
	var eg errgroup.Group
	eg.Go(func() error {
		serr := idxStore.AddIndex(ctx, pcid1, recs)
		return serr
	})

	var m multihash.Multihash
	i := 0

	blockMetadata, err := blockReader.SkipNext()
	for err == nil {
		if i == 0 {
			m = blockMetadata.Hash()
		}
		recs <- indexstore.Record{
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
	require.Equal(t, pcids[0].PieceCid.String(), pcid1.String())

	// Migrate V1 to V2
	err = idxStore.UpdatePieceCidV1ToV2(ctx, pcid1, pcid2)
	require.NoError(t, err)
	pcids, err = idxStore.PiecesContainingMultihash(ctx, m)
	require.NoError(t, err)
	require.Len(t, pcids, 1)
	require.Equal(t, pcids[0].PieceCid.String(), pcid2.String())

	// Remove all indexes from the store
	err = idxStore.RemoveIndexes(ctx, pcids[0].PieceCid)
	require.NoError(t, err)

	// Test aggregate index
	aggrRec := []indexstore.Record{
		{
			Cid:    pcid1,
			Offset: 0,
			Size:   100,
		},
		{
			Cid:    pcid2,
			Offset: 100,
			Size:   101,
		},
	}

	err = idxStore.InsertAggregateIndex(ctx, pcid2, aggrRec)
	require.NoError(t, err)

	x, err := idxStore.FindPieceInAggregate(ctx, pcid1)
	require.NoError(t, err)
	require.Len(t, x, 1)
	require.Equal(t, x[0].Cid, pcid2)

	x, err = idxStore.FindPieceInAggregate(ctx, pcid2)
	require.NoError(t, err)
	require.Len(t, x, 1)
	require.Equal(t, x[0].Cid, pcid2)

	err = idxStore.RemoveAggregateIndex(ctx, pcid2)
	require.NoError(t, err)

	// Test PDP layer
	leafs := make([]indexstore.NodeDigest, len(layer))
	for i, s := range layer {
		leafs[i] = indexstore.NodeDigest{
			Layer: layerIdx,
			Hash:  s.Hash,
			Index: int64(i),
		}
	}
	require.Equal(t, len(leafs), len(layer))

	// Insert the layer
	err = idxStore.AddPDPLayer(ctx, pcid2, leafs)
	require.NoError(t, err)

	// Verify the layer
	has, ldx, err := idxStore.GetPDPLayerIndex(ctx, pcid2)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, ldx, layerIdx)

	has, _, err = idxStore.GetPDPLayerIndex(ctx, pcid1)
	require.NoError(t, err)
	require.False(t, has)

	outLayer, err := idxStore.GetPDPLayer(ctx, pcid2, layerIdx)
	require.NoError(t, err)
	require.Equal(t, len(layer), len(outLayer))

	// Fetch a NodeDigest
	challenge := int64(rand.Intn(len(layer)))
	has, node, err := idxStore.GetPDPNode(ctx, pcid2, layerIdx, challenge)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, node.Index, challenge)
	require.Equal(t, node.Layer, layerIdx)
	require.Equal(t, node.Hash, layer[challenge].Hash)

	err = idxStore.DeletePDPLayer(ctx, pcid2)
	require.NoError(t, err)
}
