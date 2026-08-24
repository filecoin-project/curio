package paymentstatus

import (
	"context"
	"math/big"
	"sync"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

const (
	defaultCacheTTL         = 2 * time.Minute
	defaultScanBatch        = 20
	scanParallelism         = 10
	DefaultMinScanSizeBytes = 100 * 1024 // 100 KiB — skip smaller datasets in grace scans
)

// Resolver derives payment/grace status from FWSS and FilecoinPay on chain,
// overlaid with local delete-pipeline rows from pdp_delete_data_set.
type Resolver struct {
	cache     *snapshotCache
	clientsMu sync.Mutex
	clients   *chainClients
	clientsAt time.Time
}

// DefaultResolver is shared by WebRPC at-risk views.
var DefaultResolver = NewResolver(defaultCacheTTL)

func NewResolver(ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &Resolver{
		cache: newSnapshotCache(ttl),
	}
}

func IsAtRisk(s Snapshot, currentEpoch uint64) bool {
	switch s.Status {
	case StatusGrace, StatusTerminating, StatusPendingDelete:
	default:
		return false
	}
	// Past projected deletion — no longer actionable for the SP grace list.
	if s.ProjectedDeleteEpoch != nil && *s.ProjectedDeleteEpoch > 0 && uint64(*s.ProjectedDeleteEpoch) < currentEpoch {
		return false
	}
	return true
}

type snapshotCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[int64]cachedSnapshot
}

type cachedSnapshot struct {
	snap Snapshot
	at   time.Time
}

func newSnapshotCache(ttl time.Duration) *snapshotCache {
	return &snapshotCache{
		ttl:     ttl,
		entries: make(map[int64]cachedSnapshot),
	}
}

func (c *snapshotCache) get(id int64) (Snapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[id]
	if !ok || time.Since(entry.at) > c.ttl {
		return Snapshot{}, false
	}
	return entry.snap, true
}

func (c *snapshotCache) put(id int64, snap Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[id] = cachedSnapshot{snap: snap, at: time.Now()}
}

type overlayBundle struct {
	delete DeletePipelineOverlay
	prove  ProvingOverlay
}

func (r *Resolver) Resolve(
	ctx context.Context,
	db *harmonydb.DB,
	ethClient ethchain.EthClient,
	dataSetID int64,
	currentEpoch uint64,
) (Snapshot, error) {
	if snap, ok := r.cache.get(dataSetID); ok {
		return snap, nil
	}

	overlays, err := loadOverlayBundle(ctx, db, dataSetID)
	if err != nil {
		return Snapshot{}, err
	}

	snap, err := r.resolveFromChain(ctx, ethClient, dataSetID, currentEpoch, overlays)
	if err != nil {
		return Snapshot{}, err
	}
	r.cache.put(dataSetID, snap)
	return snap, nil
}

// ScanAtRiskResult is one page of a largest-first walk over local datasets.
type ScanAtRiskResult struct {
	Found         []Snapshot
	Scanned       int
	DatasetTotal  int
	Complete      bool
	NextAfterSize int64
	NextAfterID   int64
	ChainError    string
}

type dataSetSizeCursor struct {
	ID        int64 `db:"id"`
	SizeBytes int64 `db:"size_bytes"`
}

