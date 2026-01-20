package webrpc

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
	itype "github.com/filecoin-project/curio/market/ipni/types"
	"github.com/filecoin-project/curio/market/mk20"

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
	Miner         string
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
	addr, err := address.NewIDAddress(uint64(spID))
	if err != nil {
		return nil, err
	}
	asks[0].Miner = addr.String()
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
	// Cache line 1 (bytes 0-64): Hot path - piece identification and early checks
	UUID      string `db:"uuid" json:"uuid"`             // 16 bytes (0-16)
	SpID      int64  `db:"sp_id" json:"sp_id"`           // 8 bytes (16-24)
	PieceCid  string `db:"piece_cid" json:"piece_cid"`   // 16 bytes (24-40)
	PieceSize int64  `db:"piece_size" json:"piece_size"` // 8 bytes (40-48)
	Offline   bool   `db:"offline" json:"offline"`       // 1 byte (48-49) - checked early for download decisions
	Started   bool   `db:"started" json:"started"`       // 1 byte (49-50) - checked early
	// Cache line 2 (bytes 64-128): Task IDs and stage tracking (NullInt64 = 16 bytes)
	CommTaskID     sql.NullInt64 `db:"commp_task_id" json:"commp_task_id"`         // 16 bytes
	PSDTaskID      sql.NullInt64 `db:"psd_task_id" json:"psd_task_id"`             // 16 bytes
	FindDealTaskID sql.NullInt64 `db:"find_deal_task_id" json:"find_deal_task_id"` // 16 bytes
	AfterCommp     bool          `db:"after_commp" json:"after_commp"`             // 1 byte
	AfterPSD       bool          `db:"after_psd" json:"after_psd"`                 // 1 byte
	AfterFindDeal  bool          `db:"after_find_deal" json:"after_find_deal"`     // 1 byte
	// Cache line 3 (bytes 128-192): Sector placement and sizing (NullInt64 = 16 bytes)
	RawSize sql.NullInt64 `db:"raw_size" json:"raw_size"`           // 16 bytes
	Sector  sql.NullInt64 `db:"sector" json:"sector"`               // 16 bytes
	Offset  sql.NullInt64 `db:"sector_offset" json:"sector_offset"` // 16 bytes
	// Cache line 4 (bytes 192-256): Timing information
	PSDWaitTime sql.NullTime `db:"psd_wait_time" json:"psd_wait_time"` // 32 bytes (NullTime)
	CreatedAt   time.Time    `db:"created_at" json:"created_at"`       // 24 bytes
	// Cache line 5+ (bytes 256+): Data URL and larger fields (NullString = 24 bytes)
	URL        sql.NullString `db:"url" json:"url"`         // 24 bytes - only for online deals
	PieceCidV2 string         `db:"-" json:"piece_cid_v2"`  // 16 bytes - computed field
	Miner      string         `json:"miner"`                // 16 bytes - display field
	Headers    []byte         `db:"headers" json:"headers"` // 24 bytes - only for online deals
	// Status bools: rarely checked ones at end
	Indexed  bool `db:"indexed" json:"indexed"`   // checked for indexing
	Announce bool `db:"announce" json:"announce"` // checked for IPNI announce
	Complete bool `db:"complete" json:"complete"` // checked for completion
}

func (a *WebRPC) GetMK12DealPipelines(ctx context.Context, limit int, offset int) ([]*MK12Pipeline, error) {
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
		if s.RawSize.Valid {
			pcid, err := cid.Parse(s.PieceCid)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse v1 piece CID: %w", err)
			}
			pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(s.RawSize.Int64))
			if err != nil {
				return nil, xerrors.Errorf("failed to get commP from piece info: %w", err)
			}
			s.PieceCidV2 = pcid2.String()
		}
	}

	return pipelines, nil
}

