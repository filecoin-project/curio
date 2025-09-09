package webrpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/oklog/ulid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"
)

type MK20StorageDeal struct {
	Deal   *mk20.Deal     `json:"deal"`
	DDOErr sql.NullString `json:"ddoerr"`
	PDPErr sql.NullString `json:"pdperr"`
	DDOId  sql.NullInt64  `json:"ddoid"`
}

func (a *WebRPC) MK20DDOStorageDeal(ctx context.Context, id string) (*MK20StorageDeal, error) {
	pid, err := ulid.Parse(id)
	if err != nil {
		return nil, xerrors.Errorf("parsing deal ID: %w", err)
	}

	var dbDeals []mk20.DBDeal
	err = a.deps.DB.Select(ctx, &dbDeals, `SELECT id,
       												client,
													data, 
													ddo_v1,
													retrieval_v1,
													pdp_v1 FROM market_mk20_deal WHERE id = $1`, pid.String())
	if err != nil {
		return nil, xerrors.Errorf("getting deal from DB: %w", err)
	}
	if len(dbDeals) != 1 {
		return nil, xerrors.Errorf("expected 1 deal, got %d", len(dbDeals))
	}
	dbDeal := dbDeals[0]
	deal, err := dbDeal.ToDeal()
	if err != nil {
		return nil, xerrors.Errorf("converting DB deal to struct: %w", err)
	}

	ret := &MK20StorageDeal{Deal: deal}

	if len(dbDeal.DDOv1) > 0 && string(dbDeal.DDOv1) != "null" {
		var dddov1 mk20.DBDDOV1
		if err := json.Unmarshal(dbDeal.DDOv1, &dddov1); err != nil {
			return nil, fmt.Errorf("unmarshal ddov1: %w", err)
		}
		if dddov1.Error != "" {
			ret.DDOErr = sql.NullString{String: dddov1.Error, Valid: true}
		}
		if dddov1.DealID > 0 {
			ret.DDOId = sql.NullInt64{Int64: dddov1.DealID, Valid: true}
		}
	}

	if len(dbDeal.PDPV1) > 0 && string(dbDeal.PDPV1) != "null" {
		var pdpv1 mk20.DBPDPV1
		if err := json.Unmarshal(dbDeal.PDPV1, &pdpv1); err != nil {
			return nil, fmt.Errorf("unmarshal pdpv1: %w", err)
		}
		if pdpv1.Error != "" {
			ret.PDPErr = sql.NullString{String: pdpv1.Error, Valid: true}
		}
	}

	return ret, nil
}

type MK20StorageDealList struct {
	ID         string         `db:"id" json:"id"`
	CreatedAt  time.Time      `db:"created_at" json:"created_at"`
	PieceCidV2 sql.NullString `db:"piece_cid_v2" json:"piece_cid_v2"`
	Processed  bool           `db:"processed" json:"processed"`
	Error      sql.NullString `db:"error" json:"error"`
	Miner      sql.NullString `db:"miner" json:"miner"`
}

func (a *WebRPC) MK20DDOStorageDeals(ctx context.Context, limit int, offset int) ([]*MK20StorageDealList, error) {
	var mk20Summaries []*MK20StorageDealList

	err := a.deps.DB.Select(ctx, &mk20Summaries, `SELECT
    												  d.created_at,
													  d.id,
													  d.piece_cid_v2,
													  (d.ddo_v1->'ddo'->>'provider')::text AS miner,
													  (d.ddo_v1->>'error')::text AS error,
													  CASE
														WHEN EXISTS (
														  SELECT 1 FROM market_mk20_pipeline_waiting w
														  WHERE w.id = d.id
														) THEN FALSE
														WHEN EXISTS (
														  SELECT 1 FROM market_mk20_pipeline p
														  WHERE p.id = d.id AND p.complete = FALSE
														) THEN FALSE
														ELSE TRUE
													  END AS processed
													FROM market_mk20_deal d
													WHERE d.ddo_v1 IS NOT NULL AND d.ddo_v1 != 'null'
													ORDER BY d.created_at DESC
													LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal list: %w", err)
	}

	return mk20Summaries, nil
}

func (a *WebRPC) MK20DDOPipelines(ctx context.Context, limit int, offset int) ([]*MK20DDOPipeline, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var pipelines []*MK20DDOPipeline
	err := a.deps.DB.Select(ctx, &pipelines, `
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
        	ORDER BY created_at DESC
        	LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deal pipelines: %w", err)
	}

	for _, s := range pipelines {
		addr, err := address.NewIDAddress(uint64(s.SpId))
		if err != nil {
			return nil, xerrors.Errorf("failed to parse the miner ID: %w", err)
		}
		s.Miner = addr.String()
	}

	return pipelines, nil
}

type MK20PipelineFailedStats struct {
	DownloadingFailed int64
	CommPFailed       int64
	AggFailed         int64
	IndexFailed       int64
}

