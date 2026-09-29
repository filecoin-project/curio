// Package gate implements opt-in permissioning for PDP piece retrieval.
//
// Retrieval is content-addressed (a request carries only a PieceCID) and a PieceCID can belong to
// many data sets, each with its own payer. This package resolves the candidate data sets for a
// requested piece, reads each data set's on-chain "private retrieval" opt-in flag, and resolves the
// data set's on-chain payer — the identity that always has retrieval access and that signs
// off-chain vouchers granting access to others.
package gate

import (
	"context"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/pdp/contract"
	FWSS "github.com/filecoin-project/curio/pdp/contract/FWSS"
)

// RetrievalACLMetadataKey is the on-chain data-set metadata key a payer sets (at data set creation,
// via the client SDK's EnhancedDataSetInfo.metadata) to opt the data set into gated retrieval.
const RetrievalACLMetadataKey = "withRetrievalACL"

// metadataCacheTTL bounds how long a data set's resolved private flag / payer is cached. Data set
// metadata is set at creation and rarely changes, so a modest TTL keeps per-request eth_calls off
// the hot path without going stale for long.
const metadataCacheTTL = 5 * time.Minute

// PieceDatasets returns the distinct on-chain data set ids that contain the requested piece, across
// both PDP subsystems (mk20 `pdp_dataset_piece` keyed by piece_cid_v2, and pdpv0 `pdp_piecerefs` →
// `pdp_data_set_pieces` keyed by the piece CID v1). The request CID may be v1 or v2; we match mk20
// on the v2 form and pdpv0 on the v1 form (deriving v1 from v2 the same way the retrieval reader
// does). An empty result means no known data set contains the piece.
func PieceDatasets(ctx context.Context, db *harmonydb.DB, pieceCid cid.Cid) ([]uint64, error) {
	v1 := pieceCid
	if commcidv2.IsPieceCidV2(pieceCid) {
		conv, _, err := commcid.PieceCidV1FromV2(pieceCid)
		if err != nil {
			return nil, xerrors.Errorf("piece CID v1 from v2: %w", err)
		}
		v1 = conv
	}

	seen := make(map[uint64]struct{})
	var out []uint64
	add := func(ids []uint64) {
		for _, id := range ids {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
	}

	// mk20 subsystem: piece stored by v2 CID.
	var mk20 []uint64
	if err := db.Select(ctx, &mk20, `
		SELECT DISTINCT data_set_id
		FROM pdp_dataset_piece
		WHERE piece_cid_v2 = $1 AND removed = FALSE`, pieceCid.String()); err != nil {
		return nil, xerrors.Errorf("querying mk20 pdp_dataset_piece: %w", err)
	}
	add(mk20)

	// pdpv0 subsystem: pieceref keyed by v1 CID, joined to its data sets.
	var pdpv0 []uint64
	if err := db.Select(ctx, &pdpv0, `
		SELECT DISTINCT dsp.data_set
		FROM pdp_piecerefs pr
		JOIN pdp_data_set_pieces dsp ON dsp.pdp_pieceref = pr.id
		WHERE pr.piece_cid = $1`, v1.String()); err != nil {
		return nil, xerrors.Errorf("querying pdpv0 pdp_piecerefs: %w", err)
	}
	add(pdpv0)

	return out, nil
}

// Resolver answers "is this data set private?" and "who is its payer?" against the FWSS view,
// caching both per data set for metadataCacheTTL. It is safe for concurrent use.
type Resolver struct {
	db  *harmonydb.DB
	idx *indexstore.IndexStore
	eth func(context.Context) (ethchain.EthClient, error)

	mu      sync.Mutex
	private map[uint64]cachedBool
	payer   map[uint64]cachedAddr
	chainID *big.Int
	vc      *common.Address // cached EIP-712 verifyingContract (the FWSS service address)
}

// VerifyingContract implements Backend: the EIP-712 domain's verifyingContract for PDP scopes — the
// FWSS service contract. Provides cross-service domain separation (see RETRIEVAL-AUTH-SPEC.md §3).
func (r *Resolver) VerifyingContract(ctx context.Context) (common.Address, error) {
	r.mu.Lock()
	if r.vc != nil {
		v := *r.vc
		r.mu.Unlock()
		return v, nil
	}
	r.mu.Unlock()

	addr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	if addr == (common.Address{}) {
		return common.Address{}, xerrors.New("FWSS service address not configured")
	}
	r.mu.Lock()
	r.vc = &addr
	r.mu.Unlock()
	return addr, nil
}

// PieceDatasets implements Backend: the candidate data sets containing the piece.
func (r *Resolver) PieceDatasets(ctx context.Context, pieceCid cid.Cid) ([]uint64, error) {
	return PieceDatasets(ctx, r.db, pieceCid)
}

// ContentDatasets implements Backend for the IPFS gateway: it maps a payload/IPLD CID to the
// piece(s) that contain it (via the index) and unions their data sets. Content that is not indexed
// to any piece resolves to no data sets (served publicly / 404'd by the gateway as before).
func (r *Resolver) ContentDatasets(ctx context.Context, contentCid cid.Cid) ([]uint64, error) {
	if r.idx == nil {
		return nil, xerrors.New("index store not configured")
	}
	pieces, err := r.idx.PiecesContainingMultihash(ctx, contentCid.Hash())
	if err != nil {
		return nil, xerrors.Errorf("index lookup for %s: %w", contentCid, err)
	}
	seen := make(map[uint64]struct{})
	var out []uint64
	for _, p := range pieces {
		ids, err := PieceDatasets(ctx, r.db, p.PieceCid)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// ChainID implements Backend: the EVM chain id, fetched once and cached.
func (r *Resolver) ChainID(ctx context.Context) (*big.Int, error) {
	r.mu.Lock()
	if r.chainID != nil {
		id := r.chainID
		r.mu.Unlock()
		return id, nil
	}
	r.mu.Unlock()

	eth, err := r.eth(ctx)
	if err != nil {
		return nil, xerrors.Errorf("eth client: %w", err)
	}
	id, err := eth.ChainID(ctx)
	if err != nil {
		return nil, xerrors.Errorf("eth ChainID: %w", err)
	}
	r.mu.Lock()
	r.chainID = id
	r.mu.Unlock()
	return id, nil
}

type cachedBool struct {
	v   bool
	exp time.Time
}

type cachedAddr struct {
	v   common.Address
	exp time.Time
}

// NewResolver builds a Resolver. eth is a getter (the retrieval provider holds a lazily-initialized
// eth client) so we only dial the node when a gated request actually needs it. idx may be nil if the
// IPFS gateway is not gated.
func NewResolver(db *harmonydb.DB, idx *indexstore.IndexStore, eth func(context.Context) (ethchain.EthClient, error)) *Resolver {
	return &Resolver{
		db:      db,
		idx:     idx,
		eth:     eth,
		private: make(map[uint64]cachedBool),
		payer:   make(map[uint64]cachedAddr),
	}
}

// DatasetPrivate reports whether the data set opted into gated retrieval (the RetrievalACLMetadataKey
// on-chain metadata flag is set). Result is cached per data set.
func (r *Resolver) DatasetPrivate(ctx context.Context, dataSetId uint64) (bool, error) {
	now := time.Now()
	r.mu.Lock()
	if c, ok := r.private[dataSetId]; ok && now.Before(c.exp) {
		r.mu.Unlock()
		return c.v, nil
	}
	r.mu.Unlock()

	eth, err := r.eth(ctx)
	if err != nil {
		return false, xerrors.Errorf("eth client: %w", err)
	}
	pdpVerifier, err := contract.NewPDPVerifierCaller(contract.ContractAddresses().PDPVerifier, eth)
	if err != nil {
		return false, xerrors.Errorf("instantiate PDPVerifier: %w", err)
	}
	setID := new(big.Int).SetUint64(dataSetId)
	listenerAddr, err := pdpVerifier.GetDataSetListener(contract.EthCallOpts(ctx), setID)
	if err != nil {
		return false, xerrors.Errorf("GetDataSetListener(%d): %w", dataSetId, err)
	}
	private, _, err := contract.GetDataSetMetadataAtKey(ctx, listenerAddr, eth, setID, RetrievalACLMetadataKey)
	if err != nil {
		return false, xerrors.Errorf("GetDataSetMetadataAtKey(%d, %s): %w", dataSetId, RetrievalACLMetadataKey, err)
	}

	r.mu.Lock()
	r.private[dataSetId] = cachedBool{v: private, exp: now.Add(metadataCacheTTL)}
	r.mu.Unlock()
	return private, nil
}

// DatasetPayer returns the data set's on-chain payer (the FWSS view's GetDataSet().Payer). Result is
// cached per data set.
func (r *Resolver) DatasetPayer(ctx context.Context, dataSetId uint64) (common.Address, error) {
	if dataSetId == 0 {
		return common.Address{}, xerrors.New("dataSetId must be greater than 0")
	}
	now := time.Now()
	r.mu.Lock()
	if c, ok := r.payer[dataSetId]; ok && now.Before(c.exp) {
		r.mu.Unlock()
		return c.v, nil
	}
	r.mu.Unlock()

	eth, err := r.eth(ctx)
	if err != nil {
		return common.Address{}, xerrors.Errorf("eth client: %w", err)
	}
	payer, err := FWSS.DataSetPayer(ctx, eth, dataSetId)
	if err != nil {
		return common.Address{}, err
	}

	r.mu.Lock()
	r.payer[dataSetId] = cachedAddr{v: payer, exp: now.Add(metadataCacheTTL)}
	r.mu.Unlock()
	return payer, nil
}
