package pdp

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// seedPieceRef creates the parked_pieces -> parked_piece_refs -> pdp_piecerefs
// chain a piece-add tracking row references, returning the pdp_piecerefs.id.
func seedPieceRef(t *testing.T, db *harmonydb.DB, service, pieceCid string, paddedSize, rawSize int64) int64 {
	t.Helper()
	ctx := context.Background()

	var parkedID int64
	err := db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES ($1, $2, $3, TRUE, TRUE)
		RETURNING id
	`, pieceCid, paddedSize, rawSize).Scan(&parkedID)
	require.NoError(t, err)

	var refID int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, long_term)
		VALUES ($1, TRUE)
		RETURNING ref_id
	`, parkedID).Scan(&refID)
	require.NoError(t, err)

	var pieceRefID int64
	err = db.QueryRow(ctx, `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref)
		VALUES ($1, $2, $3)
		RETURNING id
	`, service, pieceCid, refID).Scan(&pieceRefID)
	require.NoError(t, err)

	return pieceRefID
}

func seedAddPiecesFixture(t *testing.T, db *harmonydb.DB, service string, dataSetID uint64) {
	t.Helper()
	ctx := context.Background()

	_, err := db.Exec(ctx, `
		INSERT INTO pdp_services (pubkey, service_label)
		VALUES ($1, $2)
		ON CONFLICT (service_label) DO NOTHING
	`, []byte(service), service)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
		VALUES ($1, $2, $3, 100, 10, FALSE)
		ON CONFLICT (id) DO NOTHING
	`, int64(dataSetID), fmt.Sprintf("0x%064x", dataSetID), service)
	require.NoError(t, err)
}

func insertAddPiecesTracking(t *testing.T, db *harmonydb.DB, dataSetID *uint64, txHash string, pieces []AddPieceRequest, infoMap map[string]*SubPieceInfo) error {
	t.Helper()
	svc := &PDPService{}
	_, err := db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (bool, error) {
		// mirror the handler: track the message, then the per-sub-piece rows
		if _, err := tx.Exec(`
			INSERT INTO message_waits_eth (signed_tx_hash, tx_status)
			VALUES ($1, 'pending')
		`, txHash); err != nil {
			return false, err
		}
		if err := svc.insertPieceAdds(tx, dataSetID, txHash, pieces, infoMap); err != nil {
			return false, err
		}
		return true, nil
	})
	return err
}

// TestInsertPieceAdds_MultiPieceBatch documents the shape that works today:
// one AddPieces message carrying several pieces, each with exactly one
// sub-piece. Every row gets a distinct add_message_index.
func TestInsertPieceAdds_MultiPieceBatch(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	const service = "subpiece-pk-test"
	seedAddPiecesFixture(t, db, service, 424201)

	txHash := fmt.Sprintf("0x%064x", 1001)
	var pieces []AddPieceRequest
	infoMap := map[string]*SubPieceInfo{}
	for i := 0; i < 2; i++ {
		sub := generatedPieceCIDV2(t, 100+i, 4096)
		refID := seedPieceRef(t, db, service, sub, 8192, 4096)
		pieces = append(pieces, AddPieceRequest{
			PieceCID:   sub,
			pieceCIDv1: sub,
			SubPieces:  []SubPieceEntry{{SubPieceCID: sub, subPieceCIDv1: sub}},
		})
		infoMap[sub] = &SubPieceInfo{PaddedSize: 8192, RawSize: 4096, PDPPieceRefID: refID, SubPieceOffset: 0}
	}

	dataSetID := uint64(424201)
	require.NoError(t, insertAddPiecesTracking(t, db, &dataSetID, txHash, pieces, infoMap))
}

// TestInsertPieceAdds_MultiSubPieceAggregate reproduces the aggregate-add
// failure: one piece whose payload is aggregated from two sub-pieces. The
// handler inserts one tracking row per sub-piece, and both rows carry the
// piece's add_message_index. With a primary key that has no sub-piece
// component the second row violates pdp_data_set_piece_adds_pk (SQLSTATE
// 23505), the transaction rolls back, and the client gets a 500 after the
// on-chain AddPieces message was already broadcast.
func TestInsertPieceAdds_MultiSubPieceAggregate(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	const service = "subpiece-pk-test"
	seedAddPiecesFixture(t, db, service, 424202)

	txHash := fmt.Sprintf("0x%064x", 1002)
	aggregate := generatedPieceCIDV2(t, 200, 16384)
	sub1 := generatedPieceCIDV2(t, 201, 4096)
	sub2 := generatedPieceCIDV2(t, 202, 4096)
	ref1 := seedPieceRef(t, db, service, sub1, 8192, 4096)
	ref2 := seedPieceRef(t, db, service, sub2, 8192, 4096)

	pieces := []AddPieceRequest{{
		PieceCID:   aggregate,
		pieceCIDv1: aggregate,
		SubPieces: []SubPieceEntry{
			{SubPieceCID: sub1, subPieceCIDv1: sub1},
			{SubPieceCID: sub2, subPieceCIDv1: sub2},
		},
	}}
	infoMap := map[string]*SubPieceInfo{
		sub1: {PaddedSize: 8192, RawSize: 4096, PDPPieceRefID: ref1, SubPieceOffset: 0},
		sub2: {PaddedSize: 8192, RawSize: 4096, PDPPieceRefID: ref2, SubPieceOffset: 8192},
	}

	dataSetID := uint64(424202)
	require.NoError(t, insertAddPiecesTracking(t, db, &dataSetID, txHash, pieces, infoMap),
		"adding one piece with two sub-pieces must insert both tracking rows")

	var rows int
	err = db.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pdp_data_set_piece_adds WHERE add_message_hash = $1
	`, txHash).Scan(&rows)
	require.NoError(t, err)
	require.Equal(t, 2, rows, "one tracking row per sub-piece")
}
