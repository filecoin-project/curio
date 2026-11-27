-- Earliest epoch at which init/next proving-period scheduling may retry a
-- dataset after an unrecognized contract revert. NULL means not deferred.
ALTER TABLE pdp_data_sets
    ADD COLUMN IF NOT EXISTS proving_retry_epoch BIGINT;

COMMENT ON COLUMN pdp_data_sets.proving_retry_epoch IS
    'Epoch at which init/next proving-period scheduling may retry this dataset after an unrecognized contract revert; NULL if not deferred';