type StorageDealSummary struct {
	ID                string         `db:"uuid" json:"id"`
	MinerID           int64          `db:"sp_id" json:"sp_id"`
	Sector            NullInt64      `db:"sector_num" json:"sector"`
	CreatedAt         time.Time      `db:"created_at" json:"created_at"`
	SignedProposalCid string         `db:"signed_proposal_cid" json:"signed_proposal_cid"`
	Offline           bool           `db:"offline" json:"offline"`
	Verified          bool           `db:"verified" json:"verified"`
	StartEpoch        int64          `db:"start_epoch" json:"start_epoch"`
	EndEpoch          int64          `db:"end_epoch" json:"end_epoch"`
	ClientPeerId      string         `db:"client_peer_id" json:"client_peer_id"`
	ChainDealId       NullInt64      `db:"chain_deal_id" json:"chain_deal_id"`
	PublishCid        NullString     `db:"publish_cid" json:"publish_cid"`
	PieceCid          string         `db:"piece_cid" json:"piece_cid"`
	PieceSize         int64          `db:"piece_size" json:"piece_size"`
	RawSize           sql.NullInt64  `db:"raw_size"`
	FastRetrieval     bool           `db:"fast_retrieval" json:"fast_retrieval"`
	AnnounceToIpni    bool           `db:"announce_to_ipni" json:"announce_to_ipni"`
	Url               sql.NullString `db:"url"`
	URLS              string         `json:"url"`
	Header            []byte         `db:"url_headers"`
	UrlHeaders        http.Header    `json:"url_headers"`
	DBError           sql.NullString `db:"error"`
	Error             string         `json:"error"`
	Miner             string         `json:"miner"`
	Indexed           sql.NullBool   `db:"indexed" json:"indexed"`
	IsDDO             bool           `db:"is_ddo" json:"is_ddo"`
	PieceCidV2        string         `json:"piece_cid_v2"`
}

func (a *WebRPC) StorageDealInfo(ctx context.Context, deal string) (*StorageDealSummary, error) {
	id, err := uuid.Parse(deal)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse deal ID: %w", err)
	}
	var summaries []StorageDealSummary
	err = a.deps.DB.Select(ctx, &summaries, `SELECT 
														deal.uuid,
														deal.sp_id,
														deal.created_at,
														deal.signed_proposal_cid,
														deal.offline,
														deal.verified,
														deal.start_epoch,
														deal.end_epoch,
														deal.client_peer_id,
														deal.chain_deal_id,
														deal.publish_cid,
														deal.piece_cid,
														deal.piece_size,
														deal.raw_size,
														deal.fast_retrieval,
														deal.announce_to_ipni,
														deal.url,
														deal.url_headers,
														deal.error,
														mpd.sector_num,
														mpm.indexed,
														deal.is_ddo -- New column indicating whether the deal is from market_direct_deals
													FROM (
														-- Query from market_mk12_deals (default, original table)
														SELECT 
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
															md.raw_size,
															md.fast_retrieval,
															md.announce_to_ipni,
															md.url,
															md.url_headers,
															md.error,
															FALSE AS is_ddo -- Not from market_direct_deals
														FROM market_mk12_deals md
														WHERE md.uuid = $1
													
														UNION ALL
													
														-- Query from market_direct_deals (new table)
														SELECT 
															mdd.uuid,
															mdd.sp_id,
															mdd.created_at,
															'' AS signed_proposal_cid,
															mdd.offline,
															mdd.verified,
															mdd.start_epoch,
															mdd.end_epoch,
															'' AS client_peer_id,
															0 AS chain_deal_id,
															'' AS publish_cid,
															mdd.piece_cid,
															mdd.piece_size,
															mdd.raw_size,
															mdd.fast_retrieval,
															mdd.announce_to_ipni,
															'' AS url,
															'{}' AS url_headers,
															'' AS error,
															TRUE AS is_ddo -- From market_direct_deals
														FROM market_direct_deals mdd
														WHERE mdd.uuid = $1
													) AS deal
													LEFT JOIN market_piece_deal mpd 
														ON mpd.id = deal.uuid AND mpd.sp_id = deal.sp_id
													LEFT JOIN market_piece_metadata mpm 
														ON mpm.piece_cid = deal.piece_cid AND mpm.piece_size = deal.piece_size;
													`, id.String())

	if err != nil {
		return &StorageDealSummary{}, xerrors.Errorf("select deal summary: %w", err)
	}

	if len(summaries) == 0 {
		return nil, xerrors.Errorf("No such deal found in database: %s", id.String())
	}

	d := summaries[0]

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

	if d.RawSize.Valid {
		pcid, err := cid.Parse(d.PieceCid)
		if err != nil {
			return &StorageDealSummary{}, xerrors.Errorf("failed to parse piece CID: %w", err)
		}
		pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(d.RawSize.Int64))
		if err != nil {
			return &StorageDealSummary{}, xerrors.Errorf("failed to get commP from piece info: %w", err)
		}
		d.PieceCidV2 = pcid2.String()
	}

	return &d, nil

}

