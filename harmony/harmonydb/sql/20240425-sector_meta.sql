CREATE TABLE sectors_meta (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,

    reg_seal_proof INT NOT NULL,
    ticket_epoch BIGINT NOT NULL,
    ticket_value BYTEA NOT NULL,

    orig_sealed_cid TEXT NOT NULL,
    orig_unsealed_cid TEXT NOT NULL,

    cur_sealed_cid TEXT NOT NULL,
    cur_unsealed_cid TEXT NOT NULL,

    msg_cid_precommit TEXT,
    msg_cid_commit TEXT,
    msg_cid_update TEXT, -- snapdeal update

    seed_epoch BIGINT NOT NULL,
    seed_value BYTEA NOT NULL,

    -- Added in 20240611-snap-pipeline.sql
    -- is_cc BOOLEAN NOT NULL DEFAULT (complex condition),
    -- expiration_epoch BIGINT, (null = not crawled)

    -- Added in 20240826-sector-partition.sql
    -- deadline BIGINT, (null = not crawled)
    -- partition BIGINT, (null = not crawled)

    PRIMARY KEY (sp_id, sector_num)
);

CREATE TABLE sectors_meta_pieces (
    sp_id BIGINT NOT NULL,
    sector_num BIGINT NOT NULL,
    piece_num BIGINT NOT NULL,

    piece_cid TEXT NOT NULL,
    piece_size BIGINT NOT NULL, -- padded size

    requested_keep_data BOOLEAN NOT NULL,
    raw_data_size BIGINT, -- null = piece_size.unpadded()

    start_epoch BIGINT,
    orig_end_epoch BIGINT,

    f05_deal_id BIGINT,
    ddo_pam jsonb,

    -- f05_deal_proposal jsonb added in 20240612-deal-proposal.sql

    PRIMARY KEY (sp_id, sector_num, piece_num),
    FOREIGN KEY (sp_id, sector_num) REFERENCES sectors_meta(sp_id, sector_num) ON DELETE CASCADE
);
