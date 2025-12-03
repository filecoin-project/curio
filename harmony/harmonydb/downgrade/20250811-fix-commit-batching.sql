
CREATE OR REPLACE FUNCTION poll_start_batch_commit_msgs(
    p_slack_epoch    BIGINT,  -- "Slack" epoch offset
    p_current_height BIGINT,  -- Current on-chain height
    p_max_batch      INT,     -- Max sectors per batch
    p_new_task_id    BIGINT,  -- Task ID to set for a chosen batch
    p_timeout_secs   INT      -- If earliest_ready + this > now(), condition is met
)
-- We return a TABLE of (updated_count BIGINT, reason TEXT),
-- but in practice it will yield exactly one row.
RETURNS TABLE (
    updated_count BIGINT,
    reason        TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
batch_rec RECORD;
    cond_slack   BOOLEAN;
    cond_timeout BOOLEAN;
    cond_fee     BOOLEAN;
BEGIN
    -- Default outputs if we never find a batch
    updated_count := 0;
    reason        := 'NONE';
    /*
      Single query logic:
        (1) Select the rows that need commit assignment.
        (2) Partition them by (sp_id, reg_seal_proof), using ROW_NUMBER() to break
            them into sub-batches of size p_max_batch.
        (3) GROUP those sub-batches to get:
            - batch_start_epoch = min(start_epoch)
            - earliest_ready_at = min(commit_ready_at)
            - sector_nums = array of sector_number
        (4) Loop over results, check conditions, update if found, return count.
        (5) If we finish the loop, return 0.
    */
    FOR batch_rec IN
        WITH initial AS (
            SELECT
                sp_id,
                sector_number,
                start_epoch,
                commit_ready_at,
                reg_seal_proof
            FROM sectors_sdr_pipeline
            WHERE after_porep        = TRUE
              AND porep_proof        IS NOT NULL
              AND task_id_commit_msg IS NULL
              AND after_commit_msg   = FALSE
              AND start_epoch        IS NOT NULL
            ORDER BY sp_id, reg_seal_proof, start_epoch
        ),
        numbered AS (
            SELECT
              l.*,
              ROW_NUMBER() OVER (
                PARTITION BY l.sp_id, l.reg_seal_proof
                ORDER BY l.commit_ready_at
              ) AS rn
            FROM initial l
        ),
        chunked AS (
            SELECT
              sp_id,
              reg_seal_proof,
              FLOOR((rn - 1)::NUMERIC / p_max_batch) AS batch_index,
              start_epoch,
              commit_ready_at,
              sector_number
            FROM numbered
        ),
        grouped AS (
            SELECT
              sp_id,
              reg_seal_proof,
              batch_index,
              MIN(start_epoch)               AS batch_start_epoch,
              MIN(commit_ready_at)           AS earliest_ready_at,
              ARRAY_AGG(sector_number)       AS sector_nums
            FROM chunked
            GROUP BY sp_id, reg_seal_proof, batch_index
            ORDER BY sp_id, reg_seal_proof, batch_index
        )
        SELECT
            sp_id,
            reg_seal_proof,
            sector_nums,
            batch_start_epoch,
            earliest_ready_at
        FROM grouped
    LOOP
             -- Evaluate conditions separately so we can pick a 'reason' if triggered.
            cond_slack   := ((batch_rec.batch_start_epoch - p_slack_epoch) <= p_current_height);
            cond_timeout := (NOW() >= (batch_rec.earliest_ready_at + MAKE_INTERVAL(secs => p_timeout_secs)));

        IF (cond_slack OR cond_timeout OR cond_fee) THEN
            -- If multiple conditions are true, pick an order of precedence.
            IF cond_slack THEN
                reason := 'SLACK (min start epoch: ' || batch_rec.batch_start_epoch || ')';
            ELSIF cond_timeout THEN
                reason := 'TIMEOUT (earliest_ready_at: ' || batch_rec.earliest_ready_at || ')';
            END IF;

            -- Perform the update
            UPDATE sectors_sdr_pipeline t
            SET task_id_commit_msg = p_new_task_id
            WHERE t.sp_id         = batch_rec.sp_id
              AND t.reg_seal_proof = batch_rec.reg_seal_proof
              AND t.sector_number = ANY(batch_rec.sector_nums)
              AND t.after_porep = TRUE
              AND t.task_id_commit_msg IS NULL
              AND t.after_commit_msg = FALSE;

            GET DIAGNOSTICS updated_count = ROW_COUNT;

            RETURN NEXT;
            RETURN;  -- Return immediately with updated_count and reason
        END IF;
    END LOOP;

    -- If we finish the loop with no triggered condition, we return updated_count=0, reason='NONE'
    RETURN NEXT;
    RETURN;
END;
$$;