type StorageDealList struct {
	ID         string     `db:"uuid" json:"id"`
	MinerID    int64      `db:"sp_id" json:"sp_id"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	PieceCidV1 string     `db:"piece_cid" json:"piece_cid"`
	PieceSize  int64      `db:"piece_size" json:"piece_size"`
	RawSize    NullInt64  `db:"raw_size"`
	PieceCidV2 string     `json:"piece_cid_v2"`
	Processed  bool       `db:"processed" json:"processed"`
	Error      NullString `db:"error" json:"error"`
	Miner      string     `json:"miner"`
}

func (a *WebRPC) MK12StorageDealList(ctx context.Context, limit int, offset int) ([]*StorageDealList, error) {
	var mk12Summaries []*StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									md.uuid,
									md.sp_id,
									md.created_at,
									md.piece_cid,
									md.piece_size,
									md.raw_size,
									md.error,
									coalesce(mm12dp.complete, true) as processed
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

		// Find PieceCidV2 only of rawSize is present
		// It will be absent only for Offline deals (mk12, mk12-ddo), waiting for data
		if mk12Summaries[i].RawSize.Valid {
			pcid, err := cid.Parse(mk12Summaries[i].PieceCidV1)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse v1 piece CID: %w", err)
			}
			pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(mk12Summaries[i].RawSize.Int64))
			if err != nil {
				return nil, xerrors.Errorf("failed to get commP from piece info: %w", err)
			}
			mk12Summaries[i].PieceCidV2 = pcid2.String()
		}
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

	for _, m := range lo.Uniq(miners) {
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
	// Cache line 1 (0-64 bytes): Hot path - identification fields used together
	ID    string `db:"id" json:"id"`       // 16 bytes - used with SpId (line 621-623)
	SpId  int64  `db:"sp_id" json:"sp_id"` // 8 bytes - checked early (line 614), used with ID (line 617)
	Miner string `json:"miner"`            // 16 bytes - set based on SpId (line 615, 625)
	// Cache line 2: Additional 8-byte types grouped together
	ChainDealId int64 `db:"chain_deal_id" json:"chain_deal_id"` // 8 bytes
	Sector      int64 `db:"sector_num" json:"sector"`           // 8 bytes
	Length      int64 `db:"piece_length" json:"length"`         // 8 bytes
	RawSize     int64 `db:"raw_size" json:"raw_size"`           // 8 bytes
	// NullInt64 (16 bytes)
	Offset NullInt64 `db:"piece_offset" json:"offset"` // 16 bytes
	// Cache line 3 (64+ bytes): Bools grouped together at the end to minimize padding
	MK20       bool `db:"-" json:"mk20"`                  // Used with ID check (line 621-623) - hot path
	BoostDeal  bool `db:"boost_deal" json:"boost_deal"`   // Less frequently accessed
	LegacyDeal bool `db:"legacy_deal" json:"legacy_deal"` // Less frequently accessed
}

type PieceInfo struct {
	// Cache line 1 (0-64 bytes): Hot path - piece identification used together
	PieceCidv2 string   `json:"piece_cid_v2"` // 16 bytes - used together with PieceCid (line 584)
	PieceCid   string   `json:"piece_cid"`    // 16 bytes - used with PieceCidv2 (line 584)
	Size       int64    `json:"size"`         // 8 bytes - used with PieceCid (line 587, 604)
	IPNIAd     []string `json:"ipni_ads"`     // 24 bytes - used for results
	// Cache line 2 (64+ bytes): Display
	CreatedAt time.Time `json:"created_at"` // 24 bytes - used for display
	IndexedAT time.Time `json:"indexed_at"` // 24 bytes - used for display
	Indexed   bool      `json:"indexed"`    // Used for display
	// Cache line 3
	Deals []PieceDeal `json:"deals"` // 24 bytes - used for results
}

func (a *WebRPC) PieceInfo(ctx context.Context, pieceCid string) (*PieceInfo, error) {
	piece, err := cid.Parse(pieceCid)
	if err != nil {
		return nil, err
	}

	if !commcidv2.IsPieceCidV2(piece) {
		return nil, xerrors.Errorf("invalid piece CID V2: %w", err)
	}

	pcid, rawSize, err := commcid.PieceCidV1FromV2(piece)
	if err != nil {
		return nil, xerrors.Errorf("failed to get pieceCidv1 from piece CID v2: %w", err)
	}

	size := padreader.PaddedSize(rawSize).Padded()

	ret := &PieceInfo{
		PieceCidv2: piece.String(),
		PieceCid:   pcid.String(),
		Size:       int64(size),
	}

	err = a.deps.DB.QueryRow(ctx, `SELECT created_at, indexed, indexed_at FROM market_piece_metadata WHERE piece_cid = $1 AND piece_size = $2`, pcid.String(), size).Scan(&ret.CreatedAt, &ret.Indexed, &ret.IndexedAT)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get piece metadata: %w", err)
	}

	pieceDeals := []PieceDeal{}

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
													WHERE piece_cid = $1 AND piece_length = $2`, pcid.String(), size)
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece deals: %w", err)
	}

	for i := range pieceDeals {
		if pieceDeals[i].SpId == -1 {
			pieceDeals[i].Miner = "PDP"
		} else {
			addr, err := address.NewIDAddress(uint64(pieceDeals[i].SpId))
			if err != nil {
				return nil, err
			}
			_, err = uuid.Parse(pieceDeals[i].ID)
			if err != nil {
				pieceDeals[i].MK20 = true
			}
			pieceDeals[i].Miner = addr.String()
		}
	}
	ret.Deals = pieceDeals

	b := new(bytes.Buffer)

	pi := abi.PieceInfo{
		PieceCID: pcid,
		Size:     size,
	}

	err = pi.MarshalCBOR(b)
	if err != nil {
		return nil, xerrors.Errorf("failed to marshal piece info: %w", err)
	}

	c1 := itype.PdpIpniContext{
		PieceCID: piece,
		Payload:  true,
	}

	c1b, err := c1.Marshal()
	if err != nil {
		return nil, xerrors.Errorf("failed to marshal PDP piece info: %w", err)
	}
	fmt.Printf("C1B: %x", c1b)

	c2 := itype.PdpIpniContext{
		PieceCID: piece,
		Payload:  false,
	}

	c2b, err := c2.Marshal()
	if err != nil {
		return nil, xerrors.Errorf("failed to marshal PDP piece info: %w", err)
	}
	fmt.Printf("C2B: %x", c2b)

	// Get only the latest Ad
	var ipniAd string
	err = a.deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, b.Bytes()).Scan(&ipniAd)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get ad ID by piece CID: %w", err)
	}

	var ipniAdPdp string
	err = a.deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, c1b).Scan(&ipniAdPdp)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get ad ID by piece CID for PDP: %w", err)
	}

	var ipniAdPdp1 string
	err = a.deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, c2b).Scan(&ipniAdPdp1)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get ad ID by piece CID for PDP: %w", err)
	}

	ret.IPNIAd = append(ret.IPNIAd, ipniAd, ipniAdPdp, ipniAdPdp1)
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
	pcid, err := cid.Parse(pieceCID)
	if err != nil {
		return nil, err
	}

	if !commcidv2.IsPieceCidV2(pcid) {
		return nil, xerrors.Errorf("invalid piece CID V2: %w", err)
	}

	pcid1, rawSize, err := commcid.PieceCidV1FromV2(pcid)
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece CID v1 from piece CID v2: %w", err)
	}

	size := padreader.PaddedSize(rawSize).Padded()

	var pps ParkedPieceState

	// Query the parked_pieces table
	err = a.deps.DB.QueryRow(ctx, `
        SELECT id, created_at, piece_cid, piece_padded_size, piece_raw_size, complete, task_id, cleanup_task_id
        FROM parked_pieces WHERE piece_cid = $1 AND piece_padded_size = $2
    `, pcid1.String(), size).Scan(
		&pps.ID, &pps.CreatedAt, &pps.PieceCID, &pps.PiecePaddedSize, &pps.PieceRawSize,
		&pps.Complete, &pps.TaskID, &pps.CleanupTaskID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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

// MK12Deal represents a record from market_mk12_deals or market_direct_deals table
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
	IsDDO             bool            `db:"is_ddo" json:"is_ddo"`

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

// MK20DealPipeline represents a record from market_mk20_ddo_pipeline table
type MK20DDOPipeline struct {
	ID               string         `db:"id" json:"id"`
	SpId             int64          `db:"sp_id" json:"sp_id"`
	Contract         string         `db:"contract" json:"contract"`
	Client           string         `db:"client" json:"client"`
	PieceCidV2       string         `db:"piece_cid_v2" json:"piece_cid_v2"`
	PieceCid         string         `db:"piece_cid" json:"piece_cid"`
	PieceSize        int64          `db:"piece_size" json:"piece_size"`
	RawSize          uint64         `db:"raw_size" json:"raw_size"`
	Offline          bool           `db:"offline" json:"offline"`
	URL              sql.NullString `db:"url" json:"url"`
	Indexing         bool           `db:"indexing" json:"indexing"`
	Announce         bool           `db:"announce" json:"announce"`
	AllocationID     sql.NullInt64  `db:"allocation_id" json:"allocation_id"`
	Duration         int64          `db:"duration" json:"duration"`
	PieceAggregation int            `db:"piece_aggregation" json:"piece_aggregation"`

	Started    bool `db:"started" json:"started"`
	Downloaded bool `db:"downloaded" json:"downloaded"`

	CommpTaskId sql.NullInt64 `db:"commp_task_id" json:"commp_task_id"`
	AfterCommp  bool          `db:"after_commp" json:"after_commp"`

	DealAggregation   int           `db:"deal_aggregation" json:"deal_aggregation"`
	AggregationIndex  int64         `db:"aggr_index" json:"aggr_index"`
	AggregationTaskID sql.NullInt64 `db:"agg_task_id" json:"agg_task_id"`
	Aggregated        bool          `db:"aggregated" json:"aggregated"`

	Sector       sql.NullInt64 `db:"sector" json:"sector"`
	RegSealProof sql.NullInt64 `db:"reg_seal_proof" json:"reg_seal_proof"`
	SectorOffset sql.NullInt64 `db:"sector_offset" json:"sector_offset"`
	Sealed       bool          `db:"sealed" json:"sealed"`

	IndexingCreatedAt sql.NullTime  `db:"indexing_created_at" json:"indexing_created_at"`
	IndexingTaskId    sql.NullInt64 `db:"indexing_task_id" json:"indexing_task_id"`
	Indexed           bool          `db:"indexed" json:"indexed"`

	Complete  bool      `db:"complete" json:"complete"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`

	Miner string `db:"-" json:"miner"`
}

type PieceInfoMK12Deals struct {
	Deal     *MK12Deal         `json:"deal"`
	Pipeline *MK12DealPipeline `json:"mk12_pipeline,omitempty"`
}

type PieceInfoMK20Deals struct {
	Deal        *MK20StorageDeal `json:"deal"`
	DDOPipeline *MK20DDOPipeline `json:"mk20_ddo_pipeline,omitempty"`
	PDPPipeline *MK20PDPPipeline `json:"mk20_pdp_pipeline,omitempty"`
}

// PieceDealDetailEntry combines a deal and its pipeline
type PieceDealDetailEntry struct {
	MK12 []PieceInfoMK12Deals `json:"mk12"`
	MK20 []PieceInfoMK20Deals `json:"mk20"`
}

func (a *WebRPC) PieceDealDetail(ctx context.Context, pieceCid string) (*PieceDealDetailEntry, error) {
	pcid, err := cid.Parse(pieceCid)
	if err != nil {
		return nil, err
	}

	if !commcidv2.IsPieceCidV2(pcid) {
		return nil, xerrors.Errorf("invalid piece CID V2: %w", err)
	}

	pcid1, rawSize, err := commcid.PieceCidV1FromV2(pcid)
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece CID v1 from piece CID v2: %w", err)
	}

	size := padreader.PaddedSize(rawSize).Padded()

	var mk12Deals []*MK12Deal

	err = a.deps.DB.Select(ctx, &mk12Deals, `
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
											error,
											FALSE AS is_ddo
										FROM market_mk12_deals
										WHERE piece_cid = $1 AND piece_size = $2
									
										UNION ALL
									
										SELECT
											uuid,
											sp_id,
											created_at,
											'' AS signed_proposal_cid,       -- Empty string for missing string field
											''::BYTEA AS proposal_signature, -- Empty byte array for missing proposal signature
											'{}'::JSONB AS proposal,         -- Empty JSON object for proposal
											'' AS proposal_cid,              -- Empty string for missing proposal_cid
											offline,
											verified,
											start_epoch,
											end_epoch,
											'' AS client_peer_id,            -- Empty string for missing client_peer_id
											NULL AS chain_deal_id,           -- NULL handled by Go (NullInt64)
											NULL AS publish_cid,             -- NULL handled by Go (NullString)
											piece_cid,
											piece_size,
											fast_retrieval,
											announce_to_ipni,
											NULL AS url,                     -- NULL handled by Go (NullString)
											'{}'::JSONB AS url_headers,      -- Empty JSON object for url_headers
											NULL AS error,                    -- NULL handled by Go (NullString)
										    TRUE AS is_ddo
										FROM market_direct_deals
										WHERE piece_cid = $1 AND piece_size = $2`, pcid1.String(), size)
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
			return nil, xerrors.Errorf("failed to query mk12 pipelines: %w", err)
		}
	}

	pipelineMap := make(map[string]MK12DealPipeline)
	for _, pipeline := range pipelines {
		pipeline := pipeline
		pipelineMap[pipeline.UUID] = pipeline
	}

	var mk20Deals []*mk20.DBDeal
	err = a.deps.DB.Select(ctx, &mk20Deals, `SELECT 
													id, 
													client,
													data,
													ddo_v1,
													retrieval_v1,
													pdp_v1 FROM market_mk20_deal WHERE piece_cid_v2 = $1`, pcid1.String())
	if err != nil {
		return nil, xerrors.Errorf("failed to query mk20 deals: %w", err)
	}

	ids := make([]string, len(mk20Deals))
	mk20deals := make([]*MK20StorageDeal, len(mk20Deals))

	for i, dbdeal := range mk20Deals {
		deal, err := dbdeal.ToDeal()
		if err != nil {
			return nil, err
		}
		ids[i] = deal.Identifier.String()

		var Err NullString

		if len(dbdeal.DDOv1) > 0 && string(dbdeal.DDOv1) != "null" {
			var dddov1 mk20.DBDDOV1
			if err := json.Unmarshal(dbdeal.DDOv1, &dddov1); err != nil {
				return nil, fmt.Errorf("unmarshal ddov1: %w", err)
			}
			if dddov1.Error != "" {
				Err.String = dddov1.Error
				Err.Valid = true
			}
		}

		mk20deals[i] = &MK20StorageDeal{
			Deal:   deal,
			DDOErr: Err,
		}
	}

	var mk20Pipelines []MK20DDOPipeline
	err = a.deps.DB.Select(ctx, &mk20Pipelines, `
										SELECT
										    created_at,
											id,
											sp_id,
											contract,
											client,
											piece_cid_v2,
											piece_cid,
											piece_size,
											raw_size,
											offline,
											url,
											indexing,
											announce,
											allocation_id,
											piece_aggregation,
											started,
											downloaded,
											commp_task_id,
											after_commp,
											deal_aggregation,
											aggr_index,
											agg_task_id,
											aggregated,
											sector,
											reg_seal_proof,
											sector_offset,
											sealed,
											indexing_created_at,
											indexing_task_id,
											indexed,
											complete
										FROM market_mk20_pipeline
										WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, xerrors.Errorf("failed to query mk20 DDO pipelines: %w", err)
	}

	var mk20PDPPipelines []MK20PDPPipeline
	err = a.deps.DB.Select(ctx, &mk20PDPPipelines, `
										SELECT
											created_at,
											id,
											client,
											piece_cid_v2,
											indexing,
											announce,
											announce_payload,
											downloaded,
											commp_task_id,
											after_commp,
											deal_aggregation,
											aggr_index,
											agg_task_id,
											aggregated,
											add_piece_task_id,
											after_add_piece,
											after_add_piece_msg,
											save_cache_task_id,
											after_save_cache,
											indexing_created_at,
											indexing_task_id,
											indexed,
											complete
										FROM pdp_pipeline
										WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, xerrors.Errorf("failed to query mk20 PDP pipelines: %w", err)
	}

	mk20pipelineMap := make(map[string]MK20DDOPipeline)
	for _, pipeline := range mk20Pipelines {
		pipeline := pipeline
		mk20pipelineMap[pipeline.ID] = pipeline
	}

	mk20PDPpipelineMap := make(map[string]MK20PDPPipeline)
	for _, pipeline := range mk20PDPPipelines {
		pipeline := pipeline
		mk20PDPpipelineMap[pipeline.ID] = pipeline
	}

	ret := &PieceDealDetailEntry{}

	for _, deal := range mk12Deals {
		entry := PieceInfoMK12Deals{
			Deal: deal,
		}
		if pipeline, exists := pipelineMap[deal.UUID]; exists {
			entry.Pipeline = &pipeline
		} else {
			entry.Pipeline = nil // Pipeline may not exist for processed and active deals
		}
		ret.MK12 = append(ret.MK12, entry)
	}

	for _, deal := range mk20deals {
		entry := PieceInfoMK20Deals{
			Deal: deal,
		}
		if pipeline, exists := mk20pipelineMap[deal.Deal.Identifier.String()]; exists {
			entry.DDOPipeline = &pipeline
		} else {
			entry.DDOPipeline = nil // Pipeline may not exist for processed and active deals
		}
		if pipeline, exists := mk20PDPpipelineMap[deal.Deal.Identifier.String()]; exists {
			entry.PDPPipeline = &pipeline
		} else {
			entry.PDPPipeline = nil
		}
		if ret.MK20 == nil {
			ret.MK20 = make([]PieceInfoMK20Deals, 0)
		}
		ret.MK20 = append(ret.MK20, entry)
	}

	return ret, nil
}

