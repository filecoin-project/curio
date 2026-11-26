-- Arrival-time read intent for uploads and pulls. Restricted pieces go to
-- acl-pieces before a data set claims them. Default FALSE is public (open-pieces).
ALTER TABLE pdp_piece_uploads
    ADD COLUMN IF NOT EXISTS read_restricted BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE pdp_piece_streaming_uploads
    ADD COLUMN IF NOT EXISTS read_restricted BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE pdp_piece_pulls
    ADD COLUMN IF NOT EXISTS read_restricted BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN pdp_piece_uploads.read_restricted IS
    'POST /pdp/piece read=restricted: place into acl-pieces at arrival';

COMMENT ON COLUMN pdp_piece_streaming_uploads.read_restricted IS
    'Streaming finalize read=restricted: place into acl-pieces at arrival';

COMMENT ON COLUMN pdp_piece_pulls.read_restricted IS
    'Pull target data set (or create extraData) is read-restricted; Place sees this via pieceacl';