func (a *WebRPC) MK20PipelineFailedTasks(ctx context.Context) (*MK20PipelineFailedStats, error) {
	// We'll create a similar query, but this time we coalesce the task IDs from harmony_task.
	// If the join fails (no matching harmony_task), all joined fields for that task will be NULL.
	// We detect failure by checking that xxx_task_id IS NOT NULL, after_xxx = false, and that no task record was found in harmony_task.

	const query = `
	WITH pipeline_data AS (
		SELECT dp.id,
			   dp.complete,
			   dp.commp_task_id,
			   dp.agg_task_id,
			   dp.indexing_task_id,
			   dp.sector,
			   dp.after_commp,
			   dp.aggregated,
			   pp.task_id AS downloading_task_id
		FROM market_mk20_pipeline dp
		LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid AND pp.piece_padded_size = dp.piece_size
		WHERE dp.complete = false
	),
	tasks AS (
		SELECT p.*,
			   dt.id AS downloading_tid,
			   ct.id AS commp_tid,
			   pt.id AS agg_tid,
			   it.id AS index_tid
		FROM pipeline_data p
		LEFT JOIN harmony_task dt ON dt.id = p.downloading_task_id
		LEFT JOIN harmony_task ct ON ct.id = p.commp_task_id
		LEFT JOIN harmony_task pt ON pt.id = p.agg_task_id
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
	
		-- Aggregation failed:
		-- agg_task_id IS NOT NULL, aggregated = false, agg_tid IS NULL
		COUNT(*) FILTER (
			WHERE agg_task_id IS NOT NULL
			  AND aggregated = false
			  AND agg_tid IS NULL
		) AS agg_failed,
	
		-- Index failed:
		-- indexing_task_id IS NOT NULL and if we assume indexing is after find_deal:
		-- If indexing_task_id is set, we are presumably at indexing stage.
		-- If index_tid IS NULL (no task found), then it's failed.
		-- We don't have after_index, now at indexing.
		COUNT(*) FILTER (
			WHERE indexing_task_id IS NOT NULL
			  AND index_tid IS NULL
			  AND aggregated = true
		) AS index_failed
	FROM tasks
	`

	var c []struct {
		DownloadingFailed int64 `db:"downloading_failed"`
		CommPFailed       int64 `db:"commp_failed"`
		AggFailed         int64 `db:"agg_failed"`
		IndexFailed       int64 `db:"index_failed"`
	}

	err := a.deps.DB.Select(ctx, &c, query)
	if err != nil {
		return nil, xerrors.Errorf("failed to run failed task query: %w", err)
	}

	counts := c[0]

	return &MK20PipelineFailedStats{
		DownloadingFailed: counts.DownloadingFailed,
		CommPFailed:       counts.CommPFailed,
		AggFailed:         counts.AggFailed,
		IndexFailed:       counts.IndexFailed,
	}, nil
}

