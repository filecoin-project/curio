package backpressure

import (
	"context"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/jellydator/ttlcache/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/lib/lazy"
)

var log = logging.Logger("backpressure")

const (
	sectorBackpressureKey = "sb"
	mk12BackpressureKey   = "mk12bp"
	mk20BackpressureKey   = "mk20bp"
)

type CachedBackPressure struct {
	cache *ttlcache.Cache
}

func (c *CachedBackPressure) checkSectorBackpressure(ctx context.Context, cfg *config.CurioIngestConfig, db *harmonydb.DB) (bool, error) {
	if cfg.DoSnap {
		var bufferedEncode, bufferedProve, waitDealSectors int
		err := db.QueryRow(ctx, `
		WITH BufferedEncode AS (
			SELECT COUNT(p.task_id_encode) - COUNT(t.owner_id) AS buffered_encode
			FROM sectors_snap_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_encode = t.id
			WHERE p.after_encode = false
		),
		 BufferedProve AS (
			 SELECT COUNT(p.task_id_prove) - COUNT(t.owner_id) AS buffered_prove
			 FROM sectors_snap_pipeline p
			 LEFT JOIN harmony_task t ON p.task_id_prove = t.id
			 WHERE p.after_prove = true AND p.after_move_storage = false
		 ),
		 WaitDealSectors AS (
			SELECT COUNT(DISTINCT osp.sector_number) AS wait_deal_sectors_count
			FROM open_sector_pieces osp
			LEFT JOIN sectors_snap_initial_pieces sip 
				 ON osp.sector_number = sip.sector_number
			WHERE sip.sector_number IS NULL
		 )
		SELECT
			(SELECT buffered_encode FROM BufferedEncode) AS total_encode,
			(SELECT buffered_prove FROM BufferedProve) AS buffered_prove,
			(SELECT wait_deal_sectors_count FROM WaitDealSectors) AS wait_deal_sectors_count
		`).Scan(&bufferedEncode, &bufferedProve, &waitDealSectors)
		if err != nil {
			return false, xerrors.Errorf("counting buffered sectors: %w", err)
		}

		if cfg.MaxQueueDealSector.Get() != 0 && waitDealSectors > cfg.MaxQueueDealSector.Get() {
			log.Infow("backpressure", "reason", "too many wait deal sectors", "wait_deal_sectors", waitDealSectors, "max", cfg.MaxQueueDealSector.Get())
			return true, nil
		}

		if cfg.MaxQueueSnapEncode.Get() != 0 && bufferedEncode > cfg.MaxQueueSnapEncode.Get() {
			log.Infow("backpressure", "reason", "too many encode tasks", "buffered", bufferedEncode, "max", cfg.MaxQueueSnapEncode.Get())
			return true, nil
		}

		if cfg.MaxQueueSnapProve.Get() != 0 && bufferedProve > cfg.MaxQueueSnapProve.Get() {
			log.Infow("backpressure", "reason", "too many prove tasks", "buffered", bufferedProve, "max", cfg.MaxQueueSnapProve.Get())
			return true, nil
		}
	}
	var bufferedSDR, bufferedTrees, bufferedPoRep, waitDealSectors int
	err := db.QueryRow(ctx, `
		WITH BufferedSDR AS (
			SELECT COUNT(p.task_id_sdr) - COUNT(t.owner_id) AS buffered_sdr_count
			FROM sectors_sdr_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_sdr = t.id
			WHERE p.after_sdr = false
		),
		BufferedTrees AS (
			SELECT COUNT(p.task_id_tree_r) - COUNT(t.owner_id) AS buffered_trees_count
			FROM sectors_sdr_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_tree_r = t.id
			WHERE p.after_sdr = true AND p.after_tree_r = false
		),
		BufferedPoRep AS (
			SELECT COUNT(p.task_id_porep) - COUNT(t.owner_id) AS buffered_porep_count
			FROM sectors_sdr_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_porep = t.id
			WHERE p.after_tree_r = true AND p.after_porep = false
		),
		WaitDealSectors AS (
			SELECT COUNT(DISTINCT osp.sector_number) AS wait_deal_sectors_count
			FROM open_sector_pieces osp
			LEFT JOIN sectors_sdr_initial_pieces sip 
				 ON osp.sector_number = sip.sector_number
			WHERE sip.sector_number IS NULL
		)
		SELECT
			(SELECT buffered_sdr_count FROM BufferedSDR) AS total_buffered_sdr,
			(SELECT buffered_trees_count FROM BufferedTrees) AS buffered_trees_count,
			(SELECT buffered_porep_count FROM BufferedPoRep) AS buffered_porep_count,
			(SELECT wait_deal_sectors_count FROM WaitDealSectors) AS wait_deal_sectors_count
		`).Scan(&bufferedSDR, &bufferedTrees, &bufferedPoRep, &waitDealSectors)
	if err != nil {
		return false, xerrors.Errorf("counting buffered sectors: %w", err)
	}

	if cfg.MaxQueueDealSector.Get() != 0 && waitDealSectors > cfg.MaxQueueDealSector.Get() {
		log.Infow("backpressure", "reason", "too many wait deal sectors", "wait_deal_sectors", waitDealSectors, "max", cfg.MaxQueueDealSector.Get())
		return true, nil
	}

	if bufferedSDR > cfg.MaxQueueSDR.Get() {
		log.Infow("backpressure", "reason", "too many SDR tasks", "buffered", bufferedSDR, "max", cfg.MaxQueueSDR.Get())
		return true, nil
	}
	if cfg.MaxQueueTrees.Get() != 0 && bufferedTrees > cfg.MaxQueueTrees.Get() {
		log.Infow("backpressure", "reason", "too many tree tasks", "buffered", bufferedTrees, "max", cfg.MaxQueueTrees.Get())
		return true, nil
	}
	if cfg.MaxQueuePoRep.Get() != 0 && bufferedPoRep > cfg.MaxQueuePoRep.Get() {
		log.Infow("backpressure", "reason", "too many PoRep tasks", "buffered", bufferedPoRep, "max", cfg.MaxQueuePoRep.Get())
		return true, nil
	}
	return false, nil
}

