package pdp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/filecoin-project/curio/lib/keysmith"
	"github.com/filecoin-project/curio/lib/storiface"
)

// restrictedSubPieces returns the subset of infos uploaded with read=restricted.
func restrictedSubPieces(infos map[string]*SubPieceInfo) map[string]*SubPieceInfo {
	out := make(map[string]*SubPieceInfo)
	for k, info := range infos {
		if info != nil && info.Restricted {
			out[k] = info
		}
	}
	return out
}

// requireRestrictedPiecesForDataset checks restricted pieces being added to a data set.
// The data set must have foc/ks (has_keyspace), each restricted piece needs a FEE
// envelope, and the envelope foc/ks must match the data set keyspace.
func (p *PDPService) requireRestrictedPiecesForDataset(
	ctx context.Context,
	dataSetId uint64,
	infos map[string]*SubPieceInfo,
) error {
	restricted := restrictedSubPieces(infos)
	if len(restricted) == 0 {
		return nil
	}

	hasKS, err := ResolveDatasetHasKeyspace(ctx, p.db, p.ethClient, dataSetId)
	if err != nil {
		return err
	}
	if !hasKS {
		return fmt.Errorf("data set %d has no foc/ks keyspace; cannot add restricted pieces", dataSetId)
	}

	datasetKS, err := ResolveDatasetKeyspace(ctx, p.ethClient, dataSetId)
	if err != nil {
		return err
	}
	if datasetKS == "" {
		return fmt.Errorf("data set %d has no foc/ks keyspace; cannot add restricted pieces", dataSetId)
	}

	return p.requireRestrictedEnvelopes(ctx, restricted, datasetKS)
}

// requireRestrictedPiecesForCreate checks restricted pieces on create-and-add
// before the data set row exists. Create extraData must include foc/ks, and
// each restricted piece envelope must match that keyspace.
func (p *PDPService) requireRestrictedPiecesForCreate(
	ctx context.Context,
	extraData []byte,
	infos map[string]*SubPieceInfo,
) error {
	restricted := restrictedSubPieces(infos)
	if len(restricted) == 0 {
		return nil
	}

	datasetKS, known := keyspaceValueFromExtraData(extraData)
	if !known {
		return fmt.Errorf("create extraData is missing or undecodable; cannot add restricted pieces")
	}
	if datasetKS == "" {
		return fmt.Errorf("create extraData has no foc/ks keyspace; cannot add restricted pieces")
	}

	return p.requireRestrictedEnvelopes(ctx, restricted, datasetKS)
}

// requireRestrictedEnvelopes checks each restricted piece has a FEE envelope
// whose foc/ks matches datasetKS (case-insensitive).
func (p *PDPService) requireRestrictedEnvelopes(ctx context.Context, infos map[string]*SubPieceInfo, datasetKS string) error {
	if p.pieceIO == nil {
		return fmt.Errorf("piece IO is not configured")
	}
	want := strings.ToLower(strings.TrimSpace(datasetKS))
	seen := map[int64]struct{}{}
	for _, info := range infos {
		if info == nil || info.PieceRef == 0 {
			continue
		}
		if _, ok := seen[info.PieceRef]; ok {
			continue
		}
		seen[info.PieceRef] = struct{}{}
		if err := p.requireRestrictedEnvelope(ctx, info, want); err != nil {
			return err
		}
	}
	return nil
}

// requireRestrictedEnvelope reads one parked piece and checks foc/ks.
func (p *PDPService) requireRestrictedEnvelope(ctx context.Context, info *SubPieceInfo, wantKeyspace string) error {
	var parkedID int64
	if err := p.db.QueryRow(ctx, `SELECT piece_id FROM parked_piece_refs WHERE ref_id = $1`, info.PieceRef).Scan(&parkedID); err != nil {
		return fmt.Errorf("reading parked piece for ref %d: %w", info.PieceRef, err)
	}
	rc, err := p.pieceIO.PieceReader(ctx, storiface.PieceNumber(parkedID))
	if err != nil {
		if p.aclCopyExists(ctx, info) {
			// Park file is gone; the acl-pieces copy was checked on finalize.
			// Keyspace was already validated then against the piece envelope.
			return nil
		}
		return fmt.Errorf("reading parked piece %d: %w", parkedID, err)
	}
	defer func() { _ = rc.Close() }()
	labels, err := keysmith.ReadEnvelopeLabels(rc)
	if err != nil {
		return err
	}
	got := strings.ToLower(strings.TrimSpace(labels.Keyspace))
	if got != wantKeyspace {
		return fmt.Errorf("%w: piece foc/ks %q does not match data set foc/ks %q", keysmith.ErrInvalidEnvelope, labels.Keyspace, wantKeyspace)
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
