package pdpv0

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-padreader"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/passcall"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/tasknames"
)

// RepairMissingPiecesTask reconciles one data set against PDPVerifier and
// rebuilds pdp_data_set_pieces rows for pieces the contract reports as live but
// which Curio has no local record of.
//
// Resolves bad state reported in https://github.com/filecoin-project/curio/issues/1359
// TODO: this task can be removed once all bad state is fixed.
//
// The add-piece path is fixed in the release that carries this task, so only
// data sets that predate the upgrade can be damaged. The work queue,
// pdp_data_set_piece_scrub, is seeded once by the migration from the
// pdp_data_sets rows present at that moment. Nothing enqueues a data set later.
// Once every seeded row is complete the task goes permanently idle.
//
// Before writing a row the task confirms the bytes are actually reachable.
// If this check fails the piece is unrecoverable by repair, and
// it is logged and recorded in the lost_data table
type RepairMissingPiecesTask struct {
	db  *harmonydb.DB
	eth ethchain.EthClient
	pio ParkedPieceStore
}

const (
	// activePiecePageSize bounds one getActivePiecesByCursor eth_call. Cursor
	// paging is used over offset paging because the contract can resume from a
	// piece id directly instead of walking the set from the start each page.
	activePiecePageSize = 1000

	// repairAddMessageHash marks rows this task reconstructed.
	repairAddMessageHash = "REPAIRED"

	// repairScheduleBatch caps how many data sets are queued per scheduler
	// pass, so an SP with many data sets drains steadily instead of creating a
	// harmony_task row for every one of them at once.
	repairScheduleBatch = 8

	// repairMaxFailures bounds requeues of a data set whose task vanished
	// without finishing, so a permanently failing set cannot cycle forever.
	repairMaxFailures = 10
)

func NewRepairMissingPiecesTask(db *harmonydb.DB, eth ethchain.EthClient, pio ParkedPieceStore) *RepairMissingPiecesTask {
	return &RepairMissingPiecesTask{
		db:  db,
		eth: eth,
		pio: pio,
	}
}

func (t *RepairMissingPiecesTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var dataSetID, nextPieceID int64
	var service string
	err = t.db.QueryRow(ctx, `
		SELECT s.data_set, s.next_piece_id, d.service
		FROM pdp_data_set_piece_scrub s
		JOIN pdp_data_sets d ON d.id = s.data_set
		WHERE s.task_id = $1 AND s.complete = FALSE
	`, taskID).Scan(&dataSetID, &nextPieceID, &service)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row finished, or its data set was deleted, between scheduling
			// and now. Nothing left to reconcile.
			return true, nil
		}
		return false, xerrors.Errorf("failed to load scrub row for task %d: %w", taskID, err)
	}

	verifierAddress := contract.ContractAddresses().PDPVerifier
	verifier, err := contract.NewPDPVerifier(verifierAddress, t.eth)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract at %s: %w", verifierAddress.Hex(), err)
	}

	setID := big.NewInt(dataSetID)

	live, err := verifier.DataSetLive(contract.EthCallOpts(ctx), setID)
	if err != nil {
		if IsPDPVerifierDataSetNotFound(err) {
			return t.finish(ctx, taskID, dataSetID, "data set not found on chain")
		}
		return false, xerrors.Errorf("failed to check if data set %d is live: %w", dataSetID, err)
	}
	if !live {
		// A dead set has no pieces to prove, so a missing row cannot hurt it.
		return t.finish(ctx, taskID, dataSetID, "data set not live")
	}

	cursor := big.NewInt(nextPieceID)
	for {
		if !stillOwned() {
			return false, nil
		}

		page, err := verifier.GetActivePiecesByCursor(contract.EthCallOpts(ctx), setID, cursor, big.NewInt(activePiecePageSize))
		if err != nil {
			if IsPDPVerifierDataSetNotFound(err) || isDataSetNotLiveRevert(err) {
				// The set was deleted while the scrub was walking it. Its pieces
				// no longer need rows, so there is nothing left to repair.
				return t.finish(ctx, taskID, dataSetID, "data set no longer live")
			}
			return false, xerrors.Errorf("failed to get active pieces for data set %d from piece %s: %w", dataSetID, cursor, err)
		}
		if len(page.Pieces) != len(page.PieceIds) {
			return false, xerrors.Errorf("PDPVerifier returned %d piece CIDs for %d piece ids on data set %d", len(page.Pieces), len(page.PieceIds), dataSetID)
		}
		if len(page.PieceIds) == 0 {
			break
		}

		stats, err := t.reconcilePage(ctx, dataSetID, service, page.PieceIds, page.Pieces)
		if err != nil {
			return false, err
		}

		// Resume past the last piece this page covered. Piece ids are returned
		// in ascending order, so the next cursor is one past the tail.
		last := page.PieceIds[len(page.PieceIds)-1]
		if !last.IsInt64() {
			return false, xerrors.Errorf("piece id %s on data set %d is out of int64 range", last, dataSetID)
		}
		next := last.Int64() + 1

		if err := t.recordProgress(ctx, taskID, next, stats); err != nil {
			return false, err
		}

		if !page.HasMore {
			break
		}
		cursor = big.NewInt(next)
	}

	return t.finish(ctx, taskID, dataSetID, "scrub complete")
}

