// Package pieceacl lists data sets linked to a PDP piece and their acl column.
// NULL is left unresolved. Pull read_restricted flags are reported as links too.
package pieceacl

import (
	"context"
	"database/sql"
	"errors"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Secured is pdp_data_sets.acl when metadata read is "restricted".
const Secured = 1

// Link is one data set linked to a piece and its cached acl value.
// Pulls without a data set use a negative ID derived from the pull row.
type Link struct {
	ID  int64         `db:"id"`
	ACL sql.NullInt32 `db:"acl"`
}

// ByPDPPieceRefID lists data sets and pull read intents linked to pdp_piecerefs.id.
func ByPDPPieceRefID(ctx context.Context, db *harmonydb.DB, pdpPieceRefID int64) ([]Link, error) {
	var links []Link
	err := db.Select(ctx, &links, `
		SELECT ds.id, ds.acl
		FROM pdp_data_sets ds
		WHERE ds.id IN (
			SELECT a.data_set FROM pdp_data_set_piece_adds a
			WHERE a.pdp_pieceref = $1 AND a.data_set IS NOT NULL
			UNION
			SELECT p.data_set FROM pdp_data_set_pieces p
			WHERE p.pdp_pieceref = $1
			UNION
			SELECT fp.data_set_id
			FROM pdp_piecerefs pr
			JOIN pdp_piece_pull_items fi ON fi.parked_piece_ref = pr.piece_ref
			JOIN pdp_piece_pulls fp ON fp.id = fi.fetch_id
			WHERE pr.id = $1 AND fp.data_set_id <> 0
		)
		OR ds.create_message_hash IN (
			SELECT a.add_message_hash FROM pdp_data_set_piece_adds a
			WHERE a.pdp_pieceref = $1 AND a.data_set IS NULL
		)`, pdpPieceRefID)
	if err != nil {
		return nil, xerrors.Errorf("listing data set acl for pdp piece ref %d: %w", pdpPieceRefID, err)
	}
	pulls, err := pullLinksByPDPPieceRefID(ctx, db, pdpPieceRefID)
	if err != nil {
		return nil, err
	}
	return append(links, pulls...), nil
}

// ByParkedPieceRef lists data sets and pull read intents linked to parked_piece_refs.ref_id.
// Pulls attached to sibling refs of the same piece CID and raw size are included
// so one ref cannot drop a bucket another ref of that piece still needs.
func ByParkedPieceRef(ctx context.Context, db *harmonydb.DB, pieceRef int64) ([]Link, error) {
	var links []Link
	err := db.Select(ctx, &links, `
		SELECT ds.id, ds.acl
		FROM pdp_data_sets ds
		WHERE ds.id IN (
			SELECT a.data_set FROM pdp_piecerefs pr
			JOIN pdp_data_set_piece_adds a ON a.pdp_pieceref = pr.id
			WHERE pr.piece_ref = $1 AND a.data_set IS NOT NULL
			UNION
			SELECT p.data_set FROM pdp_piecerefs pr
			JOIN pdp_data_set_pieces p ON p.pdp_pieceref = pr.id
			WHERE pr.piece_ref = $1
			UNION
			SELECT fp.data_set_id
			FROM pdp_piece_pull_items fi
			JOIN pdp_piece_pulls fp ON fp.id = fi.fetch_id
			WHERE fi.parked_piece_ref = $1 AND fp.data_set_id <> 0
		)
		OR ds.create_message_hash IN (
			SELECT a.add_message_hash FROM pdp_piecerefs pr
			JOIN pdp_data_set_piece_adds a ON a.pdp_pieceref = pr.id
			WHERE pr.piece_ref = $1 AND a.data_set IS NULL
		)`, pieceRef)
	if err != nil {
		return nil, xerrors.Errorf("listing data set acl for parked piece ref %d: %w", pieceRef, err)
	}
	pulls, err := pullLinksByParkedPieceRef(ctx, db, pieceRef)
	if err != nil {
		return nil, err
	}
	return append(links, pulls...), nil
}

// pullLinksByParkedPieceRef returns synthetic links from pulls on this piece,
// including pulls bound to sibling parked refs of the same CID and raw size.
func pullLinksByParkedPieceRef(ctx context.Context, db *harmonydb.DB, pieceRef int64) ([]Link, error) {
	var links []Link
	err := db.Select(ctx, &links, `
		SELECT DISTINCT
			CASE WHEN fp.data_set_id <> 0 THEN fp.data_set_id ELSE -fp.id END AS id,
			CASE WHEN fp.read_restricted THEN 1 ELSE 0 END AS acl
		FROM pdp_piece_pull_items fi
		JOIN pdp_piece_pulls fp ON fp.id = fi.fetch_id
		WHERE fi.parked_piece_ref IN (
			SELECT sib.ref_id
			FROM parked_piece_refs me
			JOIN parked_pieces pp ON pp.id = me.piece_id
			JOIN parked_pieces same
				ON same.piece_cid = pp.piece_cid
				AND same.piece_raw_size = pp.piece_raw_size
				AND same.long_term = TRUE
			JOIN parked_piece_refs sib ON sib.piece_id = same.id
			WHERE me.ref_id = $1
		)`, pieceRef)
	if err != nil {
		return nil, xerrors.Errorf("listing pull acl for parked piece ref %d: %w", pieceRef, err)
	}
	return links, nil
}

// pullLinksByPDPPieceRefID returns synthetic pull links for the parked ref of a PDP piece ref.
func pullLinksByPDPPieceRefID(ctx context.Context, db *harmonydb.DB, pdpPieceRefID int64) ([]Link, error) {
	var pieceRef int64
	err := db.QueryRow(ctx, `SELECT piece_ref FROM pdp_piecerefs WHERE id = $1`, pdpPieceRefID).Scan(&pieceRef)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, xerrors.Errorf("reading parked piece ref for pdp piece ref %d: %w", pdpPieceRefID, err)
	}
	return pullLinksByParkedPieceRef(ctx, db, pieceRef)
}

// AnySecured is true when any link has acl = Secured.
func AnySecured(links []Link) bool {
	for _, l := range links {
		if l.ACL.Valid && int(l.ACL.Int32) == Secured {
			return true
		}
	}
	return false
}

// EverySecured is true when every link has acl = Secured. An empty list is false.
func EverySecured(links []Link) bool {
	if len(links) == 0 {
		return false
	}
	for _, l := range links {
		if !l.ACL.Valid || int(l.ACL.Int32) != Secured {
			return false
		}
	}
	return true
}

// AnyPublic is true when any link has acl NULL or not Secured (acl IS DISTINCT FROM 1).
func AnyPublic(links []Link) bool {
	for _, l := range links {
		if !l.ACL.Valid || int(l.ACL.Int32) != Secured {
			return true
		}
	}
	return false
}
