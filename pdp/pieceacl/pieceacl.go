// Package pieceacl lists data sets linked to a PDP piece and their acl column.
// NULL is left unresolved.
package pieceacl

import (
	"context"
	"database/sql"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// Secured is pdp_data_sets.acl when metadata read is "restricted".
const Secured = 1

// Link is one data set linked to a piece and its cached acl value.
type Link struct {
	ID  int64         `db:"id"`
	ACL sql.NullInt32 `db:"acl"`
}

// ByPDPPieceRefID lists data sets linked to pdp_piecerefs.id.
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
	return links, nil
}

// ByParkedPieceRef lists data sets linked to parked_piece_refs.ref_id.
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
	return links, nil
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
