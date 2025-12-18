package retrieval

import (
	"crypto/rand"
	"io"
	"testing"

	"github.com/ipfs/boxo/blockstore"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dss "github.com/ipfs/go-datastore/sync"
	ipld "github.com/ipld/go-ipld-prime"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

// generateTestBlock creates a block of the given size for testing
func generateTestBlock(size int) (blocks.Block, error) {
	data := make([]byte, size)
	_, err := rand.Read(data)
	if err != nil {
		return nil, err
	}

	hash, err := multihash.Sum(data, multihash.SHA2_256, -1)
	if err != nil {
		return nil, err
	}

	c := cid.NewCidV1(cid.Raw, hash)
	return blocks.NewBlockWithCid(data, c)
}

func TestLinkSystem(t *testing.T) {
	store := blockstore.NewBlockstore(dss.MutexWrap(datastore.NewMapDatastore()))
	blk, err := generateTestBlock(1000)
	require.NoError(t, err, "Unable to generate test block")

	persistence := LinkSystemForBlockstore(store)
	buffer, commit, err := persistence.StorageWriteOpener(ipld.LinkContext{})
	require.NoError(t, err, "Unable to setup buffer")
	_, err = buffer.Write(blk.RawData())
	require.NoError(t, err, "Unable to write data to buffer")
	err = commit(cidlink.Link{Cid: blk.Cid()})
	require.NoError(t, err, "Unable to put block to store")
	data, err := persistence.StorageReadOpener(ipld.LinkContext{}, cidlink.Link{Cid: blk.Cid()})
	require.NoError(t, err, "Unable to load block with loader")
	bytes, err := io.ReadAll(data)
	require.NoError(t, err, "Unable to read bytes from reader returned by loader")
	_, err = blocks.NewBlockWithCid(bytes, blk.Cid())
	require.NoError(t, err, "Did not return correct block with loader")
}
