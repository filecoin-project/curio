package helpers

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func PipelineProgress(t *testing.T, ctx context.Context, db *harmonydb.DB, mk12UUIDs []string, mk20IDs []string, poll int, remaining time.Duration) error {
	t.Helper()

	t.Logf("E2E-PROGRESS poll=%d remaining=%s", poll, remaining.Round(time.Second))

	if err := logMK12ProgressRows(t, ctx, db, mk12UUIDs); err != nil {
		return err
	}
	if err := logMK20ProgressRows(t, ctx, db, mk20IDs); err != nil {
		return err
	}
	if err := logSectorPipelineProgressRows(t, ctx, db); err != nil {
		return err
	}
	if err := logMachineTaskProgressRows(t, ctx, db); err != nil {
		return err
	}

	return nil
}

func logMK12ProgressRows(t *testing.T, ctx context.Context, db *harmonydb.DB, uuids []string) error {
	t.Helper()

	if len(uuids) == 0 {
		t.Log("E2E-PROGRESS mk12 none")
		return nil
	}

	rows, err := queryMK12PipelineRows(ctx, db, uuids)
	if err != nil {
		return xerrors.Errorf("mk12 progress query: %w", err)
	}

	if len(rows) == 0 {
		t.Log("E2E-PROGRESS mk12 no rows")
		return nil
	}

	logMK12PipelineRows(t, rows, false, "E2E-PROGRESS mk12")
	return nil
}

func logMK20ProgressRows(t *testing.T, ctx context.Context, db *harmonydb.DB, ids []string) error {
	t.Helper()

	if len(ids) == 0 {
		t.Log("E2E-PROGRESS mk20 none")
		return nil
	}

	rows, err := queryMK20PipelineRows(ctx, db, ids)
	if err != nil {
		return xerrors.Errorf("mk20 progress query: %w", err)
	}

	waitingSet, err := queryMK20WaitingSet(ctx, db, ids)
	if err != nil {
		return xerrors.Errorf("mk20 progress query waiting set: %w", err)
	}

	logMK20PipelineRows(t, ids, rows, waitingSet, false, "E2E-PROGRESS mk20")
	return nil
}

func logSectorPipelineProgressRows(t *testing.T, ctx context.Context, db *harmonydb.DB) error {
	t.Helper()

	rows, err := querySDRPipelineRows(ctx, db, nil)
	if err != nil {
		return xerrors.Errorf("sectors progress query: %w", err)
	}

	if len(rows) == 0 {
		t.Log("E2E-PROGRESS sectors none")
		return nil
	}

	logSDRPipelineRows(t, rows, false, "E2E-PROGRESS sector")
	return nil
}

func logMachineTaskProgressRows(t *testing.T, ctx context.Context, db *harmonydb.DB) error {
	t.Helper()

	type row struct {
		MachineID int64          `db:"machine_id"`
		Name      sql.NullString `db:"machine_name"`
		Layers    sql.NullString `db:"layers"`
		Tasks     sql.NullString `db:"tasks"`
		Miners    sql.NullString `db:"miners"`
	}

	var rows []row
	if err := db.Select(ctx, &rows, `SELECT
		machine_id, machine_name, layers, tasks, miners
	FROM harmony_machine_details
	ORDER BY startup_time DESC, machine_id ASC
	LIMIT 4`); err != nil {
		return xerrors.Errorf("machine details progress query: %w", err)
	}

	if len(rows) == 0 {
		t.Log("E2E-PROGRESS machines none")
		return nil
	}

	for _, r := range rows {
		taskList := nullString(r.Tasks)
		t.Logf("E2E-PROGRESS machine id=%d name=%s layers=%s miners=%s has_commP=%t has_psd=%t has_find_deal=%t has_seal_sdr=%t has_commit_batch=%t",
			r.MachineID, nullString(r.Name), nullString(r.Layers), nullString(r.Miners),
			strings.Contains(taskList, "CommP"),
			strings.Contains(taskList, "PSD"),
			strings.Contains(taskList, "FindDeal"),
			strings.Contains(taskList, "SDR"),
			strings.Contains(taskList, "CommitBatch"),
		)
	}

	return nil
}

type SectorRef struct {
	SPID   int64
	Number int64
}

func DumpDealPipelineDiagnostics(t *testing.T, ctx context.Context, db *harmonydb.DB, mk12UUIDs []string, mk20IDs []string, sectors []SectorRef) {
	t.Helper()

	t.Log("=== diagnostics: sectors_sdr_pipeline ===")
	dumpSDRPipeline(t, ctx, db, sectors)

	t.Log("=== diagnostics: market_mk12_deal_pipeline ===")
	dumpMK12Pipeline(t, ctx, db, mk12UUIDs)

	t.Log("=== diagnostics: market_mk20_pipeline ===")
	dumpMK20Pipeline(t, ctx, db, mk20IDs)

	t.Log("=== diagnostics: sectors_sdr_initial_pieces (json fields) ===")
	dumpInitialPiecesJSON(t, ctx, db, sectors)

	t.Log("=== diagnostics: commit task errors ===")
	dumpCommitTaskErrors(t, ctx, db)
}

