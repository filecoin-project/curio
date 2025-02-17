package storage_market

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/pgx/v5"
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
	"github.com/filecoin-project/curio/market/storageingest"

	"github.com/filecoin-project/lotus/chain/proofs"
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

const dealPollerInterval = 30 * time.Second

type storageMarketAPI interface {
	mk12.MK12API
	storageingest.PieceIngesterApi
}

type CurioStorageDealMarket struct {
	cfg         *config.CurioConfig
	db          *harmonydb.DB
	pin         storageingest.Ingester
	miners      map[string][]address.Address
	api         storageMarketAPI
	MK12Handler *mk12.MK12
	sc          *ffi.SealCalls
	urls        map[string]http.Header
	adders      [numPollers]promise.Promise[harmonytask.AddTaskFunc]
}

type MK12Pipeline struct {
	UUID string `db:"uuid"`
	SpID int64  `db:"sp_id"`

	// started after data download
	Started   bool                `db:"started"`
	PieceCid  string              `db:"piece_cid"`
	PieceSize abi.PaddedPieceSize `db:"piece_size"`
	Offline   bool                `db:"offline"` // data is not downloaded before starting the deal
	RawSize   sql.NullInt64       `db:"raw_size"`
	URL       *string             `db:"url"`
	Headers   json.RawMessage     `db:"headers"`

	// commP task
	CommTaskID *int64 `db:"commp_task_id"`
	AfterCommp bool   `db:"after_commp"`

	// PSD task
	PSDWaitTime *time.Time `db:"psd_wait_time"` // set in commp to now
	PSDTaskID   *int64     `db:"psd_task_id"`
	AfterPSD    bool       `db:"after_psd"`

	// Find Deal task (just looks at the chain for the deal ID)
	FindDealTaskID *int64 `db:"find_deal_task_id"`
	AfterFindDeal  bool   `db:"after_find_deal"`

	// Sector the deal was assigned into
	Sector *int64 `db:"sector"`
	Offset *int64 `db:"sector_offset"`
}

