package webrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
)

type StorageDealSummary struct {
	ID                string      `db:"uuid"`
	MinerID           int64       `db:"sp_id"`
	Sector            int64       `db:"sector_num"`
	CreatedAt         time.Time   `db:"created_at"`
	SignedProposalCid string      `db:"signed_proposal_cid"`
	Offline           bool        `db:"offline"`
	Verified          bool        `db:"verified"`
	StartEpoch        int64       `db:"start_epoch"`
	EndEpoch          int64       `db:"end_epoch"`
	ClientPeerId      string      `db:"client_peer_id"`
	ChainDealId       int64       `db:"chain_deal_id"`
	PublishCid        string      `db:"publish_cid"`
	PieceCid          string      `db:"piece_cid"`
	PieceSize         int64       `db:"piece_size"`
	FastRetrieval     bool        `db:"fast_retrieval"`
	AnnounceToIpni    bool        `db:"announce_to_ipni"`
	Url               string      `db:"url"`
	UrlHeaders        http.Header `db:"url_headers"`
	Error             string      `db:"error"`
	Miner             string
}

type MarketMk12DealPipeline struct {
	UUID            string `db:"uuid"`
	SpID            int64  `db:"sp_id"`
	Started         bool   `db:"started"`
	PieceCID        string `db:"piece_cid"`
	Offline         bool   `db:"offline"`
	Url             string `db:"url"`
	CommpTaskID     *int64 `db:"commp_task_id"` // NULLable field, use pointer
	AfterCommp      bool   `db:"after_commp"`
	PsdTaskID       *int64 `db:"psd_task_id"` // NULLable field, use pointer
	AfterPsd        bool   `db:"after_psd"`
	FindDealTaskID  *int64 `db:"find_deal_task_id"` // NULLable field, use pointer
	AfterFindDeal   bool   `db:"after_find_deal"`
	Sector          *int64 `db:"sector"` // NULLable field, use pointer
	Sealed          bool   `db:"sealed"`
	IndexingTaskID  *int64 `db:"indexing_task_id"` // NULLable field, use pointer
	Indexed         bool   `db:"indexed"`
	Complete        bool   `db:"complete"`
	Miner           string
	ParkPieceTaskID *int64
	AfterParkPiece  bool
}

func (a *WebRPC) PendingStorageDeals(ctx context.Context) ([]MarketMk12DealPipeline, error) {
	var pipeline []MarketMk12DealPipeline
	err := a.deps.DB.Select(ctx, &pipeline, `SELECT
													uuid,
													sp_id,
													started,
													piece_cid,
													offline,
													url,
													commp_task_id,
													after_commp,
													psd_task_id,
													after_psd,
													find_deal_task_id,
													after_find_deal,
													sector,
													sealed,
													indexing_task_id,
													indexed,
													complete
												FROM
													market_mk12_deal_pipeline
												ORDER BY sp_id`)

	if err != nil {
		return nil, xerrors.Errorf("failed to get the deal pipeline from DB: %w", err)
	}

	minerMap := make(map[int64]address.Address)
	for _, s := range pipeline {
		if addr, ok := minerMap[s.SpID]; ok {
			s.Miner = addr.String()
			continue
		}

		addr, err := address.NewIDAddress(uint64(s.SpID))
		if err != nil {
			return nil, err
		}
		s.Miner = addr.String()

		if !s.Started {
			if s.Url != "" {
				goUrl, err := url.Parse(s.Url)
				if err != nil {
					return nil, err
				}

				if goUrl.Scheme == "pieceref" {
					refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
					if err != nil {
						return nil, xerrors.Errorf("parsing piece reference number: %w", err)
					}

					var ParkedPieceInfo []struct {
						TaskID   *int64 `db:"task_id"` // TaskID may be NULL, so use a pointer
						Complete bool   `db:"complete"`
					}

					err = a.deps.DB.Select(ctx, &ParkedPieceInfo, `SELECT 
														pp.task_id,
														pp.complete
													FROM 
														parked_piece_refs ppr
													JOIN 
														parked_pieces pp ON ppr.piece_id = pp.id
													WHERE 
														ppr.ref_id = $1;`, refNum)

					if err != nil {
						return nil, err
					}

					if len(ParkedPieceInfo) != 1 {
						return nil, xerrors.Errorf("expected one park piece row for deal %s but got %d", s.UUID, len(ParkedPieceInfo))
					}

					s.AfterParkPiece = ParkedPieceInfo[0].Complete
					s.ParkPieceTaskID = ParkedPieceInfo[0].TaskID
				}
			}
		}
	}

	return pipeline, nil
}

