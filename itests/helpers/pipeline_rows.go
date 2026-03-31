package helpers

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"golang.org/x/xerrors"
)

type mk12PipelineRow struct {
	UUID         string         `db:"uuid"`
	Started      bool           `db:"started"`
	AfterCommp   bool           `db:"after_commp"`
	AfterPSD     bool           `db:"after_psd"`
	AfterFind    bool           `db:"after_find_deal"`
	Sector       sql.NullInt64  `db:"sector"`
	SectorOffset sql.NullInt64  `db:"sector_offset"`
	Sealed       bool           `db:"sealed"`
	Indexed      bool           `db:"indexed"`
	Complete     bool           `db:"complete"`
	IndexingTask sql.NullInt64  `db:"indexing_task_id"`
	CommpTask    sql.NullInt64  `db:"commp_task_id"`
	PSDTask      sql.NullInt64  `db:"psd_task_id"`
	FindTask     sql.NullInt64  `db:"find_deal_task_id"`
	IsDDO        bool           `db:"is_ddo"`
	ShouldIndex  bool           `db:"should_index"`
	Error        sql.NullString `db:"error"`
}

type mk20PipelineRow struct {
	ID           string        `db:"id"`
	Started      bool          `db:"started"`
	Downloaded   bool          `db:"downloaded"`
	AfterCommp   bool          `db:"after_commp"`
	Aggregated   bool          `db:"aggregated"`
	Sector       sql.NullInt64 `db:"sector"`
	SectorOffset sql.NullInt64 `db:"sector_offset"`
	Sealed       bool          `db:"sealed"`
	Indexed      bool          `db:"indexed"`
	Complete     bool          `db:"complete"`
	Indexing     bool          `db:"indexing"`
	IndexTask    sql.NullInt64 `db:"indexing_task_id"`
	CommpTask    sql.NullInt64 `db:"commp_task_id"`
	AggTask      sql.NullInt64 `db:"agg_task_id"`
}

type mk20PipelineSummaryRow struct {
	ID             string
	Waiting        bool
	RowsInPipeline int64
	Started        bool
	Downloaded     bool
	AfterCommp     bool
	Aggregated     bool
	HasSector      bool
	Sealed         bool
	Indexed        bool
	Complete       bool
}

type sdrPipelineRow struct {
	SPID                  int64          `db:"sp_id"`
	SectorNumber          int64          `db:"sector_number"`
	AfterSDR              bool           `db:"after_sdr"`
	AfterTreeD            bool           `db:"after_tree_d"`
	AfterTreeC            bool           `db:"after_tree_c"`
	AfterTreeR            bool           `db:"after_tree_r"`
	AfterSynth            bool           `db:"after_synth"`
	AfterPrecommitMsg     bool           `db:"after_precommit_msg"`
	AfterPrecommitSuccess bool           `db:"after_precommit_msg_success"`
	AfterPoRep            bool           `db:"after_porep"`
	AfterFinalize         bool           `db:"after_finalize"`
	AfterMoveStorage      bool           `db:"after_move_storage"`
	AfterCommitMsg        bool           `db:"after_commit_msg"`
	AfterCommitSuccess    bool           `db:"after_commit_msg_success"`
	Failed                bool           `db:"failed"`
	FailedReason          sql.NullString `db:"failed_reason"`
	FailedReasonMsg       sql.NullString `db:"failed_reason_msg"`
	TaskCommitMsg         sql.NullInt64  `db:"task_id_commit_msg"`
}

func queryMK12PipelineRows(ctx context.Context, db *harmonydb.DB, uuids []string) ([]mk12PipelineRow, error) {
	var rows []mk12PipelineRow
	if err := db.Select(ctx, &rows, `SELECT
		p.uuid, p.started, p.after_commp, p.after_psd, p.after_find_deal,
		p.sector, p.sector_offset, p.sealed, p.indexed, p.complete,
		p.indexing_task_id, p.commp_task_id, p.psd_task_id, p.find_deal_task_id,
		p.is_ddo, p.should_index,
		COALESCE(d.error, dd.error) AS error
	FROM market_mk12_deal_pipeline p
	LEFT JOIN market_mk12_deals d ON d.uuid = p.uuid
	LEFT JOIN market_direct_deals dd ON dd.uuid = p.uuid
	WHERE p.uuid = ANY($1)
	ORDER BY p.uuid`, uuids); err != nil {
		return nil, xerrors.Errorf("mk12 pipeline query: %w", err)
	}
	return rows, nil
}