// isDataSetNotLiveRevert matches the plain-string require guarding
// getActivePiecesByCursor. It reverts with Error(string), not the DataSetNotLive
// custom error, so the shared selector-based matchers never see it.
func isDataSetNotLiveRevert(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Data set not live")
}

// repairStats accumulates one page worth of reconciliation counters.
type repairStats struct {
	checked  int64
	repaired int64
	lost     int64
}

// reconcilePage repairs every piece in one contract page that has no local row.
func (t *RepairMissingPiecesTask) reconcilePage(ctx context.Context, dataSetID int64, service string, pieceIDs []*big.Int, pieceCids []contract.CidsCid) (repairStats, error) {
	var stats repairStats

	if len(pieceIDs) != len(pieceCids) {
		return stats, xerrors.Errorf("PDPVerifier returned %d piece CIDs for %d piece ids on data set %d", len(pieceCids), len(pieceIDs), dataSetID)
	}

	ids := make([]int64, 0, len(pieceIDs))
	for _, id := range pieceIDs {
		if !id.IsInt64() {
			return stats, xerrors.Errorf("piece id %s on data set %d is out of int64 range", id, dataSetID)
		}
		ids = append(ids, id.Int64())
	}
	stats.checked = int64(len(ids))

	var tracked []int64
	err := t.db.Select(ctx, &tracked, `
		SELECT DISTINCT piece_id
		FROM pdp_data_set_pieces
		WHERE data_set = $1 AND piece_id = ANY($2::BIGINT[])
	`, dataSetID, ids)
	if err != nil {
		return stats, xerrors.Errorf("failed to load tracked pieces for data set %d: %w", dataSetID, err)
	}

	have := make(map[int64]struct{}, len(tracked))
	for _, id := range tracked {
		have[id] = struct{}{}
	}

	for i, id := range ids {
		if _, ok := have[id]; ok {
			continue
		}

		repaired, err := t.repairPiece(ctx, dataSetID, service, id, pieceCids[i].Data)
		if err != nil {
			return stats, err
		}
		if repaired {
			stats.repaired++
		} else {
			stats.lost++
		}
	}

	return stats, nil
}

