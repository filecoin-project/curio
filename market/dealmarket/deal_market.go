package dealmarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk12"

	"github.com/filecoin-project/lotus/storage/pipeline/piece"
)

var log = logging.Logger("market")

const (
	mk12Str = "mk12"
	mk20Str = "mk20"
)

const dealPollerInterval = 10 * time.Second

type marketAPI interface {
	mk12.MK12API
	PieceIngesterApi
}

type CurioDealMarket struct {
	cfg         *config.CurioConfig
	db          *harmonydb.DB
	pin         Ingester
	miners      map[string][]string
	api         marketAPI
	MK12Handler *mk12.MK12
}

type MK12Pipeline struct {
	UUID         string          `db:"uuid"`
	Started      bool            `db:"started"`
	PieceCid     string          `db:"piece_cid"`
	Offline      bool            `db:"offline"`
	Downloaded   bool            `db:"downloaded"`
	RawSize      int64           `db:"file_size"`
	URL          string          `db:"url"`
	Headers      json.RawMessage `db:"headers"`
	AfterFindURL bool            `db:"after_find"`
	AfterCommp   bool            `db:"after_commp"`
	AfterPSD     bool            `db:"psd_task_id"`
	Sector       *int64          `db:"sector"`
	Offset       *int64          `Db:"sector_offset"`
}

func NewCurioDealMarket(db *harmonydb.DB, cfg *config.CurioConfig, mapi marketAPI) *CurioDealMarket {

	moduleMap := make(map[string][]string)
	moduleMap[mk12Str] = cfg.Market.DealMarketConfig.MK12.Miners

	return &CurioDealMarket{
		cfg:    cfg,
		db:     db,
		api:    mapi,
		miners: moduleMap,
	}
}

func (d *CurioDealMarket) StartMarket(ctx context.Context) error {
	var err error

	for module, miners := range d.miners {
		if module == mk12Str {
			d.MK12Handler, err = mk12.NewMK12Handler(miners, d.db, d.api)
			if err != nil {
				return err
			}

			var maddrs []address.Address
			for _, m := range miners {
				maddr, err := address.NewFromString(m)
				if err != nil {
					return err
				}
				maddrs = append(maddrs, maddr)
			}

			if d.cfg.Ingest.DoSnap {
				d.pin, err = NewPieceIngesterSnap(ctx, d.db, d.api, maddrs, false, time.Duration(d.cfg.Ingest.MaxDealWaitTime))
			} else {
				d.pin, err = NewPieceIngester(ctx, d.db, d.api, maddrs, false, time.Duration(d.cfg.Ingest.MaxDealWaitTime), d.cfg.Subsystems.UseSyntheticPoRep)
			}
		}
	}

	if err != nil {
		return err
	}
	d.runPoller(ctx)

	return nil

}

func (d *CurioDealMarket) runPoller(ctx context.Context) {
	ticker := time.NewTicker(dealPollerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.poll(ctx); err != nil {
				log.Errorw("deal processing failed", "error", err)
			}
		}
	}
}

func (d *CurioDealMarket) poll(ctx context.Context) error {

	/*
		FULL DEAL FLOW:
			Online:
			1. Make an entry for each online deal in market_mk12_deal_pipeline
			2. Online should be started immediately
			3. For online deals - keep checking if piecePark is complete
			4. Create commP task for online deal
			5. Once commP is complete, add the deal using pieceIngest

			Offline:
			1. Make an entry for each online deal in market_mk12_deal_pipeline
			2. Offline deal would not be started. It will have 2 triggers
				A. We find a pieceCID <> URL binding
				B. User manually imports the data using a file (will need piecePark)
			3. Check if piece is parked for offline deal triggered manually
			4. Create commP task for offline deals
				A. If we have piecePark then do local commP
				B. Do streaming commP if we have URL
			5. Once commP is complete, add the deal using pieceIngest
	*/
	return d.processMK12Deals(ctx)
}