func queryMK20PipelineRows(ctx context.Context, db *harmonydb.DB, ids []string) ([]mk20PipelineRow, error) {
	var rows []mk20PipelineRow
	if err := db.Select(ctx, &rows, `SELECT
		id, started, downloaded, after_commp, aggregated,
		sector, sector_offset, sealed, indexed, complete, indexing,
		indexing_task_id, commp_task_id, agg_task_id
	FROM market_mk20_pipeline
	WHERE id = ANY($1)
	ORDER BY id`, ids); err != nil {
		return nil, xerrors.Errorf("mk20 pipeline query: %w", err)
	}
	return rows, nil
}

func queryMK20WaitingSet(ctx context.Context, db *harmonydb.DB, ids []string) (map[string]bool, error) {
	var waitingIDs []string
	if err := db.Select(ctx, &waitingIDs, `SELECT id FROM market_mk20_pipeline_waiting WHERE id = ANY($1)`, ids); err != nil {
		return nil, xerrors.Errorf("mk20 waiting query: %w", err)
	}

	waiting := make(map[string]bool, len(waitingIDs))
	for _, id := range waitingIDs {
		waiting[id] = true
	}
	return waiting, nil
}

func summarizeMK20PipelineRows(ids []string, rows []mk20PipelineRow, waiting map[string]bool) []mk20PipelineSummaryRow {
	summaries := make(map[string]*mk20PipelineSummaryRow, len(ids))
	idList := uniqueSortedStrings(ids)

	for _, id := range idList {
		summaries[id] = &mk20PipelineSummaryRow{
			ID:      id,
			Waiting: waiting[id],
		}
	}

	for _, r := range rows {
		s, ok := summaries[r.ID]
		if !ok {
			continue
		}
		if s.RowsInPipeline == 0 {
			s.Started = true
			s.Downloaded = true
			s.AfterCommp = true
			s.Aggregated = true
			s.Sealed = true
			s.Indexed = true
			s.Complete = true
		}
		s.RowsInPipeline++
		s.Started = s.Started && r.Started
		s.Downloaded = s.Downloaded && r.Downloaded
		s.AfterCommp = s.AfterCommp && r.AfterCommp
		s.Aggregated = s.Aggregated && r.Aggregated
		s.HasSector = s.HasSector || r.Sector.Valid
		s.Sealed = s.Sealed && r.Sealed
		s.Indexed = s.Indexed && r.Indexed
		s.Complete = s.Complete && r.Complete
	}

	out := make([]mk20PipelineSummaryRow, 0, len(idList))
	for _, id := range idList {
		out = append(out, *summaries[id])
	}
	return out
}

func querySDRPipelineRows(ctx context.Context, db *harmonydb.DB, sectors []SectorRef) ([]sdrPipelineRow, error) {
	var rows []sdrPipelineRow

	if len(sectors) > 0 {
		spids := make([]int64, 0, len(sectors))
		sectorNums := make([]int64, 0, len(sectors))
		for _, s := range sectors {
			spids = append(spids, s.SPID)
			sectorNums = append(sectorNums, s.Number)
		}

		if err := db.Select(ctx, &rows, `SELECT
			sp_id, sector_number,
			after_sdr, after_tree_d, after_tree_c, after_tree_r, after_synth,
			after_precommit_msg, after_precommit_msg_success,
			after_porep, after_finalize, after_move_storage,
			after_commit_msg, after_commit_msg_success,
			failed, failed_reason, failed_reason_msg,
			task_id_commit_msg
		FROM sectors_sdr_pipeline
		WHERE sp_id = ANY($1) AND sector_number = ANY($2)
		ORDER BY sp_id, sector_number`, spids, sectorNums); err != nil {
			return nil, xerrors.Errorf("sectors pipeline query: %w", err)
		}
		return rows, nil
	}

	if err := db.Select(ctx, &rows, `SELECT
		sp_id, sector_number,
		after_sdr, after_tree_d, after_tree_c, after_tree_r, after_synth,
		after_precommit_msg, after_precommit_msg_success,
		after_porep, after_finalize, after_move_storage,
		after_commit_msg, after_commit_msg_success,
		failed, failed_reason, failed_reason_msg,
		task_id_commit_msg
	FROM sectors_sdr_pipeline
	ORDER BY sp_id, sector_number`); err != nil {
		return nil, xerrors.Errorf("sectors pipeline query: %w", err)
	}

	return rows, nil
}