func (a *WebRPC) StorageDealInfo(ctx context.Context, deal string) (*StorageDealSummary, error) {

	var isLegacy bool
	var pcid cid.Cid

	id, err := uuid.Parse(deal)
	if err != nil {
		pcid, err = cid.Parse(deal)
		if err != nil {
			return &StorageDealSummary{}, err
		}
		isLegacy = true
	}

	if !isLegacy {
		var summaries []StorageDealSummary
		err = a.deps.DB.Select(ctx, &summaries, `SELECT 
									md.uuid,
									md.sp_id,
									md.created_at,
									md.signed_proposal_cid,
									md.offline,
									md.verified,
									md.start_epoch,
									md.end_epoch,
									md.client_peer_id,
									md.chain_deal_id,
									md.publish_cid,
									md.piece_cid,
									md.piece_size,
									md.fast_retrieval,
									md.announce_to_ipni,
									md.url,
									md.url_headers,
									md.error,
									mid.sector_num
									FROM market_mk12_deals md 
										LEFT JOIN market_piece_deal mpd ON mpd.id = md.uuid AND mpd.sp_id = md.sp_id 
									WHERE md.uuid = $1 AND mpd.boost_deal = TRUE AND mpd.legacy_deal = FALSE;`, id.String())

		if err != nil {
			return &StorageDealSummary{}, err
		}

		d := summaries[0]

		addr, err := address.NewIDAddress(uint64(d.MinerID))
		if err != nil {
			return &StorageDealSummary{}, err
		}

		d.Miner = addr.String()

		return &d, nil
	}

	var summaries []StorageDealSummary
	err = a.deps.DB.Select(ctx, &summaries, `SELECT 
									signed_proposal_cid,
									sp_id,
									piece_cid,
									piece_size,
									offline,
									verified,
									start_epoch,
									end_epoch,
									publish_cid,
									chain_deal_id,
									piece_cid,
									piece_size,
									fast_retrieval,
									created_at,
									sector_num,
									client_peer_id,
									'' AS error,
									'' AS url,
									'' AS uuid,
									NULL AS url_headers
									FALSE AS announce_to_ipni 
									FROM market_legacy_deals
									WHERE signed_proposal_cid = $1`, pcid.String())

	if err != nil {
		return &StorageDealSummary{}, err
	}

	d := summaries[0]

	addr, err := address.NewIDAddress(uint64(d.MinerID))
	if err != nil {
		return &StorageDealSummary{}, err
	}

	d.Miner = addr.String()

	return &d, nil
}

type StorageDealList struct {
	ID          string    `db:"uuid"`
	MinerID     int64     `db:"sp_id"`
	CreatedAt   time.Time `db:"created_at"`
	ChainDealId int64     `db:"chain_deal_id"`
	Sector      int64     `db:"sector_num"`
	Miner       string
}

func (a *WebRPC) MK12StorageDealList(ctx context.Context) ([]StorageDealList, error) {
	var mk12Summaries []StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									md.uuid,
									md.sp_id,
									md.created_at,
									md.chain_deal_id,
									mid.sector_num
									FROM market_mk12_deals md 
										LEFT JOIN market_piece_deal mpd ON mpd.id = md.uuid AND mpd.sp_id = md.sp_id 
									WHERE mpd.boost_deal = TRUE AND mpd.legacy_deal = FALSE;`)

	if err != nil {
		return nil, err
	}

	minerMap := make(map[int64]address.Address)
	for _, s := range mk12Summaries {
		if addr, ok := minerMap[s.MinerID]; ok {
			s.Miner = addr.String()
			continue
		}

		addr, err := address.NewIDAddress(uint64(s.MinerID))
		if err != nil {
			return nil, err
		}
		s.Miner = addr.String()
	}

	return mk12Summaries, nil

}

func (a *WebRPC) LegacyStorageDealList(ctx context.Context) (string, error) {
	var mk12Summaries []StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									signed_proposal_cid AS uuid,
									sp_id,
									created_at,
									chain_deal_id,
									sector_num, 
									FROM market_legacy_deals;`)

	if err != nil {
		return "", err
	}

	minerMap := make(map[int64]address.Address)
	for _, s := range mk12Summaries {
		if addr, ok := minerMap[s.MinerID]; ok {
			s.Miner = addr.String()
			continue
		}

		addr, err := address.NewIDAddress(uint64(s.MinerID))
		if err != nil {
			return "", err
		}
		s.Miner = addr.String()
	}

	x := new(bytes.Buffer)
	//x := bufio.NewWriter()

	err = json.NewEncoder(x).Encode(map[string]any{"data": mk12Summaries})
	if err != nil {
		return "", err
	}

	return x.String(), nil

}