// ScanAtRisk resolves payment status for up to maxScan local datasets, walking
// from largest size downward. Each batch performs a small number of FWSS/Pay
// chain reads so results can stream without enumerating all payee rails first.
// Pass afterSize=0 and afterID=0 to start. minSizeBytes skips datasets smaller than
// that threshold (0 disables the filter).
func (r *Resolver) ScanAtRisk(
	ctx context.Context,
	db *harmonydb.DB,
	ethClient ethchain.EthClient,
	currentEpoch uint64,
	afterSize int64,
	afterID int64,
	maxScan int,
	minSizeBytes int64,
) (ScanAtRiskResult, error) {
	out := ScanAtRiskResult{}
	if maxScan <= 0 {
		maxScan = defaultScanBatch
	}

	var datasetTotal int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM pdp_data_sets`).Scan(&datasetTotal); err != nil {
		return out, xerrors.Errorf("count local datasets: %w", err)
	}
	out.DatasetTotal = datasetTotal
	if datasetTotal == 0 {
		out.Complete = true
		return out, nil
	}

	rows, err := listDataSetsBySizePage(ctx, db, afterSize, afterID, maxScan, minSizeBytes)
	if err != nil {
		return out, err
	}
	if len(rows) == 0 {
		out.Complete = true
		return out, nil
	}

	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	overlays, err := loadOverlayBundles(ctx, db, ids)
	if err != nil {
		return out, err
	}

	clients, err := r.chainClients(ctx, ethClient)
	if err != nil {
		out.ChainError = err.Error()
		return out, nil
	}

	found := make([]Snapshot, 0, len(rows))
	var foundMu sync.Mutex
	sem := make(chan struct{}, scanParallelism)
	var wg sync.WaitGroup

	for _, row := range rows {
		id := row.ID
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			var snap Snapshot
			if cached, ok := r.cache.get(id); ok {
				snap = cached
			} else {
				bundle := overlays[id]
				resolved, resolveErr := resolveFromChainWithClients(ctx, clients, id, currentEpoch, bundle)
				if resolveErr != nil {
					return
				}
				snap = resolved
				r.cache.put(id, snap)
			}
			if !IsAtRisk(snap, currentEpoch) {
				return
			}
			foundMu.Lock()
			found = append(found, snap)
			foundMu.Unlock()
		}(id)
	}
	wg.Wait()
	out.Scanned = len(rows)

	last := rows[len(rows)-1]
	out.Found = found
	out.NextAfterSize = last.SizeBytes
	out.NextAfterID = last.ID
	out.Complete = len(rows) < maxScan
	return out, nil
}

func listDataSetsBySizePage(ctx context.Context, db *harmonydb.DB, afterSize, afterID int64, limit int, minSizeBytes int64) ([]dataSetSizeCursor, error) {
	var rows []dataSetSizeCursor
	if afterSize == 0 && afterID == 0 {
		if minSizeBytes > 0 {
			err := db.Select(ctx, &rows, `
				SELECT
					ds.id,
					COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes
				FROM pdp_data_sets ds
				LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
				GROUP BY ds.id
				HAVING COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) >= $2
				ORDER BY size_bytes DESC, ds.id DESC
				LIMIT $1
			`, limit, minSizeBytes)
			if err != nil {
				return nil, xerrors.Errorf("list datasets by size: %w", err)
			}
			return rows, nil
		}
		err := db.Select(ctx, &rows, `
			SELECT
				ds.id,
				COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes
			FROM pdp_data_sets ds
			LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
			GROUP BY ds.id
			ORDER BY size_bytes DESC, ds.id DESC
			LIMIT $1
		`, limit)
		if err != nil {
			return nil, xerrors.Errorf("list datasets by size: %w", err)
		}
		return rows, nil
	}

	if minSizeBytes > 0 {
		err := db.Select(ctx, &rows, `
			WITH sized AS (
				SELECT
					ds.id,
					COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes
				FROM pdp_data_sets ds
				LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
				GROUP BY ds.id
				HAVING COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) >= $4
			)
			SELECT id, size_bytes
			FROM sized
			WHERE size_bytes < $1 OR (size_bytes = $1 AND id < $2)
			ORDER BY size_bytes DESC, id DESC
			LIMIT $3
		`, afterSize, afterID, limit, minSizeBytes)
		if err != nil {
			return nil, xerrors.Errorf("list datasets by size page: %w", err)
		}
		return rows, nil
	}

	err := db.Select(ctx, &rows, `
		WITH sized AS (
			SELECT
				ds.id,
				COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes
			FROM pdp_data_sets ds
			LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
			GROUP BY ds.id
		)
		SELECT id, size_bytes
		FROM sized
		WHERE size_bytes < $1 OR (size_bytes = $1 AND id < $2)
		ORDER BY size_bytes DESC, id DESC
		LIMIT $3
	`, afterSize, afterID, limit)
	if err != nil {
		return nil, xerrors.Errorf("list datasets by size page: %w", err)
	}
	return rows, nil
}

type chainClients struct {
	fwssView *FWSS.FilecoinWarmStorageServiceStateView
	payments *filecoinpayment.Payments
	local    *chainClients // local eth fallback when primary uses Glif
}

func (r *Resolver) chainClients(ctx context.Context, ethClient ethchain.EthClient) (*chainClients, error) {
	r.clientsMu.Lock()
	defer r.clientsMu.Unlock()
	if r.clients != nil && time.Since(r.clientsAt) <= r.cache.ttl {
		return r.clients, nil
	}

	clients, err := r.newChainClientsWithFallback(ctx, ethClient)
	if err != nil {
		return nil, err
	}
	r.clients = clients
	r.clientsAt = time.Now()
	return clients, nil
}

func (r *Resolver) newChainClientsWithFallback(ctx context.Context, localEth ethchain.EthClient) (*chainClients, error) {
	glifEth, usingGlif := preferReadEthClient(ctx)
	if !usingGlif {
		return newChainClients(ctx, localEth)
	}

	primary, err := newChainClients(ctx, glifEth)
	if err != nil {
		markGlifUnhealthy()
		log.Debugw("Glif chain client init failed, using local eth client", "error", err)
		return newChainClients(ctx, localEth)
	}

	local, localErr := newChainClients(ctx, localEth)
	if localErr != nil {
		log.Warnw("local eth fallback client init failed; Glif-only for payment reads", "error", localErr)
		return primary, nil
	}

	return &chainClients{
		fwssView: primary.fwssView,
		payments: primary.payments,
		local:    local,
	}, nil
}

func (r *Resolver) resolveFromChain(
	ctx context.Context,
	ethClient ethchain.EthClient,
	dataSetID int64,
	currentEpoch uint64,
	overlays overlayBundle,
) (Snapshot, error) {
	clients, err := r.chainClients(ctx, ethClient)
	if err != nil {
		return Snapshot{}, err
	}
	return resolveFromChainWithClients(ctx, clients, dataSetID, currentEpoch, overlays)
}

func newChainClients(ctx context.Context, ethClient ethchain.EthClient) (*chainClients, error) {
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("resolve FWSS view: %w", err)
	}
	fwssView, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("fwss view client: %w", err)
	}
	paymentAddr, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return nil, err
	}
	payments, err := filecoinpayment.NewPayments(paymentAddr, ethClient)
	if err != nil {
		return nil, err
	}
	return &chainClients{fwssView: fwssView, payments: payments}, nil
}

func resolveFromChainWithClients(
	ctx context.Context,
	clients *chainClients,
	dataSetID int64,
	currentEpoch uint64,
	overlays overlayBundle,
) (Snapshot, error) {
	snap, err := resolveOnceWithClients(ctx, clients, dataSetID, currentEpoch, overlays)
	if err == nil || clients.local == nil {
		return snap, err
	}

	log.Debugw("Glif read failed, retrying on local eth client", "dataSetId", dataSetID, "error", err)
	markGlifUnhealthy()
	return resolveOnceWithClients(ctx, clients.local, dataSetID, currentEpoch, overlays)
}

func resolveOnceWithClients(
	ctx context.Context,
	clients *chainClients,
	dataSetID int64,
	currentEpoch uint64,
	overlays overlayBundle,
) (Snapshot, error) {
	info, err := clients.fwssView.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(dataSetID))
	if err != nil {
		return Snapshot{}, xerrors.Errorf("GetDataSet %d: %w", dataSetID, err)
	}

	deleteOverlay := overlays.delete
	if info.PdpEndEpoch != nil && info.PdpEndEpoch.Sign() > 0 {
		epoch := info.PdpEndEpoch.Int64()
		deleteOverlay.ServiceTerminationEpoch = &epoch
		if !deleteOverlay.InPipeline {
			deleteOverlay.InPipeline = true
		}
	}

	payer := info.Payer.Hex()
	view := filecoinpayment.PaymentsRailView{}

	if info.PdpRailId != nil && info.PdpRailId.Sign() > 0 {
		rail, railErr := clients.payments.GetRail(contract.EthCallOpts(ctx), info.PdpRailId)
		if railErr != nil {
			if !filecoinpayment.IsRailInactiveOrSettledError(railErr) {
				return Snapshot{}, xerrors.Errorf("GetRail %s: %w", info.PdpRailId, railErr)
			}
		} else {
			view = rail
		}
	}

	snap := Classify(currentEpoch, view, payer, deleteOverlay, overlays.prove)
	snap.DataSetID = dataSetID
	if info.PdpRailId != nil && info.PdpRailId.Sign() > 0 {
		railID := info.PdpRailId.Int64()
		snap.RailID = &railID
	}
	return snap, nil
}

type overlayRow struct {
	ID                        int64  `db:"id"`
	InDeletePipeline          bool   `db:"in_delete_pipeline"`
	ClientRequested           bool   `db:"client_requested_termination"`
	AfterTerminateService     bool   `db:"after_terminate_service"`
	ServiceTerminationEpoch   *int64 `db:"service_termination_epoch"`
	DeletionAllowed           bool   `db:"deletion_allowed"`
	Terminated                bool   `db:"terminated"`
	UnrecoverableFailureEpoch *int64 `db:"unrecoverable_proving_failure_epoch"`
}

func loadOverlayBundle(ctx context.Context, db *harmonydb.DB, dataSetID int64) (overlayBundle, error) {
	bundles, err := loadOverlayBundles(ctx, db, []int64{dataSetID})
	if err != nil {
		return overlayBundle{}, err
	}
	if b, ok := bundles[dataSetID]; ok {
		return b, nil
	}
	return overlayBundle{}, nil
}

func loadOverlayBundles(ctx context.Context, db *harmonydb.DB, ids []int64) (map[int64]overlayBundle, error) {
	out := make(map[int64]overlayBundle, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	var rows []overlayRow
	err := db.Select(ctx, &rows, `
		SELECT
			ds.id,
			(dd.id IS NOT NULL AND COALESCE(dd.terminated, FALSE) = FALSE) AS in_delete_pipeline,
			COALESCE(dd.client_requested_termination, FALSE) AS client_requested_termination,
			COALESCE(dd.after_terminate_service, FALSE) AS after_terminate_service,
			dd.service_termination_epoch,
			COALESCE(dd.deletion_allowed, FALSE) AS deletion_allowed,
			COALESCE(dd.terminated, FALSE) AS terminated,
			ds.unrecoverable_proving_failure_epoch
		FROM pdp_data_sets ds
		LEFT JOIN pdp_delete_data_set dd ON dd.id = ds.id
		WHERE ds.id = ANY($1)
	`, ids)
	if err != nil {
		return nil, xerrors.Errorf("load delete overlays: %w", err)
	}

	for _, row := range rows {
		out[row.ID] = overlayBundle{
			delete: DeletePipelineOverlay{
				InPipeline:              row.InDeletePipeline,
				ClientRequested:         row.ClientRequested,
				AfterTerminateService:   row.AfterTerminateService,
				ServiceTerminationEpoch: row.ServiceTerminationEpoch,
				DeletionAllowed:         row.DeletionAllowed,
				Terminated:              row.Terminated,
			},
			prove: ProvingOverlay{UnrecoverableFailureEpoch: row.UnrecoverableFailureEpoch},
		}
	}
	return out, nil
}