func (a *WebRPC) MK20BulkRestartFailedMarketTasks(ctx context.Context, taskType string) error {
	didCommit, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rows *harmonydb.Query
		var err error

		switch taskType {
		case "downloading":
			rows, err = tx.Query(`
							SELECT pp.task_id
							FROM market_mk20_pipeline dp
							LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid AND pp.piece_padded_size = dp.piece_size
							LEFT JOIN harmony_task h ON h.id = pp.task_id
							WHERE dp.downloaded = false
							  AND h.id IS NULL
						`)
		case "commp":
			rows, err = tx.Query(`
							SELECT dp.commp_task_id
							FROM market_mk20_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.commp_task_id
							WHERE dp.complete = false
							  AND dp.downloaded = true
							  AND dp.commp_task_id IS NOT NULL
							  AND dp.after_commp = false
							  AND h.id IS NULL
						`)
		case "aggregate":
			rows, err = tx.Query(`
							SELECT dp.agg_task_id
							FROM market_mk20_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.agg_task_id
							WHERE dp.complete = false
							  AND dp.after_commp = true
							  AND dp.agg_task_id IS NOT NULL
							  AND dp.aggregated = false
							  AND h.id IS NULL
						`)
		case "index":
			rows, err = tx.Query(`
							SELECT dp.indexing_task_id
							FROM market_mk20_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.indexing_task_id
							WHERE dp.complete = false
							  AND dp.indexing_task_id IS NOT NULL
							  AND dp.sealed = true
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
			if errors.Is(err, pgx.ErrNoRows) {
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

func (a *WebRPC) MK20BulkRemoveFailedMarketPipelines(ctx context.Context, taskType string) error {
	didCommit, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rows *harmonydb.Query
		var err error

		// We'll select pipeline fields directly based on the stage conditions
		switch taskType {
		case "downloading":
			rows, err = tx.Query(`
				SELECT dp.id, dp.url, dp.sector,
				       dp.commp_task_id, dp.agg_task_id, dp.indexing_task_id
				FROM market_mk20_pipeline dp
				LEFT JOIN parked_pieces pp ON pp.piece_cid = dp.piece_cid AND pp.piece_padded_size = dp.piece_size
				LEFT JOIN harmony_task h ON h.id = pp.task_id
				WHERE dp.complete = false
				  AND dp.downloaded = false
				  AND pp.task_id IS NOT NULL
				  AND h.id IS NULL
			`)
		case "commp":
			rows, err = tx.Query(`
				SELECT dp.id, dp.url, dp.sector,
				        dp.commp_task_id, dp.agg_task_id, dp.indexing_task_id
				FROM market_mk20_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.commp_task_id
				WHERE dp.complete = false
				  AND dp.downloaded = true
				  AND dp.commp_task_id IS NOT NULL
				  AND dp.after_commp = false
				  AND h.id IS NULL
			`)
		case "aggregate":
			rows, err = tx.Query(`
				SELECT dp.id, dp.url, dp.sector,
				        dp.commp_task_id, dp.agg_task_id, dp.indexing_task_id
				FROM market_mk20_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.agg_task_id
				WHERE dp.complete = false
				  AND after_commp = true
				  AND dp.agg_task_id IS NOT NULL
				  AND dp.aggregated = false
				  AND h.id IS NULL
			`)
		case "index":
			rows, err = tx.Query(`
				SELECT dp.id, dp.url, dp.sector,
				       dp.commp_task_id, dp.agg_task_id, dp.indexing_task_id
				FROM market_mk20_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.indexing_task_id
				WHERE dp.complete = false
				  AND sealed = true
				  AND dp.indexing_task_id IS NOT NULL
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
			id             string
			url            string
			sector         sql.NullInt64
			commpTaskID    sql.NullInt64
			aggTaskID      sql.NullInt64
			indexingTaskID sql.NullInt64
		}

		var pipelines []pipelineInfo
		for rows.Next() {
			var p pipelineInfo
			if err := rows.Scan(&p.id, &p.url, &p.sector, &p.commpTaskID, &p.aggTaskID, &p.indexingTaskID); err != nil {
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
			if p.aggTaskID.Valid {
				taskIDs = append(taskIDs, p.aggTaskID.Int64)
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
					return false, fmt.Errorf("cannot remove deal pipeline %s: tasks are still running", p.id)
				}
			}

			_, err = tx.Exec(`UPDATE market_mk20_deal SET error = $1 WHERE id = $2`, "Deal pipeline removed by SP", p.id)
			if err != nil {
				return false, xerrors.Errorf("store deal failure: updating deal pipeline: %w", err)
			}

			_, err = tx.Exec(`DELETE FROM market_mk20_pipeline WHERE id = $1`, p.id)
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
						return false, fmt.Errorf("invalid refID in URL for pipeline %s: %v", p.id, err)
					}
					_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
					if err != nil {
						return false, fmt.Errorf("failed to remove parked_piece_refs for pipeline %s: %w", p.id, err)
					}
				}
			}

			log.Infow("removed failed pipeline", "id", p.id)
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

func (a *WebRPC) AddMarketContract(ctx context.Context, contract, abiString string) error {
	if contract == "" {
		return fmt.Errorf("empty contract")
	}
	if abiString == "" {
		return fmt.Errorf("empty abi")
	}

	if !strings.HasPrefix(contract, "0x") {
		return fmt.Errorf("contract must start with 0x")
	}

	if !common.IsHexAddress(contract) {
		return fmt.Errorf("invalid contract address")
	}

	ethabi, err := eabi.JSON(strings.NewReader(abiString))
	if err != nil {
		return fmt.Errorf("invalid abi: %w", err)
	}

	if len(ethabi.Methods) == 0 {
		return fmt.Errorf("invalid abi: no methods")
	}

	n, err := a.deps.DB.Exec(ctx, `INSERT INTO ddo_contracts (address, abi) VALUES ($1, $2) ON CONFLICT (address) DO NOTHING`, contract, abiString)
	if err != nil {
		return xerrors.Errorf("failed to add contract: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("contract already exists")
	}
	return nil
}

func (a *WebRPC) UpdateMarketContract(ctx context.Context, contract, abiString string) error {
	if contract == "" {
		return fmt.Errorf("empty contract")
	}

	if abiString == "" {
		return fmt.Errorf("empty abi")
	}

	if !strings.HasPrefix(contract, "0x") {
		return fmt.Errorf("contract must start with 0x")
	}

	if !common.IsHexAddress(contract) {
		return fmt.Errorf("invalid contract address")
	}

	ethabi, err := eabi.JSON(strings.NewReader(abiString))
	if err != nil {
		return fmt.Errorf("invalid abi: %w", err)
	}

	if len(ethabi.Methods) == 0 {
		return fmt.Errorf("invalid abi: no methods")
	}

	// Check if contract exists in DB
	var count int
	err = a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM ddo_contracts WHERE address = $1`, contract).Scan(&count)
	if err != nil {
		return xerrors.Errorf("failed to check contract: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("contract does not exist")
	}

	n, err := a.deps.DB.Exec(ctx, `UPDATE ddo_contracts SET abi = $2 WHERE address = $1`, contract, abiString)
	if err != nil {
		return xerrors.Errorf("failed to update contract ABI: %w", err)
	}

	if n == 0 {
		return fmt.Errorf("failed to update the contract ABI")
	}

	return nil
}

func (a *WebRPC) RemoveMarketContract(ctx context.Context, contract string) error {
	if contract == "" {
		return fmt.Errorf("empty contract")
	}
	if !strings.HasPrefix(contract, "0x") {
		return fmt.Errorf("contract must start with 0x")
	}
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM ddo_contracts WHERE address = $1`, contract)
	if err != nil {
		return xerrors.Errorf("failed to remove contract: %w", err)
	}
	return nil
}

