package indexstore

import (
	"context"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
)

// ---------------------------------------------------------------------------
// Index query operations
// ---------------------------------------------------------------------------

// PiecesContainingMultihash returns every piece that indexes the given
// multihash, together with the stored block size. The returned PieceCid
// may be either v1 or v2 depending on how the index was originally written.
func (i *IndexStore) PiecesContainingMultihash(ctx context.Context, m multihash.Multihash) ([]PieceInfo, error) {
	var pieces []PieceInfo
	var pieceCidBytes []byte
	var blockSize uint64

	qry := `SELECT PieceCid, BlockSize FROM PayloadToPieces WHERE PayloadMultihash = ?`
	iter := i.session.Query(qry, []byte(m)).WithContext(ctx).Iter()
	for iter.Scan(&pieceCidBytes, &blockSize) {
		// Parse the raw bytes back into a CID.
		pcid, err := cid.Parse(pieceCidBytes)
		if err != nil {
			return nil, fmt.Errorf("parsing piece cid: %w", err)
		}
		pieces = append(pieces, PieceInfo{PieceCid: pcid, BlockSize: blockSize})
	}
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("getting pieces containing multihash %s: %w", m, err)
	}
	return pieces, nil
}

// GetOffset returns the byte offset of a block (identified by its multihash)
// within the given piece.
func (i *IndexStore) GetOffset(ctx context.Context, pieceCid cid.Cid, hash multihash.Multihash) (uint64, error) {
	var offset uint64
	qry := `SELECT BlockOffset FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash = ?`
	if err := i.session.Query(qry, pieceCid.Bytes(), []byte(hash)).WithContext(ctx).Scan(&offset); err != nil {
		return 0, fmt.Errorf("getting offset: %w", err)
	}
	return offset, nil
}

// GetPieceHashRange returns num multihashes from the piece starting at (or
// after) start, ordered ascending. If the pieceCid is v2 and no rows are
// found, it falls back to the corresponding v1 CID to handle indexes that
// were written before a v1→v2 migration.
func (i *IndexStore) GetPieceHashRange(ctx context.Context, pieceCid cid.Cid, start multihash.Multihash, num int64) ([]multihash.Multihash, error) {
	// First attempt with the CID as given.
	hashes, err := i.queryHashRange(ctx, pieceCid, start, num)
	if err != nil {
		return nil, err
	}

	// Fallback: try the v1 CID derived from a v2 CID.
	if len(hashes) == 0 {
		v1, _, err := commcid.PieceCidV1FromV2(pieceCid)
		if err != nil {
			return nil, xerrors.Errorf("deriving v1 piece cid from v2: %w", err)
		}
		hashes, err = i.queryHashRange(ctx, v1, start, num)
		if err != nil {
			return nil, err
		}
	}

	// Verify we got the expected number of results.
	if int64(len(hashes)) != num {
		return nil, xerrors.Errorf("expected %d hashes, got %d (possibly missing indexes)", num, len(hashes))
	}
	return hashes, nil
}

// queryHashRange is the low-level query behind GetPieceHashRange.
func (i *IndexStore) queryHashRange(ctx context.Context, pieceCid cid.Cid, start multihash.Multihash, num int64) ([]multihash.Multihash, error) {
	qry := `SELECT PayloadMultihash FROM PieceBlockOffsetSize WHERE PieceCid = ? AND PayloadMultihash >= ? ORDER BY PayloadMultihash ASC LIMIT ?`
	iter := i.session.Query(qry, pieceCid.Bytes(), []byte(start), num).WithContext(ctx).Iter()

	var hashes []multihash.Multihash
	var r []byte
	for iter.Scan(&r) {
		hashes = append(hashes, multihash.Multihash(r))
		r = make([]byte, 0, typicalMultihashSize) // pre-allocate typical multihash size
	}
	if err := iter.Close(); err != nil {
		return nil, xerrors.Errorf("iterating hash range (piece 0x%02x, start 0x%02x, n %d): %w", pieceCid.Bytes(), []byte(start), num, err)
	}
	return hashes, nil
}

// CheckHasPiece returns true if at least one block is indexed for the given
// piece CID.
func (i *IndexStore) CheckHasPiece(ctx context.Context, pieceCid cid.Cid) (bool, error) {
	// Ask for a single hash starting at byte 0x00 – any hit means the piece exists.
	hashes, err := i.queryHashRange(ctx, pieceCid, multihash.Multihash([]byte{0}), 1)
	if err != nil {
		return false, err
	}
	return len(hashes) > 0, nil
}
