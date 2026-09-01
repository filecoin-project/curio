package pdpv0

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

const (
	notifyTestRawSize    = int64(1024)
	notifyTestPaddedSize = int64(2048)
)

type notifyUploadFixture struct {
	uploadID     string
	service      string
	pieceCID     string
	parkedPiece  int64
	pieceRef     int64
	notifyTaskID sql.NullInt64
}

func newNotifyUploadFixture(prefix string) notifyUploadFixture {
	return notifyUploadFixture{
		uploadID: uuid.NewString(),
		service:  "public",
		pieceCID: prefix + "-" + uuid.NewString(),
	}
}

func insertUploadIntent(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture, notifyURL string) {
	t.Helper()

	var pieceRef any
	if fixture.pieceRef != 0 {
		pieceRef = fixture.pieceRef
	}
	var notifyTaskID any
	if fixture.notifyTaskID.Valid {
		notifyTaskID = fixture.notifyTaskID.Int64
	}

	_, err := db.Exec(t.Context(), `
		INSERT INTO pdp_piece_uploads (
			id, service, piece_cid, notify_url, check_hash_codec,
			check_hash, check_size, piece_ref, notify_task_id
		)
		VALUES ($1, $2, $3, $4, 'legacy', $5, $6, $7, $8)
	`, fixture.uploadID, fixture.service, fixture.pieceCID, notifyURL, []byte{1}, notifyTestRawSize, pieceRef, notifyTaskID)
	require.NoError(t, err)
}

func insertParkedPiece(t *testing.T, db *harmonydb.DB, fixture *notifyUploadFixture, complete, skip bool, dataURL *string) {
	t.Helper()

	err := db.QueryRow(t.Context(), `
		INSERT INTO parked_pieces (
			piece_cid, piece_padded_size, piece_raw_size,
			complete, long_term, skip
		)
		VALUES ($1, $2, $3, $4, TRUE, $5)
		RETURNING id
	`, fixture.pieceCID, notifyTestPaddedSize, notifyTestRawSize, complete, skip).Scan(&fixture.parkedPiece)
	require.NoError(t, err)

	err = db.QueryRow(t.Context(), `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
		VALUES ($1, $2, TRUE)
		RETURNING ref_id
	`, fixture.parkedPiece, dataURL).Scan(&fixture.pieceRef)
	require.NoError(t, err)
}

