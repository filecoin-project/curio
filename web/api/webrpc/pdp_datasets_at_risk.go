package webrpc

import (
	"context"
	"sort"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/pdp/paymentstatus"
)

type PDPDataSetAtRiskSummary struct {
	PDPDataSetSummary
	Payer                string     `json:"payer,omitempty"`
	Status               string     `json:"status"`
	Reason               string     `json:"reason,omitempty"`
	ProjectedDeleteEpoch *int64     `json:"projectedDeleteEpoch,omitempty"`
	ProjectedDeleteAt    *time.Time `json:"projectedDeleteAt,omitempty"`
	DeleteDatePending    bool       `json:"deleteDatePending,omitempty"`
	HeadEpoch            int64      `json:"headEpoch"`
}

type PDPDataSetAtRiskListResult struct {
	Items []PDPDataSetAtRiskSummary `json:"items"`
	Total int                       `json:"total"`
}

type PDPDataSetAtRiskDetail struct {
	Payer                string     `json:"payer,omitempty"`
	Status               string     `json:"status,omitempty"`
	Reason               string     `json:"reason,omitempty"`
	ProjectedDeleteEpoch *int64     `json:"projectedDeleteEpoch,omitempty"`
	ProjectedDeleteAt    *time.Time `json:"projectedDeleteAt,omitempty"`
	DeleteDatePending    bool       `json:"deleteDatePending,omitempty"`
	AtRisk               bool       `json:"atRisk"`
}

type PDPDataSetAtRiskScanCursor struct {
	AfterSizeBytes int64 `json:"afterSizeBytes"`
	AfterID        int64 `json:"afterId"`
	Scanned        int   `json:"scanned"`
}

type PDPDataSetAtRiskScanResult struct {
	Items        []PDPDataSetAtRiskSummary  `json:"items"`
	Scanned      int                        `json:"scanned"`
	DatasetTotal int                        `json:"datasetTotal"`
	Complete     bool                       `json:"complete"`
	Cursor       PDPDataSetAtRiskScanCursor `json:"cursor"`
	ChainError   string                     `json:"chainError,omitempty"`
}

const (
	defaultAtRiskScanBatch = 20
	atRiskChainTimeout     = 120 * time.Second
)

type atRiskEntry struct {
	stats pdpDataSetStatsRow
	snap  paymentstatus.Snapshot
}

func normalizeAtRiskSortBy(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "size_bytes", "sizebytes", "size":
		return "size_bytes"
	case "id":
		return "id"
	case "projected_delete_epoch", "projecteddelete", "projected_delete":
		return "projected_delete_epoch"
	default:
		return "projected_delete_epoch"
	}
}

func (a *WebRPC) scanAtRiskBatch(
	ctx context.Context,
	afterSize int64,
	afterID int64,
	scannedSoFar int,
	maxScan int,
	minSizeBytes int64,
) (PDPDataSetAtRiskScanResult, error) {
	out := PDPDataSetAtRiskScanResult{
		Items: []PDPDataSetAtRiskSummary{},
		Cursor: PDPDataSetAtRiskScanCursor{
			AfterSizeBytes: afterSize,
			AfterID:        afterID,
			Scanned:        scannedSoFar,
		},
	}
	if maxScan <= 0 {
		maxScan = defaultAtRiskScanBatch
	}

	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return out, xerrors.Errorf("eth client: %w", err)
	}

	head := uint64(a.chainHeadEpoch(ctx))
	chainCtx, cancel := context.WithTimeout(context.Background(), atRiskChainTimeout)
	defer cancel()

	scan, err := paymentstatus.DefaultResolver.ScanAtRisk(chainCtx, a.Deps.DB, eclient, head, afterSize, afterID, maxScan, minSizeBytes)
	if err != nil {
		return out, xerrors.Errorf("scan payment status: %w", err)
	}

	out.Scanned = scannedSoFar + scan.Scanned
	out.DatasetTotal = scan.DatasetTotal
	out.Complete = scan.Complete
	out.ChainError = scan.ChainError
	out.Cursor = PDPDataSetAtRiskScanCursor{
		AfterSizeBytes: scan.NextAfterSize,
		AfterID:        scan.NextAfterID,
		Scanned:        out.Scanned,
	}
	if scan.ChainError != "" {
		return out, nil
	}

	if len(scan.Found) == 0 {
		return out, nil
	}

	ids := make([]int64, len(scan.Found))
	for i, snap := range scan.Found {
		ids[i] = snap.DataSetID
	}
	stats, err := a.loadDataSetStatsByIDs(ctx, ids)
	if err != nil {
		return out, err
	}
	statsByID := make(map[int64]pdpDataSetStatsRow, len(stats))
	for _, row := range stats {
		statsByID[row.ID] = row
	}

	headEpoch := a.chainHeadEpoch(ctx)
	headTime := a.chainHeadTime(ctx)
	blockDelay := time.Duration(build.BlockDelaySecs) * time.Second

	out.Items = make([]PDPDataSetAtRiskSummary, 0, len(scan.Found))
	for _, snap := range scan.Found {
		row, ok := statsByID[snap.DataSetID]
		if !ok {
			continue
		}
		out.Items = append(out.Items, atRiskSummaryFromEntry(atRiskEntry{stats: row, snap: snap}, headEpoch, headTime, blockDelay))
	}
	return out, nil
}

