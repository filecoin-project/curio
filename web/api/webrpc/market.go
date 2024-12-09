package webrpc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/snadrus/must"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"

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

func (a *WebRPC) GetDealPipelines(ctx context.Context, limit int, offset int) ([]*MK12Pipeline, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var pipelines []*MK12Pipeline
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

	for _, s := range pipelines {
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
	Sector            *int64         `db:"sector_num" json:"sector"`
	CreatedAt         time.Time      `db:"created_at" json:"created_at"`
	SignedProposalCid string         `db:"signed_proposal_cid" json:"signed_proposal_cid"`
	Offline           bool           `db:"offline" json:"offline"`
	Verified          bool           `db:"verified" json:"verified"`
	StartEpoch        int64          `db:"start_epoch" json:"start_epoch"`
	EndEpoch          int64          `db:"end_epoch" json:"end_epoch"`
	ClientPeerId      string         `db:"client_peer_id" json:"client_peer_id"`
	ChainDealId       *int64         `db:"chain_deal_id" json:"chain_deal_id"`
	PublishCid        *string        `db:"publish_cid" json:"publish_cid"`
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
	Indexed           *bool          `db:"indexed" json:"indexed"`
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
									WHERE md.uuid = $1`, id.String())

		if err != nil {
			return &StorageDealSummary{}, xerrors.Errorf("select deal summary: %w", err)
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
									FALSE as offline,
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
	ID        string    `db:"uuid" json:"id"`
	MinerID   int64     `db:"sp_id" json:"sp_id"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	PieceCid  string    `db:"piece_cid" json:"piece_cid"`
	PieceSize int64     `db:"piece_size" json:"piece_size"`
	Complete  bool      `db:"complete" json:"complete"`
	Miner     string    `json:"miner"`
}

func (a *WebRPC) MK12StorageDealList(ctx context.Context, limit int, offset int) ([]*StorageDealList, error) {
	var mk12Summaries []*StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									md.uuid,
									md.sp_id,
									md.created_at,
									md.piece_cid,
									md.piece_size,
									coalesce(mm12dp.complete, true) as complete
									FROM market_mk12_deals md
									LEFT JOIN market_mk12_deal_pipeline mm12dp ON md.uuid = mm12dp.uuid
									ORDER BY created_at DESC
											LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal list: %w", err)
	}

	for i := range mk12Summaries {
		addr, err := address.NewIDAddress(uint64(mk12Summaries[i].MinerID))
		if err != nil {
			return nil, err
		}
		mk12Summaries[i].Miner = addr.String()
	}
	return mk12Summaries, nil

}