func (c *CachedBackPressure) checkMK20Backpressure(ctx context.Context, cfg *config.CurioIngestConfig, db *harmonydb.DB) (bool, error) {
	var runningPipelines, downloadingPending, commpPending, aggPending int64
	err := db.QueryRow(ctx, `WITH pipeline_data AS (
										SELECT dp.id,
											   dp.aggr_index,
											   dp.complete,
											   dp.started,
											   dp.downloaded,
											   dp.aggregated,
											   dp.commp_task_id,
											   dp.agg_task_id,
											   dp.indexing_task_id,
											   pp.task_id AS downloading_task_id
										FROM market_mk20_pipeline dp
										LEFT JOIN parked_pieces pp
										  ON pp.piece_cid = dp.piece_cid
										 AND pp.piece_padded_size = dp.piece_size
										WHERE dp.complete = false
									),
									joined AS (
										SELECT p.*,
											   dt.owner_id AS downloading_owner,
											   ct.owner_id AS commp_owner,
											   at.owner_id AS agg_owner,
											   it.owner_id AS index_owner
										FROM pipeline_data p
										LEFT JOIN harmony_task dt ON dt.id = p.downloading_task_id
										LEFT JOIN harmony_task ct ON ct.id = p.commp_task_id
										LEFT JOIN harmony_task at ON at.id = p.agg_task_id
										LEFT JOIN harmony_task it ON it.id = p.indexing_task_id
									)
									SELECT
										COUNT(DISTINCT id) FILTER (
											WHERE (downloading_task_id IS NOT NULL AND downloading_owner IS NOT NULL)
											   OR (commp_task_id IS NOT NULL AND commp_owner IS NOT NULL)
											   OR (agg_task_id IS NOT NULL AND agg_owner IS NOT NULL)
											   OR (indexing_task_id IS NOT NULL AND index_owner IS NOT NULL)
										) AS running_pipelines,
									
										COUNT(DISTINCT id) FILTER (
											WHERE downloading_task_id IS NOT NULL AND downloading_owner IS NULL
										) AS downloading_pending,
									
										COUNT(DISTINCT id) FILTER (
											WHERE commp_task_id IS NOT NULL AND commp_owner IS NULL
										) AS commp_pending,
									
										COUNT(DISTINCT id) FILTER (
											WHERE agg_task_id IS NOT NULL AND agg_owner IS NULL
										) AS agg_pending,
									FROM joined;`).Scan(&runningPipelines, &downloadingPending, &commpPending, &aggPending)
	if err != nil {
		return false, xerrors.Errorf("failed to query market pipeline backpressure stats: %w", err)
	}

	if cfg.MaxMarketRunningPipelines.Get() != 0 && runningPipelines > int64(cfg.MaxMarketRunningPipelines.Get()) {
		log.Infow("backpressure", "reason", "too many running market pipelines", "running_pipelines", runningPipelines, "max", cfg.MaxMarketRunningPipelines.Get())
		return true, nil
	}

	if cfg.MaxQueueDownload.Get() != 0 && downloadingPending > int64(cfg.MaxQueueDownload.Get()) {
		log.Infow("backpressure", "reason", "too many pending downloads", "pending_downloads", downloadingPending, "max", cfg.MaxQueueDownload.Get())
		return true, nil
	}

	if cfg.MaxQueueCommP.Get() != 0 && commpPending > int64(cfg.MaxQueueCommP.Get()) {
		log.Infow("backpressure", "reason", "too many pending CommP tasks", "pending_commp", commpPending, "max", cfg.MaxQueueCommP.Get())
		return true, nil
	}

	// TODO: add aggregation check, upload queue, download queue and offline queue
	return false, nil
}