// PDPDataSetAtRiskScanBatch resolves payment status for the next chunk of local
// datasets, largest size first. Pass afterSize=0 and afterID=0 to start.
func (a *WebRPC) PDPDataSetAtRiskScanBatch(
	ctx context.Context,
	afterSize int64,
	afterID int64,
	scannedSoFar int,
	maxScan int,
	minSizeBytes int64,
) (PDPDataSetAtRiskScanResult, error) {
	if minSizeBytes <= 0 {
		minSizeBytes = paymentstatus.DefaultMinScanSizeBytes
	}
	return a.scanAtRiskBatch(ctx, afterSize, afterID, scannedSoFar, maxScan, minSizeBytes)
}

func (a *WebRPC) resolveAllAtRiskEntries(ctx context.Context) ([]atRiskEntry, error) {
	var all []atRiskEntry
	afterSize := int64(0)
	afterID := int64(0)

	for {
		eclient, err := a.Deps.EthClient.Val()
		if err != nil {
			return nil, xerrors.Errorf("eth client: %w", err)
		}
		head := uint64(a.chainHeadEpoch(ctx))
		chainCtx, cancel := context.WithTimeout(context.Background(), atRiskChainTimeout)
		scan, err := paymentstatus.DefaultResolver.ScanAtRisk(chainCtx, a.Deps.DB, eclient, head, afterSize, afterID, defaultAtRiskScanBatch, paymentstatus.DefaultMinScanSizeBytes)
		cancel()
		if err != nil {
			return nil, xerrors.Errorf("scan payment status: %w", err)
		}
		if scan.ChainError != "" {
			return nil, xerrors.Errorf("scan payment status: %s", scan.ChainError)
		}
		if len(scan.Found) > 0 {
			ids := make([]int64, len(scan.Found))
			for i, snap := range scan.Found {
				ids[i] = snap.DataSetID
			}
			stats, statsErr := a.loadDataSetStatsByIDs(ctx, ids)
			if statsErr != nil {
				return nil, statsErr
			}
			statsByID := make(map[int64]pdpDataSetStatsRow, len(stats))
			for _, row := range stats {
				statsByID[row.ID] = row
			}
			for _, snap := range scan.Found {
				row, ok := statsByID[snap.DataSetID]
				if !ok {
					continue
				}
				all = append(all, atRiskEntry{stats: row, snap: snap})
			}
		}
		if scan.Complete {
			break
		}
		afterSize = scan.NextAfterSize
		afterID = scan.NextAfterID
	}
	return all, nil
}

func (a *WebRPC) loadDataSetStatsByIDs(ctx context.Context, ids []int64) ([]pdpDataSetStatsRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []pdpDataSetStatsRow
	err := a.Deps.DB.Select(ctx, &rows, `
		SELECT
			ds.id,
			COUNT(DISTINCT dsp.piece_id) FILTER (WHERE dsp.removed IS NOT TRUE) AS object_count,
			COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes,
			MIN(pr.created_at) AS first_upload_at,
			ds.prove_at_epoch,
			ds.challenge_window,
			ds.unrecoverable_proving_failure_epoch
		FROM pdp_data_sets ds
		LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
		LEFT JOIN pdp_piecerefs pr ON pr.id = dsp.pdp_pieceref
		WHERE ds.id = ANY($1)
		GROUP BY ds.id, ds.prove_at_epoch, ds.challenge_window,
		         ds.unrecoverable_proving_failure_epoch
	`, ids)
	if err != nil {
		return nil, xerrors.Errorf("load dataset stats: %w", err)
	}
	return rows, nil
}