func dumpSDRPipeline(t *testing.T, ctx context.Context, db *harmonydb.DB, sectors []SectorRef) {
	t.Helper()

	if len(sectors) == 0 {
		t.Log("no sector refs")
		return
	}

	rows, err := querySDRPipelineRows(ctx, db, sectors)
	if err != nil {
		t.Logf("query error: %s", err)
		return
	}
	if len(rows) == 0 {
		t.Log("no matching sectors_sdr_pipeline rows")
		return
	}

	logSDRPipelineRows(t, rows, true, "")
}

func dumpMK12Pipeline(t *testing.T, ctx context.Context, db *harmonydb.DB, uuids []string) {
	t.Helper()
	if len(uuids) == 0 {
		t.Log("no mk12 ids")
		return
	}

	rows, err := queryMK12PipelineRows(ctx, db, uuids)
	if err != nil {
		t.Logf("query error: %s", err)
		return
	}
	if len(rows) == 0 {
		t.Log("no mk12 pipeline rows")
		return
	}
	logMK12PipelineRows(t, rows, true, "")
}

func dumpMK20Pipeline(t *testing.T, ctx context.Context, db *harmonydb.DB, ids []string) {
	t.Helper()
	if len(ids) == 0 {
		t.Log("no mk20 ids")
		return
	}

	rows, err := queryMK20PipelineRows(ctx, db, ids)
	if err != nil {
		t.Logf("query error: %s", err)
		return
	}
	if len(rows) == 0 {
		t.Log("no mk20 pipeline rows")
		return
	}
	logMK20PipelineRows(t, ids, rows, nil, true, "")
}

func dumpInitialPiecesJSON(t *testing.T, ctx context.Context, db *harmonydb.DB, sectors []SectorRef) {
	t.Helper()
	if len(sectors) == 0 {
		t.Log("no sector refs")
		return
	}

	spids := make([]int64, 0, len(sectors))
	sectorNums := make([]int64, 0, len(sectors))
	for _, s := range sectors {
		spids = append(spids, s.SPID)
		sectorNums = append(sectorNums, s.Number)
	}

	type row struct {
		SPID      int64           `db:"sp_id"`
		Sector    int64           `db:"sector_number"`
		PieceIdx  int64           `db:"piece_index"`
		PieceCID  string          `db:"piece_cid"`
		PieceSize int64           `db:"piece_size"`
		F05DealID sql.NullInt64   `db:"f05_deal_id"`
		F05Prop   json.RawMessage `db:"f05_deal_proposal"`
		DDOPAM    json.RawMessage `db:"direct_piece_activation_manifest"`
	}

	var rows []row
	err := db.Select(ctx, &rows, `SELECT
		sp_id, sector_number, piece_index, piece_cid, piece_size,
		f05_deal_id, f05_deal_proposal, direct_piece_activation_manifest
	FROM sectors_sdr_initial_pieces
	WHERE sp_id = ANY($1) AND sector_number = ANY($2)
	ORDER BY sp_id, sector_number, piece_index`, spids, sectorNums)
	if err != nil {
		t.Logf("query error: %s", err)
		return
	}
	if len(rows) == 0 {
		t.Log("no sectors_sdr_initial_pieces rows")
		return
	}
	for _, r := range rows {
		t.Logf("sp=%d sector=%d idx=%d cid=%s size=%d f05_deal_id=%s f05_proposal=%s ddo_manifest=%s",
			r.SPID, r.Sector, r.PieceIdx, r.PieceCID, r.PieceSize,
			nullInt64(r.F05DealID), stringOrNull(r.F05Prop), stringOrNull(r.DDOPAM))
	}
}

func dumpCommitTaskErrors(t *testing.T, ctx context.Context, db *harmonydb.DB) {
	t.Helper()

	type row struct {
		TaskID  int64          `db:"task_id"`
		Name    string         `db:"name"`
		WorkEnd time.Time      `db:"work_end"`
		Err     sql.NullString `db:"err"`
	}

	var rows []row
	err := db.Select(ctx, &rows, `SELECT task_id, name, work_end, err
		FROM harmony_task_history
		WHERE result = FALSE
		  AND (name = 'CommitBatch' OR name ILIKE '%commit%')
		ORDER BY work_end DESC
		LIMIT 20`)
	if err != nil {
		t.Logf("query error: %s", err)
		return
	}
	if len(rows) == 0 {
		t.Log("no failed commit-related task history rows")
		return
	}
	for _, r := range rows {
		t.Logf("task_id=%d name=%s work_end=%s err=%s", r.TaskID, r.Name, r.WorkEnd.UTC().Format(time.RFC3339), nullString(r.Err))
	}
}

func AssertNoFailedSectors(t *testing.T, ctx context.Context, db *harmonydb.DB) {
	t.Helper()
	var failed int
	require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE failed = TRUE`).Scan(&failed))
	require.Equal(t, 0, failed, "no sectors should fail")
}