func (d *CurioDealMarket) processMK12Deals(ctx context.Context) error {
	// Get all deal sorted by start_epoch
	var deals []MK12Pipeline

	err := d.db.Select(ctx, &deals, `SELECT 
									p.uuid as uuid,
									p.started as started,
									p.piece_cid as piece_cid,
									p.offline as offline,
									p.downloaded as downloaded,
									p.file_size as file_size,
									p.url as url,
									p.url_headers as url_headers,
									p.commp_task_id as commp_task_id,
									p.after_commp as after_commp,
									p.attach_task_id as find_task_id,
									p.after_attach as after_find,
									b.start_epoch as start_epoch
								FROM 
									market_mk12_deal_pipeline p
								LEFT JOIN 
									market_mk12_deals b ON p.uuid = b.uuid
								WHERE p.started = TRUE
								ORDER BY b.start_epoch ASC;`)

	if err != nil {
		return err
	}

	// Process deals
	for _, deal := range deals {
		deal := deal

		// Check if download is finished and update the db
		if !deal.Downloaded {
			var finished bool
			err = d.db.Select(ctx, &finished, `SELECT complete FROM parked_pieces WHERE piece_cid = $1`, deal.PieceCid)
			if err != nil {
				return xerrors.Errorf("failed to check if piece %s finished downloading: %w", deal.PieceCid, err)
			}

			if finished {
				_, err = d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET downloaded = TRUE WHERE piece_cid = $1`, deal.PieceCid)
				if err != nil {
					return xerrors.Errorf("failed mark piece %s is finished downloading: %w", deal.PieceCid, err)
				}
				deal.Downloaded = finished
			}
		}

		// TODO: Add offline manual deal trigger
		// TODO: Add mechanism to override sector if sector fails

		// If offline deal has been assigned a URL and commP is finished then proceed to add the deal to ingestor
		// If it is not already part of a sector
		if deal.AfterFindURL && deal.AfterCommp && deal.Sector == nil && deal.Offset == nil {

			// MarketBoostDeal represents a record from the market_mk12_deals table
			type MarketMK12Deal struct {
				UUID              string
				CreatedAt         time.Time
				SignedProposalCid string
				ProposalSignature []byte
				Proposal          []byte
				PieceCid          string
				PieceSize         int64
				Offline           bool
				Verified          bool
				SpID              int64
				StartEpoch        int64
				EndEpoch          int64
				ClientPeerID      string
				ChainDealID       int64
				PublishCid        string
				FastRetrieval     bool
				AnnounceToIpni    bool
				Error             string
			}

			// Prepare a variable to hold the result
			var dbdeals []MarketMK12Deal

			err = d.db.Select(ctx, &dbdeals, `SELECT 
										uuid,
										created_at,
										signed_proposal_cid,
										proposal_signature,
										proposal,
										piece_cid,
										piece_size,
										offline,
										verified,
										sp_id,
										start_epoch,
										end_epoch,
										client_peer_id,
										chain_deal_id,
										publish_cid,
										sector_num,
										piece_offset,
										length,
										fast_retrieval,
										announce_to_ipni,
										url,
										url_headers,
										error
									FROM market_mk12_deals
									WHERE uuid = $1;`, deal.UUID)
			if err != nil {
				return xerrors.Errorf("failed to get MK12 deals from DB")
			}

			if len(dbdeals) != 1 {
				return xerrors.Errorf("expected 1 deal, got %d for UUID %s", len(dbdeals), deal.UUID)
			}

			dbdeal := dbdeals[0]

			maddr, err := address.NewIDAddress(uint64(dbdeal.SpID))
			if err != nil {
				return xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
			}

			var prop market.DealProposal
			err = json.Unmarshal(dbdeal.Proposal, &prop)
			if err != nil {
				return xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
			}

			pcid, err := cid.Parse(dbdeal.PublishCid)
			if err != nil {
				return xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
			}

			pi := piece.PieceDealInfo{
				PublishCid:   &pcid,
				DealID:       abi.DealID(dbdeal.ChainDealID),
				DealProposal: &prop,
				DealSchedule: piece.DealSchedule{
					StartEpoch: abi.ChainEpoch(dbdeal.StartEpoch),
					EndEpoch:   abi.ChainEpoch(dbdeal.EndEpoch),
				},
				PieceActivationManifest: nil,
				KeepUnsealed:            true,
			}

			dealUrl, err := url.Parse(deal.URL)
			if err != nil {
				return xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
			}

			headers := make(http.Header)
			err = json.Unmarshal(deal.Headers, &headers)
			if err != nil {
				return xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
			}

			info, err := d.pin.AllocatePieceToSector(ctx, maddr, pi, deal.RawSize, *dealUrl, headers)
			if err != nil {
				return xerrors.Errorf("UUID: %s: failed to add deal to a sector: %w", deal.UUID, err)
			}

			n, err := d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET sector = $1, sector_offset = $2 WHERE uuid = $3`, info.Sector, info.Offset, deal.UUID)
			if err != nil {
				return xerrors.Errorf("UUID: %s: failed to add sector %d and offset %d details to DB: %w", deal.UUID, info.Sector, info.Offset, err)
			}
			if n != 1 {
				if err != nil {
					return xerrors.Errorf("UUID: %s: expected 1 deal update for add sector %d and offset %d details to DB but found %d", deal.UUID, info.Sector, info.Offset, n)
				}
			}
			log.Infof("Added deal %s to sector %d at %d", deal.UUID, info.Sector, info.Offset)
		}
	}
	return nil
}