func sortAtRiskEntries(entries []atRiskEntry, sortBy string, ascending bool) {
	switch normalizeAtRiskSortBy(sortBy) {
	case "size_bytes":
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].stats.SizeBytes == entries[j].stats.SizeBytes {
				if ascending {
					return entries[i].stats.ID < entries[j].stats.ID
				}
				return entries[i].stats.ID > entries[j].stats.ID
			}
			if ascending {
				return entries[i].stats.SizeBytes < entries[j].stats.SizeBytes
			}
			return entries[i].stats.SizeBytes > entries[j].stats.SizeBytes
		})
	case "id":
		sort.Slice(entries, func(i, j int) bool {
			if ascending {
				return entries[i].stats.ID < entries[j].stats.ID
			}
			return entries[i].stats.ID > entries[j].stats.ID
		})
	default:
		sort.Slice(entries, func(i, j int) bool {
			ai, aj := entries[i].snap.ProjectedDeleteEpoch, entries[j].snap.ProjectedDeleteEpoch
			switch {
			case ai == nil && aj == nil:
				if ascending {
					return entries[i].stats.ID < entries[j].stats.ID
				}
				return entries[i].stats.ID > entries[j].stats.ID
			case ai == nil:
				return false
			case aj == nil:
				return true
			case *ai == *aj:
				if ascending {
					return entries[i].stats.ID < entries[j].stats.ID
				}
				return entries[i].stats.ID > entries[j].stats.ID
			default:
				if ascending {
					return *ai < *aj
				}
				return *ai > *aj
			}
		})
	}
}

func (a *WebRPC) PDPDataSetAtRiskCount(ctx context.Context) (int, error) {
	entries, err := a.resolveAllAtRiskEntries(ctx)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func (a *WebRPC) PDPDataSetAtRiskList(ctx context.Context, limit, offset int, sortBy string, ascending bool) (PDPDataSetAtRiskListResult, error) {
	out := PDPDataSetAtRiskListResult{Items: []PDPDataSetAtRiskSummary{}}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	entries, err := a.resolveAllAtRiskEntries(ctx)
	if err != nil {
		return out, err
	}
	out.Total = len(entries)
	if out.Total == 0 {
		return out, nil
	}

	sortAtRiskEntries(entries, sortBy, ascending)
	if offset >= len(entries) {
		return out, nil
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	page := entries[offset:end]

	head := a.chainHeadEpoch(ctx)
	headTime := a.chainHeadTime(ctx)
	blockDelay := time.Duration(build.BlockDelaySecs) * time.Second

	out.Items = make([]PDPDataSetAtRiskSummary, 0, len(page))
	for _, entry := range page {
		out.Items = append(out.Items, atRiskSummaryFromEntry(entry, head, headTime, blockDelay))
	}
	return out, nil
}

func (a *WebRPC) loadDataSetAtRiskDetail(ctx context.Context, id int64) (*PDPDataSetAtRiskDetail, error) {
	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, xerrors.Errorf("eth client: %w", err)
	}

	head := uint64(a.chainHeadEpoch(ctx))
	chainCtx, cancel := context.WithTimeout(context.Background(), atRiskChainTimeout)
	defer cancel()
	snap, err := paymentstatus.DefaultResolver.Resolve(chainCtx, a.Deps.DB, eclient, id, head)
	if err != nil {
		return nil, xerrors.Errorf("resolve payment status: %w", err)
	}

	out := &PDPDataSetAtRiskDetail{
		Payer:             snap.Payer,
		Status:            snap.Status,
		Reason:            snap.Reason,
		AtRisk:            paymentstatus.IsAtRisk(snap, uint64(head)),
		DeleteDatePending: snap.DeleteDatePending,
	}
	if snap.ProjectedDeleteEpoch != nil {
		out.ProjectedDeleteEpoch = snap.ProjectedDeleteEpoch
	}

	headTime := a.chainHeadTime(ctx)
	blockDelay := time.Duration(build.BlockDelaySecs) * time.Second
	if out.ProjectedDeleteEpoch != nil {
		out.ProjectedDeleteAt = epochToCalendarTime(headTime, a.chainHeadEpoch(ctx), *out.ProjectedDeleteEpoch, blockDelay)
	}
	return out, nil
}

func atRiskSummaryFromEntry(entry atRiskEntry, head int64, headTime time.Time, blockDelay time.Duration) PDPDataSetAtRiskSummary {
	snap := entry.snap
	summary := PDPDataSetAtRiskSummary{
		PDPDataSetSummary: summaryFromStatsRow(entry.stats, head),
		Payer:             snap.Payer,
		Status:            snap.Status,
		Reason:            snap.Reason,
		DeleteDatePending: snap.DeleteDatePending,
		HeadEpoch:         head,
	}
	if snap.ProjectedDeleteEpoch != nil {
		epoch := *snap.ProjectedDeleteEpoch
		summary.ProjectedDeleteEpoch = &epoch
		summary.ProjectedDeleteAt = epochToCalendarTime(headTime, head, epoch, blockDelay)
	}
	return summary
}

func epochToCalendarTime(headTime time.Time, headEpoch, epoch int64, blockDelay time.Duration) *time.Time {
	delta := epoch - headEpoch
	t := headTime.Add(time.Duration(delta) * blockDelay)
	return &t
}

func (a *WebRPC) chainHeadTime(ctx context.Context) time.Time {
	return time.Now()
}