func (a *WebRPC) LegacyStorageDealList(ctx context.Context, limit int, offset int) ([]StorageDealList, error) {
	var mk12Summaries []StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									signed_proposal_cid AS uuid,
									sp_id,
									created_at,
									piece_cid,
									piece_size 
									FROM market_legacy_deals
									ORDER BY created_at DESC
									LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal list: %w", err)
	}

	for i := range mk12Summaries {
		addr, err := address.NewIDAddress(uint64(mk12Summaries[i].MinerID))
		if err != nil {
			return nil, err
		}
		mk12Summaries[i].Miner = addr.String()
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

type PieceDeal struct {
	ID          string `db:"id" json:"id"`
	BoostDeal   bool   `db:"boost_deal" json:"boost_deal"`
	LegacyDeal  bool   `db:"legacy_deal" json:"legacy_deal"`
	SpId        int64  `db:"sp_id" json:"sp_id"`
	ChainDealId int64  `db:"chain_deal_id" json:"chain_deal_id"`
	Sector      int64  `db:"sector_num" json:"sector"`
	Offset      int64  `db:"piece_offset" json:"offset"`
	Length      int64  `db:"piece_length" json:"length"`
	RawSize     int64  `db:"raw_size" json:"raw_size"`
	Miner       string `json:"miner"`
}

type PieceInfo struct {
	PieceCid  string       `json:"piece_cid"`
	Size      int64        `json:"size"`
	CreatedAt time.Time    `json:"created_at"`
	Indexed   bool         `json:"indexed"`
	IndexedAT time.Time    `json:"indexed_at"`
	IPNIAd    string       `json:"ipni_ad"`
	Deals     []*PieceDeal `json:"deals"`
}

func (a *WebRPC) PieceInfo(ctx context.Context, pieceCid string) (*PieceInfo, error) {
	piece, err := cid.Parse(pieceCid)
	if err != nil {
		return nil, err
	}

	ret := &PieceInfo{}

	err = a.deps.DB.QueryRow(ctx, `SELECT created_at, indexed, indexed_at FROM market_piece_metadata WHERE piece_cid = $1`, piece.String()).Scan(&ret.CreatedAt, &ret.Indexed, &ret.IndexedAT)
	if err != nil && err != pgx.ErrNoRows {
		return nil, xerrors.Errorf("failed to get piece metadata: %w", err)
	}

	pieceDeals := []*PieceDeal{}

	err = a.deps.DB.Select(ctx, &pieceDeals, `SELECT 
														id, 
														boost_deal, 
														legacy_deal, 
														chain_deal_id, 
														sp_id, 
														sector_num, 
														piece_offset, 
														piece_length, 
														raw_size 
													FROM market_piece_deal
													WHERE piece_cid = $1`, piece.String())
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece deals: %w", err)
	}

	for i := range pieceDeals {
		addr, err := address.NewIDAddress(uint64(pieceDeals[i].SpId))
		if err != nil {
			return nil, err
		}
		pieceDeals[i].Miner = addr.String()
		ret.Size = pieceDeals[i].Length
	}
	ret.Deals = pieceDeals
	ret.PieceCid = piece.String()

	pi := abi.PieceInfo{
		PieceCID: piece,
		Size:     abi.PaddedPieceSize(ret.Size),
	}

	b := new(bytes.Buffer)

	err = pi.MarshalCBOR(b)
	if err != nil {
		return nil, xerrors.Errorf("failed to marshal piece info: %w", err)
	}

	// Get only the latest Ad
	var ipniAd string
	err = a.deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, b.Bytes()).Scan(&ipniAd)
	if err != nil && err != pgx.ErrNoRows {
		return nil, xerrors.Errorf("failed to get deal ID by piece CID: %w", err)
	}

	ret.IPNIAd = ipniAd
	return ret, nil
}

type ParkedPieceState struct {
	ID              int64            `db:"id" json:"id"`
	PieceCID        string           `db:"piece_cid" json:"piece_cid"`
	PiecePaddedSize int64            `db:"piece_padded_size" json:"piece_padded_size"`
	PieceRawSize    int64            `db:"piece_raw_size" json:"piece_raw_size"`
	Complete        bool             `db:"complete" json:"complete"`
	CreatedAt       time.Time        `db:"created_at" json:"created_at"`
	TaskID          sql.NullInt64    `db:"task_id" json:"task_id"`
	CleanupTaskID   sql.NullInt64    `db:"cleanup_task_id" json:"cleanup_task_id"`
	Refs            []ParkedPieceRef `json:"refs"`
}

type ParkedPieceRef struct {
	RefID       int64           `db:"ref_id" json:"ref_id"`
	PieceID     int64           `db:"piece_id" json:"piece_id"`
	DataURL     sql.NullString  `db:"data_url" json:"data_url"`
	DataHeaders json.RawMessage `db:"data_headers" json:"data_headers"`
}

