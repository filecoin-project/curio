package webrpc

import (
	"context"
	"errors"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/commcidv2"
)

// ContentInfo represents information about content location
type ContentInfo struct {
	PieceCID string `json:"piece_cid"`
	Offset   uint64 `json:"offset"`
	Size     uint64 `json:"size"`

	Err string `json:"err"`
}

// FindContentByCID finds content by CID
func (a *WebRPC) FindContentByCID(ctx context.Context, cs string) ([]ContentInfo, error) {
	cid, err := cid.Parse(cs)
	if err != nil {
		return nil, err
	}

	if commcidv2.IsPieceCidV2(cid) || commcidv2.IsCidV1PieceCid(cid) {
		_, pcid2, err := a.maybeUpgradePieceCid(ctx, cid)
		if err != nil {
			return nil, xerrors.Errorf("failed to upgrade piece cid: %w", err)
		}
		return []ContentInfo{
			{
				PieceCID: pcid2.String(),
				Offset:   0,
				Size:     0,
			},
		}, nil
	}

	mh := cid.Hash()

	offsets, err := a.deps.IndexStore.PiecesContainingMultihash(ctx, mh)
	if err != nil {
		return nil, xerrors.Errorf("pieces containing multihash %s: %w", mh, err)
	}

	var res []ContentInfo
	for _, offset := range offsets {
		off, err := a.deps.IndexStore.GetOffset(ctx, offset.PieceCid, mh)
		if err != nil {
			_, pcid2, err := a.maybeUpgradePieceCid(ctx, offset.PieceCid)
			if err != nil {
				return nil, xerrors.Errorf("failed to upgrade piece cid: %w", err)
			}
			res = append(res, ContentInfo{
				PieceCID: pcid2.String(),
				Offset:   off,
				Size:     offset.BlockSize,
				Err:      err.Error(),
			})
			continue
		}

		_, pcid2, err := a.maybeUpgradePieceCid(ctx, offset.PieceCid)
		if err != nil {
			return nil, xerrors.Errorf("failed to upgrade piece cid: %w", err)
		}
		res = append(res, ContentInfo{
			PieceCID: pcid2.String(),
			Offset:   off,
			Size:     offset.BlockSize,
		})
	}

	return res, nil
}

func (a *WebRPC) maybeUpgradePieceCid(ctx context.Context, c cid.Cid) (bool, cid.Cid, error) {
	if commcidv2.IsPieceCidV2(c) {
		return true, c, nil
	}

	if !commcidv2.IsCidV1PieceCid(c) {
		return false, c, nil
	}

	// Lookup piece_cid in market_piece_deal (always v1), get raw_size.
	// If raw_size is unavailable we keep using v1.
	var rawSize uint64

	err := a.deps.DB.QueryRow(ctx, `
			SELECT COALESCE(raw_size, 0)
			FROM market_piece_deal 
			WHERE piece_cid = $1
			LIMIT 1
		`, c.String()).Scan(&rawSize)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, c, nil
		}
		return false, c, xerrors.Errorf("failed to lookup piece info: %w", err)
	}

	if rawSize < 127 {
		return false, c, nil
	}

	pcid2, err := commcid.PieceCidV2FromV1(c, rawSize)
	if err != nil {
		return false, c, nil
	}

	return true, pcid2, nil
}
