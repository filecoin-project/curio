package itests

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/tasks/seal"
)

func TestBatching(t *testing.T) {
	ctx := context.Background()
	
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	var machineID int
	err = db.QueryRow(ctx, `INSERT INTO harmony_machines (host_and_port, cpu, ram, gpu)
		VALUES ('test:1234', 4, 8000000000, 0) RETURNING id`).Scan(&machineID)
	require.NoError(t, err)

	testCases := []struct {
		name     string
		mode     string
		taskName string
		spID     int64
	}{
		{
			name:     "PrecommitNoConcurrentEmptyBatch",
			mode:     "precommit",
			taskName: "PreCommitBatch",
			spID:     1000,
		},
		{
			name:     "CommitNoConcurrentEmptyBatch",
			mode:     "commit",
			taskName: "CommitBatch",
			spID:     2000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runBatchingModeTest(t, ctx, db, machineID, tc.mode, tc.taskName, tc.spID)
		})
	}
}

func runBatchingModeTest(t *testing.T, ctx context.Context, db *harmonydb.DB, machineID int, mode, taskName string, spID int64) {
	t.Helper()

	const sealProof int64 = 8
	const numSectors int64 = 20
	const maxBatch int = 10

	for i := range numSectors {
		if mode == "precommit" {
			_, err := db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline (
				sp_id, sector_number, reg_seal_proof,
				after_sdr, after_tree_d, after_tree_c, after_tree_r, after_synth,
				precommit_ready_at, ticket_epoch
			) VALUES ($1, $2, $3, TRUE, TRUE, TRUE, TRUE, TRUE, $4, 100)`,
				spID, i, sealProof, time.Now().Add(-time.Hour))
			require.NoError(t, err)
			continue
		}

		_, err := db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline (
			sp_id, sector_number, reg_seal_proof,
			after_sdr, after_tree_d, after_tree_c, after_tree_r, after_synth,
			after_precommit_msg, after_precommit_msg_success,
			after_porep, porep_proof, after_finalize,
			commit_ready_at, start_epoch, ticket_epoch
		) VALUES ($1, $2, $3, TRUE, TRUE, TRUE, TRUE, TRUE, TRUE, TRUE, TRUE, $4, TRUE, $5, 500, 100)`,
			spID, i, sealProof, []byte{0}, time.Now().Add(-time.Hour))
		require.NoError(t, err)
	}

	sectorCounts := runBatchRace(t, ctx, db, machineID, 3, taskName, func(tx *harmonydb.Tx, taskID int64) (int, error) {
		rows, err := selectBatchRows(tx, mode, maxBatch)
		if err != nil {
			return 0, err
		}
		if len(rows) == 0 {
			return 0, nil
		}

		now := time.Now()
		for _, batch := range seal.GroupBatchRows(rows) {
			r := seal.EvalBatchTimeout(batch.EarliestReady, 0, batch.MinStartEpoch, 0, 0, now, len(batch.SectorNums), maxBatch)
			if !r.ShouldFire {
				continue
			}
			n, err := assignBatch(tx, mode, taskID, batch.SpID, batch.RegSealProof, batch.SectorNums)
			if err != nil {
				return 0, err
			}
			if n > 0 {
				return n, nil
			}
		}
		return 0, nil
	})

	var total int
	for _, c := range sectorCounts {
		require.Greater(t, c, 0, "committed task must have at least 1 sector")
		total += c
	}
	require.Equal(t, 2, len(sectorCounts), "expected 2 committed batches for %d sectors with maxBatch=%d", numSectors, maxBatch)
	require.Equal(t, numSectors, int64(total), "all sectors should be assigned")

	emptyTasks, err := countEmptyBatchTasks(ctx, db, taskName, mode)
	require.NoError(t, err)
	require.Equal(t, 0, emptyTasks, "no empty %s batch tasks should exist in DB", mode)
}

