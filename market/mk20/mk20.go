package mk20

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/ethclient"
	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/miner"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"
	verifreg9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/multictladdr"
	"github.com/filecoin-project/curio/lib/paths"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("mk20")

type MK20API interface {
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifreg9.AllocationId, tsk types.TipSetKey) (*verifreg9.Allocation, error)
}

type MK20 struct {
	miners             []address.Address
	db                 *harmonydb.DB
	api                MK20API
	ethClient          *ethclient.Client
	si                 paths.SectorIndex
	cfg                *config.CurioConfig
	sm                 map[address.Address]abi.SectorSize
	as                 *multictladdr.MultiAddressSelector
	stor               paths.StashStore
	maxParallelUploads *atomic.Int64
}

func NewMK20Handler(miners []address.Address, db *harmonydb.DB, si paths.SectorIndex, mapi MK20API, ethClient *ethclient.Client, cfg *config.CurioConfig, as *multictladdr.MultiAddressSelector, stor paths.StashStore) (*MK20, error) {
	ctx := context.Background()

	// Ensure MinChunk size and max chunkSize is a power of 2
	if cfg.Market.StorageMarketConfig.MK20.MinimumChunkSize&(cfg.Market.StorageMarketConfig.MK20.MinimumChunkSize-1) != 0 {
		return nil, xerrors.Errorf("MinimumChunkSize must be a power of 2")
	}

	if cfg.Market.StorageMarketConfig.MK20.MaximumChunkSize&(cfg.Market.StorageMarketConfig.MK20.MaximumChunkSize-1) != 0 {
		return nil, xerrors.Errorf("MaximumChunkSize must be a power of 2")
	}

	sm := make(map[address.Address]abi.SectorSize)

	for _, m := range miners {
		info, err := mapi.StateMinerInfo(ctx, m, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting miner info: %w", err)
		}
		if _, ok := sm[m]; !ok {
			sm[m] = info.SectorSize
		}
	}

	return &MK20{
		miners:             miners,
		db:                 db,
		api:                mapi,
		ethClient:          ethClient,
		si:                 si,
		cfg:                cfg,
		sm:                 sm,
		as:                 as,
		stor:               stor,
		maxParallelUploads: new(atomic.Int64),
	}, nil
}

func (m *MK20) ExecuteDeal(ctx context.Context, deal *Deal) *ProviderDealRejectionInfo {
	// Validate the DataSource
	code, err := deal.Validate(m.db, &m.cfg.Market.StorageMarketConfig.MK20)
	if err != nil {
		log.Errorw("deal rejected", "deal", deal, "error", err)
		ret := &ProviderDealRejectionInfo{
			HTTPCode: int(code),
		}
		if code == http.StatusInternalServerError {
			ret.Reason = "Internal server error"
		} else {
			ret.Reason = err.Error()
		}
		return ret
	}

	log.Debugw("deal validated", "deal", deal.Identifier.String())

	return m.processDDODeal(ctx, deal)

}