func firstOrZero[T any](a []T) T {
	if len(a) == 0 {
		return *new(T)
	}
	return a[0]
}

func (a *WebRPC) DealPipelineRemove(ctx context.Context, id string) error {
	_, err := ulid.Parse(id)
	if err != nil {
		_, err = uuid.Parse(id)
		if err != nil {
			return xerrors.Errorf("invalid pipeline id: %w", err)
		}
		return a.mk12DealPipelineRemove(ctx, id)
	}
	return a.mk20DealPipelineRemove(ctx, id)
}

func (a *WebRPC) mk20DealPipelineRemove(ctx context.Context, id string) error {
	_, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var pipelines []struct {
			Url    NullString `db:"url"`
			Sector NullInt64  `db:"sector"`

			CommpTaskID    NullInt64 `db:"commp_task_id"`
			AggrTaskID     NullInt64 `db:"agg_task_id"`
			IndexingTaskID NullInt64 `db:"indexing_task_id"`
		}

		err = tx.Select(&pipelines, `SELECT url, sector, commp_task_id, agg_task_id, indexing_task_id
			FROM market_mk20_pipeline WHERE id = $1`, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, fmt.Errorf("no deal pipeline found with id %s", id)
			}
			return false, err
		}

		if len(pipelines) == 0 {
			return false, fmt.Errorf("no deal pipeline found with id %s", id)
		}

		// Collect non-null task IDs
		var taskIDs []int64
		for _, pipeline := range pipelines {
			if pipeline.CommpTaskID.Valid {
				taskIDs = append(taskIDs, pipeline.CommpTaskID.Int64)
			}
			if pipeline.AggrTaskID.Valid {
				taskIDs = append(taskIDs, pipeline.AggrTaskID.Int64)
			}
			if pipeline.IndexingTaskID.Valid {
				taskIDs = append(taskIDs, pipeline.IndexingTaskID.Int64)
			}
		}

		// Check if any tasks are still running
		if len(taskIDs) > 0 {
			var runningTasks int
			err = tx.QueryRow(`SELECT COUNT(*) FROM harmony_task WHERE id = ANY($1)`, taskIDs).Scan(&runningTasks)
			if err != nil {
				return false, err
			}
			if runningTasks > 0 {
				return false, fmt.Errorf("cannot remove deal pipeline %s: tasks are still running", id)
			}
		}

		//Mark failure for deal
		_, err = tx.Exec(`UPDATE market_mk20_deal SET error = $1 WHERE id = $2`, "Deal pipeline removed by SP", id)
		if err != nil {
			return false, xerrors.Errorf("failed to mark deal %s as failed", id)
		}

		// Remove market_mk20_pipeline entry
		_, err = tx.Exec(`DELETE FROM market_mk20_pipeline WHERE id = $1`, id)
		if err != nil {
			return false, err
		}

		// If sector is null, remove related pieceref
		for _, pipeline := range pipelines {
			if !pipeline.Sector.Valid && pipeline.Url.Valid {
				const prefix = "pieceref:"
				if strings.HasPrefix(pipeline.Url.String, prefix) {
					refIDStr := pipeline.Url.String[len(prefix):]
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
		}
		return true, nil
	}, harmonydb.OptionRetry())
	return err
}

