package webrpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/types"
)

type StorageAsk struct {
	SpID          int64 `db:"sp_id"`
	Price         int64 `db:"price"`
	VerifiedPrice int64 `db:"verified_price"`
	MinSize       int64 `db:"min_size"`
	MaxSize       int64 `db:"max_size"`
	CreatedAt     int64 `db:"created_at"`
	Expiry        int64 `db:"expiry"`
	Sequence      int64 `db:"sequence"`
}

func (a *WebRPC) GetStorageAsk(ctx context.Context, spID int64) (*StorageAsk, error) {
	var asks []StorageAsk
	err := a.deps.DB.Select(ctx, &asks, `
        SELECT sp_id, price, verified_price, min_size, max_size, created_at, expiry, sequence
        FROM market_mk12_storage_ask
        WHERE sp_id = $1
    `, spID)
	if err != nil {
		return nil, err
	}
	if len(asks) == 0 {
		return nil, fmt.Errorf("no storage ask found for sp_id %d", spID)
	}
	return &asks[0], nil
}

func (a *WebRPC) SetStorageAsk(ctx context.Context, ask *StorageAsk) error {
	err := a.deps.DB.QueryRow(ctx, `
        INSERT INTO market_mk12_storage_ask (
            sp_id, price, verified_price, min_size, max_size, created_at, expiry, sequence
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, 0
        )
        ON CONFLICT (sp_id) DO UPDATE SET
            price = EXCLUDED.price,
            verified_price = EXCLUDED.verified_price,
            min_size = EXCLUDED.min_size,
            max_size = EXCLUDED.max_size,
            created_at = EXCLUDED.created_at,
            expiry = EXCLUDED.expiry,
            sequence = market_mk12_storage_ask.sequence + 1
        RETURNING sequence
    `, ask.SpID, ask.Price, ask.VerifiedPrice, ask.MinSize, ask.MaxSize, ask.CreatedAt, ask.Expiry).Scan(&ask.Sequence)
	if err != nil {
		return fmt.Errorf("failed to insert/update storage ask: %w", err)
	}

	return nil
}

