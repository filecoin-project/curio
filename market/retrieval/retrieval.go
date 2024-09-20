package retrieval

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-graphsync/storeutil"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipld/frisbii"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/retrieval/remoteblockstore"
)

var log = logging.Logger("retrievals")

type Provider struct {
	db *harmonydb.DB
	bs *remoteblockstore.RemoteBlockstore
	fr *frisbii.HttpIpfs
}

const piecePrefix = "/piece/"
const ipfsPrefix = "/ipfs/"

func NewRetrievalProvider(ctx context.Context, db *harmonydb.DB, idxStore *indexstore.IndexStore, pp *pieceprovider.PieceProvider) *Provider {
	bs := remoteblockstore.NewRemoteBlockstore(idxStore, db, pp)
	lsys := storeutil.LinkSystemForBlockstore(bs)
	fr := frisbii.NewHttpIpfs(ctx, lsys)

	return &Provider{
		db: db,
		bs: bs,
		fr: fr,
	}
}

func Router(mux *chi.Mux, rp *Provider) {
	mux.Get(piecePrefix, rp.handleByPieceCid)
	mux.Get(ipfsPrefix, rp.fr.ServeHTTP)
}
