package webrpc

import (
	"context"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

// ContentInfo represents information about content location
type ContentInfo struct {
	PieceCID string `json:"piece_cid"`
	Offset   uint64  `json:"offset"`
	Size     uint64  `json:"size"`

	Err string `json:"err"`
}

// FindContentByCID finds content by CID
func (a *WebRPC) FindContentByCID(ctx context.Context, cs string) ([]ContentInfo, error) {
	cid, err := cid.Parse(cs)
	if err != nil {
		return nil, err
	}

	mh := cid.Hash()

	offsets, err := a.deps.IndexStore.PiecesContainingMultihash(ctx, mh)
	if err != nil {
		return nil, xerrors.Errorf("pieces containing multihash %s: %w", mh, err)
	}

	var res []ContentInfo
	for _, offset := range offsets {
		off, err := a.deps.IndexStore.GetOffset(ctx, offset.PieceCidV2, mh)
		if err != nil {
			res = append(res, ContentInfo{
				PieceCID: offset.PieceCidV2.String(),
				Offset:   off,
				Size:     uint64(offset.BlockSize),
				Err:      err.Error(),
			})
			continue
		}
		res = append(res, ContentInfo{
			PieceCID: offset.PieceCidV2.String(),
			Offset:   off,
			Size:     uint64(offset.BlockSize),
		})
	}

	return res, nil
}
