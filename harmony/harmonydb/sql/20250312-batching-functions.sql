ALTER TABLE sectors_sdr_pipeline
    ADD COLUMN start_epoch BIGINT DEFAULT NULL;

/*
  This revised function removes any named temporary tables and instead uses a
  single CTE chain plus a loop in PL/pgSQL. This way, multiple pollers can run
  concurrently without hitting errors about existing temp tables.

  Steps:
    1) Lock all rows needing a commit message:
         - after_porep = TRUE
         - porep_proof IS NOT NULL
         - task_id_commit_msg IS NULL
         - after_commit_msg = FALSE
         - start_epoch IS NOT NULL
       We do so with "FOR UPDATE SKIP LOCKED" to avoid collisions among pollers.

    2) In the same query, number these rows per group (sp_id, reg_seal_proof),
       chunk them by p_max_batch size, and group them to get each batch's:
         - sp_id, reg_seal_proof
         - Array of sector_number
         - MIN(start_epoch)      as batch_start_epoch
         - MIN(commit_ready_at)  as earliest_ready_at

    3) Iterate over these batches in ascending order, checking for the first batch
       that meets ANY of:
         (a) (batch_start_epoch - p_slack_epoch - p_current_height) <= 0
         (b) (earliest_ready_at + p_timeout_secs) > now()
         (c) p_basefee_ok = TRUE
       If found, update those rows in sectors_sdr_pipeline => task_id_commit_msg = p_new_task_id
       and return the number of rows updated.

    4) If no batch meets a condition, return 0.

  Usage example:
    SELECT poll_start_batch_commit_msg(
      slack_epoch_value,
      current_height_value,
      max_batch_size,
      new_task_id_value,
      basefee_ok_boolean,
      timeout_secs
    );

  Important:
    - Call this function in a transaction.
    - Because we do "FOR UPDATE SKIP LOCKED," multiple pollers do not process
      the same rows simultaneously.
*/

