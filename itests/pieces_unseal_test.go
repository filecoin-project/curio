package itests

import (
	"context"
	"crypto/rand"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/piecesunseal"
)

// dummyCID is a placeholder CID string used for sectors_meta fields that require a valid CID
// but are not relevant to the unseal plan logic.
const dummyCID = "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

// makePieceCid generates a fresh random v1 piece CID.
func makePieceCid(t *testing.T) string {
	t.Helper()
	digest := make([]byte, 32)
	_, err := rand.Read(digest)
	require.NoError(t, err)
	c, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	return c.String()
}

// insertSector inserts a row into sectors_meta. targetUnseal nil = no preference.
func insertSector(t *testing.T, ctx context.Context, db *harmonydb.DB, spID, sectorNum int64, targetUnseal *bool) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO sectors_meta (
			sp_id, sector_num, reg_seal_proof,
			ticket_epoch, ticket_value,
			orig_sealed_cid, orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid,
			seed_epoch, seed_value,
			is_cc, target_unseal_state
		) VALUES ($1, $2, 3, 0, '\x00', $3, $3, $3, $3, 0, '\x00', false, $4)
	`, spID, sectorNum, dummyCID, targetUnseal)
	require.NoError(t, err)
}

// insertDeal inserts a row into market_piece_deal.
func insertDeal(t *testing.T, ctx context.Context, db *harmonydb.DB, spID, sectorNum int64, pieceCid string, pieceSize int64) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO market_piece_deal (id, boost_deal, legacy_deal, sp_id, sector_num, piece_offset, piece_cid, piece_length, raw_size)
		VALUES ($1, true, false, $2, $3, 0, $4, $5, $5)
	`, uuid.New().String(), spID, sectorNum, pieceCid, pieceSize)
	require.NoError(t, err)
}

// insertPieceMetadata inserts a row into market_piece_metadata so the size lookup works for v1 CIDs.
func insertPieceMetadata(t *testing.T, ctx context.Context, db *harmonydb.DB, pieceCid string, pieceSize int64) {
	t.Helper()
	_, err := db.Exec(ctx, `
		INSERT INTO market_piece_metadata (piece_cid, piece_size)
		VALUES ($1, $2)
	`, pieceCid, pieceSize)
	require.NoError(t, err)
}

// TestPiecesToSectorsBatch_Empty verifies that an empty input returns an empty plan.
func TestPiecesToSectorsBatch_Empty(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t, harmonydb.ITestNewID(), true)
	require.NoError(t, err)

	plan, err := piecesunseal.PiecesToSectorsBatch(ctx, db, nil)
	require.NoError(t, err)
	require.Equal(t, 0, plan.TotalSectors())
	require.Empty(t, plan.NoDeal)
	require.Empty(t, plan.AlreadyTargeted)
}

// TestPiecesToSectorsBatch verifies all filtering logic in one scenario. For each category,
// some values are included and others are excluded:
//
//	NoDeal:         D in (has metadata, no deal)     | A,B,C out (have deals)
//	AlreadyTargeted: C in (sector 12 targeted)     | A,B out (sectors not targeted)
//	WillUnseal:     A,B in (sector 10 selected)   | C,D out (C covered, D no deal)
//	Dedup:          A passed twice → still 1 sector
//
// Setup — SP 1001, size 2048:
//
//	Sector 10: pieces A, B
//	Sector 11: pieces B, C
//	Sector 12: piece C — target_unseal_state = true (already unsealing)
//	Piece D: metadata only, no deal
//
// Input: [A, A, B, C, D]
//
// Greedy: C covered by sector 12. Remaining A,B. Sector 10 has 2, sector 11 has 1 → pick 10.
func TestPiecesToSectorsBatch(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t, harmonydb.ITestNewID(), true)
	require.NoError(t, err)

	const (
		spID      = int64(1001)
		pieceSize = int64(2048)
	)

	pieceA := makePieceCid(t)
	pieceB := makePieceCid(t)
	pieceC := makePieceCid(t)
	pieceD := makePieceCid(t)

	for _, pc := range []string{pieceA, pieceB, pieceC, pieceD} {
		insertPieceMetadata(t, ctx, db, pc, pieceSize)
	}

	insertSector(t, ctx, db, spID, 10, nil)
	insertSector(t, ctx, db, spID, 11, nil)
	trueVal := true
	insertSector(t, ctx, db, spID, 12, &trueVal)

	insertDeal(t, ctx, db, spID, 10, pieceA, pieceSize)
	insertDeal(t, ctx, db, spID, 10, pieceB, pieceSize)
	insertDeal(t, ctx, db, spID, 11, pieceB, pieceSize)
	insertDeal(t, ctx, db, spID, 11, pieceC, pieceSize)
	insertDeal(t, ctx, db, spID, 12, pieceC, pieceSize)
	// D: metadata only, no deal

	// A passed twice to verify dedup
	pieceCids := parseCids(t, pieceA, pieceA, pieceB, pieceC, pieceD)
	plan, err := piecesunseal.PiecesToSectorsBatch(ctx, db, pieceCids)
	require.NoError(t, err)

	// NoDeal: D in | A,B,C out (have deals)
	require.Equal(t, []string{pieceD}, plan.NoDeal)
	require.NotContains(t, plan.NoDeal, pieceA)
	require.NotContains(t, plan.NoDeal, pieceB)
	require.NotContains(t, plan.NoDeal, pieceC)

	// AlreadyTargeted: sector 12 + C in | sectors 10,11 out (not targeted)
	sid12 := piecesunseal.SectorID{SpID: spID, SectorNum: 12}
	require.Contains(t, plan.AlreadyTargeted, sid12)
	require.Equal(t, []string{pieceC}, sorted(plan.AlreadyTargeted[sid12]))
	sid10 := piecesunseal.SectorID{SpID: spID, SectorNum: 10}
	sid11 := piecesunseal.SectorID{SpID: spID, SectorNum: 11}
	require.NotContains(t, plan.AlreadyTargeted, sid10)
	require.NotContains(t, plan.AlreadyTargeted, sid11)

	// WillUnseal: sector 10 + A,B in | C,D out (C covered by 12, D no deal)
	require.Equal(t, 1, plan.TotalSectors())
	require.Equal(t, []int64{10}, plan.SpIdToSectorNum[spID])
	require.Equal(t, sorted([]string{pieceA, pieceB}), sorted(plan.SectorIdToPieces[sid10]))
	require.NotContains(t, plan.SectorIdToPieces[sid10], pieceC)
	require.NotContains(t, plan.SectorIdToPieces[sid10], pieceD)
}

// parseCids parses a list of CID strings into []cid.Cid.
func parseCids(t *testing.T, cidStrs ...string) []cid.Cid {
	t.Helper()
	out := make([]cid.Cid, len(cidStrs))
	for i, s := range cidStrs {
		c, err := cid.Parse(s)
		require.NoError(t, err)
		out[i] = c
	}
	return out
}

// sorted returns a sorted copy of a string slice.
func sorted(s []string) []string {
	cp := append([]string(nil), s...)
	sort.Strings(cp)
	return cp
}