func NewCurioStorageDealMarket(miners []address.Address, db *harmonydb.DB, cfg *config.CurioConfig, sc *ffi.SealCalls, mapi storageMarketAPI) *CurioStorageDealMarket {

	moduleMap := make(map[string][]address.Address)
	moduleMap[mk12Str] = append(moduleMap[mk12Str], miners...)

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
			d.MK12Handler, err = mk12.NewMK12Handler(miners, d.db, d.sc, d.api, d.cfg)
			if err != nil {
				return err
			}

			if d.cfg.Ingest.DoSnap {
				d.pin, err = storageingest.NewPieceIngesterSnap(ctx, d.db, d.api, miners, d.cfg)
			} else {
				d.pin, err = storageingest.NewPieceIngester(ctx, d.db, d.api, miners, d.cfg)
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

	d.createIndexingTaskForMigratedDeals(ctx)
	/*
		FULL DEAL FLOW:
			Online:
			1. Make an entry for each online deal in market_mk12_deal_pipeline
			2. For online deals - keep checking if piecePark is complete
			4. Create commP task for online deal
			5. Once commP is complete, send PSD and find the allocated deal ID
			6. Add the deal using pieceIngest

			Offline:
			1. Make an entry for each online deal in market_mk12_deal_pipeline
			2. Offline deal would not be started till we find a pieceCID <> URL binding
			3. Create commP task for offline deals
				A. Do streaming commP
			5. Once commP is complete, send PSD and find the allocated deal ID
			6. Add the deal using pieceIngest
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
	// Catch any panics if encountered as we are working with user provided data
	defer func() {
		if r := recover(); r != nil {
			trace := make([]byte, 1<<16)
			n := runtime.Stack(trace, false)

			log.Errorf("panic occurred: %v\n%s", r, trace[:n])
		}
	}()

	// Get all deal sorted by start_epoch
	var deals []MK12Pipeline

	err := d.db.Select(ctx, &deals, `SELECT 
									p.uuid as uuid,
									p.sp_id as sp_id,
									p.started as started,
									p.piece_cid as piece_cid,
									p.piece_size as piece_size,
									p.raw_size as raw_size,
									
									p.offline as offline,
									p.url as url,
									p.headers as headers,
									
									p.commp_task_id as commp_task_id,
									p.after_commp as after_commp,
									p.psd_task_id as psd_task_id,
									p.after_psd as after_psd,
									p.find_deal_task_id as find_deal_task_id,
									p.after_find_deal as after_find_deal,
									p.psd_wait_time as psd_wait_time,
									
									p.sector as sector,
									p.sector_offset as sector_offset
								FROM 
									market_mk12_deal_pipeline p
								LEFT JOIN 
									market_mk12_deals b ON p.uuid = b.uuid
								ORDER BY b.start_epoch ASC;`)

	if err != nil {
		log.Errorf("failed to get deal pipeline status from DB: %w", err)
	}

	// Add PSD task - PSD is an exception which is processed for multiple deals at once to save
	// gas cost for PSD messages
	err = d.addPSDTask(ctx)
	if err != nil {
		log.Errorf("%w", err)
	}

	// Process deals
	for _, deal := range deals {
		deal := deal
		err := d.processMk12Deal(ctx, deal)
		if err != nil {
			log.Errorf("process deal: %s", err)
		}
	}
}

func (d *CurioStorageDealMarket) processMk12Deal(ctx context.Context, deal MK12Pipeline) error {

	// Try to mark the deal as started
	if !deal.Started {
		// Check if download is finished and update the deal state in DB
		if deal.URL != nil && *deal.URL != "" {
			goUrl, err := url.Parse(*deal.URL)
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
				}
			}
		} else {
			// If no URL found for offline deal then we should try to find one
			if deal.Offline {
				err := d.findURLForOfflineDeals(ctx, deal.UUID, deal.PieceCid)
				if err != nil {
					return err
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

		if d.adders[pollerCommP].IsSet() {
			d.adders[pollerCommP].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
				// update
				n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET commp_task_id = $1 
                                 WHERE uuid = $2 AND started = TRUE AND commp_task_id IS NULL AND after_commp = FALSE`, id, deal.UUID)
				if err != nil {
					return false, xerrors.Errorf("UUID: %s: updating deal pipeline: %w", deal.UUID, err)
				}

				// commit only if we updated the piece
				return n > 0, nil
			})
			log.Infof("UUID: %s: commP task created successfully", deal.UUID)
		}

		return nil
	}

	// Create Find Deal task
	if deal.Started && deal.AfterCommp && deal.AfterPSD && !deal.AfterFindDeal && deal.FindDealTaskID == nil {
		var executed bool
		err := d.db.QueryRow(ctx, `SELECT EXISTS(SELECT TRUE FROM market_mk12_deals d
                          INNER JOIN message_waits mw ON mw.signed_message_cid = d.publish_cid
                          WHERE mw.executed_tsk_cid IS NOT NULL AND d.uuid = $1)`, deal.UUID).Scan(&executed)
		if err != nil {
			return xerrors.Errorf("UUID: %s: checking if the message is executed: %w", deal.UUID, err)
		}
		if !executed {
			return nil
		}

		d.adders[pollerFindDeal].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
			// update
			n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET find_deal_task_id = $1 
                                 WHERE uuid = $2 AND started = TRUE AND find_deal_task_id IS NULL 
                                   AND after_commp = TRUE AND after_psd = TRUE AND after_find_deal = FALSE`, id, deal.UUID)
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
	if deal.AfterFindDeal && deal.Sector == nil {
		err := d.ingestDeal(ctx, deal)
		if err != nil {
			return xerrors.Errorf("ingest deal: %w", err)
		}
	}

	// Get the deal offset if sector has started sealing
	if deal.AfterFindDeal && deal.Sector != nil && deal.Offset == nil {
		type pieces struct {
			Cid   string              `db:"piece_cid"`
			Size  abi.PaddedPieceSize `db:"piece_size"`
			Index int64               `db:"piece_index"`
		}

		var pieceList []pieces
		err := d.db.Select(ctx, &pieceList, `SELECT piece_cid, piece_size, piece_index
												FROM sectors_sdr_initial_pieces
												WHERE sp_id = $1 AND sector_number = $2
												
												UNION ALL
												
												SELECT piece_cid, piece_size, piece_index
												FROM sectors_snap_initial_pieces
												WHERE sp_id = $1 AND sector_number = $2
												
												ORDER BY piece_index ASC;`, deal.SpID, deal.Sector)
		if err != nil {
			return xerrors.Errorf("UUID: %s: getting pieces for sector: %w", deal.UUID, err)
		}

		if len(pieceList) == 0 {
			return xerrors.Errorf("UUID: %s: no pieces found for the sector %d", deal.UUID, *deal.Sector)
		}

		var offset abi.UnpaddedPieceSize

		for _, p := range pieceList {
			_, padLength := proofs.GetRequiredPadding(offset.Padded(), p.Size)
			offset += padLength.Unpadded()
			if p.Cid == deal.PieceCid && p.Size == deal.PieceSize {
				n, err := d.db.Exec(ctx, `UPDATE market_mk12_deal_pipeline SET sector_offset = $1 WHERE uuid = $2 AND sector = $3 AND sector_offset IS NULL`, offset.Padded(), deal.UUID, deal.Sector)
				if err != nil {
					return xerrors.Errorf("UUID: %s: updating deal pipeline with sector offset: %w", deal.UUID, err)
				}
				if n != 1 {
					return xerrors.Errorf("UUID: %s: expected 1 row for sector offset update in DB but found %d", deal.UUID, n)
				}
				return nil
			}
			offset += p.Size.Unpadded()
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
	Error             *string   `db:"error"`
	Label             []byte    `db:"label"`
}

func (d *CurioStorageDealMarket) findURLForOfflineDeals(ctx context.Context, deal string, pcid string) error {

	comm, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var updated bool
		err = tx.QueryRow(`
						WITH selected_data AS (
						SELECT url, headers, raw_size
						FROM market_offline_urls
						WHERE uuid = $1
						)
						UPDATE market_mk12_deal_pipeline
						SET url = selected_data.url,
							headers = selected_data.headers,
							raw_size = selected_data.raw_size,
							started = TRUE
						FROM selected_data
						WHERE market_mk12_deal_pipeline.uuid = $1
						RETURNING CASE 
							WHEN EXISTS (SELECT 1 FROM selected_data) THEN TRUE 
							ELSE FALSE 
						END;`, deal).Scan(&updated)
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return false, xerrors.Errorf("failed to update the pipeline for deal %s: %w", deal, err)
			}
		}

		if updated {
			return true, nil
		}

		// Check if We can find the URL for this piece on remote servers
		for rUrl, headers := range d.urls {
			// Create a new HTTP request
			urlString := fmt.Sprintf("%s?id=%s", rUrl, pcid)
			req, err := http.NewRequest(http.MethodHead, urlString, nil)
			if err != nil {
				return false, xerrors.Errorf("error creating request: %w", err)
			}

			req.Header = headers

			// Create a client and make the request
			client := &http.Client{
				Timeout: 10 * time.Second,
			}
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

			_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET url = $1, headers = $2, raw_size = $3, started = TRUE 
                           WHERE uuid = $4 AND started = FALSE`, urlString, hdrs, rawSize, deal)
			if err != nil {
				return false, xerrors.Errorf("store url for piece %s: updating pipeline: %w", pcid, err)
			}

			return true, nil
		}
		return false, nil

	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("deal %s: %w", deal, err)
	}
	if !comm {
		return xerrors.Errorf("faile to commit the transaction for deal %s", deal)
	}
	return nil
}

func (d *CurioStorageDealMarket) addPSDTask(ctx context.Context) error {
	publishPeriod := time.Duration(d.cfg.Market.StorageMarketConfig.MK12.PublishMsgPeriod)
	maxDeals := d.cfg.Market.StorageMarketConfig.MK12.MaxDealsPerPublishMsg

	d.adders[pollerPSD].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, err error) {
		n, err := tx.Exec(`WITH grouped_deals AS (
									-- Step 1: Group by sp_id and get the earliest psd_wait_time for each sp_id
									SELECT 
										sp_id,
										MIN(psd_wait_time) AS earliest_wait_time
									FROM market_mk12_deal_pipeline
									WHERE started = TRUE
									  AND after_commp = TRUE
									  AND psd_task_id IS NULL
									  AND after_psd = FALSE
									GROUP BY sp_id
								),
								eligible_sp_ids AS (
									-- Step 2: Select sp_ids where at least one deal has waited past publishPeriod
									SELECT sp_id
									FROM grouped_deals
									WHERE earliest_wait_time + INTERVAL '1 second' * $1 < NOW()
								),
								eligible_deals AS (
									-- Step 3: Select all deals for those sp_ids, ensuring selection is ordered by earliest psd_wait_time
									SELECT d.uuid, d.sp_id, d.psd_wait_time
									FROM market_mk12_deal_pipeline d
									JOIN eligible_sp_ids e ON d.sp_id = e.sp_id
									WHERE d.started = TRUE
									  AND d.after_commp = TRUE
									  AND d.psd_task_id IS NULL
									  AND d.after_psd = FALSE
									ORDER BY d.psd_wait_time ASC -- Ensures deals are selected based on the earliest time first
								),
								deals_to_update AS (
									-- Step 4: Select only the first maxDeals deals (ensuring no more than maxDeals are updated)
									SELECT uuid
									FROM eligible_deals
									LIMIT $2
								)
								-- Step 5: Update only the selected maxDeals
								UPDATE market_mk12_deal_pipeline
								SET psd_task_id = $3
								WHERE uuid IN (SELECT uuid FROM deals_to_update);
								`, publishPeriod.Seconds(), maxDeals, id)
		if err != nil {
			return false, xerrors.Errorf("creating psd tasks: %w", err)
		}

		return n > 0, nil
	})

	return nil
}

func (d *CurioStorageDealMarket) ingestDeal(ctx context.Context, deal MK12Pipeline) error {
	var sector *abi.SectorNumber

	comm, err := d.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Prepare a variable to hold the result
		var dbdeals []MarketMK12Deal

		err = tx.Select(&dbdeals, `SELECT 
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
										fast_retrieval,
										announce_to_ipni,
										error,
										label
									FROM market_mk12_deals
									WHERE uuid = $1;`, deal.UUID)
		if err != nil {
			return false, xerrors.Errorf("failed to get MK12 deals from DB: %w", err)
		}

		if len(dbdeals) != 1 {
			return false, xerrors.Errorf("expected 1 deal, got %d for UUID %s", len(dbdeals), deal.UUID)
		}

		dbdeal := dbdeals[0]

		maddr, err := address.NewIDAddress(uint64(dbdeal.SpID))
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
		}

		var prop market.DealProposal
		err = json.Unmarshal(dbdeal.Proposal, &prop)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
		}

		// Unmarshal Label from cbor and replace in proposal. This fixes the problem where non-string
		// labels are saved as "" in json in DB
		var l market.DealLabel
		lr := bytes.NewReader(dbdeal.Label)
		err = l.UnmarshalCBOR(lr)
		if err != nil {
			return false, xerrors.Errorf("unmarshal label: %w", err)
		}
		prop.Label = l

		pcid, err := cid.Parse(dbdeal.PublishCid)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
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

		dealUrl, err := url.Parse(*deal.URL)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
		}

		headers := make(http.Header)
		err = json.Unmarshal(deal.Headers, &headers)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: %w", deal.UUID, err)
		}

		var shouldProceed bool

		err = tx.QueryRow(`SELECT EXISTS(SELECT TRUE FROM market_mk12_deal_pipeline WHERE uuid = $1 AND sector IS NULL)`, deal.UUID).Scan(&shouldProceed)
		if err != nil {
			return false, xerrors.Errorf("failed to check status in DB before adding to sector: %w", err)
		}

		if !shouldProceed {
			// Exit early
			return false, xerrors.Errorf("deal %s already added to sector by another process", deal.UUID)
		}

		var sp *abi.RegisteredSealProof
		sector, sp, err = d.pin.AllocatePieceToSector(ctx, tx, maddr, pi, deal.RawSize.Int64, *dealUrl, headers)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: failed to add deal to a sector: %w", deal.UUID, err)
		}

		n, err := tx.Exec(`UPDATE market_mk12_deal_pipeline SET sector = $1, reg_seal_proof = $2 WHERE uuid = $3 AND sector IS NULL`, *sector, *sp, deal.UUID)
		if err != nil {
			return false, xerrors.Errorf("UUID: %s: failed to add sector %d details to DB: %w", deal.UUID, *sector, err)
		}
		if n != 1 {
			return false, xerrors.Errorf("UUID: %s: expected 1 deal update for add sector %d details to DB but found %d", deal.UUID, *sector, n)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("UUID: %s: failed to add deal to a sector: %w", deal.UUID, err)
	}

	if !comm {
		return xerrors.Errorf("UUID: %s: failed to commit transaction: %w", deal.UUID, err)
	}

	log.Infof("Added deal %s to sector %d", deal.UUID, *sector)
	return nil
}

func (d *CurioStorageDealMarket) createIndexingTaskForMigratedDeals(ctx context.Context) {
	// Call the migration function and get the number of rows moved
	rowsMoved, err := d.db.Exec(ctx, "SELECT migrate_deal_pipeline_entries()")
	if err != nil {
		log.Errorf("Error creating indexing tasks for migrated deals: %s", err)
		return
	}
	log.Debugf("Successfully created indexing tasks for %d migrated deals", rowsMoved)
}