type MK12Pipeline struct {
	UUID           string     `db:"uuid" json:"uuid"`
	SpID           int64      `db:"sp_id" json:"sp_id"`
	Started        bool       `db:"started" json:"started"`
	PieceCid       string     `db:"piece_cid" json:"piece_cid"`
	PieceSize      int64      `db:"piece_size" json:"piece_size"`
	RawSize        *int64     `db:"raw_size" json:"raw_size"`
	Offline        bool       `db:"offline" json:"offline"`
	URL            *string    `db:"url" json:"url"`
	Headers        []byte     `db:"headers" json:"headers"`
	CommTaskID     *int64     `db:"commp_task_id" json:"commp_task_id"`
	AfterCommp     bool       `db:"after_commp" json:"after_commp"`
	PSDTaskID      *int64     `db:"psd_task_id" json:"psd_task_id"`
	AfterPSD       bool       `db:"after_psd" json:"after_psd"`
	PSDWaitTime    *time.Time `db:"psd_wait_time" json:"psd_wait_time"`
	FindDealTaskID *int64     `db:"find_deal_task_id" json:"find_deal_task_id"`
	AfterFindDeal  bool       `db:"after_find_deal" json:"after_find_deal"`
	Sector         *int64     `db:"sector" json:"sector"`
	Offset         *int64     `db:"sector_offset" json:"sector_offset"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	Indexed        bool       `db:"indexed" json:"indexed"`
	Announce       bool       `db:"announce" json:"announce"`
	Complete       bool       `db:"complete" json:"complete"`
	Miner          string     `json:"miner"`
}

func (a *WebRPC) GetDealPipelines(ctx context.Context, limit int, offset int) ([]MK12Pipeline, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var pipelines []MK12Pipeline
	err := a.deps.DB.Select(ctx, &pipelines, `
        SELECT
            uuid,
            sp_id,
            started,
            piece_cid,
            piece_size,
            raw_size,
            offline,
            url,
            headers,
            commp_task_id,
            after_commp,
            psd_task_id,
            after_psd,
            psd_wait_time,
            find_deal_task_id,
            after_find_deal,
            sector,
            sector_offset,
            created_at,
            indexed,
            announce,
            complete
        FROM market_mk12_deal_pipeline
        ORDER BY created_at DESC
        LIMIT $1 OFFSET $2
    `, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal pipelines: %w", err)
	}

	minerMap := make(map[int64]address.Address)
	for _, s := range pipelines {
		if addr, ok := minerMap[s.SpID]; ok {
			s.Miner = addr.String()
			continue
		}

		addr, err := address.NewIDAddress(uint64(s.SpID))
		if err != nil {
			return nil, xerrors.Errorf("failed to parse the miner ID: %w", err)
		}
		s.Miner = addr.String()
	}

	return pipelines, nil
}

type StorageDealSummary struct {
	ID                string         `db:"uuid" json:"id"`
	MinerID           int64          `db:"sp_id" json:"sp_id"`
	Sector            int64          `db:"sector_num" json:"sector"`
	CreatedAt         time.Time      `db:"created_at" json:"created_at"`
	SignedProposalCid string         `db:"signed_proposal_cid" json:"signed_proposal_cid"`
	Offline           bool           `db:"offline" json:"offline"`
	Verified          bool           `db:"verified" json:"verified"`
	StartEpoch        int64          `db:"start_epoch" json:"start_epoch"`
	EndEpoch          int64          `db:"end_epoch" json:"end_epoch"`
	ClientPeerId      string         `db:"client_peer_id" json:"client_peer_id"`
	ChainDealId       int64          `db:"chain_deal_id" json:"chain_deal_id"`
	PublishCid        string         `db:"publish_cid" json:"publish_cid"`
	PieceCid          string         `db:"piece_cid" json:"piece_cid"`
	PieceSize         int64          `db:"piece_size" json:"piece_size"`
	FastRetrieval     bool           `db:"fast_retrieval" json:"fast_retrieval"`
	AnnounceToIpni    bool           `db:"announce_to_ipni" json:"announce_to_ipni"`
	Url               sql.NullString `db:"url"`
	URLS              string         `json:"url"`
	Header            []byte         `db:"url_headers"`
	UrlHeaders        http.Header    `json:"url_headers"`
	DBError           sql.NullString `db:"error"`
	Error             string         `json:"error"`
	Miner             string         `json:"miner"`
	IsLegacy          bool           `json:"is_legacy"`
	Indexed           bool           `db:"indexed" json:"indexed"`
}

func (a *WebRPC) StorageDealInfo(ctx context.Context, deal string) (*StorageDealSummary, error) {

	var isLegacy bool
	var pcid cid.Cid

	id, err := uuid.Parse(deal)
	if err != nil {
		p, perr := cid.Parse(deal)
		if perr != nil {
			return &StorageDealSummary{}, xerrors.Errorf("failed to parse the deal ID: %w and %w", err, perr)
		}
		isLegacy = true
		pcid = p
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
									mpd.sector_num,
									mpm.indexed
									FROM market_mk12_deals md 
										LEFT JOIN market_piece_deal mpd ON mpd.id = md.uuid AND mpd.sp_id = md.sp_id
										LEFT JOIN market_piece_metadata mpm ON mpm.piece_cid = md.piece_cid
									WHERE md.uuid = $1 AND mpd.boost_deal = TRUE AND mpd.legacy_deal = FALSE;`, id.String())

		if err != nil {
			return &StorageDealSummary{}, err
		}

		if len(summaries) == 0 {
			return nil, xerrors.Errorf("No such deal found in database: %s", id.String())
		}

		d := summaries[0]
		d.IsLegacy = isLegacy

		addr, err := address.NewIDAddress(uint64(d.MinerID))
		if err != nil {
			return &StorageDealSummary{}, err
		}

		if d.Header != nil {
			var h http.Header
			err = json.Unmarshal(d.Header, &h)
			if err != nil {
				return &StorageDealSummary{}, err
			}
			d.UrlHeaders = h
		}

		if !d.Url.Valid {
			d.URLS = ""
		} else {
			d.URLS = d.Url.String
		}

		if !d.DBError.Valid {
			d.Error = ""
		} else {
			d.Error = d.DBError.String
		}

		d.Miner = addr.String()

		return &d, nil
	}

	var summaries []StorageDealSummary
	err = a.deps.DB.Select(ctx, &summaries, `SELECT 
									'' AS uuid,
									sp_id,
									created_at,
									signed_proposal_cid,
									offline,
									verified,
									start_epoch,
									end_epoch,
									client_peer_id,
									chain_deal_id,
									publish_cid,
									piece_cid,
									piece_size,
									fast_retrieval,
									FALSE AS announce_to_ipni,
									'' AS url,
									'{}' AS url_headers,
									'' AS error,
									sector_num,
									FALSE AS indexed
									FROM market_legacy_deals
									WHERE signed_proposal_cid = $1`, pcid.String())

	if err != nil {
		return &StorageDealSummary{}, err
	}

	if len(summaries) == 0 {
		return nil, xerrors.Errorf("No such deal found in database :%s", pcid.String())
	}

	d := summaries[0]
	d.IsLegacy = isLegacy

	addr, err := address.NewIDAddress(uint64(d.MinerID))
	if err != nil {
		return &StorageDealSummary{}, err
	}

	d.Miner = addr.String()

	return &d, nil
}