func attachUploadPiece(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture) {
	t.Helper()

	n, err := db.Exec(t.Context(), `
		UPDATE pdp_piece_uploads
		SET piece_ref = $1, piece_cid = $2
		WHERE id = $3 AND piece_ref IS NULL
	`, fixture.pieceRef, fixture.pieceCID, fixture.uploadID)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func insertStreamingIntent(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture) {
	t.Helper()

	n, err := db.Exec(t.Context(), `
		INSERT INTO pdp_piece_streaming_uploads (id, service)
		VALUES ($1, $2)
	`, fixture.uploadID, fixture.service)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func attachStreamingPiece(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture, complete bool) {
	t.Helper()

	if complete {
		n, err := db.Exec(t.Context(), `
			UPDATE pdp_piece_streaming_uploads
			SET piece_ref = $1,
				piece_cid = $2,
				piece_size = $3,
				raw_size = $4,
				complete = TRUE,
				completed_at = NOW()
			WHERE id = $5 AND service = $6 AND piece_ref IS NULL
		`, fixture.pieceRef, fixture.pieceCID, notifyTestPaddedSize, notifyTestRawSize, fixture.uploadID, fixture.service)
		require.NoError(t, err)
		require.Equal(t, 1, n)
		return
	}

	n, err := db.Exec(t.Context(), `
		UPDATE pdp_piece_streaming_uploads
		SET piece_ref = $1, created_at = NOW()
		WHERE id = $2 AND service = $3 AND piece_ref IS NULL
	`, fixture.pieceRef, fixture.uploadID, fixture.service)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func completeStreamingPiece(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture) {
	t.Helper()

	n, err := db.Exec(t.Context(), `
		UPDATE parked_pieces
		SET piece_cid = $1,
			piece_padded_size = $2,
			piece_raw_size = $3,
			complete = TRUE
		WHERE id = $4 AND complete = FALSE
	`, fixture.pieceCID, notifyTestPaddedSize, notifyTestRawSize, fixture.parkedPiece)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = db.Exec(t.Context(), `
		UPDATE pdp_piece_streaming_uploads
		SET piece_cid = $1,
			piece_size = $2,
			raw_size = $3,
			complete = TRUE,
			completed_at = NOW()
		WHERE id = $4 AND service = $5 AND piece_ref = $6
	`, fixture.pieceCID, notifyTestPaddedSize, notifyTestRawSize, fixture.uploadID, fixture.service, fixture.pieceRef)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func finalizeLegacyStreamingUpload(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture, notifyURL string) {
	t.Helper()

	committed, err := db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
		var notifyTaskID any
		if fixture.notifyTaskID.Valid {
			notifyTaskID = fixture.notifyTaskID.Int64
		}
		_, err := tx.Exec(`
			INSERT INTO pdp_piece_uploads (
				id, service, piece_cid, notify_url, check_hash_codec,
				check_hash, check_size, piece_ref, notify_task_id
			)
			VALUES ($1, $2, $3, $4, 'legacy-streaming', $5, $6, $7, $8)
		`, fixture.uploadID, fixture.service, fixture.pieceCID, notifyURL, []byte{1}, notifyTestRawSize, fixture.pieceRef, notifyTaskID)
		if err != nil {
			return false, err
		}

		_, err = tx.Exec(`
			DELETE FROM pdp_piece_streaming_uploads
			WHERE id = $1 AND service = $2 AND complete = TRUE
		`, fixture.uploadID, fixture.service)
		if err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)
}

func publishCurrentUpload(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture, streaming bool) {
	t.Helper()

	committed, err := db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
		if !streaming {
			_, err := tx.Exec(`UPDATE parked_pieces SET complete = TRUE WHERE id = $1`, fixture.parkedPiece)
			if err != nil {
				return false, err
			}
		}

		_, err := tx.Exec(`
			INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
			VALUES ($1, $2, $3, NOW(), FALSE)
		`, fixture.service, fixture.pieceCID, fixture.pieceRef)
		if err != nil {
			return false, err
		}

		if streaming {
			_, err = tx.Exec(`
				DELETE FROM pdp_piece_streaming_uploads
				WHERE id = $1 AND service = $2 AND piece_ref = $3 AND complete = TRUE
			`, fixture.uploadID, fixture.service, fixture.pieceRef)
		} else {
			_, err = tx.Exec(`
				DELETE FROM pdp_piece_uploads
				WHERE id = $1 AND piece_ref = $2
			`, fixture.uploadID, fixture.pieceRef)
		}
		if err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)
}

func markParkedPieceComplete(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture) {
	t.Helper()
	n, err := db.Exec(t.Context(), `UPDATE parked_pieces SET complete = TRUE WHERE id = $1`, fixture.parkedPiece)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func runLegacyUploadScheduler(t *testing.T, db *harmonydb.DB, task *PDPNotifyTask, firstTaskID harmonytask.TaskID) []harmonytask.TaskID {
	t.Helper()

	nextTaskID := firstTaskID
	var assigned []harmonytask.TaskID
	err := task.schedule(func(extraInfo func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		taskID := nextTaskID
		nextTaskID++
		committed, err := db.BeginTransaction(t.Context(), func(tx *harmonydb.Tx) (bool, error) {
			return extraInfo(taskID, tx)
		}, harmonydb.OptionRetry())
		require.NoError(t, err)
		if committed {
			assigned = append(assigned, taskID)
		}
	})
	require.NoError(t, err)
	return assigned
}

func drainScheduledUploads(t *testing.T, task *PDPNotifyTask, taskIDs []harmonytask.TaskID) {
	t.Helper()
	for _, taskID := range taskIDs {
		done, err := task.Do(t.Context(), taskID, func() bool { return true })
		require.NoError(t, err)
		require.True(t, done)
	}
}

func requireUploadPublished(t *testing.T, db *harmonydb.DB, fixture notifyUploadFixture) {
	t.Helper()

	var uploadExists bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pdp_piece_uploads WHERE id = $1)`, fixture.uploadID).Scan(&uploadExists))
	require.False(t, uploadExists)

	var service, pieceCID string
	require.NoError(t, db.QueryRow(t.Context(), `
		SELECT service, piece_cid
		FROM pdp_piecerefs
		WHERE piece_ref = $1
	`, fixture.pieceRef).Scan(&service, &pieceCID))
	require.Equal(t, fixture.service, service)
	require.Equal(t, fixture.pieceCID, pieceCID)
}

func requireUploadNotScheduled(t *testing.T, db *harmonydb.DB, uploadID string) {
	t.Helper()
	var taskID sql.NullInt64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT notify_task_id FROM pdp_piece_uploads WHERE id = $1`, uploadID).Scan(&taskID))
	require.False(t, taskID.Valid)
}

