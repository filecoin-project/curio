package pdp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/filecoin-project/curio/lib/keysmith"
	"github.com/filecoin-project/curio/lib/storiface"
)

// requireACLEnvelopes checks each piece being added to a read-restricted data set.
// A piece whose park file is already gone is accepted when an acl-pieces copy
// exists; that copy was checked on the first finalize.
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
		if err := p.requireACLEnvelope(ctx, info); err != nil {
			return err
		}
	}
	return nil
}

// requireACLEnvelope reads one parked piece. The caller has established the data set is read-restricted.
func (p *PDPService) requireACLEnvelope(ctx context.Context, info *SubPieceInfo) error {
	var parkedID int64
	if err := p.db.QueryRow(ctx, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, info.PieceRef).Scan(&parkedID); err != nil {
		return fmt.Errorf("reading parked piece for ref %d: %w", info.PieceRef, err)
	}
	rc, err := p.pieceIO.PieceReader(ctx, storiface.PieceNumber(parkedID))
	if err != nil {
		if p.aclCopyExists(ctx, info) {
			return nil
		}
		return fmt.Errorf("reading parked piece %d: %w", parkedID, err)
	}
	defer func() { _ = rc.Close() }()
	if _, err := keysmith.ReadEnvelopeLabels(rc); err != nil {
		return err
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

// aclCopyExists reports whether this piece is already stored in acl-pieces.
func (p *PDPService) aclCopyExists(ctx context.Context, info *SubPieceInfo) bool {
	if p.hs == nil || p.hs.ACL == nil {
		return false
	}
	parsed, err := PieceCidV2FromV1(info.PieceCIDv1, info.RawSize)
	if err != nil {
		return false
	}
	ok, err := p.hs.ACL.HasFile(ctx, parsed.CidV2)
	return err == nil && ok
}
