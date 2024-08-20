package storage_market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/market/mk12"
	"github.com/filecoin-project/curio/market/storageIngest"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/storage/pipeline/piece"
)

var log = logging.Logger("storage-market")

const (
	mk12Str = "mk12"
	mk20Str = "mk20"
)

const (
	pollerCommP = iota
	pollerPSD
	pollerFindDeal

	numPollers
)

const dealPollerInterval = 10 * time.Second

type storageMarketAPI interface {
	mk12.MK12API
	storageIngest.PieceIngesterApi
}

type CurioStorageDealMarket struct {
	cfg         *config.CurioConfig
	db          *harmonydb.DB
	pin         storageIngest.Ingester
	miners      map[string][]string
	api         storageMarketAPI
	MK12Handler *mk12.MK12
	sc          *ffi.SealCalls
	urls        map[string]http.Header
	pollers     [numPollers]promise.Promise[harmonytask.AddTaskFunc]
}

type MK12Pipeline struct {
	UUID           string          `db:"uuid"`
	SpID           int64           `db:"sp_id"`
	Started        bool            `db:"started"`
	PieceCid       string          `db:"piece_cid"`
	Offline        bool            `db:"offline"`
	Downloaded     bool            `db:"downloaded"`
	RawSize        int64           `db:"raw_size"`
	URL            string          `db:"url"`
	Headers        json.RawMessage `db:"headers"`
	CommTaskID     *int64          `db:"commp_task_id"`
	AfterCommp     bool            `db:"after_commp"`
	PSDWaitTime    time.Time       `db:"psd_wait_time"`
	PSDTaskID      *int64          `db:"psd_task_id"`
	AfterPSD       bool            `db:"after_psd"`
	FindDealTaskID *int64          `db:"find_deal_task_id"`
	AfterFindDeal  bool            `db:"after_find_deal"`
	Sector         *int64          `db:"sector"`
	Offset         *int64          `Db:"sector_offset"`
}

func NewCurioStorageDealMarket(db *harmonydb.DB, cfg *config.CurioConfig, sc *ffi.SealCalls, mapi storageMarketAPI) *CurioStorageDealMarket {

	moduleMap := make(map[string][]string)
	for _, l := range cfg.Market.StorageMarketConfig.MK12.Libp2p {
		moduleMap[mk12Str] = append(moduleMap[mk12Str], l.Miner)
	}

	urls := make(map[string]http.Header)
	for _, curl := range cfg.Market.StorageMarketConfig.PieceLocator {
		urls[curl.URL] = curl.Headers
	}

	return &CurioStorageDealMarket{
		cfg:    cfg,
		db:     db,
		api:    mapi,
		miners: moduleMap,
		sc:     sc,
		urls:   urls,
	}
}

func (d *CurioStorageDealMarket) StartMarket(ctx context.Context) error {
	var err error

	for module, miners := range d.miners {
		if module == mk12Str {
			if len(miners) == 0 {
				// Do not start the poller if no minerID present
				return nil
			}
			d.MK12Handler, err = mk12.NewMK12Handler(miners, d.db, d.sc, d.api)
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
				d.pin, err = storageIngest.NewPieceIngesterSnap(ctx, d.db, d.api, maddrs, false, d.cfg)
			} else {
				d.pin, err = storageIngest.NewPieceIngester(ctx, d.db, d.api, maddrs, false, d.cfg)
			}
		}
	}

	if err != nil {
		return err
	}
	go d.runPoller(ctx)

	return nil

}

func (d *CurioStorageDealMarket) runPoller(ctx context.Context) {
	ticker := time.NewTicker(dealPollerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.poll(ctx)
		}
	}
}