func logMK12PipelineRows(t *testing.T, rows []mk12PipelineRow, verbose bool, prefix string) {
	t.Helper()
	for _, r := range rows {
		if verbose {
			t.Logf("%suuid=%s started=%t commp=%t psd=%t find=%t sector=%s offset=%s sealed=%t indexed=%t complete=%t indexing_task=%s commp_task=%s psd_task=%s find_task=%s is_ddo=%t should_index=%t error=%s",
				withPipelinePrefix(prefix),
				r.UUID, r.Started, r.AfterCommp, r.AfterPSD, r.AfterFind,
				nullInt64(r.Sector), nullInt64(r.SectorOffset),
				r.Sealed, r.Indexed, r.Complete,
				nullInt64(r.IndexingTask), nullInt64(r.CommpTask), nullInt64(r.PSDTask), nullInt64(r.FindTask),
				r.IsDDO, r.ShouldIndex, nullString(r.Error))
			continue
		}

		t.Logf("%suuid=%s started=%t commp=%t psd=%t find=%t sector=%s offset=%s sealed=%t indexed=%t complete=%t is_ddo=%t",
			withPipelinePrefix(prefix),
			r.UUID, r.Started, r.AfterCommp, r.AfterPSD, r.AfterFind,
			nullInt64(r.Sector), nullInt64(r.SectorOffset),
			r.Sealed, r.Indexed, r.Complete, r.IsDDO)
	}
}

func logMK20PipelineRows(t *testing.T, ids []string, rows []mk20PipelineRow, waiting map[string]bool, verbose bool, prefix string) {
	t.Helper()

	if !verbose {
		for _, r := range summarizeMK20PipelineRows(ids, rows, waiting) {
			t.Logf("%sid=%s waiting=%t pipeline_rows=%d started=%t downloaded=%t commp=%t aggregated=%t has_sector=%t sealed=%t indexed=%t complete=%t",
				withPipelinePrefix(prefix),
				r.ID, r.Waiting, r.RowsInPipeline, r.Started, r.Downloaded, r.AfterCommp, r.Aggregated, r.HasSector, r.Sealed, r.Indexed, r.Complete)
		}
		return
	}

	for _, r := range rows {
		t.Logf("%sid=%s started=%t downloaded=%t commp=%t aggregated=%t sector=%s offset=%s sealed=%t indexed=%t complete=%t indexing=%t indexing_task=%s commp_task=%s agg_task=%s",
			withPipelinePrefix(prefix),
			r.ID, r.Started, r.Downloaded, r.AfterCommp, r.Aggregated,
			nullInt64(r.Sector), nullInt64(r.SectorOffset),
			r.Sealed, r.Indexed, r.Complete, r.Indexing,
			nullInt64(r.IndexTask), nullInt64(r.CommpTask), nullInt64(r.AggTask))
	}
}

func logSDRPipelineRows(t *testing.T, rows []sdrPipelineRow, verbose bool, prefix string) {
	t.Helper()
	for _, r := range rows {
		if verbose {
			t.Logf("%ssp=%d sector=%d sdr=%t tree_d=%t tree_c=%t tree_r=%t synth=%t precommit=%t precommit_ok=%t porep=%t finalize=%t move=%t commit=%t commit_ok=%t failed=%t failed_reason=%s failed_reason_msg=%s task_commit=%s",
				withPipelinePrefix(prefix),
				r.SPID, r.SectorNumber,
				r.AfterSDR, r.AfterTreeD, r.AfterTreeC, r.AfterTreeR, r.AfterSynth,
				r.AfterPrecommitMsg, r.AfterPrecommitSuccess,
				r.AfterPoRep, r.AfterFinalize, r.AfterMoveStorage,
				r.AfterCommitMsg, r.AfterCommitSuccess,
				r.Failed, nullString(r.FailedReason), nullString(r.FailedReasonMsg), nullInt64(r.TaskCommitMsg))
			continue
		}

		t.Logf("%ssp=%d num=%d sdr=%t tree_d=%t tree_c=%t tree_r=%t synth=%t precommit_ok=%t porep=%t commit_ok=%t failed=%t failed_reason=%s failed_reason_msg=%s",
			withPipelinePrefix(prefix),
			r.SPID, r.SectorNumber,
			r.AfterSDR, r.AfterTreeD, r.AfterTreeC, r.AfterTreeR, r.AfterSynth,
			r.AfterPrecommitSuccess, r.AfterPoRep, r.AfterCommitSuccess,
			r.Failed, nullString(r.FailedReason), nullString(r.FailedReasonMsg))
	}
}

func withPipelinePrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	return prefix + " "
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
