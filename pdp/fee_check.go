package pdp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/filecoin-project/curio/lib/keysmith"
	"github.com/filecoin-project/curio/lib/storiface"
)

// requireACLEnvelopes checks each piece added to a read-restricted data set.
// A missing park file is accepted when an acl-pieces copy already exists.
func (p *PDPService) requireACLEnvelopes(ctx context.Context, infos map[string]*SubPieceInfo) error {
	if p.pieceIO == nil {
		return fmt.Errorf("piece IO is not configured")
	}
	seen := map[int64]struct{}{}
	for _, info := range infos {
		if info == nil || info.PieceRef == 0 {
			continue
		}
		if _, ok := seen[info.PieceRef]; ok {
			continue
		}
		seen[info.PieceRef] = struct{}{}
		var parkedID int64
		if err := p.db.QueryRow(ctx, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, info.PieceRef).Scan(&parkedID); err != nil {
			return fmt.Errorf("reading parked piece for ref %d: %w", info.PieceRef, err)
		}
		rc, err := p.pieceIO.PieceReader(ctx, storiface.PieceNumber(parkedID))
		if err != nil {
			if p.hashSpaces != nil && p.hashSpaces.ACL != nil {
				if parsed, perr := PieceCidV2FromV1(info.PieceCIDv1, info.RawSize); perr == nil {
					if ok, herr := p.hashSpaces.ACL.HasFile(ctx, parsed.CidV2); herr == nil && ok {
						continue
					}
				}
			}
			return fmt.Errorf("reading parked piece %d: %w", parkedID, err)
		}
		_, err = keysmith.ReadEnvelopeLabels(rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// httpPlaceError writes the hash-space placement failure. An invalid envelope is a client error.
func httpPlaceError(w http.ResponseWriter, err error) {
	if errors.Is(err, keysmith.ErrInvalidEnvelope) {
		httpServerError(w, http.StatusBadRequest, "Piece is missing a valid encryption envelope", err)
		return
	}
	httpServerError(w, http.StatusInternalServerError, "Failed to place piece in hash space", err)
}