func (a *WebRPC) ListMarketContracts(ctx context.Context) (map[string]string, error) {
	var contracts []struct {
		Address string `db:"address"`
		Abi     string `db:"abi"`
	}
	err := a.deps.DB.Select(ctx, &contracts, `SELECT address, abi FROM ddo_contracts`)
	if err != nil {
		return nil, xerrors.Errorf("failed to get contracts from DB: %w", err)
	}

	contractMap := make(map[string]string)
	for _, contract := range contracts {
		contractMap[contract.Address] = contract.Abi
	}

	return contractMap, nil
}

func (a *WebRPC) EnableProduct(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("empty product name")
	}

	// Check if product exists in market_mk20_products
	var count int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_products WHERE name = $1`, name).Scan(&count)
	if err != nil {
		return xerrors.Errorf("failed to check product: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("product does not exist")
	}
	n, err := a.deps.DB.Exec(ctx, `UPDATE market_mk20_products SET enabled = true WHERE name = $1`, name)
	if err != nil {
		return xerrors.Errorf("failed to enable product: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("failed to enable the product")
	}
	return nil
}

func (a *WebRPC) DisableProduct(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("empty product name")
	}

	// Check if product exists in market_mk20_products
	var count int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_products WHERE name = $1`, name).Scan(&count)
	if err != nil {
		return xerrors.Errorf("failed to check product: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("product does not exist")
	}
	n, err := a.deps.DB.Exec(ctx, `UPDATE market_mk20_products SET enabled = false WHERE name = $1`, name)
	if err != nil {
		return xerrors.Errorf("failed to disable product: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("failed to disable the product")
	}
	return nil
}

func (a *WebRPC) ListProducts(ctx context.Context) (map[string]bool, error) {
	var products []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	err := a.deps.DB.Select(ctx, &products, `SELECT name, enabled FROM market_mk20_products`)
	if err != nil {
		return nil, xerrors.Errorf("failed to get products from DB: %w", err)
	}
	productMap := make(map[string]bool)
	for _, product := range products {
		productMap[product.Name] = product.Enabled
	}
	return productMap, nil
}

func (a *WebRPC) EnableDataSource(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("empty data source name")
	}

	// check if datasource exists in market_mk20_data_source
	var count int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_data_source WHERE name = $1`, name).Scan(&count)
	if err != nil {
		return xerrors.Errorf("failed to check datasource: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("datasource does not exist")
	}
	n, err := a.deps.DB.Exec(ctx, `UPDATE market_mk20_data_source SET enabled = true WHERE name = $1`, name)
	if err != nil {
		return xerrors.Errorf("failed to enable datasource: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("failed to enable the datasource")
	}
	return nil
}

func (a *WebRPC) DisableDataSource(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("empty data source name")
	}
	// check if datasource exists in market_mk20_data_source
	var count int
	err := a.deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM market_mk20_data_source WHERE name = $1`, name).Scan(&count)
	if err != nil {
		return xerrors.Errorf("failed to check datasource: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("datasource does not exist")
	}
	n, err := a.deps.DB.Exec(ctx, `UPDATE market_mk20_data_source SET enabled = false WHERE name = $1`, name)
	if err != nil {
		return xerrors.Errorf("failed to disable datasource: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("failed to disable the datasource")
	}
	return nil
}

func (a *WebRPC) ListDataSources(ctx context.Context) (map[string]bool, error) {
	var datasources []struct {
		Name    string `db:"name"`
		Enabled bool   `db:"enabled"`
	}
	err := a.deps.DB.Select(ctx, &datasources, `SELECT name, enabled FROM market_mk20_data_source`)
	if err != nil {
		return nil, xerrors.Errorf("failed to get datasources from DB: %w", err)
	}

	datasourceMap := make(map[string]bool)
	for _, datasource := range datasources {
		datasourceMap[datasource.Name] = datasource.Enabled
	}
	return datasourceMap, nil
}

type UploadStatus struct {
	ID     string            `json:"id"`
	Status mk20.UploadStatus `json:"status"`
}

func (a *WebRPC) ChunkUploadStatus(ctx context.Context, idStr string) (*UploadStatus, error) {
	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid chunk upload id: %w", err)
	}

	var status mk20.UploadStatus

	err = a.deps.DB.QueryRow(ctx, `SELECT
								  COUNT(*) AS total,
								  COUNT(*) FILTER (WHERE complete) AS complete,
								  COUNT(*) FILTER (WHERE NOT complete) AS missing,
								  ARRAY_AGG(chunk ORDER BY chunk) FILTER (WHERE complete) AS completed_chunks,
								  ARRAY_AGG(chunk ORDER BY chunk) FILTER (WHERE NOT complete) AS incomplete_chunks
								FROM
								  market_mk20_deal_chunk
								WHERE
								  id = $1
								GROUP BY
								  id;`, id.String()).Scan(&status.TotalChunks, &status.Uploaded, &status.Missing, &status.UploadedChunks, &status.MissingChunks)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, xerrors.Errorf("failed to get chunk upload status: %w", err)
		}
		return nil, nil
	}

	return &UploadStatus{
		ID:     idStr,
		Status: status,
	}, nil
}