type StorageDealList struct {
	ID          string    `db:"uuid" json:"id"`
	MinerID     int64     `db:"sp_id" json:"sp_id"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	ChainDealId int64     `db:"chain_deal_id" json:"chain_deal_id"`
	Sector      int64     `db:"sector_num" json:"sector"`
	Miner       string    `json:"miner"`
}

func (a *WebRPC) MK12StorageDealList(ctx context.Context, limit int, offset int) ([]StorageDealList, error) {
	var mk12Summaries []StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									md.uuid,
									md.sp_id,
									md.created_at,
									md.chain_deal_id,
									mpd.sector_num
									FROM market_mk12_deals md 
										LEFT JOIN market_piece_deal mpd ON mpd.id = md.uuid AND mpd.sp_id = md.sp_id 
									WHERE mpd.boost_deal = TRUE AND mpd.legacy_deal = FALSE
									ORDER BY md.created_at DESC
											LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal list: %w", err)
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

func (a *WebRPC) LegacyStorageDealList(ctx context.Context, limit int, offset int) ([]StorageDealList, error) {
	var mk12Summaries []StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									signed_proposal_cid AS uuid,
									sp_id,
									created_at,
									chain_deal_id,
									sector_num 
									FROM market_legacy_deals
									ORDER BY created_at DESC
									LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal list: %w", err)
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

type WalletBalances struct {
	Address string `json:"address"`
	Balance string `json:"balance"`
}

type MarketBalanceStatus struct {
	Miner         string           `json:"miner"`
	MarketBalance string           `json:"market_balance"`
	Balances      []WalletBalances `json:"balances"`
}

func (a *WebRPC) MarketBalance(ctx context.Context) ([]MarketBalanceStatus, error) {
	var ret []MarketBalanceStatus

	var miners []address.Address

	err := forEachConfig(a, func(name string, info minimalActorInfo) error {
		for _, aset := range info.Addresses {
			for _, addr := range aset.MinerAddresses {
				maddr, err := address.NewFromString(addr)
				if err != nil {
					return xerrors.Errorf("parsing address: %w", err)
				}
				miners = append(miners, maddr)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, m := range miners {
		balance, err := a.deps.Chain.StateMarketBalance(ctx, m, types.EmptyTSK)
		if err != nil {
			return nil, err
		}
		avail := types.FIL(big.Sub(balance.Escrow, balance.Locked))

		mb := MarketBalanceStatus{
			Miner:         m.String(),
			MarketBalance: avail.String(),
		}

		for _, w := range a.deps.As.MinerMap[m].DealPublishControl {
			ac, err := a.deps.Chain.StateGetActor(ctx, w, types.EmptyTSK)
			if err != nil {
				return nil, err
			}
			mb.Balances = append(mb.Balances, WalletBalances{
				Address: w.String(),
				Balance: types.FIL(ac.Balance).String(),
			})
		}

		ret = append(ret, mb)
	}

	return ret, nil
}

func (a *WebRPC) MoveBalanceToEscrow(ctx context.Context, miner string, amount string, wallet string) (string, error) {
	maddr, err := address.NewFromString(miner)
	if err != nil {
		return "", xerrors.Errorf("failed parse the miner address :%w", err)
	}

	addr, err := address.NewFromString(wallet)
	if err != nil {
		return "", xerrors.Errorf("parsing wallet address: %w", err)
	}

	amts, err := types.ParseFIL(amount)
	if err != nil {
		return "", xerrors.Errorf("failed to parse the input amount: %w", err)
	}

	amt := abi.TokenAmount(amts)

	params, err := actors.SerializeParams(&maddr)
	if err != nil {
		return "", xerrors.Errorf("failed to serialize the parameters: %w", err)
	}

	maxfee, err := types.ParseFIL("0.5 FIL")
	if err != nil {
		return "", xerrors.Errorf("failed to parse the maximum fee: %w", err)
	}

	msp := &lapi.MessageSendSpec{
		MaxFee: abi.TokenAmount(maxfee),
	}

	w, err := a.deps.Chain.StateGetActor(ctx, addr, types.EmptyTSK)
	if err != nil {
		return "", err
	}
	if w.Balance.LessThan(amt) {
		return "", xerrors.Errorf("Wallet balance %s is lower than specified amount %s", w.Balance.String(), amt.String())

	}

	msg := &types.Message{
		To:     market.Address,
		From:   addr,
		Value:  amt,
		Method: market.Methods.AddBalance,
		Params: params,
	}

	smsg, err := a.deps.Chain.MpoolPushMessage(ctx, msg, msp)
	if err != nil {
		return "", xerrors.Errorf("moving %s to escrow wallet %s from %s: %w", amt.String(), maddr.String(), addr.String(), err)
	}

	log.Infof("Funds moved to escrow in message %s", smsg.Cid().String())
	return smsg.Cid().String(), nil

}