func TestPDPNotifyTaskDrainsOldKnownCIDUploadStates(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	var callbackCount atomic.Int64
	callback := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		callbackCount.Add(1)
	}))
	t.Cleanup(callback.Close)

	notifyTask := NewPDPNotifyTask(db)
	require.Equal(t, tasknames.PDPv0_Notify, notifyTask.TypeDetails().Name)
	require.NotNil(t, notifyTask.TypeDetails().IAmBored)

	// Old POST, missing piece: only the upload intent exists.
	uploadedPiece := newNotifyUploadFixture("old-known-cid")
	insertUploadIntent(t, db, uploadedPiece, callback.URL)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 700))
	requireUploadNotScheduled(t, db, uploadedPiece.uploadID)

	// Old PUT: the bytes are in scratch and final storage is still incomplete.
	legacyScratchURL := "custore://legacy/known-cid/" + uploadedPiece.uploadID
	insertParkedPiece(t, db, &uploadedPiece, false, false, &legacyScratchURL)
	attachUploadPiece(t, db, uploadedPiece)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 700))
	requireUploadNotScheduled(t, db, uploadedPiece.uploadID)

	// Old POST, already-stored branch: it immediately left a complete upload row.
	alreadyStored := newNotifyUploadFixture("old-known-cid-existing")
	insertParkedPiece(t, db, &alreadyStored, true, false, nil)
	insertUploadIntent(t, db, alreadyStored, callback.URL)

	// An old task may already have been assigned when the node is upgraded.
	preassigned := newNotifyUploadFixture("old-known-cid-preassigned")
	preassigned.notifyTaskID = sql.NullInt64{Int64: 699, Valid: true}
	insertParkedPiece(t, db, &preassigned, true, false, nil)
	insertUploadIntent(t, db, preassigned, callback.URL)

	// StorePiece finishes the scratch-backed upload. Both unassigned completed
	// rows must be scheduled in this single unbounded pass.
	markParkedPieceComplete(t, db, uploadedPiece)
	assigned := runLegacyUploadScheduler(t, db, notifyTask, 700)
	require.Equal(t, []harmonytask.TaskID{700, 701}, assigned)

	drainScheduledUploads(t, notifyTask, assigned)
	drainScheduledUploads(t, notifyTask, []harmonytask.TaskID{699})
	requireUploadPublished(t, db, uploadedPiece)
	requireUploadPublished(t, db, alreadyStored)
	requireUploadPublished(t, db, preassigned)
	require.Zero(t, callbackCount.Load())

	// A replay after the upload row was consumed is a successful no-op.
	done, err := notifyTask.Do(t.Context(), 699, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
}