func selectBatchRows(tx *harmonydb.Tx, mode string, maxBatch int) ([]seal.BatchRow, error) {
	var rows []seal.BatchRow

	if mode == "precommit" {
		err := tx.Select(&rows, `
			WITH initial AS (
				SELECT p.sp_id, p.sector_number,
					p.ticket_epoch + 150 AS start_epoch,
					COALESCE(p.precommit_ready_at, '1970-01-01 00:00:00+00'::TIMESTAMPTZ) AS ready_at,
					p.reg_seal_proof
				FROM sectors_sdr_pipeline p
				WHERE p.after_synth = TRUE
					AND p.task_id_precommit_msg IS NULL
					AND p.after_precommit_msg = FALSE
			), numbered AS (
				SELECT l.*,
					ROW_NUMBER() OVER (PARTITION BY l.sp_id, l.reg_seal_proof ORDER BY l.start_epoch) AS rn
				FROM initial l
			)
			SELECT sp_id, sector_number, start_epoch, ready_at, reg_seal_proof,
				FLOOR((rn - 1)::NUMERIC / $1)::BIGINT AS batch_index
			FROM numbered
			ORDER BY sp_id, reg_seal_proof, batch_index, start_epoch`,
			maxBatch)
		return rows, err
	}

	err := tx.Select(&rows, `
		WITH initial AS (
			SELECT sp_id, sector_number, start_epoch,
				COALESCE(commit_ready_at, '1970-01-01 00:00:00+00'::TIMESTAMPTZ) AS ready_at,
				reg_seal_proof
			FROM sectors_sdr_pipeline
			WHERE after_porep = TRUE
				AND porep_proof IS NOT NULL
				AND task_id_commit_msg IS NULL
				AND after_commit_msg = FALSE
				AND start_epoch IS NOT NULL
		), numbered AS (
			SELECT l.*,
				ROW_NUMBER() OVER (PARTITION BY l.sp_id, l.reg_seal_proof ORDER BY l.ready_at) AS rn
			FROM initial l
		)
		SELECT sp_id, sector_number, start_epoch, ready_at, reg_seal_proof,
			FLOOR((rn - 1)::NUMERIC / $1)::BIGINT AS batch_index
		FROM numbered
		ORDER BY sp_id, reg_seal_proof, batch_index, ready_at`,
		maxBatch)
	return rows, err
}

func assignBatch(tx *harmonydb.Tx, mode string, taskID, spID, regSealProof int64, sectorNums []int64) (int, error) {
	if mode == "precommit" {
		return tx.Exec(`UPDATE sectors_sdr_pipeline
			SET task_id_precommit_msg = $1
			WHERE sp_id = $2 AND reg_seal_proof = $3
				AND sector_number = ANY($4::bigint[])
				AND after_synth = TRUE
				AND task_id_precommit_msg IS NULL
				AND after_precommit_msg = FALSE`,
			taskID, spID, regSealProof, sectorNums)
	}

	return tx.Exec(`UPDATE sectors_sdr_pipeline
		SET task_id_commit_msg = $1
		WHERE sp_id = $2 AND reg_seal_proof = $3
			AND sector_number = ANY($4::bigint[])
			AND after_porep = TRUE
			AND task_id_commit_msg IS NULL
			AND after_commit_msg = FALSE`,
		taskID, spID, regSealProof, sectorNums)
}

func countEmptyBatchTasks(ctx context.Context, db *harmonydb.DB, taskName, mode string) (int, error) {
	var emptyTasks int

	if mode == "precommit" {
		err := db.QueryRow(ctx, `SELECT COUNT(*) FROM harmony_task ht
			WHERE ht.name = $1
			AND NOT EXISTS (SELECT 1 FROM sectors_sdr_pipeline sp WHERE sp.task_id_precommit_msg = ht.id)`, taskName).Scan(&emptyTasks)
		return emptyTasks, err
	}

	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM harmony_task ht
		WHERE ht.name = $1
		AND NOT EXISTS (SELECT 1 FROM sectors_sdr_pipeline sp WHERE sp.task_id_commit_msg = ht.id)`, taskName).Scan(&emptyTasks)
	return emptyTasks, err
}

// runBatchRace starts numWorkers goroutines concurrently, each simulating
// an AddTask call that creates a harmony_task row and runs batchFunc within
// the same transaction. Returns sector counts for the workers that committed.
func runBatchRace(
	t *testing.T,
	ctx context.Context,
	db *harmonydb.DB,
	machineID, numWorkers int,
	taskName string,
	batchFunc func(tx *harmonydb.Tx, taskID int64) (sectors int, err error),
) []int {
	type result struct {
		committed bool
		sectors   int
		err       error
	}
	results := make([]result, numWorkers)

	var barrier, wg sync.WaitGroup
	barrier.Add(numWorkers)

	for w := range numWorkers {
		wg.Go(func() {
			barrier.Done()
			barrier.Wait()

			var sectors int
			retryWait := 100 * time.Millisecond

			for {
				committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
					var tID int64
					if err := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time)
						VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, taskName, machineID).Scan(&tID); err != nil {
						return false, fmt.Errorf("insert task: %w", err)
					}
					n, err := batchFunc(tx, tID)
					if err != nil {
						return false, err
					}
					if n > 0 {
						sectors = n
						return true, nil
					}
					return false, nil
				})

				if err != nil {
					if harmonydb.IsErrSerialization(err) {
						time.Sleep(retryWait)
						retryWait *= 2
						continue
					}
					results[w] = result{err: err}
					return
				}

				if committed {
					results[w] = result{committed: true, sectors: sectors}
				}
				return
			}
		})
	}

	wg.Wait()

	var sectorCounts []int
	for i, r := range results {
		require.NoError(t, r.err, "worker %d", i)
		if r.committed {
			sectorCounts = append(sectorCounts, r.sectors)
		}
	}
	return sectorCounts
}