// repairPiece rebuilds the pdp_data_set_pieces row for one live-on-chain piece
// with no local record. It reports false when the piece data could not be
// found, in which case the loss has already been logged and recorded.
func (t *RepairMissingPiecesTask) repairPiece(ctx context.Context, dataSetID int64, service string, pieceID int64, chainCid []byte) (bool, error) {
	pieceCid, err := cid.Cast(chainCid)
	if err != nil {
		// Without a CID there is nothing to look for and nothing to write. Keep
		// the raw bytes so the piece can still be identified by hand.
		return false, t.markLost(ctx, dataSetID, pieceID, "0x"+hex.EncodeToString(chainCid), nil, nil,
			xerrors.Errorf("on-chain piece CID could not be decoded: %w", err))
	}

	info, err := pdp.ParsePieceCidV2(pieceCid.String())
	if err != nil {
		// PDPVerifier holds PieceCID v2, which carries the raw size. Anything
		// else leaves the sub-piece size undetermined, so no row can be built.
		return false, t.markLost(ctx, dataSetID, pieceID, pieceCid.String(), nil, nil,
			xerrors.Errorf("on-chain piece CID is not a PieceCID v2: %w", err))
	}

	pieceCidV1 := info.CidV1.String()
	rawSize := int64(info.RawSize)
	paddedSize := int64(padreader.PaddedSize(info.RawSize).Padded())

	// Prefer a parked piece that already has a pdp_pieceref for this CID; that
	// ref can simply be reused. Otherwise any complete long-term parked copy
	// will do and a ref is created for it.
	var loc []struct {
		PieceRefID    int64 `db:"pdp_pieceref"`
		ParkedPieceID int64 `db:"parked_piece_id"`
	}
	err = t.db.Select(ctx, &loc, `
		SELECT COALESCE(pr.id, 0) AS pdp_pieceref, pp.id AS parked_piece_id
		FROM parked_pieces pp
		LEFT JOIN parked_piece_refs pprf ON pprf.piece_id = pp.id
		LEFT JOIN pdp_piecerefs pr ON pr.piece_ref = pprf.ref_id AND pr.piece_cid = pp.piece_cid
		WHERE pp.piece_cid = $1
			AND pp.piece_padded_size = $2
			AND pp.complete = TRUE
			AND pp.long_term = TRUE
			AND pp.cleanup_task_id IS NULL
		ORDER BY (pr.id IS NULL), pr.id ASC, pp.id ASC
		LIMIT 1
	`, pieceCidV1, paddedSize)
	if err != nil {
		return false, xerrors.Errorf("failed to look up parked piece for %s: %w", pieceCidV1, err)
	}
	if len(loc) == 0 {
		return false, t.markLost(ctx, dataSetID, pieceID, info.CidV2.String(), &pieceCidV1, &rawSize,
			xerrors.New("no complete long-term parked piece holds this CID"))
	}

	// A parked_pieces row only records intent to store. Open the piece to
	// confirm the bytes are really there before claiming the piece is provable.
	reader, err := t.pio.PieceReader(ctx, storiface.PieceNumber(loc[0].ParkedPieceID))
	if err != nil {
		return false, t.markLost(ctx, dataSetID, pieceID, info.CidV2.String(), &pieceCidV1, &rawSize,
			xerrors.Errorf("parked piece %d is not readable from storage: %w", loc[0].ParkedPieceID, err))
	}
	if err := reader.Close(); err != nil {
		log.Warnw("failed to close parked piece reader after repair check",
			"dataSetId", dataSetID, "pieceId", pieceID, "parkedPieceId", loc[0].ParkedPieceID, "error", err)
	}

	pieceRefID := loc[0].PieceRefID
	comm, err := t.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// The chain CID describes the whole piece, so the piece is its own only
		// sub-piece: offset 0, size the full padded size.
		n, err := tx.Exec(`
			INSERT INTO pdp_data_set_pieces (
				data_set,
				piece,
				piece_id,
				sub_piece,
				sub_piece_offset,
				sub_piece_size,
				pdp_pieceref,
				add_message_hash,
				add_message_index
			) VALUES ($1, $2, $3, $2, 0, $4, $5, $6, 0)
			ON CONFLICT DO NOTHING
		`, dataSetID, pieceCidV1, pieceID, paddedSize, pieceRefID, repairAddMessageHash)
		if err != nil {
			return false, xerrors.Errorf("failed to insert repaired pdp_data_set_pieces row: %w", err)
		}
		if n == 0 {
			// Someone else got there first. Roll back so the ref created above,
			// if any, does not leak.
			return false, nil
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("failed to repair piece %d on data set %d: %w", pieceID, dataSetID, err)
	}
	if !comm {
		log.Infow("PDPv0 repair: piece row already present, skipping",
			"dataSetId", dataSetID, "pieceId", pieceID, "pieceCid", info.CidV2.String())
		return true, nil
	}

	log.Infow("PDPv0 repair: rebuilt missing data set piece row",
		"dataSetId", dataSetID,
		"pieceId", pieceID,
		"pieceCid", info.CidV2.String(),
		"pieceCidV1", pieceCidV1,
		"paddedSize", paddedSize,
		"parkedPieceId", loc[0].ParkedPieceID)

	return true, nil
}

// markLost logs and persists a piece that repair cannot recover.
func (t *RepairMissingPiecesTask) markLost(ctx context.Context, dataSetID, pieceID int64, pieceCid string, pieceCidV1 *string, rawSize *int64, reason error) error {
	log.Errorw("PDPv0 repair: piece is live on chain but its data is missing locally",
		"dataSetId", dataSetID,
		"pieceId", pieceID,
		"pieceCid", pieceCid,
		"error", reason)

	_, err := t.db.Exec(ctx, `
		INSERT INTO lost_data (data_set, piece_id, piece_cid, piece_cid_v1, piece_raw_size, reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (data_set, piece_id) DO UPDATE
		SET piece_cid = EXCLUDED.piece_cid,
			piece_cid_v1 = EXCLUDED.piece_cid_v1,
			piece_raw_size = EXCLUDED.piece_raw_size,
			reason = EXCLUDED.reason,
			detected_at = TIMEZONE('UTC', NOW())
	`, dataSetID, pieceID, pieceCid, pieceCidV1, rawSize, reason.Error())
	if err != nil {
		return xerrors.Errorf("failed to record lost data for piece %d on data set %d: %w", pieceID, dataSetID, err)
	}
	return nil
}