// MK20PDPPipeline represents a record from market_mk20_PDP_pipeline table
type MK20PDPPipeline struct {
	ID              string `db:"id" json:"id"`
	Client          string `db:"client" json:"client"`
	PieceCidV2      string `db:"piece_cid_v2" json:"piece_cid_v2"`
	Indexing        bool   `db:"indexing" json:"indexing"`
	Announce        bool   `db:"announce" json:"announce"`
	AnnouncePayload bool   `db:"announce_payload" json:"announce_payload"`

	Downloaded bool `db:"downloaded" json:"downloaded"`

	CommpTaskId sql.NullInt64 `db:"commp_task_id" json:"commp_task_id"`
	AfterCommp  bool          `db:"after_commp" json:"after_commp"`

	DealAggregation   int           `db:"deal_aggregation" json:"deal_aggregation"`
	AggregationIndex  int64         `db:"aggr_index" json:"aggr_index"`
	AggregationTaskID sql.NullInt64 `db:"agg_task_id" json:"agg_task_id"`
	Aggregated        bool          `db:"aggregated" json:"aggregated"`

	AddPieceTaskID sql.NullInt64 `db:"add_piece_task_id" json:"add_piece_task_id"`
	AfterAddPiece  bool          `db:"after_add_piece" json:"after_add_piece"`

	AfterAddPieceMsg bool `db:"after_add_piece_msg" json:"after_add_piece_msg"`

	SaveCacheTaskID sql.NullInt64 `db:"save_cache_task_id" json:"save_cache_task_id"`
	AfterSaveCache  bool          `db:"after_save_cache" json:"after_save_cache"`

	IndexingCreatedAt sql.NullTime  `db:"indexing_created_at" json:"indexing_created_at"`
	IndexingTaskId    sql.NullInt64 `db:"indexing_task_id" json:"indexing_task_id"`
	Indexed           bool          `db:"indexed" json:"indexed"`

	Complete  bool      `db:"complete" json:"complete"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`

	Miner string `db:"-" json:"miner"`
}

type MK20PDPDealList struct {
	ID         string         `db:"id" json:"id"`
	CreatedAt  time.Time      `db:"created_at" json:"created_at"`
	PieceCidV2 sql.NullString `db:"piece_cid_v2" json:"piece_cid_v2"`
	Processed  bool           `db:"processed" json:"processed"`
	Error      sql.NullString `db:"error" json:"error"`
}

func (a *WebRPC) MK20PDPStorageDeals(ctx context.Context, limit int, offset int) ([]*MK20PDPDealList, error) {
	var pdpSummaries []*MK20PDPDealList

	err := a.deps.DB.Select(ctx, &pdpSummaries, `SELECT
    												  d.created_at,
													  d.id,
													  d.piece_cid_v2,
													  (d.pdp_v1->>'error')::text AS error,
													  (d.pdp_v1->>'complete')::boolean as processed
													FROM market_mk20_deal d
													WHERE d.pdp_v1 IS NOT NULL AND d.pdp_v1 != 'null'
													ORDER BY d.created_at DESC
													LIMIT $1 OFFSET $2;`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch PDP deal list: %w", err)
	}

	return pdpSummaries, nil
}

