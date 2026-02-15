
CREATE TABLE IF NOT EXISTS rseal_delegated_partners ( -- provider side
    id BIGSERIAL PRIMARY KEY,

    partner_token TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    allowance_remaining BIGINT NOT NULL,
    allowance_total BIGINT NOT NULL,

    partner_name TEXT NOT NULL,
    partner_url TEXT NOT NULL
);

-- rseal_client_providers tracks remote seal providers configured on the client side.
-- Tied to sp_id so that different miners on the same curio cluster can have different
-- provider configurations. A client curio may delegate sealing for multiple miners.
CREATE TABLE IF NOT EXISTS rseal_client_providers ( -- client side
    id BIGSERIAL PRIMARY KEY,

    sp_id bigint not null,            -- which miner this provider config is for

    provider_url text not null,       -- base URL of the remote seal provider API
    provider_token text not null,     -- auth token for the provider

    provider_name text not null default '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    enabled bool not null default true,

    UNIQUE (sp_id, provider_url)
);

-- rseal_client_pipeline tracks sectors where SDR+trees are delegated to a remote
-- provider. A row here corresponds 1:1 with a row in sectors_sdr_pipeline.
-- The SDR/tree task_ids are shared between both tables (a single combined task
-- handles all of sdr/tree_d/tree_c/tree_r by delegating to the remote provider).
-- After trees complete remotely, the normal sdr_pipeline flow continues from
-- precommit onward.
CREATE TABLE IF NOT EXISTS rseal_client_pipeline (
    sp_id bigint not null,
    sector_number bigint not null,

    -- Provider reference
    provider_id bigint not null references rseal_client_providers (id),

    -- at request time
    create_time timestamptz not null default current_timestamp,
    reg_seal_proof int not null,

    -- SDR + Trees: task_ids are shared with sectors_sdr_pipeline.
    -- A single task covering sdr/tree_d/tree_c/tree_r delegates computation
    -- to the remote provider. All four task_id columns will hold the same
    -- harmony task id. The poller detects rseal_client_pipeline rows and
    -- creates the combined remote-seal task instead of individual local tasks.

    -- sdr
    ticket_epoch bigint,
    ticket_value bytea,

    task_id_sdr bigint,
    after_sdr bool not null default false,

    -- tree D
    tree_d_cid text,

    task_id_tree_d bigint,
    after_tree_d bool not null default false,

    -- tree C
    task_id_tree_c bigint,
    after_tree_c bool not null default false,

    -- tree R
    tree_r_cid text,

    task_id_tree_r bigint,
    after_tree_r bool not null default false,

    -- Data fetch: after remote SDR+trees complete, download sealed file (32 GiB)
    -- and finalized cache (p_aux, t_aux, tree-r-last) from the provider.
    -- Must complete before client finalize/move-storage can run.
    task_id_fetch bigint,
    after_fetch bool not null default false,

    -- Provider cleanup: after PoRep/finalize, request the provider to
    -- release sealed sector data (layers, trees) on its side.
    task_id_cleanup bigint,
    after_cleanup bool not null default false,

    -- Failure handling
    failed bool not null default false,
    failed_at timestamptz,
    failed_reason varchar(20) not null default '',
    failed_reason_msg text not null default '',

    primary key (sp_id, sector_number),
    foreign key (sp_id, sector_number) references sectors_sdr_pipeline (sp_id, sector_number)
);

-- rseal_provider_pipeline tracks sectors being sealed on behalf of a remote client.
-- sp_id/sector_number here is the CLIENT's miner identity - the provider seals under
-- the client's miner actor because ReplicaId is derived from (sp_id, sector_number, ticket).
-- These sectors are always CC (no deal data), so CommD is the static zero-commitment
-- for the sector size (derived from reg_seal_proof).
CREATE TABLE IF NOT EXISTS rseal_provider_pipeline (
    partner_id BIGINT NOT NULL REFERENCES rseal_delegated_partners (id),

    -- client's sp_id and sector_number - used for ReplicaId computation
    sp_id bigint not null,
    sector_number bigint not null,

    -- at request time
    create_time timestamptz not null default current_timestamp,
    reg_seal_proof int not null,

    -- sdr
    ticket_epoch bigint,
    ticket_value bytea,

    task_id_sdr bigint,
    after_sdr bool not null default false,

    -- tree D
    tree_d_cid text, -- commd from treeD compute, matches zero-comm for sector size

    task_id_tree_d bigint,
    after_tree_d bool not null default false,

    -- tree C
    task_id_tree_c bigint,
    after_tree_c bool not null default false,

    -- tree R
    tree_r_cid text, -- commr from treeR compute

    task_id_tree_r bigint,
    after_tree_r bool not null default false,

    -- notify client that SDR+trees are done
    task_id_notify_client bigint,
    after_notify_client bool not null default false,

    -- C1: client supplies seed after precommit, provider computes C1 output
    after_c1_supplied bool not null default false,

    -- finalize: after C1 is supplied and client confirms, provider can drop layers
    task_id_finalize bigint,
    after_finalize bool not null default false,

    -- cleanup: client requests cleanup or timeout triggers it
    cleanup_requested bool not null default false,
    cleanup_timeout timestamptz, -- non-graceful cleanup timeout, null until SDR+trees complete

    task_id_cleanup bigint,
    after_cleanup bool not null default false,

    -- Failure handling
    failed bool not null default false,
    failed_at timestamptz,
    failed_reason varchar(20) not null default '',
    failed_reason_msg text not null default '',

    primary key (sp_id, sector_number)
);

-- batch_sector_refs has a FK to sectors_sdr_pipeline, but SupraSeal batches can now
-- include remote sectors from rseal_provider_pipeline. Drop the FK and add a pipeline
-- source column so the slot manager knows which table to reference.
ALTER TABLE batch_sector_refs DROP CONSTRAINT IF EXISTS batch_sector_refs_sp_id_sector_number_fkey;
ALTER TABLE batch_sector_refs ADD COLUMN IF NOT EXISTS pipeline_source TEXT NOT NULL DEFAULT 'local';
-- pipeline_source: 'local' = sectors_sdr_pipeline, 'remote' = rseal_provider_pipeline
-- TODO: add a ref check on batch_sector_refs + trigger to ensure cascading delete from rseal_provider_pipeline and sectors_sdr_pipeline
