//go:build !skiff

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
	"github.com/snadrus/must"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/lists"
	itype "github.com/filecoin-project/curio/market/ipni/types"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/tasks/indexing"

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
		s.PieceCidV2 = s.PieceCid
		if s.RawSize.Valid && s.RawSize.Int64 >= 127 {
			pcid, err := cid.Parse(s.PieceCid)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse v1 piece CID: %w", err)
			}
			pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(s.RawSize.Int64))
			if err != nil {
				log.Warnw("failed to generate piece cid v2, using piece cid v1",
					"piece_cid", s.PieceCid, "raw_size", s.RawSize.Int64, "err", err)
			} else {
				s.PieceCidV2 = pcid2.String()
			}
		} else if s.RawSize.Valid {
			log.Warnw("raw_size unavailable, using piece cid v1", "piece_cid", s.PieceCid, "raw_size", s.RawSize.Int64)
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

	d.PieceCidV2 = d.PieceCid
	if d.RawSize.Valid && d.RawSize.Int64 >= 127 {
		pcid, err := cid.Parse(d.PieceCid)
		if err != nil {
			return &StorageDealSummary{}, xerrors.Errorf("failed to parse piece CID: %w", err)
		}
		pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(d.RawSize.Int64))
		if err != nil {
			log.Warnw("failed to generate piece cid v2, using piece cid v1",
				"piece_cid", d.PieceCid, "raw_size", d.RawSize.Int64, "err", err)
		} else {
			d.PieceCidV2 = pcid2.String()
		}
	} else if d.RawSize.Valid {
		log.Warnw("raw_size unavailable, using piece cid v1", "piece_cid", d.PieceCid, "raw_size", d.RawSize.Int64)
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
		mk12Summaries[i].PieceCidV2 = mk12Summaries[i].PieceCidV1

		// Find PieceCidV2 only of rawSize is present
		// It will be absent only for Offline deals (mk12, mk12-ddo), waiting for data
		if mk12Summaries[i].RawSize.Valid && mk12Summaries[i].RawSize.Int64 >= 127 {
			pcid, err := cid.Parse(mk12Summaries[i].PieceCidV1)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse v1 piece CID: %w", err)
			}
			pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(mk12Summaries[i].RawSize.Int64))
			if err != nil {
				log.Warnw("failed to generate piece cid v2, using piece cid v1",
					"piece_cid", mk12Summaries[i].PieceCidV1, "raw_size", mk12Summaries[i].RawSize.Int64, "err", err)
			} else {
				mk12Summaries[i].PieceCidV2 = pcid2.String()
			}
		} else if mk12Summaries[i].RawSize.Valid {
			log.Warnw("raw_size unavailable, using piece cid v1", "piece_cid", mk12Summaries[i].PieceCidV1, "raw_size", mk12Summaries[i].RawSize.Int64)
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

	err := config.ForEachConfig(ctx, a.deps.DB, func(name string, info minimalActorInfo) error {
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

	for _, m := range lists.UniqNoAllocWithComparator(miners, func(i, j int) bool {
		return bytes.Compare(miners[i].Bytes(), miners[j].Bytes()) < 0
	}) {
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

	var ret PieceInfo
	var pcid cid.Cid
	var size abi.PaddedPieceSize

	if commcidv2.IsPieceCidV2(piece) {
		pcid1, rawSize, err := commcid.PieceCidV1FromV2(piece)
		if err != nil {
			return nil, xerrors.Errorf("failed to get pieceCidv1 from piece CID v2: %w", err)
		}

		psize := padreader.PaddedSize(rawSize).Padded()
		ret.PieceCidv2 = piece.String()
		ret.PieceCid = pcid1.String()
		ret.Size = int64(psize)
		pcid = pcid1
		size = psize
		err = a.deps.DB.QueryRow(ctx, `SELECT created_at, indexed, indexed_at FROM market_piece_metadata WHERE piece_cid = $1 AND piece_size = $2`, pcid1.String(), psize).Scan(&ret.CreatedAt, &ret.Indexed, &ret.IndexedAT)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, xerrors.Errorf("failed to get piece metadata: %w", err)
		}

		c1 := itype.PdpIpniContext{
			PieceCID: piece,
			Payload:  true,
		}

		c1b, err := c1.Marshal()
		if err != nil {
			return nil, xerrors.Errorf("failed to marshal PDP piece info: %w", err)
		}

		c2 := itype.PdpIpniContext{
			PieceCID: piece,
			Payload:  false,
		}

		c2b, err := c2.Marshal()
		if err != nil {
			return nil, xerrors.Errorf("failed to marshal PDP piece info: %w", err)
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

		ret.IPNIAd = append(ret.IPNIAd, ipniAdPdp, ipniAdPdp1)
	} else {
		ret.PieceCid = piece.String()
		pcid = piece
		err = a.deps.DB.QueryRow(ctx, `SELECT piece_size, created_at, indexed, indexed_at FROM market_piece_metadata WHERE piece_cid = $1`, piece.String()).Scan(&ret.Size, &ret.CreatedAt, &ret.Indexed, &ret.IndexedAT)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, xerrors.Errorf("failed to get piece metadata: %w", err)
		}
		size = abi.PaddedPieceSize(ret.Size)
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
													WHERE piece_cid = $1`, ret.PieceCid)
	if err != nil {
		return nil, xerrors.Errorf("failed to get piece deals: %w", err)
	}

	for i := range pieceDeals {
		if pieceDeals[i].SpId == indexing.PDP_v1_SP_ID {
			pieceDeals[i].Miner = "PDP V1"
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

	// Get only the latest Ad
	var ipniAd string
	err = a.deps.DB.QueryRow(ctx, `SELECT ad_cid FROM ipni WHERE context_id = $1 ORDER BY order_number DESC LIMIT 1`, b.Bytes()).Scan(&ipniAd)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, xerrors.Errorf("failed to get ad ID by piece CID: %w", err)
	}

	ret.IPNIAd = append(ret.IPNIAd, ipniAd)
	return &ret, nil
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
	pcid2, err := cid.Parse(pieceCID)
	if err != nil {
		return nil, err
	}

	var pcid1 cid.Cid

	if commcidv2.IsPieceCidV2(pcid2) {
		pcid, _, err := commcid.PieceCidV1FromV2(pcid2)
		if err != nil {
			return nil, xerrors.Errorf("failed to get piece CID v1 from piece CID v2: %w", err)
		}
		pcid1 = pcid
	} else {
		pcid1 = pcid2
	}

	var pps ParkedPieceState

	// Query the parked_pieces table
	err = a.deps.DB.QueryRow(ctx, `
        SELECT id, created_at, piece_cid, piece_padded_size, piece_raw_size, complete, task_id, cleanup_task_id
        FROM parked_pieces WHERE piece_cid = $1
    `, pcid1.String()).Scan(
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
	pcid2, err := cid.Parse(pieceCid)
	if err != nil {
		return nil, err
	}

	var pcid1 cid.Cid

	if commcidv2.IsPieceCidV2(pcid2) {
		pcid, _, err := commcid.PieceCidV1FromV2(pcid2)
		if err != nil {
			return nil, xerrors.Errorf("failed to get piece CID v1 from piece CID v2: %w", err)
		}
		pcid1 = pcid
	} else {
		pcid1 = pcid2
	}

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
										WHERE piece_cid = $1
									
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
										WHERE piece_cid = $1`, pcid1.String())
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
		pipelineMap[pipeline.UUID] = pipeline
	}

	var mk20Deals []*mk20.DBDeal
	err = a.deps.DB.Select(ctx, &mk20Deals, `SELECT 
													id, 
													client,
													data,
													ddo_v1,
													retrieval_v1,
													pdp_v1 FROM market_mk20_deal WHERE piece_cid_v2 = $1`, pcid2.String())
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
		mk20pipelineMap[pipeline.ID] = pipeline
	}

	mk20PDPpipelineMap := make(map[string]MK20PDPPipeline)
	for _, pipeline := range mk20PDPPipelines {
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
