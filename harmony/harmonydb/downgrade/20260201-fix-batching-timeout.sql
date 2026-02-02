-- Downgrade: Restore previous batching functions (from 20250423-remove-fee-aggregation.sql)

CREATE OR REPLACE FUNCTION poll_start_batch_commit_msgs(
    p_slack_epoch    BIGINT,
    p_current_height BIGINT,
    p_max_batch      INT,
    p_new_task_id    BIGINT,
    p_timeout_secs   INT
)
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
    updated_count := 0;
    reason        := 'NONE';

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
            cond_slack   := ((batch_rec.batch_start_epoch - p_slack_epoch) <= p_current_height);
            cond_timeout := (NOW() >= (batch_rec.earliest_ready_at + MAKE_INTERVAL(secs => p_timeout_secs)));

        IF (cond_slack OR cond_timeout OR cond_fee) THEN
            IF cond_slack THEN
                reason := 'SLACK (min start epoch: ' || batch_rec.batch_start_epoch || ')';
            ELSIF cond_timeout THEN
                reason := 'TIMEOUT (earliest_ready_at: ' || batch_rec.earliest_ready_at || ')';
            END IF;

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
            RETURN;
        END IF;
    END LOOP;

    RETURN NEXT;
    RETURN;
END;
$$;

CREATE OR REPLACE FUNCTION poll_start_batch_precommit_msgs(
    p_randomnessLookBack BIGINT,
    p_slack_epoch    BIGINT,
    p_current_height BIGINT,
    p_max_batch      INT,
    p_new_task_id    BIGINT,
    p_timeout_secs   INT
)
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
    updated_count := 0;
    reason        := 'NONE';

    FOR batch_rec IN
        WITH initial AS (
            SELECT
              p.sp_id,
              p.sector_number,
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
            cond_slack   := ((batch_rec.min_start_epoch - p_slack_epoch) <= p_current_height);
            cond_timeout := ((batch_rec.earliest_ready + MAKE_INTERVAL(secs => p_timeout_secs)) < NOW() AT TIME ZONE 'UTC');

            IF (cond_slack OR cond_timeout OR cond_fee) THEN
            IF cond_slack THEN
                reason := 'SLACK (min start epoch: ' || batch_rec.min_start_epoch || ')';
            ELSIF cond_timeout THEN
                reason := 'TIMEOUT (earliest_ready_at: ' || batch_rec.earliest_ready || ')';
            END IF;

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
            RETURN;
        END IF;
    END LOOP;

    RETURN NEXT;
    RETURN;
END;
$$;