func TestPDPNotifyTaskDrainsOldStreamingUploadStates(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	notifyTask := NewPDPNotifyTask(db)

	// First ordering: the client finalizes while StorePiece is still copying
	// from scratch. The resulting upload row must wait for final storage.
	finalizedFirst := newNotifyUploadFixture("old-streaming-finalized-first")
	insertStreamingIntent(t, db, finalizedFirst)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 800))

	legacyScratchURL := "custore://legacy/streaming/" + finalizedFirst.uploadID
	insertParkedPiece(t, db, &finalizedFirst, false, false, &legacyScratchURL)
	attachStreamingPiece(t, db, finalizedFirst, true)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 800))

	finalizeLegacyStreamingUpload(t, db, finalizedFirst, "https://legacy.invalid/finalized-first")
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 800))
	requireUploadNotScheduled(t, db, finalizedFirst.uploadID)

	// Second ordering: StorePiece finishes before the client finalizes. There is
	// still no upload row for Notify to see until old finalize runs.
	storedFirst := newNotifyUploadFixture("old-streaming-stored-first")
	insertStreamingIntent(t, db, storedFirst)
	legacyScratchURL = "custore://legacy/streaming/" + storedFirst.uploadID
	insertParkedPiece(t, db, &storedFirst, false, false, &legacyScratchURL)
	attachStreamingPiece(t, db, storedFirst, true)
	markParkedPieceComplete(t, db, storedFirst)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 800))

	finalizeLegacyStreamingUpload(t, db, storedFirst, "https://legacy.invalid/stored-first")
	markParkedPieceComplete(t, db, finalizedFirst)

	assigned := runLegacyUploadScheduler(t, db, notifyTask, 800)
	require.Equal(t, []harmonytask.TaskID{800, 801}, assigned)
	drainScheduledUploads(t, notifyTask, assigned)
	requireUploadPublished(t, db, finalizedFirst)
	requireUploadPublished(t, db, storedFirst)
}

func TestPDPNotifyTaskIgnoresCurrentUploadStates(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	notifyTask := NewPDPNotifyTask(db)

	// Current known-CID POST: an unclaimed upload intent has no piece ref.
	direct := newNotifyUploadFixture("current-known-cid")
	insertUploadIntent(t, db, direct, "")
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))
	requireUploadNotScheduled(t, db, direct.uploadID)

	// Current known-CID PUT: the handler owns an incomplete skip=true piece,
	// with no scratch URL, until its final transaction commits.
	insertParkedPiece(t, db, &direct, false, true, nil)
	attachUploadPiece(t, db, direct)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))
	requireUploadNotScheduled(t, db, direct.uploadID)

	// Current PUT success atomically publishes the PDP ref and removes the
	// upload intent, so no complete upload row is ever visible to Notify.
	publishCurrentUpload(t, db, direct, false)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))
	requireUploadPublished(t, db, direct)

	// Current known-CID POST with an already-stored piece publishes immediately
	// and never creates an upload intent.
	alreadyStored := newNotifyUploadFixture("current-known-cid-existing")
	insertParkedPiece(t, db, &alreadyStored, true, false, nil)
	_, err = db.Exec(t.Context(), `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		VALUES ($1, $2, $3, NOW(), FALSE)
	`, alreadyStored.service, alreadyStored.pieceCID, alreadyStored.pieceRef)
	require.NoError(t, err)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))
	requireUploadPublished(t, db, alreadyStored)

	// Current streaming POST: only the streaming session exists.
	streaming := newNotifyUploadFixture("current-streaming")
	insertStreamingIntent(t, db, streaming)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))

	// Current streaming PUT claim: the session points at an incomplete
	// provisional skip=true piece and there is still no generic upload row.
	provisional := streaming
	provisional.pieceCID = "current-streaming-provisional-" + uuid.NewString()
	insertParkedPiece(t, db, &provisional, false, true, nil)
	streaming.parkedPiece = provisional.parkedPiece
	streaming.pieceRef = provisional.pieceRef
	attachStreamingPiece(t, db, streaming, false)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))

	// Current streaming PUT completion keeps the completed state in the
	// streaming table; finalize publishes it directly without Notify.
	completeStreamingPiece(t, db, streaming)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))
	publishCurrentUpload(t, db, streaming, true)
	require.Empty(t, runLegacyUploadScheduler(t, db, notifyTask, 900))
	requireUploadPublished(t, db, streaming)
}