func (m *MK20) processDDODeal(ctx context.Context, deal *Deal) *ProviderDealRejectionInfo {
	rejection, err := m.sanitizeDDODeal(ctx, deal)
	if err != nil {
		log.Errorw("deal rejected", "deal", deal, "error", err)
		return rejection
	}

	log.Debugw("deal sanitized", "deal", deal.Identifier.String())

	if rejection != nil {
		return rejection
	}

	id, code, err := deal.Products.DDOV1.GetDealID(ctx, m.db, m.ethClient)
	if err != nil {
		log.Errorw("error getting deal ID", "deal", deal, "error", err)
		ret := &ProviderDealRejectionInfo{
			HTTPCode: int(code),
		}
		if code == http.StatusInternalServerError {
			ret.Reason = "Internal server error"
		} else {
			ret.Reason = err.Error()
		}
		return ret
	}

	log.Debugw("deal ID found", "deal", deal.Identifier.String(), "id", id)

	// TODO: Backpressure, client filter

	comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = deal.SaveToDB(tx)
		if err != nil {
			return false, err
		}
		n, err := tx.Exec(`Update market_mk20_deal SET market_deal_id = $1 WHERE id = $2`, id, deal.Identifier.String())
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, fmt.Errorf("expected 1 row to be updated, got %d", n)
		}
		if deal.Data.SourceHttpPut != nil {
			_, err = tx.Exec(`INSERT INTO market_mk20_pipeline_waiting (id, waiting_for_data) VALUES ($1, TRUE) ON CONFLICT (id) DO NOTHING`, deal.Identifier.String())

		} else {
			_, err = tx.Exec(`INSERT INTO market_mk20_pipeline_waiting (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, deal.Identifier.String())
		}

		if err != nil {
			return false, xerrors.Errorf("adding deal to waiting pipeline: %w", err)
		}
		return true, nil
	})

	if err != nil {
		log.Errorw("error inserting deal into DB", "deal", deal, "error", err)
		return &ProviderDealRejectionInfo{
			HTTPCode: http.StatusInternalServerError,
		}
	}

	if !comm {
		log.Errorw("error committing deal into DB", "deal", deal)
		return &ProviderDealRejectionInfo{
			HTTPCode: http.StatusInternalServerError,
		}
	}

	log.Debugw("deal inserted in DB", "deal", deal.Identifier.String())

	return &ProviderDealRejectionInfo{
		HTTPCode: http.StatusOK,
	}
}

func (m *MK20) sanitizeDDODeal(ctx context.Context, deal *Deal) (*ProviderDealRejectionInfo, error) {
	if !lo.Contains(m.miners, deal.Products.DDOV1.Provider) {
		return &ProviderDealRejectionInfo{
			HTTPCode: http.StatusBadRequest,
			Reason:   "Provider not available in Curio cluster",
		}, nil
	}

	if deal.Data.Size > abi.PaddedPieceSize(m.sm[deal.Products.DDOV1.Provider]) {
		return &ProviderDealRejectionInfo{
			HTTPCode: http.StatusBadRequest,
			Reason:   "Deal size is larger than the miner's sector size",
		}, nil
	}

	if deal.Data.Format.Raw != nil {
		if deal.Products.DDOV1.Indexing {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Raw bytes deal cannot be indexed",
			}, nil
		}
	}

	if deal.Products.DDOV1.AllocationId != nil {
		if deal.Data.Size < abi.PaddedPieceSize(verifreg.MinimumVerifiedAllocationSize) {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Verified piece size must be at least 1MB",
			}, nil
		}

		alloc, err := m.api.StateGetAllocation(ctx, deal.Products.DDOV1.Client, verifreg9.AllocationId(*deal.Products.DDOV1.AllocationId), types.EmptyTSK)
		if err != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusInternalServerError,
			}, xerrors.Errorf("getting allocation: %w", err)
		}

		if alloc == nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Verified piece must have a valid allocation ID",
			}, nil
		}

		clientID, err := address.IDFromAddress(deal.Products.DDOV1.Client)
		if err != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Invalid client address",
			}, nil
		}

		if alloc.Client != abi.ActorID(clientID) {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "client address does not match the allocation client address",
			}, nil
		}

		prov, err := address.NewIDAddress(uint64(alloc.Provider))
		if err != nil {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusInternalServerError,
			}, xerrors.Errorf("getting provider address: %w", err)
		}

		if !lo.Contains(m.miners, prov) {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Allocation provider does not belong to the list of miners in Curio cluster",
			}, nil
		}

		if !deal.Data.PieceCID.Equals(alloc.Data) {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Allocation data CID does not match the piece CID",
			}, nil
		}

		if deal.Data.Size != alloc.Size {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Allocation size does not match the piece size",
			}, nil
		}

		if alloc.TermMin > miner.MaxSectorExpirationExtension-policy.SealRandomnessLookback {
			return &ProviderDealRejectionInfo{
				HTTPCode: http.StatusBadRequest,
				Reason:   "Allocation term min is greater than the maximum sector expiration extension",
			}, nil
		}
	}

	return nil, nil
}

// To be used later for when data source is minerID
//func validateMinerAddresses(madrs []abi.Multiaddrs, pcid cid.Cid, psize abi.PaddedPieceSize, rawSize int64) bool {
//	var surls []*url.URL
//	for _, adr := range madrs {
//		surl, err := maurl.ToURL(multiaddr.Cast(adr))
//		if err != nil {
//			continue
//		}
//		surls = append(surls, surl)
//	}
//
//	var validUrls []*url.URL
//
//	for _, surl := range surls {
//		if surl.Scheme == "ws" {
//			surl.Scheme = "http"
//		}
//
//		if surl.Scheme == "wss" {
//			surl.Scheme = "https"
//		}
//
//		if surl.Port() == "443" {
//			surl.Host = surl.Hostname()
//		}
//
//		if surl.Port() == "80" {
//			surl.Host = surl.Hostname()
//		}
//
//		resp, err := http.Head(surl.String() + "/piece/" + pcid.String())
//		if err != nil {
//			continue
//		}
//		if resp.StatusCode != 200 {
//			continue
//		}
//
//		if resp.Header.Get("Content-Length") != fmt.Sprint(psize) {
//			continue
//		}
//
//		validUrls = append(validUrls, surl)
//	}
//	return len(validUrls) > 0
//}