func (c *CachedBackPressure) checkMK12Backpressure(ctx context.Context, cfg *config.CurioIngestConfig, db *harmonydb.DB) (bool, error) {
	// Check market pipeline conditions
	// We reuse the pipeline stages logic from PipelineStatsMarket to determine
	// how many pipelines are running and how many are queued at downloading/verify stages.
	var runningPipelines, downloadingPending, verifyPending int64
	err := db.QueryRow(ctx, `
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
						joined AS (
							SELECT p.*,
								   dt.owner_id AS downloading_owner,
								   ct.owner_id AS commp_owner,
								   pt.owner_id AS psd_owner,
								   ft.owner_id AS find_deal_owner,
								   it.owner_id AS index_owner
							FROM pipeline_data p
							LEFT JOIN harmony_task dt ON dt.id = p.downloading_task_id
							LEFT JOIN harmony_task ct ON ct.id = p.commp_task_id
							LEFT JOIN harmony_task pt ON pt.id = p.psd_task_id
							LEFT JOIN harmony_task ft ON ft.id = p.find_deal_task_id
							LEFT JOIN harmony_task it ON it.id = p.indexing_task_id
						)
						SELECT
							COUNT(DISTINCT uuid) FILTER (
								WHERE (downloading_task_id IS NOT NULL AND downloading_owner IS NOT NULL)
								   OR (commp_task_id IS NOT NULL AND commp_owner IS NOT NULL)
								   OR (psd_task_id IS NOT NULL AND psd_owner IS NOT NULL)
								   OR (find_deal_task_id IS NOT NULL AND find_deal_owner IS NOT NULL)
								   OR (indexing_task_id IS NOT NULL AND index_owner IS NOT NULL)
							) AS running_pipelines,
							COUNT(*) FILTER (WHERE downloading_task_id IS NOT NULL AND downloading_owner IS NULL) AS downloading_pending,
							COUNT(*) FILTER (WHERE commp_task_id IS NOT NULL AND commp_owner IS NULL) AS verify_pending
						FROM joined
						`).Scan(&runningPipelines, &downloadingPending, &verifyPending)
	if err != nil {
		return false, xerrors.Errorf("failed to query market pipeline backpressure stats: %w", err)
	}

	if cfg.MaxMarketRunningPipelines.Get() != 0 && runningPipelines > int64(cfg.MaxMarketRunningPipelines.Get()) {
		log.Infow("backpressure", "reason", "too many running market pipelines", "running_pipelines", runningPipelines, "max", cfg.MaxMarketRunningPipelines.Get())
		return true, nil
	}

	if cfg.MaxQueueDownload.Get() != 0 && downloadingPending > int64(cfg.MaxQueueDownload.Get()) {
		log.Infow("backpressure", "reason", "too many pending downloads", "pending_downloads", downloadingPending, "max", cfg.MaxQueueDownload.Get())
		return true, nil
	}

	if cfg.MaxQueueCommP.Get() != 0 && verifyPending > int64(cfg.MaxQueueCommP.Get()) {
		log.Infow("backpressure", "reason", "too many pending CommP tasks", "pending_commp", verifyPending, "max", cfg.MaxQueueCommP.Get())
		return true, nil
	}
	return false, nil
}

func NewCachedBackPressure() *lazy.Lazy[*CachedBackPressure] {
	return lazy.MakeLazy(func() (*CachedBackPressure, error) {
		log.Info("initializing backpressure module")
		c := ttlcache.NewCache()
		c.SkipTTLExtensionOnHit(true)
		return &CachedBackPressure{cache: c}, nil
	})
}

func (c *CachedBackPressure) SectorPressure(ctx context.Context, cfg *config.CurioIngestConfig, db *harmonydb.DB) (bool, error) {
	pressure, err := c.cache.Get(sectorBackpressureKey)
	if err == nil {
		return pressure.(bool), nil
	}
	p, err := c.checkSectorBackpressure(ctx, cfg, db)
	if err != nil {
		return false, err
	}
	_ = c.cache.SetWithTTL(sectorBackpressureKey, p, time.Minute*10)
	return p, nil
}

func (c *CachedBackPressure) MK12Pressure(ctx context.Context, cfg *config.CurioIngestConfig, db *harmonydb.DB) (bool, error) {
	pressure, err := c.cache.Get(mk12BackpressureKey)
	if err == nil {
		return pressure.(bool), nil
	}
	p, err := c.checkMK12Backpressure(ctx, cfg, db)
	if err != nil {
		return false, err
	}
	_ = c.cache.SetWithTTL(mk12BackpressureKey, p, time.Minute*2)
	return p, nil
}

func (c *CachedBackPressure) MK20Pressure(ctx context.Context, cfg *config.CurioIngestConfig, db *harmonydb.DB) (bool, error) {
	pressure, err := c.cache.Get(mk20BackpressureKey)
	if err == nil {
		return pressure.(bool), nil
	}
	p, err := c.checkMK20Backpressure(ctx, cfg, db)
	if err != nil {
		return false, err
	}
	_ = c.cache.SetWithTTL(mk20BackpressureKey, p, time.Minute*2)
	return p, nil
}
