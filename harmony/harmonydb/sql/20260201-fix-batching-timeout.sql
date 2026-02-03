/*
  Fix: Batching timeout conditions for precommit and commit.
  
  Issues fixed:
  1. Timezone bug in precommit: Used `NOW() AT TIME ZONE 'UTC'` which returns TIMESTAMP
     (without timezone). When compared to TIMESTAMPTZ, PostgreSQL assumes session timezone,
     causing incorrect comparisons if the session is not UTC. Fixed by using `NOW()` directly.
  
  2. NULL timestamp handling: In PostgreSQL, `MIN(timestamp)` ignores NULLs and only returns  
     NULL if *all* inputs are NULL. In that all-NULL case, the computed earliest_ready is NULL,  
     and `NULL < NOW()` evaluates to NULL (not TRUE), so the timeout condition never triggers  
     for batches where every precommit_ready_at or commit_ready_at is NULL. Fixed by using  
     COALESCE to treat NULL as the epoch (1970-01-01 00:00:00+00), which makes such batches  
     appear immediately timed out and eligible for selection.  
  
  3. Removed unused cond_fee variable from both functions.
*/

CREATE OR REPLACE FUNCTION poll_start_batch_commit_msgs(
    p_slack_epoch    BIGINT,  -- "Slack" epoch offset
    p_current_height BIGINT,  -- Current on-chain height
    p_max_batch      INT,     -- Max sectors per batch
    p_new_task_id    BIGINT,  -- Task ID to set for a chosen batch
    p_timeout_secs   INT      -- If earliest_ready + this > now(), condition is met
)
RETURNS TABLE (
    updated_count BIGINT,
    reason        TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
    batch_rec    RECORD;
    cond_slack   BOOLEAN;
    cond_timeout BOOLEAN;
BEGIN
    -- Default outputs if we never find a batch
    updated_count := 0;
    reason        := 'NONE';

    FOR batch_rec IN
        WITH initial AS (
            SELECT
                sp_id,
                sector_number,
                start_epoch,
                -- COALESCE NULL timestamps to epoch, ensuring timeout triggers immediately
                COALESCE(commit_ready_at, '1970-01-01 00:00:00+00'::TIMESTAMPTZ) AS commit_ready_at,
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

        IF (cond_slack OR cond_timeout) THEN
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

CREATE OR REPLACE FUNCTION poll_start_batch_precommit_msgs(
    p_randomnessLookBack BIGINT, -- policy.MaxPreCommitRandomnessLookback
    p_slack_epoch    BIGINT,  -- "Slack" epoch to compare against a sector's start_epoch
    p_current_height BIGINT,  -- Current on-chain height
    p_max_batch      INT,     -- Max number of sectors per batch
    p_new_task_id    BIGINT,  -- Task ID to assign if a batch is chosen
    p_timeout_secs   INT      -- Timeout in seconds for earliest_ready_at check
)
RETURNS TABLE (
    updated_count BIGINT,
    reason        TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
    batch_rec    RECORD;
    cond_slack   BOOLEAN;
    cond_timeout BOOLEAN;
BEGIN
    -- Default outputs if we never find a batch
    updated_count := 0;
    reason        := 'NONE';

    FOR batch_rec IN
        WITH initial AS (
            SELECT
              p.sp_id,
              p.sector_number,
              -- Compute the precommit "start_epoch" via deal info or fallback:
              COALESCE(
                (
                  SELECT MIN(LEAST(s.f05_deal_start_epoch, s.direct_start_epoch))
                    FROM sectors_sdr_initial_pieces s
                   WHERE s.sp_id = p.sp_id
                     AND s.sector_number = p.sector_number
                ),
                p.ticket_epoch + p_randomnessLookBack
              ) AS start_epoch,
              -- COALESCE NULL timestamps to epoch, ensuring timeout triggers immediately
              COALESCE(p.precommit_ready_at, '1970-01-01 00:00:00+00'::TIMESTAMPTZ) AS precommit_ready_at,
              p.reg_seal_proof
            FROM sectors_sdr_pipeline p
            WHERE p.after_synth = TRUE
              AND p.task_id_precommit_msg IS NULL
              AND p.after_precommit_msg = FALSE
            ORDER BY p.sp_id, p.reg_seal_proof
        ),
        numbered AS (
            SELECT
              l.*,
              ROW_NUMBER() OVER (
                PARTITION BY l.sp_id, l.reg_seal_proof
                ORDER BY l.start_epoch
              ) AS rn
            FROM initial l
        ),
        chunked AS (
            SELECT
              sp_id,
              reg_seal_proof,
              FLOOR((rn - 1)::NUMERIC / p_max_batch) AS batch_index,
              start_epoch,
              precommit_ready_at,
              sector_number
            FROM numbered
        ),
        grouped AS (
            SELECT
              sp_id,
              reg_seal_proof,
              batch_index,
              MIN(start_epoch)               AS min_start_epoch,
              MIN(precommit_ready_at)        AS earliest_ready,
              ARRAY_AGG(sector_number)       AS sector_nums
            FROM chunked
            GROUP BY sp_id, reg_seal_proof, batch_index
            ORDER BY sp_id, reg_seal_proof, batch_index
        )
        SELECT
            sp_id,
            reg_seal_proof,
            sector_nums,
            min_start_epoch,
            earliest_ready
        FROM grouped
    LOOP
        -- Evaluate each condition
        cond_slack   := ((batch_rec.min_start_epoch - p_slack_epoch) <= p_current_height);
        -- Fix: Use NOW() directly instead of NOW() AT TIME ZONE 'UTC' to avoid timezone issues
        cond_timeout := (NOW() >= (batch_rec.earliest_ready + MAKE_INTERVAL(secs => p_timeout_secs)));

        IF (cond_slack OR cond_timeout) THEN
            -- Decide which reason triggered it (if multiple are true, pick priority order)
            IF cond_slack THEN
                reason := 'SLACK (min start epoch: ' || batch_rec.min_start_epoch || ')';
            ELSIF cond_timeout THEN
                reason := 'TIMEOUT (earliest_ready_at: ' || batch_rec.earliest_ready || ')';
            END IF;

            -- Update these sectors in sectors_sdr_pipeline
            UPDATE sectors_sdr_pipeline t
            SET task_id_precommit_msg = p_new_task_id
            WHERE t.sp_id = batch_rec.sp_id
              AND t.reg_seal_proof = batch_rec.reg_seal_proof
              AND t.sector_number = ANY(batch_rec.sector_nums)
              AND t.after_synth           = TRUE
              AND t.task_id_precommit_msg IS NULL
              AND t.after_precommit_msg   = FALSE;

            GET DIAGNOSTICS updated_count = ROW_COUNT;
            RETURN NEXT;
            RETURN;  -- Return immediately with updated_count, reason
        END IF;
    END LOOP;

    -- If we finish the loop with no triggered condition, we return updated_count=0, reason='NONE'
    RETURN NEXT;
    RETURN;
END;
$$;