// recordProgress advances the resume cursor and counters after a page.
func (t *RepairMissingPiecesTask) recordProgress(ctx context.Context, taskID harmonytask.TaskID, nextPieceID int64, stats repairStats) error {
	_, err := t.db.Exec(ctx, `
		UPDATE pdp_data_set_piece_scrub
		SET next_piece_id = $2,
			pieces_checked = pieces_checked + $3,
			pieces_repaired = pieces_repaired + $4,
			pieces_lost = pieces_lost + $5
		WHERE task_id = $1
	`, taskID, nextPieceID, stats.checked, stats.repaired, stats.lost)
	if err != nil {
		return xerrors.Errorf("failed to record scrub progress for task %d: %w", taskID, err)
	}
	return nil
}

// finish marks the data set done so it is never scrubbed again.
func (t *RepairMissingPiecesTask) finish(ctx context.Context, taskID harmonytask.TaskID, dataSetID int64, reason string) (bool, error) {
	var checked, repaired, lost int64
	err := t.db.QueryRow(ctx, `
		UPDATE pdp_data_set_piece_scrub
		SET complete = TRUE,
			task_id = NULL,
			completed_at = TIMEZONE('UTC', NOW())
		WHERE task_id = $1
		RETURNING pieces_checked, pieces_repaired, pieces_lost
	`, taskID).Scan(&checked, &repaired, &lost)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Row already gone or already completed; nothing to do.
			return true, nil
		}
		return false, xerrors.Errorf("failed to complete scrub for data set %d: %w", dataSetID, err)
	}

	log.Infow("PDPv0 repair: data set scrub finished",
		"dataSetId", dataSetID,
		"reason", reason,
		"piecesChecked", checked,
		"piecesRepaired", repaired,
		"piecesLost", lost)

	return true, nil
}

// schedule reclaims abandoned rows and queues a bounded batch of data sets.
func (t *RepairMissingPiecesTask) schedule(ctx context.Context, taskFunc harmonytask.AddTaskFunc) error {
	// A claimed row whose harmony_task row is gone was abandoned mid-scrub, for
	// example because its machine died. next_piece_id keeps the progress it did
	// make, so requeueing resumes rather than restarts.
	_, err := t.db.Exec(ctx, `
		UPDATE pdp_data_set_piece_scrub s
		SET task_id = NULL, failures = failures + 1
		WHERE s.task_id IS NOT NULL
			AND s.complete = FALSE
			AND NOT EXISTS (SELECT 1 FROM harmony_task ht WHERE ht.id = s.task_id)
	`)
	if err != nil {
		return xerrors.Errorf("failed to reclaim abandoned scrub rows: %w", err)
	}

	for range repairScheduleBatch {
		stop := true
		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec(`WITH pending AS (
					SELECT data_set
					FROM pdp_data_set_piece_scrub
					WHERE task_id IS NULL
						AND complete = FALSE
						AND failures < $2
					ORDER BY data_set
					LIMIT 1
				)
				UPDATE pdp_data_set_piece_scrub s
				SET task_id = $1
				FROM pending
				WHERE s.data_set = pending.data_set
					AND s.task_id IS NULL
					AND s.complete = FALSE`, id, repairMaxFailures)
			if err != nil {
				return false, xerrors.Errorf("failed to assign scrub task: %w", err)
			}
			if n == 0 {
				return false, nil
			}
			if n != 1 {
				return false, xerrors.Errorf("updated %d rows assigning scrub task", n)
			}

			stop = false
			return true, nil
		})
		if stop {
			return nil
		}
	}

	return nil
}

func (t *RepairMissingPiecesTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (t *RepairMissingPiecesTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(2),
		Name: tasknames.PDPv0_FixPiece,
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20,
		},
		MaxFailures: 3,
		IAmBored: passcall.Every(time.Hour, func(taskFunc harmonytask.AddTaskFunc) error {
			return t.schedule(context.Background(), taskFunc)
		}),
	}
}

func (t *RepairMissingPiecesTask) Adder(taskFunc harmonytask.AddTaskFunc) {}

var _ harmonytask.TaskInterface = &RepairMissingPiecesTask{}
var _ = harmonytask.Reg(&RepairMissingPiecesTask{})
