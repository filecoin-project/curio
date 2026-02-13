package indexstore

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
)

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}

func TestNewIndexStore(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultCurioConfig()

	// ---- Setup: connect to Cassandra in a test keyspace ----

	idxStore, err := NewIndexStore([]string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, cfg)
	require.NoError(t, err)

	err = idxStore.Start(ctx, true)
	require.NoError(t, err)

	// ---- Fixture: build a CAR file, derive v1 and v2 piece CIDs ----

	dir, err := os.MkdirTemp(os.TempDir(), "curio-indexstore")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	rf, err := testutils.CreateRandomTmpFile(dir, 8000000)
	require.NoError(t, err)

	_, carPath, err := testutils.CreateDenseCARWith(dir, rf, 1024, 1024, []carv2.Option{
		blockstore.WriteAsCarV1(true),
	})
	require.NoError(t, err)

	f, err := os.Open(carPath)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	require.NoError(t, err)

	cp := savecache.NewCommPWithSizeForTest(uint64(stat.Size()))
	_, err = io.Copy(cp, f)
	require.NoError(t, err)

	digest, _, layerIdx, _, layer, err := cp.DigestWithSnapShot()
	require.NoError(t, err)
	t.Logf("Layer number: %d, nodes: %d", layerIdx, len(layer))

	pieceCidV1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)

	pieceCidV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(stat.Size()))
	require.NoError(t, err)

	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)

	// ---- Test: AddIndex (write under v1 CID) ----

	dealCfg := cfg.Market.StorageMarketConfig
	recs := make(chan Record, dealCfg.Indexing.InsertConcurrency*dealCfg.Indexing.InsertBatchSize)

	blockReader, err := carv2.NewBlockReader(f, carv2.ZeroLengthSectionAsEOF(true))
	require.NoError(t, err)

	var eg errgroup.Group
	eg.Go(func() error { return idxStore.AddIndex(ctx, pieceCidV1, recs) })

	var sampleMH multihash.Multihash
	blockCount := 0

	blockMeta, err := blockReader.SkipNext()
	for err == nil {
		if blockCount == 0 {
			sampleMH = blockMeta.Hash()
		}
		recs <- Record{
			Cid:    blockMeta.Cid,
			Offset: blockMeta.SourceOffset,
			Size:   blockMeta.Size,
		}
		blockCount++
		blockMeta, err = blockReader.SkipNext()
	}
	require.Error(t, io.EOF)
	close(recs)
	require.NoError(t, eg.Wait())

	// ---- Test: PiecesContainingMultihash (lookup) ----

	pieces, err := idxStore.PiecesContainingMultihash(ctx, sampleMH)
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	require.Equal(t, pieceCidV1.String(), pieces[0].PieceCid.String())

	// ---- Test: UpdatePieceCidV1ToV2 (migration) ----

	err = idxStore.UpdatePieceCidV1ToV2(ctx, pieceCidV1, pieceCidV2)
	require.NoError(t, err)

	pieces, err = idxStore.PiecesContainingMultihash(ctx, sampleMH)
	require.NoError(t, err)
	require.Len(t, pieces, 1)
	require.Equal(t, pieceCidV2.String(), pieces[0].PieceCid.String())

	// ---- Test: RemoveIndexes ----

	err = idxStore.RemoveIndexes(ctx, pieces[0].PieceCid)
	require.NoError(t, err)

	// ---- Test: PDP layer operations ----

	leafs := make([]NodeDigest, len(layer))
	for idx, s := range layer {
		leafs[idx] = NodeDigest{Layer: layerIdx, Hash: s.Hash, Index: int64(idx)}
	}
	require.Equal(t, len(leafs), len(layer))

	err = idxStore.AddPDPLayer(ctx, pieceCidV2, leafs)
	require.NoError(t, err)

	has, ldx, err := idxStore.GetPDPLayerIndex(ctx, pieceCidV2)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, layerIdx, ldx)

	has, _, err = idxStore.GetPDPLayerIndex(ctx, pieceCidV1)
	require.NoError(t, err)
	require.False(t, has)

	outLayer, err := idxStore.GetPDPLayer(ctx, pieceCidV2, layerIdx)
	require.NoError(t, err)
	require.Equal(t, len(layer), len(outLayer))

	challenge := int64(rand.Intn(len(layer)))
	has, node, err := idxStore.GetPDPNode(ctx, pieceCidV2, layerIdx, challenge)
	require.NoError(t, err)
	require.True(t, has)
	require.Equal(t, challenge, node.Index)
	require.Equal(t, layerIdx, node.Layer)
	require.Equal(t, layer[challenge].Hash, node.Hash)

	err = idxStore.DeletePDPLayer(ctx, pieceCidV2)
	require.NoError(t, err)

	// ---- Cleanup: drop test tables ----

	require.NoError(t, idxStore.session.Query("DROP TABLE PayloadToPieces").Exec())
	require.NoError(t, idxStore.session.Query("DROP TABLE PieceBlockOffsetSize").Exec())
	require.NoError(t, idxStore.session.Query("DROP TABLE pdp_cache_layer").Exec())
}
