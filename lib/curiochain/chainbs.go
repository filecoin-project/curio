package curiochain

import (
	lru "github.com/hashicorp/golang-lru/v2"
	blocks "github.com/ipfs/go-block-format"

	"github.com/filecoin-project/lotus/blockstore"
	"github.com/filecoin-project/lotus/lib/must"
)

var ChainBlockCache = must.One(lru.New[blockstore.MhString, blocks.Block](4096))

type CurioBlockstore blockstore.Blockstore

func NewChainBlockstore(io blockstore.ChainIO) CurioBlockstore {
	apiStore := blockstore.NewAPIBlockstore(io)
	blockstore.NewReadCachedBlockstore(apiStore, ChainBlockCache)
	return CurioBlockstore(apiStore)
}