func (d *CurioStorageDealMarket) poll(ctx context.Context) {

	/*
		FULL DEAL FLOW:
			Online:
			1. Make an entry for each online deal in market_mk12_deal_pipeline
			2. For online deals - keep checking if piecePark is complete
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
	for module, miners := range d.miners {
		if module == mk12Str {
			if len(miners) > 0 {
				d.processMK12Deals(ctx)
			}
		}
	}
}

func (d *CurioStorageDealMarket) processMK12Deals(ctx context.Context) {
	// Get all deal sorted by start_epoch
	var deals []MK12Pipeline

	err := d.db.Select(ctx, &deals, `SELECT 
									p.uuid as uuid,
									p.sp_id as sp_id,
									p.started as started,
									p.piece_cid as piece_cid,
									p.offline as offline,
									p.raw_size as raw_size,
									p.url as url,
									p.headers as headers,
									p.commp_task_id as commp_task_id,
									p.after_commp as after_commp,
									p.psd_task_id as psd_task_id,
									p.after_psd as after_psd,
									p.find_deal_task_id as find_deal_task_id,
									p.after_find_deal as after_find_deal,
									p.psd_wait_time as psd_wait_time,
									b.start_epoch as start_epoch
								FROM 
									market_mk12_deal_pipeline p
								LEFT JOIN 
									market_mk12_deals b ON p.uuid = b.uuid
								WHERE p.started = TRUE
								ORDER BY b.start_epoch ASC;`)

	if err != nil {
		log.Errorf("failed to get deal pipeline status from DB: %w", err)
	}

	// Add PSD task - PSD is an exception which is processed for multiple deals at once to save
	// gas cost for PSD messages
	err = d.addPSDTask(ctx, deals)
	if err != nil {
		log.Errorf("%w", err)
	}

	// Process deals
	for _, deal := range deals {
		deal := deal
		err := d.processMk12Deal(ctx, deal)
		if err != nil {
			log.Errorf("%w", err)
		}
	}
}

func (d *CurioStorageDealMarket) processMk12Deal(ctx context.Context, deal MK12Pipeline) error {

	// Try to mark the deal as started
	if !deal.Started {
		// Check if download is finished and update the deal state in DB
		if deal.URL != "" {
			goUrl, err := url.Parse(deal.URL)
			if err != nil {
				return xerrors.Errorf("UUID: %s parsing data URL: %w", deal.UUID, err)
			}

			// If park piece ref URL
			if goUrl.Scheme == "pieceref" {
				refNum, err := strconv.ParseInt(goUrl.Opaque, 10, 64)
				if err != nil {
					return xerrors.Errorf("UUID: %s parsing piece reference number: %w", deal.UUID, err)
				}

				var complete bool
				err = d.db.QueryRow(ctx, `SELECT pp.complete
												FROM parked_pieces pp
												JOIN parked_piece_refs ppr ON pp.id = ppr.piece_id
												WHERE ppr.ref_id = $1;`, refNum).Scan(&complete)
				if err != nil {
					return xerrors.Errorf("UUID: %s getting piece park status: %w", deal.UUID, err)
				}

				if complete {
					deal.Started = true
					_, err = d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET started = TRUE WHERE uuid = $1`, deal.UUID)
					if err != nil {
						return xerrors.Errorf("failed to mark deal %s as started: %w", deal.UUID, err)
					}
					log.Infof("UUID: %s deal started successfully", deal.UUID)
					return nil
				}
			}
		} else {
			// If no URL found for offline deal then we should try to find one
			if deal.Offline {
				found, err := d.findURLForOfflineDeals(ctx, deal.UUID, deal.PieceCid)
				if err != nil {
					return err
				}
				if found {
					_, err = d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET started = TRUE WHERE uuid = $1`, deal.UUID)
					if err != nil {
						return xerrors.Errorf("UUID: %s: failed to mark deal as started: %w", deal.UUID, err)
					}
					log.Infof("UUID: %s deal started successfully", deal.UUID)
					return nil
				}
			}
		}
	}

	// Create commP task
	if deal.Started && !deal.AfterCommp && deal.CommTaskID == nil {
		// Skip commP is configured to do so
		if d.cfg.Market.StorageMarketConfig.MK12.SkipCommP {
			_, err := d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET after_commp = TRUE, commp_task_id = NULL WHERE uuid = $1`, deal.UUID)
			if err != nil {
				return xerrors.Errorf("UUID: %s: updating deal pipeline: %w", deal.UUID, err)
			}
			log.Infof("UUID: %s: commP skipped successfully", deal.UUID)
			return nil
		}

		d.pollers[pollerCommP].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			// update
			n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET commp_task_id = $1 WHERE uuid = $2 AND commp_task_id IS NULL`, id, deal.UUID)
			if err != nil {
				return false, xerrors.Errorf("UUID: %s: updating deal pipeline: %w", deal.UUID, err)
			}

			// commit only if we updated the piece
			return n > 0, nil
		})
		log.Infof("UUID: %s: commP task created successfully", deal.UUID)
		return nil
	}

	// Create Find Deal task
	if deal.Started && deal.AfterCommp && deal.AfterPSD && !deal.AfterFindDeal && deal.FindDealTaskID == nil {
		d.pollers[pollerFindDeal].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			// update
			n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET find_deal_task_id = $1 WHERE uuid = $2 AND find_deal_task_id IS NULL`, id, deal.UUID)
			if err != nil {
				return false, xerrors.Errorf("UUID: %s: updating deal pipeline: %w", deal.UUID, err)
			}

			// commit only if we updated the piece
			return n > 0, nil
		})
		log.Infof("UUID: %s: FindDeal task created successfully", deal.UUID)
		return nil
	}

	// If on chain deal ID is present, we should add the deal to a sector
	if deal.AfterFindDeal && deal.Sector == nil && deal.Offset == nil {
		err := d.ingestDeal(ctx, deal)
		if err != nil {
			return err
		}
	}
	return nil
}

type MarketMK12Deal struct {
	UUID              string    `db:"uuid"`
	CreatedAt         time.Time `db:"created_at"`
	SignedProposalCid string    `db:"signed_proposal_cid"`
	ProposalSignature []byte    `db:"proposal_signature"`
	Proposal          []byte    `db:"proposal"`
	PieceCid          string    `db:"piece_cid"`
	PieceSize         int64     `db:"piece_size"`
	Offline           bool      `db:"offline"`
	Verified          bool      `db:"verified"`
	SpID              int64     `db:"sp_id"`
	StartEpoch        int64     `db:"start_epoch"`
	EndEpoch          int64     `db:"end_epoch"`
	ClientPeerID      string    `db:"client_peer_id"`
	ChainDealID       int64     `db:"chain_deal_id"`
	PublishCid        string    `db:"publish_cid"`
	FastRetrieval     bool      `db:"fast_retrieval"`
	AnnounceToIpni    bool      `db:"announce_to_ipni"`
	Error             string    `db:"error"`
}

func (d *CurioStorageDealMarket) findURLForOfflineDeals(ctx context.Context, deal string, pcid string) (bool, error) {

	// Check if DB has a URL
	var goUrls []struct {
		Url     string          `db:"url"`
		Headerb json.RawMessage `db:"headers"`
		RawSize int64           `db:"raw_size"`
	}

	err := d.db.Select(ctx, &goUrls, `SELECT url, headers, raw_size   
								FROM market_offline_urls WHERE piece_cid = $1`, pcid)

	if err != nil {
		return false, xerrors.Errorf("getting url and headers from db: %w", err)
	}

	if len(goUrls) > 1 {
		return false, xerrors.Errorf("expected 1 row per piece, got %d", len(goUrls))
	}

	if len(goUrls) == 1 {
		_, err := d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET url = $1, headers = $2, raw_size = $3
                           WHERE uuid = $4`, goUrls[0].Url, goUrls[0].Headerb, goUrls[0].RawSize, deal)
		if err != nil {
			return false, xerrors.Errorf("store url for piece %s: updating pipeline: %w", pcid, err)
		}

		return true, nil
	}

	// Check if We can find the URL for this piece on remote servers i.e. len(goUrls) == 0
	for rUrl, headers := range d.urls {
		// Create a new HTTP request
		urlString := fmt.Sprintf("%s/pieces?id=%s", rUrl, pcid)
		req, err := http.NewRequest(http.MethodGet, urlString, nil)
		if err != nil {
			return false, xerrors.Errorf("error creating request: %w", err)
		}

		req.Header = headers

		// Create a client and make the request
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return false, xerrors.Errorf("error making GET request: %w", err)
		}

		// Check the response code for 404
		if resp.StatusCode != http.StatusOK {
			if resp.StatusCode != 404 {
				return false, xerrors.Errorf("not ok response from HTTP server: %s", resp.Status)
			}
			continue
		}

		hdrs, err := json.Marshal(headers)
		if err != nil {
			return false, xerrors.Errorf("marshaling headers: %w", err)
		}

		rawSizeStr := resp.Header.Get("Content-Length")
		if rawSizeStr == "" {
			continue
		}
		rawSize, err := strconv.ParseInt(rawSizeStr, 10, 64)
		if err != nil {
			return false, xerrors.Errorf("failed to parse the raw size: %w", err)
		}

		_, err = d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET url = $1, headers = $2, raw_size = $3   
                           WHERE WHERE uuid = $4`, urlString, hdrs, rawSize, deal)
		if err != nil {
			return false, xerrors.Errorf("store url for piece %s: updating pipeline: %w", pcid, err)
		}

		return true, nil
	}

	return false, nil
}

func (d *CurioStorageDealMarket) addPSDTask(ctx context.Context, deals []MK12Pipeline) error {
	type queue struct {
		deals []string
		t     time.Time
	}

	dm := make(map[int64]queue)

	for _, deal := range deals {
		if deal.Started && deal.AfterCommp && !deal.AfterPSD && deal.PSDTaskID == nil {
			// Check if the spID is already in the map
			if q, exists := dm[deal.SpID]; exists {
				// Append the UUID to the deals list
				q.deals = append(q.deals, deal.UUID)

				// Update the time if the current deal's time is older
				if deal.PSDWaitTime.Before(q.t) {
					q.t = deal.PSDWaitTime
				}

				// Update the map with the new queue
				dm[deal.SpID] = q
			} else {
				// Add a new entry to the map if spID is not present
				dm[deal.SpID] = queue{
					deals: []string{deal.UUID},
					t:     deal.PSDWaitTime,
				}
			}
		}
	}

	publishPeriod := d.cfg.Market.StorageMarketConfig.MK12.PublishMsgPeriod
	maxDeals := d.cfg.Market.StorageMarketConfig.MK12.MaxDealsPerPublishMsg

	for _, q := range dm {
		if q.t.Add(time.Duration(publishPeriod)).After(time.Now()) || uint64(len(q.deals)) > maxDeals {
			d.pollers[pollerPSD].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// update
				n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET psd_task_id = $1 WHERE uuid = ANY($2) AND psd_task_id IS NULL`, id, q.deals)
				if err != nil {
					return false, xerrors.Errorf("updating deal pipeline: %w", err)
				}
				return n > 0, nil
			})
		}
		log.Infof("PSD task created successfully for deals %s", q.deals)
	}
	return nil
}

func (d *CurioStorageDealMarket) ingestDeal(ctx context.Context, deal MK12Pipeline) error {
	// Prepare a variable to hold the result
	var dbdeals []MarketMK12Deal

	err := d.db.Select(ctx, &dbdeals, `SELECT 
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

	var info api.SectorOffset

	comm, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		info, err = d.pin.AllocatePieceToSector(ctx, tx, maddr, pi, deal.RawSize, *dealUrl, headers)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: failed to add deal to a sector: %w", deal.UUID, err)
		}

		n, err := d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET sector = $1, sector_offset = $2 
                                 WHERE uuid = $3 AND sector = NULL AND sector_offset = NULL`, info.Sector, info.Offset, deal.UUID)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: failed to add sector %d and offset %d details to DB: %w", deal.UUID, info.Sector, info.Offset, err)
		}
		if n != 1 {
			if err != nil {
				return false, xerrors.Errorf("UUID: %s: expected 1 deal update for add sector %d and offset %d details to DB but found %d", deal.UUID, info.Sector, info.Offset, n)
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("UUID: %s: failed to add deal to a sector: %w", deal.UUID, err)
	}

	if !comm {
		return xerrors.Errorf("UUID: %s: failed to commit transaction: %w", deal.UUID, err)
	}

	log.Infof("Added deal %s to sector %d at %d", deal.UUID, info.Sector, info.Offset)
	return nil
}
