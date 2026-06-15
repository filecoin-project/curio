-- Hot-path indexes for PDPv0 IPNI and Indexing tasks.
--
-- pdp_piecerefs: the per-task params queries filter WHERE <task_id_col> = $1.
-- The existing partial pending-indexes only cover IS NULL rows, so these
-- lookups were full table scans (measured 1.4s at 1.4M rows, paid once per
-- task). Partial indexes over claimed rows are tiny and near-free to maintain.
CREATE INDEX IF NOT EXISTS pdp_piecerefs_ipni_task_id
    ON pdp_piecerefs (ipni_task_id) WHERE ipni_task_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS pdp_piecerefs_indexing_task_id
    ON pdp_piecerefs (indexing_task_id) WHERE indexing_task_id IS NOT NULL;

-- ipni: the "already published" pre-check in PDPv0_IPNI reads
-- WHERE piece_cid = $1 ORDER BY order_number DESC LIMIT 1. With no index on
-- piece_cid this is a table scan that grows with every advertisement
-- published (291ms at 134k rows, seconds at millions).
CREATE INDEX IF NOT EXISTS ipni_piece_cid_order
    ON ipni (piece_cid, order_number DESC);
