package webrpc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
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
	Deal     MK12Deal          `json:"deal"`
	Pipeline *MK12DealPipeline `json:"pipeline,omitempty"`
}

func (a *WebRPC) MK12DealDetail(ctx context.Context, pieceCid string) ([]MK12DealDetailEntry, error) {
	var mk12Deals []MK12Deal
	err := a.deps.DB.Select(ctx, &mk12Deals, `
        SELECT
            uuid,
            sp_id,
            created_at,
            signed_proposal_cid,
            proposal_signature,
            proposal,
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