func (a *WebRPC) mk12DealPipelineRemove(ctx context.Context, uuid string) error {
	_, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// First, get deal_pipeline.url, task_ids, and sector values
		var (
			url    NullString
			sector NullInt64

			commpTaskID    NullInt64
			psdTaskID      NullInt64
			findDealTaskID NullInt64
			indexingTaskID NullInt64
		)

		err = tx.QueryRow(`SELECT url, sector, commp_task_id, psd_task_id, find_deal_task_id, indexing_task_id
			FROM market_mk12_deal_pipeline WHERE uuid = $1`, uuid).Scan(
			&url, &sector, &commpTaskID, &psdTaskID, &findDealTaskID, &indexingTaskID,
		)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
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

		//Mark failure for deal
		_, err = tx.Exec(`WITH updated AS (
									UPDATE market_mk12_deals
									SET error = $1
									WHERE uuid = $2
									RETURNING uuid
								)
								UPDATE market_direct_deals
								SET error = $1
								WHERE uuid = $2 AND NOT EXISTS (SELECT 1 FROM updated)`,
			"Deal pipeline removed by SP", uuid)
		if err != nil {
			return false, xerrors.Errorf("failed to mark deal %s as failed", uuid)
		}

		// Remove market_mk12_deal_pipeline entry
		_, err = tx.Exec(`DELETE FROM market_mk12_deal_pipeline WHERE uuid = $1`, uuid)
		if err != nil {
			return false, err
		}

		// If sector is null, remove related pieceref
		if !sector.Valid && url.Valid {
			// Extract refID from deal_pipeline.url (format: "pieceref:[refid]")
			const prefix = "pieceref:"
			if strings.HasPrefix(url.String, prefix) {
				refIDStr := url.String[len(prefix):]
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

type MK12PipelineFailedStats struct {
	DownloadingFailed int64
	CommPFailed       int64
	PSDFailed         int64
	FindDealFailed    int64
	IndexFailed       int64
}

func (a *WebRPC) MK12PipelineFailedTasks(ctx context.Context) (*MK12PipelineFailedStats, error) {
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
    LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid AND pp.piece_padded_size = dp.piece_size
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

	return &MK12PipelineFailedStats{
		DownloadingFailed: counts.DownloadingFailed,
		CommPFailed:       counts.CommPFailed,
		PSDFailed:         counts.PSDFailed,
		FindDealFailed:    counts.FindDealFailed,
		IndexFailed:       counts.IndexFailed,
	}, nil
}

func (a *WebRPC) MK12BulkRestartFailedMarketTasks(ctx context.Context, taskType string) error {
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

func (a *WebRPC) MK12BulkRemoveFailedMarketPipelines(ctx context.Context, taskType string) error {
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
			sector         NullInt64
			commpTaskID    NullInt64
			psdTaskID      NullInt64
			findDealTaskID NullInt64
			indexingTaskID NullInt64
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

			_, err = tx.Exec(`WITH updated AS (
									UPDATE market_mk12_deals
									SET error = $1
									WHERE uuid = $2
									RETURNING uuid
								)
								UPDATE market_direct_deals
								SET error = $1
								WHERE uuid = $2 AND NOT EXISTS (SELECT 1 FROM updated)`, "Deal pipeline removed by SP", p.uuid)
			if err != nil {
				return false, xerrors.Errorf("store deal failure: updating deal pipeline: %w", err)
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

func (a *WebRPC) MK12DDOStorageDealList(ctx context.Context, limit int, offset int) ([]*StorageDealList, error) {
	var mk12Summaries []*StorageDealList

	err := a.deps.DB.Select(ctx, &mk12Summaries, `SELECT 
									md.uuid,
									md.sp_id,
									md.created_at,
									md.piece_cid,
									md.piece_size,
									md.raw_size,
									md.error,
									coalesce(mm12dp.complete, true) as processed
									FROM market_direct_deals md
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

		if mk12Summaries[i].RawSize.Valid {
			pcid, err := cid.Parse(mk12Summaries[i].PieceCidV1)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse v1 piece CID: %w", err)
			}
			pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(mk12Summaries[i].RawSize.Int64))
			if err != nil {
				return nil, xerrors.Errorf("failed to convert v1 piece CID to v2: %w", err)
			}
			mk12Summaries[i].PieceCidV2 = pcid2.String()
		}
	}
	return mk12Summaries, nil

}