CREATE OR REPLACE FUNCTION poll_start_batch_commit_msg(
    p_slack_epoch    BIGINT,  -- "Slack" epoch offset
    p_current_height BIGINT,  -- Current on-chain height
    p_max_batch      INT,     -- Max sectors per batch
    p_new_task_id    BIGINT,  -- Task ID to set for a chosen batch
    p_basefee_ok     BOOLEAN, -- If TRUE, the base fee condition is satisfied
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
        (1) Lock rows that need commit assignment (FOR UPDATE SKIP LOCKED).
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
        WITH locked AS (
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
            FOR UPDATE SKIP LOCKED
        ),
        numbered AS (
            SELECT
              l.*,
              ROW_NUMBER() OVER (
                PARTITION BY l.sp_id, l.reg_seal_proof
                ORDER BY l.commit_ready_at
              ) AS rn
            FROM locked l
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
        cond_fee     := p_basefee_ok;  -- user-supplied boolean

        IF (cond_slack OR cond_timeout OR cond_fee) THEN
            -- If multiple conditions are true, pick an order of precedence.
            IF cond_slack THEN
                reason := 'SLACK (min start epoch: ' || batch_rec.batch_start_epoch || ')';
            ELSIF cond_timeout THEN
                reason := 'TIMEOUT (earliest_ready_at: ' || batch_rec.earliest_ready_at || ')';
            ELSIF cond_fee THEN
                reason := 'FEE';
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

/*
  This function replicates the "batching" logic for sending PreCommit messages,
  fully in SQL/PLpgSQL.

  Logic in brief:
    1) Lock all rows that need a precommit message:
         - after_synth           = TRUE
         - task_id_precommit_msg = NULL
         - after_precommit_msg   = FALSE
       For each row, we compute a "start_epoch" via COALESCE of any deal-based
       epoch (in sectors_sdr_initial_pieces) or fallback to (ticket_epoch + 0).
       You can replace that "0" with your actual fallback if needed.

    2) Within the same query, we group the locked rows by (sp_id, reg_seal_proof),
       chunk them into sub-batches of size up to p_max_batch. For each chunk/batch,
       we record:
         - sp_id, reg_seal_proof
         - sector_nums: an array of sector_number
         - batch_start_epoch: the MIN start_epoch in that chunk
         - earliest_ready: the MIN precommit_ready_at in that chunk

    3) We iterate over each batch (in ascending order), and for the first batch that
       satisfies ANY of:
         (a) (batch_start_epoch - p_slack_epoch - p_current_height) <= 0
         (b) (earliest_ready + p_timeout_secs) > NOW()
         (c) p_basefee_ok = TRUE
       we update those sectors in sectors_sdr_pipeline => task_id_precommit_msg = p_new_task_id,
       then return the number of rows updated.

    4) If no batch meets the conditions, return 0. The locked rows will remain
       without task_id_precommit_msg until the next polling iteration. Because
       we do "FOR UPDATE SKIP LOCKED," other pollers can still operate on rows
       that we haven't locked.

  IMPORTANT:
    - Call this function inside a single transaction.
    - "FOR UPDATE SKIP LOCKED" ensures multiple pollers do not process the same
      rows concurrently. Whichever poller locks them first "wins," while others
      skip them.

  Example usage:

    SELECT poll_start_batch_precommit_msg(
      randomnessLookBack,    -- p_randomnessLookBack
      slack_epoch_value,     -- p_slack_epoch
      current_height_value,  -- p_current_height
      max_batch_size,        -- p_max_batch
      new_task_id_value,     -- p_new_task_id
      basefee_ok_boolean,    -- p_basefee_ok
      timeout_secs           -- p_timeout_secs
    );

  Returns:
    The number of sectors updated (i.e., assigned a precommit task) if a batch
    is found that meets any condition. Otherwise, 0.
*/

CREATE OR REPLACE FUNCTION poll_start_batch_precommit_msg(
    p_randomnessLookBack BIGINT, -- policy.MaxPreCommitRandomnessLookback
    p_slack_epoch    BIGINT,  -- "Slack" epoch to compare against a sector's start_epoch
    p_current_height BIGINT,  -- Current on-chain height
    p_max_batch      INT,     -- Max number of sectors per batch
    p_new_task_id    BIGINT,  -- Task ID to assign if a batch is chosen
    p_basefee_ok     BOOLEAN, -- If TRUE, fee-based condition is considered met
    p_timeout_secs   INT      -- Timeout in seconds for earliest_ready_at check
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
      We'll do all logic in a single CTE chain, which:
        (1) Locks relevant rows in "sectors_sdr_pipeline".
        (2) Computes each row's "start_epoch" by coalescing data from
            sectors_sdr_initial_pieces or fallback.
        (3) Numbers them per group, forming sub-batches of size p_max_batch.
        (4) Groups by (sp_id, reg_seal_proof, batch_index).
        (5) Finally selects sp_id, reg_seal_proof, an array of sector_numbers,
            plus the min start_epoch and earliest_ready for each batch.
      Then we loop over those grouped results in ascending order.
    */

    FOR batch_rec IN
        WITH locked AS (
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
              p.precommit_ready_at,
              p.reg_seal_proof
            FROM sectors_sdr_pipeline p
            WHERE p.after_synth = TRUE
              AND p.task_id_precommit_msg IS NULL
              AND p.after_precommit_msg = FALSE
            ORDER BY p.sp_id, p.reg_seal_proof
            FOR UPDATE SKIP LOCKED
        ),
        numbered AS (
            SELECT
              l.*,
              ROW_NUMBER() OVER (
                PARTITION BY l.sp_id, l.reg_seal_proof
                ORDER BY l.start_epoch
              ) AS rn
            FROM locked l
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
        cond_timeout := ((batch_rec.earliest_ready + MAKE_INTERVAL(secs => p_timeout_secs)) < NOW() AT TIME ZONE 'UTC');
        cond_fee     := p_basefee_ok;

        IF (cond_slack OR cond_timeout OR cond_fee) THEN
            -- Decide which reason triggered it (if multiple are true, pick priority order)
            IF cond_slack THEN
                reason := 'SLACK (min start epoch: ' || batch_rec.min_start_epoch || ')';
            ELSIF cond_timeout THEN
                reason := 'TIMEOUT (earliest_ready_at: ' || batch_rec.earliest_ready || ')';
            ELSIF cond_fee THEN
                reason := 'FEE';
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
