package pdpv0

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/pdp"
)

func TestPullPieceCompleteAlreadyParkedItemsCompletesDuplicateSources(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	service := "pull-task-complete-duplicate-sources"
	require.NoError(t, insertPullTaskService(ctx, db, service))

	pieceCid, rawSize := testPullPieceCID(t, 1)
	var parkedPieceID int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		VALUES ($1, $2, $3, TRUE, TRUE)
		RETURNING id
	`, pieceCid, pdp.PadPieceSize(int64(rawSize)), rawSize).Scan(&parkedPieceID)
	require.NoError(t, err)

	var pullID int64
	err = db.QueryRow(ctx, `
		INSERT INTO pdp_piece_pulls (service, extra_data_hash, data_set_id, record_keeper, client_address)
		VALUES ($1, $2, 1, '', '0x1')
		RETURNING id
	`, service, []byte("pull-task-complete")).Scan(&pullID)
	require.NoError(t, err)

	urls := []string{
		"https://source-a.example/piece/" + pieceCid,
		"https://source-b.example/piece/" + pieceCid,
	}
	for _, sourceURL := range urls {
		_, err = db.Exec(ctx, `
			INSERT INTO pdp_piece_pull_items (fetch_id, piece_cid, piece_raw_size, source_url)
			VALUES ($1, $2, $3, $4)
		`, pullID, pieceCid, rawSize, sourceURL)
		require.NoError(t, err)
	}

	task := &PDPPullPieceTask{db: db}
	require.NoError(t, task.completeAlreadyParkedItems(ctx))

	var completeItems int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pdp_piece_pull_items
		WHERE fetch_id = $1
			AND complete = TRUE
			AND failed = FALSE
			AND task_id IS NULL
			AND parked_piece_ref IS NOT NULL
	`, pullID).Scan(&completeItems)
	require.NoError(t, err)
	require.Equal(t, len(urls), completeItems)

	var pieceRefs int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pdp_piecerefs ppr
		JOIN parked_piece_refs pr ON pr.ref_id = ppr.piece_ref
		WHERE ppr.service = $1
			AND pr.piece_id = $2
			AND pr.data_url = ANY($3::TEXT[])
	`, service, parkedPieceID, urls).Scan(&pieceRefs)
	require.NoError(t, err)
	require.Equal(t, len(urls), pieceRefs)
}

func TestCleanupPullCreatedParkedPieceOnlyDeletesPullRefsAndEnablesParkTask(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	service := "pull-task-cleanup"
	require.NoError(t, insertPullTaskService(ctx, db, service))

	pieceCid, rawSize := testPullPieceCID(t, 2)
	var parkedPieceID int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term, skip)
		VALUES ($1, $2, $3, FALSE, TRUE, TRUE)
		RETURNING id
	`, pieceCid, pdp.PadPieceSize(int64(rawSize)), rawSize).Scan(&parkedPieceID)
	require.NoError(t, err)

	var pullRef int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
		VALUES ($1, 'https://pull.example/piece', TRUE)
		RETURNING ref_id
	`, parkedPieceID).Scan(&pullRef)
	require.NoError(t, err)

	var otherRef int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
		VALUES ($1, 'https://other.example/piece', TRUE)
		RETURNING ref_id
	`, parkedPieceID).Scan(&otherRef)
	require.NoError(t, err)

	var pullID int64
	err = db.QueryRow(ctx, `
		INSERT INTO pdp_piece_pulls (service, extra_data_hash, data_set_id, record_keeper, client_address)
		VALUES ($1, $2, 1, '', '0x1')
		RETURNING id
	`, service, []byte("pull-task-cleanup")).Scan(&pullID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_piece_pull_items (
			fetch_id, piece_cid, piece_raw_size, source_url, parked_piece_ref, pull_parked_piece_id
		)
		VALUES ($1, $2, $3, 'https://pull.example/piece', $4, $5)
	`, pullID, pieceCid, rawSize, pullRef, parkedPieceID)
	require.NoError(t, err)

	var removed bool
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		removed, err = cleanupPullCreatedParkedPieceTx(tx, parkedPieceID)
		return err == nil, err
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)
	require.False(t, removed)

	var pullRefCount, otherRefCount int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_piece_refs WHERE ref_id = $1`, pullRef).Scan(&pullRefCount)
	require.NoError(t, err)
	require.Zero(t, pullRefCount)

	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_piece_refs WHERE ref_id = $1`, otherRef).Scan(&otherRefCount)
	require.NoError(t, err)
	require.Equal(t, 1, otherRefCount)

	var skip bool
	err = db.QueryRow(ctx, `SELECT skip FROM parked_pieces WHERE id = $1`, parkedPieceID).Scan(&skip)
	require.NoError(t, err)
	require.False(t, skip)

	var itemRefs int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pdp_piece_pull_items
		WHERE fetch_id = $1
			AND parked_piece_ref IS NULL
			AND pull_parked_piece_id IS NULL
	`, pullID).Scan(&itemRefs)
	require.NoError(t, err)
	require.Equal(t, 1, itemRefs)
}

