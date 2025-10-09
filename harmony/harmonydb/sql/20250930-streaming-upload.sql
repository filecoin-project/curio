CREATE TABLE pdp_piece_streaming_uploads (
    id UUID PRIMARY KEY NOT NULL,
    service TEXT NOT NULL, -- pdp_services.id

    piece_cid TEXT, -- piece cid v1
    piece_size BIGINT,
    raw_size BIGINT,

    piece_ref BIGINT, -- packed_piece_refs.ref_id

    created_at TIMESTAMPTZ NOT NULL DEFAULT TIMEZONE('UTC', NOW()),
    complete bool,
    completed_at TIMESTAMPTZ
);