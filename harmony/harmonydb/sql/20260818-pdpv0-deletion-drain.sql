-- PDPv0 scheduled-removal draining (filecoin-project/curio#1422).
--
-- PDPVerifier no longer applies scheduled piece removals inside
-- nextProvingPeriod; the storage provider drains the queue explicitly with
-- processPieceDeletions, and nextProvingPeriod reverts while the queue is
-- non-empty. See https://github.com/FilOzone/pdp/pull/297.
--
-- This table coordinates removal draining. Rows are candidates rather than
-- confirmed work: the task's first action is an on-chain queue read, and a row
-- whose data set has an empty queue is simply dropped. So the seed below can be
-- indiscriminate and needs no chain access at migration time.
--
-- Two writers: this one-time seed, which picks up data sets already carrying a
-- removal queue at upgrade time (including any stuck by FilOzone/pdp#283), and
-- proving-period code, which inserts a row when it observes confirmed delete
-- intent that still needs explicit draining.

CREATE TABLE IF NOT EXISTS pdpv0_deletion_drain (
    data_set BIGINT PRIMARY KEY REFERENCES pdp_data_sets(id) ON DELETE CASCADE,

    -- ON DELETE SET NULL so an abandoned or exhausted harmony task releases its
    -- claim automatically, the same way pdp_data_sets.challenge_request_task_id
    -- works. Without it a row lost mid-task would never be re-claimed.
    task_id BIGINT REFERENCES harmony_task(id) ON DELETE SET NULL,

    -- In-flight processPieceDeletions transaction. At most one per data set:
    -- drains must be sequential because each one re-reads the queue length.
    msg_hash TEXT DEFAULT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE pdpv0_deletion_drain IS
    'Data sets that may have a non-empty PDPVerifier scheduled-removal queue to drain via processPieceDeletions.';

-- The watcher only ever looks for unclaimed rows with no drain in flight.
CREATE INDEX IF NOT EXISTS idx_pdpv0_deletion_drain_pending
    ON pdpv0_deletion_drain (data_set)
    WHERE task_id IS NULL AND msg_hash IS NULL;

-- Reorg rollback locates drain rows by in-flight message.
CREATE INDEX IF NOT EXISTS idx_pdpv0_deletion_drain_msg_hash
    ON pdpv0_deletion_drain (msg_hash)
    WHERE msg_hash IS NOT NULL;

-- Reclaiming abandoned rows scans by task_id.
CREATE INDEX IF NOT EXISTS idx_pdpv0_deletion_drain_task_id
    ON pdpv0_deletion_drain (task_id)
    WHERE task_id IS NOT NULL;

INSERT INTO pdpv0_deletion_drain (data_set)
SELECT id FROM pdp_data_sets
ON CONFLICT (data_set) DO NOTHING;