func TestExpireStalePullItemsEnablesParkTaskWhenOtherRefsRemain(t *testing.T) {
	ctx := t.Context()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	service := "pull-task-expire"
	require.NoError(t, insertPullTaskService(ctx, db, service))

	pieceCid, rawSize := testPullPieceCID(t, 3)
	var parkedPieceID int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term, skip)
		VALUES ($1, $2, $3, FALSE, TRUE, TRUE)
		RETURNING id
	`, pieceCid, pdp.PadPieceSize(int64(rawSize)), rawSize).Scan(&parkedPieceID)
	require.NoError(t, err)

	var pullRef int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
		VALUES ($1, 'https://pull-expire.example/piece', TRUE)
		RETURNING ref_id
	`, parkedPieceID).Scan(&pullRef)
	require.NoError(t, err)

	var otherRef int64
	err = db.QueryRow(ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
		VALUES ($1, 'https://other-expire.example/piece', TRUE)
		RETURNING ref_id
	`, parkedPieceID).Scan(&otherRef)
	require.NoError(t, err)

	var pullID int64
	err = db.QueryRow(ctx, `
		INSERT INTO pdp_piece_pulls (service, extra_data_hash, data_set_id, record_keeper, client_address)
		VALUES ($1, $2, 1, '', '0x1')
		RETURNING id
	`, service, []byte("pull-task-expire")).Scan(&pullID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_piece_pull_items (
			fetch_id, piece_cid, piece_raw_size, source_url, created_at, parked_piece_ref, pull_parked_piece_id
		)
		VALUES ($1, $2, $3, 'https://pull-expire.example/piece', NOW() - INTERVAL '31 minutes', $4, $5)
	`, pullID, pieceCid, rawSize, pullRef, parkedPieceID)
	require.NoError(t, err)

	task := &PDPPullPieceTask{db: db}
	require.NoError(t, task.expireStalePullItems(ctx))

	var failedItems int
	err = db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pdp_piece_pull_items
		WHERE fetch_id = $1
			AND failed = TRUE
			AND fail_reason = 'pull budget exceeded'
			AND parked_piece_ref IS NULL
			AND pull_parked_piece_id IS NULL
	`, pullID).Scan(&failedItems)
	require.NoError(t, err)
	require.Equal(t, 1, failedItems)

	var pullRefCount, otherRefCount int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_piece_refs WHERE ref_id = $1`, pullRef).Scan(&pullRefCount)
	require.NoError(t, err)
	require.Zero(t, pullRefCount)

	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM parked_piece_refs WHERE ref_id = $1`, otherRef).Scan(&otherRefCount)
	require.NoError(t, err)
	require.Equal(t, 1, otherRefCount)

	var skip bool
	err = db.QueryRow(ctx, `SELECT skip FROM parked_pieces WHERE id = $1`, parkedPieceID).Scan(&skip)
	require.NoError(t, err)
	require.False(t, skip)
}

func insertPullTaskService(ctx context.Context, db *harmonydb.DB, service string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO pdp_services (pubkey, service_label)
		VALUES ($1, $2)
	`, []byte(service), service)
	return err
}

func testPullPieceCID(t *testing.T, seed uint64) (string, uint64) {
	t.Helper()

	rawSize := uint64(127)
	var commP [32]byte
	binary.BigEndian.PutUint64(commP[:8], seed)
	binary.BigEndian.PutUint64(commP[8:16], rawSize)
	pieceCID, err := commcid.DataCommitmentToPieceCidv2(commP[:], rawSize)
	require.NoError(t, err)

	info, err := pdp.ParsePieceCidV2(pieceCID.String())
	require.NoError(t, err)

	return info.CidV1.String(), rawSize
}