// PieceParkStates retrieves the park states for a given piece CID
func (a *WebRPC) PieceParkStates(ctx context.Context, pieceCID string) (*ParkedPieceState, error) {
	var pps ParkedPieceState

	// Query the parked_pieces table
	err := a.deps.DB.QueryRow(ctx, `
        SELECT id, created_at, piece_cid, piece_padded_size, piece_raw_size, complete, task_id, cleanup_task_id
        FROM parked_pieces WHERE piece_cid = $1
    `, pieceCID).Scan(
		&pps.ID, &pps.CreatedAt, &pps.PieceCID, &pps.PiecePaddedSize, &pps.PieceRawSize,
		&pps.Complete, &pps.TaskID, &pps.CleanupTaskID,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query parked piece: %w", err)
	}

	// Query the parked_piece_refs table for references
	rows, err := a.deps.DB.Query(ctx, `
        SELECT ref_id, piece_id, data_url, data_headers
        FROM parked_piece_refs WHERE piece_id = $1
    `, pps.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query parked piece refs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ref ParkedPieceRef
		var dataHeaders []byte
		err := rows.Scan(&ref.RefID, &ref.PieceID, &ref.DataURL, &dataHeaders)
		if err != nil {
			return nil, fmt.Errorf("failed to scan parked piece ref: %w", err)
		}
		ref.DataHeaders = json.RawMessage(dataHeaders)
		pps.Refs = append(pps.Refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over parked piece refs: %w", err)
	}

	return &pps, nil
}

type PieceSummary struct {
	Total       int64     `db:"total" json:"total"`
	Indexed     int64     `db:"indexed" json:"indexed"`
	Announced   int64     `db:"announced" json:"announced"`
	LastUpdated time.Time `db:"last_updated" json:"last_updated"`
}

func (a *WebRPC) PieceSummary(ctx context.Context) (*PieceSummary, error) {
	s := PieceSummary{}
	err := a.deps.DB.QueryRow(ctx, `SELECT total, indexed, announced, last_updated FROM piece_summary`).Scan(&s.Total, &s.Indexed, &s.Announced, &s.LastUpdated)
	if err != nil {
		return nil, xerrors.Errorf("failed to query piece summary: %w", err)
	}
	return &s, nil
}

// MK12Deal represents a record from market_mk12_deals table
type MK12Deal struct {
	UUID              string          `db:"uuid" json:"uuid"`
	SpId              int64           `db:"sp_id" json:"sp_id"`
	CreatedAt         time.Time       `db:"created_at" json:"created_at"`
	SignedProposalCid string          `db:"signed_proposal_cid" json:"signed_proposal_cid"`
	ProposalSignature []byte          `db:"proposal_signature" json:"proposal_signature"`
	Proposal          json.RawMessage `db:"proposal" json:"proposal"`
	ProposalCid       string          `db:"proposal_cid" json:"proposal_cid"`
	Offline           bool            `db:"offline" json:"offline"`
	Verified          bool            `db:"verified" json:"verified"`
	StartEpoch        int64           `db:"start_epoch" json:"start_epoch"`
	EndEpoch          int64           `db:"end_epoch" json:"end_epoch"`
	ClientPeerId      string          `db:"client_peer_id" json:"client_peer_id"`
	ChainDealId       sql.NullInt64   `db:"chain_deal_id" json:"chain_deal_id"`
	PublishCid        sql.NullString  `db:"publish_cid" json:"publish_cid"`
	PieceCid          string          `db:"piece_cid" json:"piece_cid"`
	PieceSize         int64           `db:"piece_size" json:"piece_size"`
	FastRetrieval     bool            `db:"fast_retrieval" json:"fast_retrieval"`
	AnnounceToIPNI    bool            `db:"announce_to_ipni" json:"announce_to_ipni"`
	URL               sql.NullString  `db:"url" json:"url"`
	URLHeaders        json.RawMessage `db:"url_headers" json:"url_headers"`
	Error             sql.NullString  `db:"error" json:"error"`

	Addr string `db:"-" json:"addr"`
}

// MK12DealPipeline represents a record from market_mk12_deal_pipeline table
type MK12DealPipeline struct {
	UUID              string          `db:"uuid" json:"uuid"`
	SpId              int64           `db:"sp_id" json:"sp_id"`
	Started           sql.NullBool    `db:"started" json:"started"`
	PieceCid          string          `db:"piece_cid" json:"piece_cid"`
	PieceSize         int64           `db:"piece_size" json:"piece_size"`
	RawSize           sql.NullInt64   `db:"raw_size" json:"raw_size"`
	Offline           bool            `db:"offline" json:"offline"`
	URL               sql.NullString  `db:"url" json:"url"`
	Headers           json.RawMessage `db:"headers" json:"headers"`
	CommpTaskId       sql.NullInt64   `db:"commp_task_id" json:"commp_task_id"`
	AfterCommp        sql.NullBool    `db:"after_commp" json:"after_commp"`
	PsdTaskId         sql.NullInt64   `db:"psd_task_id" json:"psd_task_id"`
	AfterPsd          sql.NullBool    `db:"after_psd" json:"after_psd"`
	PsdWaitTime       sql.NullTime    `db:"psd_wait_time" json:"psd_wait_time"`
	FindDealTaskId    sql.NullInt64   `db:"find_deal_task_id" json:"find_deal_task_id"`
	AfterFindDeal     sql.NullBool    `db:"after_find_deal" json:"after_find_deal"`
	Sector            sql.NullInt64   `db:"sector" json:"sector"`
	RegSealProof      sql.NullInt64   `db:"reg_seal_proof" json:"reg_seal_proof"`
	SectorOffset      sql.NullInt64   `db:"sector_offset" json:"sector_offset"`
	Sealed            sql.NullBool    `db:"sealed" json:"sealed"`
	ShouldIndex       sql.NullBool    `db:"should_index" json:"should_index"`
	IndexingCreatedAt sql.NullTime    `db:"indexing_created_at" json:"indexing_created_at"`
	IndexingTaskId    sql.NullInt64   `db:"indexing_task_id" json:"indexing_task_id"`
	Indexed           sql.NullBool    `db:"indexed" json:"indexed"`
	Announce          sql.NullBool    `db:"announce" json:"announce"`
	Complete          bool            `db:"complete" json:"complete"`
	CreatedAt         time.Time       `db:"created_at" json:"created_at"`
}

// MK12DealDetailEntry combines a deal and its pipeline
type MK12DealDetailEntry struct {
	Deal     *MK12Deal         `json:"deal"`
	Pipeline *MK12DealPipeline `json:"pipeline,omitempty"`
}

func (a *WebRPC) MK12DealDetail(ctx context.Context, pieceCid string) ([]MK12DealDetailEntry, error) {
	var mk12Deals []*MK12Deal
	err := a.deps.DB.Select(ctx, &mk12Deals, `
        SELECT
            uuid,
            sp_id,
            created_at,
            signed_proposal_cid,
            proposal_signature,
            proposal,
            proposal_cid,
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
            announce_to_ipni,
            url,
            url_headers,
            error
        FROM market_mk12_deals
        WHERE piece_cid = $1
    `, pieceCid)
	if err != nil {
		return nil, err
	}

	// Collect UUIDs from deals
	uuids := make([]string, len(mk12Deals))
	for i, deal := range mk12Deals {
		deal.Addr = must.One(address.NewIDAddress(uint64(deal.SpId))).String()

		uuids[i] = deal.UUID
	}

	// Fetch pipelines matching the UUIDs
	var pipelines []MK12DealPipeline
	if len(uuids) > 0 {
		err = a.deps.DB.Select(ctx, &pipelines, `
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
                reg_seal_proof,
                sector_offset,
                sealed,
                should_index,
                indexing_created_at,
                indexing_task_id,
                indexed,
                announce,
                complete,
                created_at
            FROM market_mk12_deal_pipeline
            WHERE uuid = ANY($1)
        `, uuids)
		if err != nil {
			return nil, err
		}
	}

	pipelineMap := make(map[string]MK12DealPipeline)
	for _, pipeline := range pipelines {
		pipeline := pipeline
		pipelineMap[pipeline.UUID] = pipeline
	}

	var entries []MK12DealDetailEntry
	for _, deal := range mk12Deals {
		entry := MK12DealDetailEntry{
			Deal: deal,
		}
		if pipeline, exists := pipelineMap[deal.UUID]; exists {
			entry.Pipeline = &pipeline
		} else {
			entry.Pipeline = nil // Pipeline may not exist for processed and active deals
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func firstOrZero[T any](a []T) T {
	if len(a) == 0 {
		return *new(T)
	}
	return a[0]
}

func (a *WebRPC) MK12DealPipelineRemove(ctx context.Context, uuid string) error {
	_, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// First, get deal_pipeline.url, task_ids, and sector values
		var (
			url    string
			sector sql.NullInt64

			commpTaskID    sql.NullInt64
			psdTaskID      sql.NullInt64
			findDealTaskID sql.NullInt64
			indexingTaskID sql.NullInt64
		)

		err = tx.QueryRow(`SELECT url, sector, commp_task_id, psd_task_id, find_deal_task_id, indexing_task_id
			FROM market_mk12_deal_pipeline WHERE uuid = $1`, uuid).Scan(
			&url, &sector, &commpTaskID, &psdTaskID, &findDealTaskID, &indexingTaskID,
		)
		if err != nil {
			if err == sql.ErrNoRows {
				return false, fmt.Errorf("no deal pipeline found with uuid %s", uuid)
			}
			return false, err
		}

		// Collect non-null task IDs
		var taskIDs []int64
		if commpTaskID.Valid {
			taskIDs = append(taskIDs, commpTaskID.Int64)
		}
		if psdTaskID.Valid {
			taskIDs = append(taskIDs, psdTaskID.Int64)
		}
		if findDealTaskID.Valid {
			taskIDs = append(taskIDs, findDealTaskID.Int64)
		}
		if indexingTaskID.Valid {
			taskIDs = append(taskIDs, indexingTaskID.Int64)
		}

		// Check if any tasks are still running
		if len(taskIDs) > 0 {
			var runningTasks int
			err = tx.QueryRow(`SELECT COUNT(*) FROM harmony_task WHERE id = ANY($1)`, taskIDs).Scan(&runningTasks)
			if err != nil {
				return false, err
			}
			if runningTasks > 0 {
				return false, fmt.Errorf("cannot remove deal pipeline %s: tasks are still running", uuid)
			}
		}

		// Remove market_mk12_deal_pipeline entry
		_, err = tx.Exec(`DELETE FROM market_mk12_deal_pipeline WHERE uuid = $1`, uuid)
		if err != nil {
			return false, err
		}

		// If sector is null, remove related pieceref
		if !sector.Valid {
			// Extract refID from deal_pipeline.url (format: "pieceref:[refid]")
			const prefix = "pieceref:"
			if strings.HasPrefix(url, prefix) {
				refIDStr := url[len(prefix):]
				refID, err := strconv.ParseInt(refIDStr, 10, 64)
				if err != nil {
					return false, fmt.Errorf("invalid refID in URL: %v", err)
				}
				// Remove from parked_piece_refs where ref_id = refID
				_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
				if err != nil {
					return false, err
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	return err
}

type PipelineFailedStats struct {
	DownloadingFailed int64
	CommPFailed       int64
	PSDFailed         int64
	FindDealFailed    int64
	IndexFailed       int64
}

func (a *WebRPC) PipelineFailedTasksMarket(ctx context.Context) (*PipelineFailedStats, error) {
	// We'll create a similar query, but this time we coalesce the task IDs from harmony_task.
	// If the join fails (no matching harmony_task), all joined fields for that task will be NULL.
	// We detect failure by checking that xxx_task_id IS NOT NULL, after_xxx = false, and that no task record was found in harmony_task.

	const query = `
WITH pipeline_data AS (
    SELECT dp.uuid,
           dp.complete,
           dp.commp_task_id,
           dp.psd_task_id,
           dp.find_deal_task_id,
           dp.indexing_task_id,
           dp.sector,
           dp.after_commp,
           dp.after_psd,
           dp.after_find_deal,
           pp.task_id AS downloading_task_id
    FROM market_mk12_deal_pipeline dp
    LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid
    WHERE dp.complete = false
),
tasks AS (
    SELECT p.*,
           dt.id AS downloading_tid,
           ct.id AS commp_tid,
           pt.id AS psd_tid,
           ft.id AS find_deal_tid,
           it.id AS index_tid
    FROM pipeline_data p
    LEFT JOIN harmony_task dt ON dt.id = p.downloading_task_id
    LEFT JOIN harmony_task ct ON ct.id = p.commp_task_id
    LEFT JOIN harmony_task pt ON pt.id = p.psd_task_id
    LEFT JOIN harmony_task ft ON ft.id = p.find_deal_task_id
    LEFT JOIN harmony_task it ON it.id = p.indexing_task_id
)
SELECT
    -- Downloading failed:
    -- downloading_task_id IS NOT NULL, after_commp = false (haven't completed commp stage),
    -- and downloading_tid IS NULL (no harmony_task record)
    COUNT(*) FILTER (
        WHERE downloading_task_id IS NOT NULL
          AND after_commp = false
          AND downloading_tid IS NULL
    ) AS downloading_failed,

    -- CommP (verify) failed:
    -- commp_task_id IS NOT NULL, after_commp = false, commp_tid IS NULL
    COUNT(*) FILTER (
        WHERE commp_task_id IS NOT NULL
          AND after_commp = false
          AND commp_tid IS NULL
    ) AS commp_failed,

    -- PSD failed:
    -- psd_task_id IS NOT NULL, after_psd = false, psd_tid IS NULL
    COUNT(*) FILTER (
        WHERE psd_task_id IS NOT NULL
          AND after_psd = false
          AND psd_tid IS NULL
    ) AS psd_failed,

    -- Find_Deal failed:
    -- find_deal_task_id IS NOT NULL, after_find_deal = false, find_deal_tid IS NULL
    COUNT(*) FILTER (
        WHERE find_deal_task_id IS NOT NULL
          AND after_find_deal = false
          AND find_deal_tid IS NULL
    ) AS find_deal_failed,

    -- Index failed:
    -- indexing_task_id IS NOT NULL and if we assume indexing is after find_deal:
    -- If indexing_task_id is set, we are presumably at indexing stage.
    -- If index_tid IS NULL (no task found), then it's failed.
    -- We don't have after_index, so let's assume after_find_deal = true means we've passed publish stage and now at indexing.
    COUNT(*) FILTER (
        WHERE indexing_task_id IS NOT NULL
          AND index_tid IS NULL
          AND after_find_deal = true
    ) AS index_failed
FROM tasks
`

	var c []struct {
		DownloadingFailed int64 `db:"downloading_failed"`
		CommPFailed       int64 `db:"commp_failed"`
		PSDFailed         int64 `db:"psd_failed"`
		FindDealFailed    int64 `db:"find_deal_failed"`
		IndexFailed       int64 `db:"index_failed"`
	}

	err := a.deps.DB.Select(ctx, &c, query)
	if err != nil {
		return nil, xerrors.Errorf("failed to run failed task query: %w", err)
	}

	counts := c[0]

	return &PipelineFailedStats{
		DownloadingFailed: counts.DownloadingFailed,
		CommPFailed:       counts.CommPFailed,
		PSDFailed:         counts.PSDFailed,
		FindDealFailed:    counts.FindDealFailed,
		IndexFailed:       counts.IndexFailed,
	}, nil
}

func (a *WebRPC) BulkRestartFailedMarketTasks(ctx context.Context, taskType string) error {
	didCommit, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rows *harmonydb.Query
		var err error

		switch taskType {
		case "downloading":
			rows, err = tx.Query(`
							SELECT pp.task_id
							FROM market_mk12_deal_pipeline dp
							LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid
							LEFT JOIN harmony_task h ON h.id = pp.task_id
							WHERE dp.complete = false
							  AND pp.task_id IS NOT NULL
							  AND dp.after_commp = false
							  AND h.id IS NULL
						`)
		case "commp":
			rows, err = tx.Query(`
							SELECT dp.commp_task_id
							FROM market_mk12_deal_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.commp_task_id
							WHERE dp.complete = false
							  AND dp.commp_task_id IS NOT NULL
							  AND dp.after_commp = false
							  AND h.id IS NULL
						`)
		case "psd":
			rows, err = tx.Query(`
							SELECT dp.psd_task_id
							FROM market_mk12_deal_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.psd_task_id
							WHERE dp.complete = false
							  AND dp.psd_task_id IS NOT NULL
							  AND dp.after_psd = false
							  AND h.id IS NULL
						`)
		case "find_deal":
			rows, err = tx.Query(`
							SELECT dp.find_deal_task_id
							FROM market_mk12_deal_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.find_deal_task_id
							WHERE dp.complete = false
							  AND dp.find_deal_task_id IS NOT NULL
							  AND dp.after_find_deal = false
							  AND h.id IS NULL
						`)
		case "index":
			rows, err = tx.Query(`
							SELECT dp.indexing_task_id
							FROM market_mk12_deal_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.indexing_task_id
							WHERE dp.complete = false
							  AND dp.indexing_task_id IS NOT NULL
							  AND dp.after_find_deal = true
							  AND h.id IS NULL
						`)
		default:
			return false, fmt.Errorf("unknown task type: %s", taskType)
		}

		if err != nil {
			return false, fmt.Errorf("failed to query failed tasks: %w", err)
		}
		defer rows.Close()

		var taskIDs []int64
		for rows.Next() {
			var tid int64
			if err := rows.Scan(&tid); err != nil {
				return false, fmt.Errorf("failed to scan task_id: %w", err)
			}
			taskIDs = append(taskIDs, tid)
		}

		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("row iteration error: %w", err)
		}

		for _, taskID := range taskIDs {
			var name string
			var posted time.Time
			var result bool
			err = tx.QueryRow(`
							SELECT name, posted, result 
							FROM harmony_task_history 
							WHERE task_id = $1 
							ORDER BY id DESC LIMIT 1
						`, taskID).Scan(&name, &posted, &result)
			if err == pgx.ErrNoRows {
				// No history means can't restart this task
				continue
			} else if err != nil {
				return false, fmt.Errorf("failed to query history: %w", err)
			}

			// If result=true means the task ended successfully, no restart needed
			if result {
				continue
			}

			log.Infow("restarting task", "task_id", taskID, "name", name)

			_, err = tx.Exec(`
							INSERT INTO harmony_task (id, initiated_by, update_time, posted_time, owner_id, added_by, previous_task, name)
							VALUES ($1, NULL, NOW(), $2, NULL, $3, NULL, $4)
						`, taskID, posted, a.deps.MachineID, name)
			if err != nil {
				return false, fmt.Errorf("failed to insert harmony_task for task_id %d: %w", taskID, err)
			}
		}

		// All done successfully, commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return err
	}
	if !didCommit {
		return fmt.Errorf("transaction did not commit")
	}

	return nil
}

func (a *WebRPC) BulkRemoveFailedMarketPipelines(ctx context.Context, taskType string) error {
	didCommit, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rows *harmonydb.Query
		var err error

		// We'll select pipeline fields directly based on the stage conditions
		switch taskType {
		case "downloading":
			rows, err = tx.Query(`
				SELECT dp.uuid, dp.url, dp.sector,
				       dp.commp_task_id, dp.psd_task_id, dp.find_deal_task_id, dp.indexing_task_id
				FROM market_mk12_deal_pipeline dp
				LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid
				LEFT JOIN harmony_task h ON h.id = pp.task_id
				WHERE dp.complete = false
				  AND pp.task_id IS NOT NULL
				  AND dp.after_commp = false
				  AND h.id IS NULL
			`)
		case "commp":
			rows, err = tx.Query(`
				SELECT dp.uuid, dp.url, dp.sector,
				       dp.commp_task_id, dp.psd_task_id, dp.find_deal_task_id, dp.indexing_task_id
				FROM market_mk12_deal_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.commp_task_id
				WHERE dp.complete = false
				  AND dp.commp_task_id IS NOT NULL
				  AND dp.after_commp = false
				  AND h.id IS NULL
			`)
		case "psd":
			rows, err = tx.Query(`
				SELECT dp.uuid, dp.url, dp.sector,
				       dp.commp_task_id, dp.psd_task_id, dp.find_deal_task_id, dp.indexing_task_id
				FROM market_mk12_deal_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.psd_task_id
				WHERE dp.complete = false
				  AND dp.psd_task_id IS NOT NULL
				  AND dp.after_psd = false
				  AND h.id IS NULL
			`)
		case "find_deal":
			rows, err = tx.Query(`
				SELECT dp.uuid, dp.url, dp.sector,
				       dp.commp_task_id, dp.psd_task_id, dp.find_deal_task_id, dp.indexing_task_id
				FROM market_mk12_deal_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.find_deal_task_id
				WHERE dp.complete = false
				  AND dp.find_deal_task_id IS NOT NULL
				  AND dp.after_find_deal = false
				  AND h.id IS NULL
			`)
		case "index":
			rows, err = tx.Query(`
				SELECT dp.uuid, dp.url, dp.sector,
				       dp.commp_task_id, dp.psd_task_id, dp.find_deal_task_id, dp.indexing_task_id
				FROM market_mk12_deal_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.indexing_task_id
				WHERE dp.complete = false
				  AND dp.indexing_task_id IS NOT NULL
				  AND dp.after_find_deal = true
				  AND h.id IS NULL
			`)
		default:
			return false, fmt.Errorf("unknown task type: %s", taskType)
		}

		if err != nil {
			return false, fmt.Errorf("failed to query failed pipelines: %w", err)
		}
		defer rows.Close()

		type pipelineInfo struct {
			uuid           string
			url            string
			sector         sql.NullInt64
			commpTaskID    sql.NullInt64
			psdTaskID      sql.NullInt64
			findDealTaskID sql.NullInt64
			indexingTaskID sql.NullInt64
		}

		var pipelines []pipelineInfo
		for rows.Next() {
			var p pipelineInfo
			if err := rows.Scan(&p.uuid, &p.url, &p.sector, &p.commpTaskID, &p.psdTaskID, &p.findDealTaskID, &p.indexingTaskID); err != nil {
				return false, fmt.Errorf("failed to scan pipeline info: %w", err)
			}
			pipelines = append(pipelines, p)
		}
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("row iteration error: %w", err)
		}

		for _, p := range pipelines {
			// Gather task IDs
			var taskIDs []int64
			if p.commpTaskID.Valid {
				taskIDs = append(taskIDs, p.commpTaskID.Int64)
			}
			if p.psdTaskID.Valid {
				taskIDs = append(taskIDs, p.psdTaskID.Int64)
			}
			if p.findDealTaskID.Valid {
				taskIDs = append(taskIDs, p.findDealTaskID.Int64)
			}
			if p.indexingTaskID.Valid {
				taskIDs = append(taskIDs, p.indexingTaskID.Int64)
			}

			if len(taskIDs) > 0 {
				var runningTasks int
				err = tx.QueryRow(`SELECT COUNT(*) FROM harmony_task WHERE id = ANY($1)`, taskIDs).Scan(&runningTasks)
				if err != nil {
					return false, err
				}
				if runningTasks > 0 {
					// This should not happen if they are failed, but just in case
					return false, fmt.Errorf("cannot remove deal pipeline %s: tasks are still running", p.uuid)
				}
			}

			_, err = tx.Exec(`DELETE FROM market_mk12_deal_pipeline WHERE uuid = $1`, p.uuid)
			if err != nil {
				return false, err
			}

			// If sector is null, remove related pieceref
			if !p.sector.Valid {
				const prefix = "pieceref:"
				if strings.HasPrefix(p.url, prefix) {
					refIDStr := p.url[len(prefix):]
					refID, err := strconv.ParseInt(refIDStr, 10, 64)
					if err != nil {
						return false, fmt.Errorf("invalid refID in URL for pipeline %s: %v", p.uuid, err)
					}
					_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
					if err != nil {
						return false, fmt.Errorf("failed to remove parked_piece_refs for pipeline %s: %w", p.uuid, err)
					}
				}
			}

			log.Infow("removed failed pipeline", "uuid", p.uuid)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return err
	}
	if !didCommit {
		return fmt.Errorf("transaction did not commit")
	}

	return nil
}