func (a *WebRPC) MK20PDPPipelines(ctx context.Context, limit int, offset int) ([]*MK20PDPPipeline, error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var pipelines []*MK20PDPPipeline
	err := a.deps.DB.Select(ctx, &pipelines, `
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
        	ORDER BY created_at DESC
        	LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pdp pipelines: %w", err)
	}

	return pipelines, nil
}

type MK20PDPPipelineFailedStats struct {
	DownloadingFailed int64
	CommPFailed       int64
	AggFailed         int64
	AddPieceFailed    int64
	SaveCacheFailed   int64
	IndexFailed       int64
}

func (a *WebRPC) MK20PDPPipelineFailedTasks(ctx context.Context) (*MK20PDPPipelineFailedStats, error) {
	// We'll create a similar query, but this time we coalesce the task IDs from harmony_task.
	// If the join fails (no matching harmony_task), all joined fields for that task will be NULL.
	// We detect failure by checking that xxx_task_id IS NOT NULL, after_xxx = false, and that no task record was found in harmony_task.

	const query = `
	WITH pipeline_data AS (
		  SELECT
			dp.id,
			dp.complete,
			dp.commp_task_id,
			dp.agg_task_id,
			dp.add_piece_task_id,
			dp.save_cache_task_id,
			dp.indexing_task_id,
			dp.after_commp,
			dp.aggregated,
			dp.after_add_piece,
			dp.after_save_cache,
			t.downloading_task_id
		  FROM pdp_pipeline dp
		  LEFT JOIN market_mk20_download_pipeline mdp
				 ON mdp.id = dp.id
				AND mdp.piece_cid_v2 = dp.piece_cid_v2
				AND mdp.product = $1
		  LEFT JOIN LATERAL (
			SELECT pp.task_id AS downloading_task_id
			FROM unnest(mdp.ref_ids) AS r(ref_id)
			JOIN parked_piece_refs pr ON pr.ref_id = r.ref_id
			JOIN parked_pieces pp     ON pp.id = pr.piece_id
			WHERE pp.complete = FALSE
			LIMIT 1
		  ) t ON TRUE
		  WHERE dp.complete = FALSE
	),
	tasks AS (
		SELECT p.*,
			   dt.id AS downloading_tid,
			   ct.id AS commp_tid,
			   at.id AS agg_tid,
			   ap.id as add_piece_tid,
			   sc.id as save_cache_tid,
			   it.id AS index_tid
		FROM pipeline_data p
		LEFT JOIN harmony_task dt ON dt.id = p.downloading_task_id
		LEFT JOIN harmony_task ct ON ct.id = p.commp_task_id
		LEFT JOIN harmony_task at ON at.id = p.agg_task_id
		LEFT JOIN harmony_task ap ON ap.id = p.add_piece_task_id
		LEFT JOIN harmony_task sc ON sc.id = p.save_cache_task_id
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
	
		-- Aggregation failed:
		-- agg_task_id IS NOT NULL, aggregated = false, agg_tid IS NULL
		COUNT(*) FILTER (
			WHERE agg_task_id IS NOT NULL
			  AND aggregated = false
			  AND agg_tid IS NULL
		) AS agg_failed,

		-- Add Piece failed:
		-- add_piece_task_id IS NOT NULL, after_add_piece = false, add_piece_tid IS NULL
		COUNT(*) FILTER (
			WHERE add_piece_task_id IS NOT NULL
			  AND after_add_piece = false
			  AND add_piece_tid IS NULL
		) AS add_piece_failed,

		-- Save Cache failed:
		-- save_cache_task_id IS NOT NULL, after_save_cache = false, save_cache_tid IS NULL
		COUNT(*) FILTER (
			WHERE save_cache_task_id IS NOT NULL
			  AND after_save_cache = false
			  AND save_cache_tid IS NULL
		) AS save_cache_failed,
	
		-- Index failed:
		-- indexing_task_id IS NOT NULL and if we assume indexing is after find_deal:
		-- If indexing_task_id is set, we are presumably at indexing stage.
		-- If index_tid IS NULL (no task found), then it's failed.
		-- We don't have after_index, now at indexing.
		COUNT(*) FILTER (
			WHERE indexing_task_id IS NOT NULL
			  AND index_tid IS NULL
			  AND after_save_cache = true
		) AS index_failed
	FROM tasks
	`

	var c []struct {
		DownloadingFailed int64 `db:"downloading_failed"`
		CommPFailed       int64 `db:"commp_failed"`
		AggFailed         int64 `db:"agg_failed"`
		AddPieceFailed    int64 `db:"add_piece_failed"`
		SaveCacheFailed   int64 `db:"save_cache_failed"`
		IndexFailed       int64 `db:"index_failed"`
	}

	err := a.deps.DB.Select(ctx, &c, query, mk20.ProductNamePDPV1)
	if err != nil {
		return nil, xerrors.Errorf("failed to run failed task query: %w", err)
	}

	counts := c[0]

	return &MK20PDPPipelineFailedStats{
		DownloadingFailed: counts.DownloadingFailed,
		CommPFailed:       counts.CommPFailed,
		AggFailed:         counts.AggFailed,
		AddPieceFailed:    counts.AddPieceFailed,
		SaveCacheFailed:   counts.SaveCacheFailed,
		IndexFailed:       counts.IndexFailed,
	}, nil
}

func (a *WebRPC) MK20BulkRestartFailedPDPTasks(ctx context.Context, taskType string) error {
	didCommit, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rows *harmonydb.Query
		var err error

		switch taskType {
		case "downloading":
			rows, err = tx.Query(`
							SELECT
							  t.task_id
							FROM pdp_pipeline dp
							LEFT JOIN market_mk20_download_pipeline mdp
							  ON mdp.id = dp.id
							 AND mdp.piece_cid_v2 = dp.piece_cid_v2
							 AND mdp.product = $1
							LEFT JOIN LATERAL (
							  SELECT pp.task_id
							  FROM unnest(mdp.ref_ids) AS r(ref_id)
							  JOIN parked_piece_refs pr ON pr.ref_id = r.ref_id
							  JOIN parked_pieces pp     ON pp.id = pr.piece_id
							  WHERE pp.complete = FALSE
							  LIMIT 1
							) AS t ON TRUE
							LEFT JOIN harmony_task h ON h.id = t.task_id
							WHERE dp.downloaded = FALSE
							  AND h.id IS NULL;
						`, mk20.ProductNamePDPV1)
		case "commp":
			rows, err = tx.Query(`
							SELECT dp.commp_task_id
							FROM pdp_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.commp_task_id
							WHERE dp.complete = false
							  AND dp.downloaded = true
							  AND dp.commp_task_id IS NOT NULL
							  AND dp.after_commp = false
							  AND h.id IS NULL
						`)
		case "aggregate":
			rows, err = tx.Query(`
							SELECT dp.agg_task_id
							FROM pdp_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.agg_task_id
							WHERE dp.complete = false
							  AND dp.after_commp = true
							  AND dp.agg_task_id IS NOT NULL
							  AND dp.aggregated = false
							  AND h.id IS NULL
						`)
		case "add_piece":
			rows, err = tx.Query(`
							SELECT dp.add_piece_task_id
							FROM pdp_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.add_piece_task_id
							WHERE dp.complete = false
							  AND dp.aggregated = true
							  AND dp.add_piece_task_id IS NOT NULL
							  AND dp.after_add_piece = false
							  AND h.id IS NULL
						`)
		case "save_cache":
			rows, err = tx.Query(`
							SELECT dp.save_cache_task_id
							FROM pdp_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.save_cache_task_id
							WHERE dp.complete = false
							  AND dp.after_add_piece = true
							  AND dp.after_add_piece_msg = true
							  AND dp.save_cache_task_id IS NOT NULL
							  AND dp.after_save_cache = false
							  AND h.id IS NULL
						`)
		case "index":
			rows, err = tx.Query(`
							SELECT dp.indexing_task_id
							FROM pdp_pipeline dp
							LEFT JOIN harmony_task h ON h.id = dp.indexing_task_id
							WHERE dp.complete = false
							  AND dp.indexing_task_id IS NOT NULL
							  AND dp.after_save_cache = true
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
			if errors.Is(err, pgx.ErrNoRows) {
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

func (a *WebRPC) MK20BulkRemoveFailedPDPPipelines(ctx context.Context, taskType string) error {
	didCommit, err := a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var rows *harmonydb.Query
		var err error

		// We'll select pipeline fields directly based on the stage conditions
		switch taskType {
		case "downloading":
			rows, err = tx.Query(`
				SELECT
				  dp.id,
				  dp.piece_ref,
				  dp.commp_task_id,
				  dp.agg_task_id,
				  dp.add_piece_task_id,
				  dp.save_cache_task_id,
				  dp.indexing_task_id
				FROM pdp_pipeline dp
				LEFT JOIN market_mk20_download_pipeline mdp
				  ON mdp.id = dp.id
				 AND mdp.piece_cid_v2 = dp.piece_cid_v2
				 AND mdp.product = $1
				LEFT JOIN LATERAL (
				  SELECT pp.task_id
				  FROM unnest(mdp.ref_ids) AS r(ref_id)
				  JOIN parked_piece_refs pr ON pr.ref_id = r.ref_id
				  JOIN parked_pieces pp     ON pp.id = pr.piece_id
				  WHERE pp.task_id IS NOT NULL
				  LIMIT 1
				) t ON TRUE
				LEFT JOIN harmony_task h ON h.id = t.task_id
				WHERE dp.complete = FALSE
				  AND dp.downloaded = FALSE
				  AND t.task_id IS NOT NULL
				  AND h.id IS NULL;
			`, mk20.ProductNamePDPV1)
		case "commp":
			rows, err = tx.Query(`
				SELECT dp.id, dp.piece_ref, dp.commp_task_id, dp.agg_task_id, dp.add_piece_task_id, dp.save_cache_task_id, dp.indexing_task_id
				FROM pdp_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.commp_task_id
				WHERE dp.complete = false
				  AND dp.downloaded = true
				  AND dp.commp_task_id IS NOT NULL
				  AND dp.after_commp = false
				  AND h.id IS NULL
			`)
		case "aggregate":
			rows, err = tx.Query(`
				SELECT dp.id, dp.piece_ref, dp.commp_task_id, dp.agg_task_id, dp.add_piece_task_id, dp.save_cache_task_id, dp.indexing_task_id
				FROM pdp_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.agg_task_id
				WHERE dp.complete = false
				  AND after_commp = true
				  AND dp.agg_task_id IS NOT NULL
				  AND dp.aggregated = false
				  AND h.id IS NULL
			`)
		case "add_piece":
			rows, err = tx.Query(`
				SELECT dp.id, dp.piece_ref, dp.commp_task_id, dp.agg_task_id, dp.add_piece_task_id, dp.save_cache_task_id, dp.indexing_task_id
				FROM pdp_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.agg_task_id
				WHERE dp.complete = false
				  AND aggregated = true
				  AND dp.add_piece_task_id IS NOT NULL
				  AND dp.after_add_piece = false
				  AND h.id IS NULL
			`)
		case "save_cache":
			rows, err = tx.Query(`
				SELECT dp.id, dp.piece_ref, dp.commp_task_id, dp.agg_task_id, dp.add_piece_task_id, dp.save_cache_task_id, dp.indexing_task_id
				FROM pdp_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.agg_task_id
				WHERE dp.complete = false
				  AND after_add_piece = true
				  AND after_add_piece_msg = true
				  AND dp.save_cache_task_id IS NOT NULL
				  AND dp.after_save_cache = false
				  AND h.id IS NULL
			`)
		case "index":
			rows, err = tx.Query(`
				SELECT dp.id, dp.piece_ref, dp.commp_task_id, dp.agg_task_id, dp.add_piece_task_id, dp.save_cache_task_id, dp.indexing_task_id
				FROM pdp_pipeline dp
				LEFT JOIN harmony_task h ON h.id = dp.indexing_task_id
				WHERE dp.complete = false
				  AND after_save_cache = true
				  AND dp.indexing_task_id IS NOT NULL
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
			id             string
			refID          sql.NullInt64
			commpTaskID    sql.NullInt64
			aggTaskID      sql.NullInt64
			addPieceTaskID sql.NullInt64
			saveCacheTask  sql.NullInt64
			indexingTaskID sql.NullInt64
		}

		var pipelines []pipelineInfo
		for rows.Next() {
			var p pipelineInfo
			if err := rows.Scan(&p.id, &p.refID, &p.commpTaskID, &p.aggTaskID, &p.addPieceTaskID, &p.saveCacheTask, &p.indexingTaskID); err != nil {
				return false, fmt.Errorf("failed to scan pdp pipeline info: %w", err)
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
			if p.aggTaskID.Valid {
				taskIDs = append(taskIDs, p.aggTaskID.Int64)
			}
			if p.addPieceTaskID.Valid {
				taskIDs = append(taskIDs, p.addPieceTaskID.Int64)
			}
			if p.saveCacheTask.Valid {
				taskIDs = append(taskIDs, p.saveCacheTask.Int64)
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
					return false, fmt.Errorf("cannot remove deal pipeline %s: tasks are still running", p.id)
				}
			}

			n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", p.id) // TODO: Add Correct error

			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}

			_, err = tx.Exec(`DELETE FROM pdp_pipeline WHERE id = $1`, p.id)
			if err != nil {
				return false, xerrors.Errorf("failed to clean up pdp pipeline: %w", err)
			}

			if p.refID.Valid {
				_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, p.refID.Int64)
				if err != nil {
					return false, fmt.Errorf("failed to remove parked_piece_refs for pipeline %s: %w", p.id, err)
				}
			}

			log.Infow("removed failed PDP pipeline", "id", p.id)
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

func (a *WebRPC) MK20PDPPipelineRemove(ctx context.Context, id string) error {
	_, err := ulid.Parse(id)
	if err != nil {
		return err
	}

	_, err = a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var pipelines []struct {
			Ref sql.NullInt64 `db:"piece_ref"`

			CommpTaskID    sql.NullInt64 `db:"commp_task_id"`
			AggrTaskID     sql.NullInt64 `db:"agg_task_id"`
			AddPieceTaskID sql.NullInt64 `db:"add_piece_task_id"`
			SaveCacheTask  sql.NullInt64 `db:"save_cache_task"`
			IndexingTaskID sql.NullInt64 `db:"indexing_task_id"`
		}

		err = tx.Select(&pipelines, `SELECT piece_ref, sector, commp_task_id, agg_task_id, indexing_task_id
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
			if pipeline.AddPieceTaskID.Valid {
				taskIDs = append(taskIDs, pipeline.AddPieceTaskID.Int64)
			}
			if pipeline.SaveCacheTask.Valid {
				taskIDs = append(taskIDs, pipeline.SaveCacheTask.Int64)
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

		n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", id) // TODO: Add Correct error

		if err != nil {
			return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
		}

		_, err = tx.Exec(`DELETE FROM pdp_pipeline WHERE id = $1`, id)
		if err != nil {
			return false, xerrors.Errorf("failed to clean up pdp pipeline: %w", err)
		}

		for _, pipeline := range pipelines {
			if pipeline.Ref.Valid {
				_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, pipeline.Ref)
				if err != nil {
					return false, fmt.Errorf("failed to remove parked_piece_refs for pipeline %s: %w", id, err)
				}
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())
	return err
}
