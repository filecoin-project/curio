package webrpc

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/lib/urlhelper"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

type PDPDataSetListResult struct {
	Items []PDPDataSetSummary `json:"items"`
	Total int                 `json:"total"`
}

type PDPDataSetSummary struct {
	ID                        int64      `json:"id" db:"id"`
	ObjectCount               int64      `json:"objectCount" db:"object_count"`
	SizeBytes                 int64      `json:"sizeBytes" db:"size_bytes"`
	FirstUploadAt             *time.Time `json:"firstUploadAt,omitempty" db:"first_upload_at"`
	ProveAtEpoch              *int64     `json:"proveAtEpoch,omitempty" db:"prove_at_epoch"`
	ChallengeWindow           *int64     `json:"challengeWindow,omitempty" db:"challenge_window"`
	ConsecutiveProveFailures  int        `json:"consecutiveProveFailures" db:"consecutive_prove_failures"`
	UnrecoverableFailureEpoch *int64     `json:"unrecoverableFailureEpoch,omitempty" db:"unrecoverable_proving_failure_epoch"`
	ProvingStatus             string     `json:"provingStatus"`
}

type PDPDataSetInteraction struct {
	Kind      string    `json:"kind"`
	TxHash    string    `json:"txHash,omitempty"`
	TaskID    *int64    `json:"taskId,omitempty"`
	Success   *bool     `json:"success,omitempty"`
	Err       string    `json:"err,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Detail    string    `json:"detail,omitempty"`
}

type PDPDataSetDetail struct {
	ID                        int64      `json:"id"`
	ObjectCount               int64      `json:"objectCount"`
	SizeBytes                 int64      `json:"sizeBytes"`
	FirstUploadAt             *time.Time `json:"firstUploadAt,omitempty"`
	LifespanSeconds           *int64     `json:"lifespanSeconds,omitempty"`
	ProveAtEpoch              *int64     `json:"proveAtEpoch,omitempty"`
	ChallengeWindow           *int64     `json:"challengeWindow,omitempty"`
	ProvingPeriod             *int64     `json:"provingPeriod,omitempty"`
	ConsecutiveProveFailures  int        `json:"consecutiveProveFailures"`
	NextProveAttemptAt        *int64     `json:"nextProveAttemptAt,omitempty"`
	UnrecoverableFailureEpoch *int64     `json:"unrecoverableFailureEpoch,omitempty"`
	InitReady                 bool       `json:"initReady"`
	Service                   string     `json:"service"`
	HeadEpoch                 int64      `json:"headEpoch"`
	ProvingStatus             string     `json:"provingStatus"`
	ExploreURL                string     `json:"exploreUrl,omitempty"`
}

// PDPDataSetPayments is chain-derived wallet/payment state for a dataset.
// Loaded separately so the detail page can render DB fields without waiting on RPC.
type PDPDataSetPayments struct {
	Payer             string `json:"payer"`
	Payee             string `json:"payee"`
	ServiceProvider   string `json:"serviceProvider"`
	PdpRailID         string `json:"pdpRailId"`
	PdpEndEpoch       string `json:"pdpEndEpoch"`
	PaymentsRemaining *int64 `json:"paymentsRemaining,omitempty"`
	AvailableFunds    string `json:"availableFunds"`
	FundedUntilEpoch  string `json:"fundedUntilEpoch"`
	LockupPeriod      string `json:"lockupPeriod"`
	PaymentRate       string `json:"paymentRate"`
	ProvenThisPeriod  *bool  `json:"provenThisPeriod,omitempty"`
	HeadEpoch         int64  `json:"headEpoch"`
	Error             string `json:"error,omitempty"`
}

const pdpDataSetStatsFrom = `
	FROM pdp_data_sets ds
	LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
	LEFT JOIN pdp_piecerefs pr ON pr.id = dsp.pdp_pieceref`

const pdpDataSetStatsSelect = `
	SELECT
		ds.id,
		COUNT(DISTINCT dsp.piece_id) FILTER (WHERE dsp.removed IS NOT TRUE) AS object_count,
		COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes,
		MIN(pr.created_at) AS first_upload_at,
		ds.prove_at_epoch,
		ds.challenge_window,
		ds.consecutive_prove_failures,
		ds.unrecoverable_proving_failure_epoch` + pdpDataSetStatsFrom

const pdpDataSetStatsGroupBy = `
	GROUP BY ds.id, ds.prove_at_epoch, ds.challenge_window,
	         ds.consecutive_prove_failures, ds.unrecoverable_proving_failure_epoch`

// Predefined page queries — harmonyquery only accepts SQL string literals (not fmt-built strings).
const (
	pdpDataSetStatsPageByIDDesc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY ds.id DESC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageByIDAsc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY ds.id ASC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageByObjectsDesc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY object_count DESC, ds.id DESC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageByObjectsAsc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY object_count ASC, ds.id DESC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageBySizeDesc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY size_bytes DESC, ds.id DESC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageBySizeAsc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY size_bytes ASC, ds.id DESC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageByFirstUploadDesc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY first_upload_at DESC NULLS LAST, ds.id DESC
	LIMIT $1 OFFSET $2`
	pdpDataSetStatsPageByFirstUploadAsc = pdpDataSetStatsSelect + pdpDataSetStatsGroupBy + `
	ORDER BY first_upload_at ASC NULLS LAST, ds.id DESC
	LIMIT $1 OFFSET $2`

	pdpDataSetStatsByIDsPageByIDDesc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY ds.id DESC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageByIDAsc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY ds.id ASC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageByObjectsDesc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY object_count DESC, ds.id DESC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageByObjectsAsc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY object_count ASC, ds.id DESC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageBySizeDesc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY size_bytes DESC, ds.id DESC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageBySizeAsc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY size_bytes ASC, ds.id DESC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageByFirstUploadDesc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY first_upload_at DESC NULLS LAST, ds.id DESC
	LIMIT $2 OFFSET $3`
	pdpDataSetStatsByIDsPageByFirstUploadAsc = pdpDataSetStatsSelect + `
	WHERE ds.id = ANY($1)` + pdpDataSetStatsGroupBy + `
	ORDER BY first_upload_at ASC NULLS LAST, ds.id DESC
	LIMIT $2 OFFSET $3`
)

func normalizeDataSetSortBy(sortBy string) string {
	switch strings.ToLower(strings.TrimSpace(sortBy)) {
	case "object_count", "objectcount", "objects":
		return "object_count"
	case "size_bytes", "sizebytes", "size":
		return "size_bytes"
	case "first_upload_at", "firstupload", "first_upload":
		return "first_upload_at"
	default:
		return "id"
	}
}

const pdpDataSetStatsByID = `
	SELECT
		ds.id,
		COUNT(DISTINCT dsp.piece_id) FILTER (WHERE dsp.removed IS NOT TRUE) AS object_count,
		COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes,
		MIN(pr.created_at) AS first_upload_at,
		ds.prove_at_epoch,
		ds.challenge_window,
		ds.consecutive_prove_failures,
		ds.unrecoverable_proving_failure_epoch
	FROM pdp_data_sets ds
	LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
	LEFT JOIN pdp_piecerefs pr ON pr.id = dsp.pdp_pieceref
	WHERE ds.id = $1
	GROUP BY ds.id, ds.prove_at_epoch, ds.challenge_window,
	         ds.consecutive_prove_failures, ds.unrecoverable_proving_failure_epoch`

type pdpDataSetStatsRow struct {
	ID                        int64         `db:"id"`
	ObjectCount               int64         `db:"object_count"`
	SizeBytes                 int64         `db:"size_bytes"`
	FirstUploadAt             sql.NullTime  `db:"first_upload_at"`
	ProveAtEpoch              sql.NullInt64 `db:"prove_at_epoch"`
	ChallengeWindow           sql.NullInt64 `db:"challenge_window"`
	ConsecutiveProveFailures  int           `db:"consecutive_prove_failures"`
	UnrecoverableFailureEpoch sql.NullInt64 `db:"unrecoverable_proving_failure_epoch"`
}

func (a *WebRPC) PDPDataSetList(ctx context.Context, limit, offset int, filter, sortBy string, ascending bool) (PDPDataSetListResult, error) {
	out := PDPDataSetListResult{Items: []PDPDataSetSummary{}}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	sortBy = normalizeDataSetSortBy(sortBy)

	filter = strings.TrimSpace(filter)
	net, _ := a.NetSummary(ctx)
	head := net.Epoch

	if looksLikeEthAddress(filter) {
		addr := common.HexToAddress(filter)
		total, terr := a.clientDataSetTotal(ctx, addr)
		if terr != nil {
			return out, terr
		}
		out.Total = total
		if total == 0 {
			return out, nil
		}
		// Fetch all client dataset IDs so DB sort/pagination can apply across the full set.
		ids, err := a.clientDataSetIDs(ctx, addr, 0, total)
		if err != nil {
			return out, err
		}
		if len(ids) == 0 {
			return out, nil
		}
		rows, err := a.selectDataSetStatsByIDsPage(ctx, ids, limit, offset, sortBy, ascending)
		if err != nil {
			return out, xerrors.Errorf("list datasets by wallet: %w", err)
		}
		out.Items = make([]PDPDataSetSummary, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, summaryFromStatsRow(row, head))
		}
		return out, nil
	}

	if filter != "" {
		id, err := strconv.ParseInt(filter, 10, 64)
		if err != nil {
			return out, xerrors.Errorf("filter must be a dataset ID or 0x wallet address")
		}
		var rows []pdpDataSetStatsRow
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByID, id)
		if err != nil {
			return out, xerrors.Errorf("list dataset by id: %w", err)
		}
		out.Total = len(rows)
		out.Items = make([]PDPDataSetSummary, 0, len(rows))
		for _, row := range rows {
			out.Items = append(out.Items, summaryFromStatsRow(row, head))
		}
		return out, nil
	}

	err := a.Deps.DB.QueryRow(ctx, `SELECT COUNT(*) FROM pdp_data_sets`).Scan(&out.Total)
	if err != nil {
		return out, xerrors.Errorf("count datasets: %w", err)
	}

	rows, err := a.selectDataSetStatsPage(ctx, limit, offset, sortBy, ascending)
	if err != nil {
		return out, xerrors.Errorf("list datasets: %w", err)
	}

	out.Items = make([]PDPDataSetSummary, 0, len(rows))
	for _, row := range rows {
		out.Items = append(out.Items, summaryFromStatsRow(row, head))
	}
	return out, nil
}

func (a *WebRPC) selectDataSetStatsPage(ctx context.Context, limit, offset int, sortBy string, ascending bool) ([]pdpDataSetStatsRow, error) {
	var rows []pdpDataSetStatsRow
	var err error
	switch {
	case sortBy == "object_count" && ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageByObjectsAsc, limit, offset)
	case sortBy == "object_count":
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageByObjectsDesc, limit, offset)
	case sortBy == "size_bytes" && ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageBySizeAsc, limit, offset)
	case sortBy == "size_bytes":
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageBySizeDesc, limit, offset)
	case sortBy == "first_upload_at" && ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageByFirstUploadAsc, limit, offset)
	case sortBy == "first_upload_at":
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageByFirstUploadDesc, limit, offset)
	case ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageByIDAsc, limit, offset)
	default:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPageByIDDesc, limit, offset)
	}
	return rows, err
}

func (a *WebRPC) selectDataSetStatsByIDsPage(ctx context.Context, ids []int64, limit, offset int, sortBy string, ascending bool) ([]pdpDataSetStatsRow, error) {
	var rows []pdpDataSetStatsRow
	var err error
	switch {
	case sortBy == "object_count" && ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageByObjectsAsc, ids, limit, offset)
	case sortBy == "object_count":
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageByObjectsDesc, ids, limit, offset)
	case sortBy == "size_bytes" && ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageBySizeAsc, ids, limit, offset)
	case sortBy == "size_bytes":
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageBySizeDesc, ids, limit, offset)
	case sortBy == "first_upload_at" && ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageByFirstUploadAsc, ids, limit, offset)
	case sortBy == "first_upload_at":
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageByFirstUploadDesc, ids, limit, offset)
	case ascending:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageByIDAsc, ids, limit, offset)
	default:
		err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDsPageByIDDesc, ids, limit, offset)
	}
	return rows, err
}

func (a *WebRPC) PDPDataSetDetail(ctx context.Context, id int64) (*PDPDataSetDetail, error) {
	if id <= 0 {
		return nil, xerrors.Errorf("invalid dataset id")
	}

	var row struct {
		ID                        int64         `db:"id"`
		ObjectCount               int64         `db:"object_count"`
		SizeBytes                 int64         `db:"size_bytes"`
		FirstUploadAt             sql.NullTime  `db:"first_upload_at"`
		ProveAtEpoch              sql.NullInt64 `db:"prove_at_epoch"`
		ChallengeWindow           sql.NullInt64 `db:"challenge_window"`
		ProvingPeriod             sql.NullInt64 `db:"proving_period"`
		ConsecutiveProveFailures  int           `db:"consecutive_prove_failures"`
		NextProveAttemptAt        sql.NullInt64 `db:"next_prove_attempt_at"`
		UnrecoverableFailureEpoch sql.NullInt64 `db:"unrecoverable_proving_failure_epoch"`
		InitReady                 bool          `db:"init_ready"`
		Service                   string        `db:"service"`
	}
	err := a.Deps.DB.QueryRow(ctx, `
		SELECT
			ds.id,
			COUNT(DISTINCT dsp.piece_id) FILTER (WHERE dsp.removed IS NOT TRUE) AS object_count,
			COALESCE(SUM(CASE WHEN dsp.removed IS NOT TRUE THEN dsp.sub_piece_size ELSE 0 END), 0) AS size_bytes,
			MIN(pr.created_at) AS first_upload_at,
			ds.prove_at_epoch,
			ds.challenge_window,
			ds.proving_period,
			ds.consecutive_prove_failures,
			ds.next_prove_attempt_at,
			ds.unrecoverable_proving_failure_epoch,
			ds.init_ready,
			ds.service
		FROM pdp_data_sets ds
		LEFT JOIN pdp_data_set_pieces dsp ON dsp.data_set = ds.id
		LEFT JOIN pdp_piecerefs pr ON pr.id = dsp.pdp_pieceref
		WHERE ds.id = $1
		GROUP BY ds.id, ds.prove_at_epoch, ds.challenge_window, ds.proving_period,
		         ds.consecutive_prove_failures, ds.next_prove_attempt_at,
		         ds.unrecoverable_proving_failure_epoch, ds.init_ready, ds.service`, id).Scan(
		&row.ID, &row.ObjectCount, &row.SizeBytes, &row.FirstUploadAt,
		&row.ProveAtEpoch, &row.ChallengeWindow, &row.ProvingPeriod,
		&row.ConsecutiveProveFailures, &row.NextProveAttemptAt, &row.UnrecoverableFailureEpoch,
		&row.InitReady, &row.Service)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return nil, xerrors.Errorf("dataset %d not found", id)
		}
		return nil, xerrors.Errorf("dataset detail: %w", err)
	}

	detail := &PDPDataSetDetail{
		ID:                        row.ID,
		ObjectCount:               row.ObjectCount,
		SizeBytes:                 row.SizeBytes,
		ConsecutiveProveFailures:  row.ConsecutiveProveFailures,
		InitReady:                 row.InitReady,
		Service:                   row.Service,
		ProveAtEpoch:              nullInt64Ptr(row.ProveAtEpoch),
		ChallengeWindow:           nullInt64Ptr(row.ChallengeWindow),
		ProvingPeriod:             nullInt64Ptr(row.ProvingPeriod),
		NextProveAttemptAt:        nullInt64Ptr(row.NextProveAttemptAt),
		UnrecoverableFailureEpoch: nullInt64Ptr(row.UnrecoverableFailureEpoch),
	}
	if row.FirstUploadAt.Valid {
		t := row.FirstUploadAt.Time
		detail.FirstUploadAt = &t
		secs := int64(time.Since(t).Seconds())
		if secs < 0 {
			secs = 0
		}
		detail.LifespanSeconds = &secs
	}

	net, netErr := a.NetSummary(ctx)
	if netErr == nil {
		detail.HeadEpoch = net.Epoch
	}
	detail.ProvingStatus = provingStatusLabel(row.ProveAtEpoch, row.ChallengeWindow, row.ConsecutiveProveFailures, row.UnrecoverableFailureEpoch, detail.HeadEpoch)

	if base, err := urlhelper.GetExternalURL(&a.Deps.Cfg.HTTP); err == nil && base != nil && strings.TrimSpace(base.Host) != "" {
		detail.ExploreURL = strings.TrimRight(base.String(), "/") + "/explore/data-sets/" + strconv.FormatInt(id, 10)
	}

	return detail, nil
}

func (a *WebRPC) PDPDataSetPayments(ctx context.Context, id int64) (*PDPDataSetPayments, error) {
	if id <= 0 {
		return nil, xerrors.Errorf("invalid dataset id")
	}
	out := &PDPDataSetPayments{
		AvailableFunds:   "—",
		FundedUntilEpoch: "—",
		LockupPeriod:     "—",
		PaymentRate:      "—",
	}
	if net, err := a.NetSummary(ctx); err == nil {
		out.HeadEpoch = net.Epoch
	}

	// Bound chain RPCs so a stuck eth client cannot hang the UI forever.
	chainCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	a.enrichDataSetFWSS(chainCtx, id, out)
	if out.Payer == "" && out.PdpRailID == "" {
		out.Error = "chain payment data unavailable"
	}
	return out, nil
}

func (a *WebRPC) PDPDataSetInteractions(ctx context.Context, id int64) ([]PDPDataSetInteraction, error) {
	if id <= 0 {
		return nil, xerrors.Errorf("invalid dataset id")
	}
	return a.dataSetInteractions(ctx, id)
}

func (a *WebRPC) enrichDataSetFWSS(ctx context.Context, id int64, out *PDPDataSetPayments) {
	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return
	}
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, eclient)
	if err != nil {
		log.Warnw("PDPDataSetPayments: resolve FWSS view", "error", err)
		return
	}
	fwssView, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, eclient)
	if err != nil {
		return
	}

	info, err := fwssView.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(id))
	if err != nil {
		log.Warnw("PDPDataSetPayments: GetDataSet", "id", id, "error", err)
		return
	}
	out.Payer = info.Payer.Hex()
	out.Payee = info.Payee.Hex()
	out.ServiceProvider = info.ServiceProvider.Hex()
	if info.PdpRailId != nil {
		out.PdpRailID = info.PdpRailId.String()
	}
	if info.PdpEndEpoch != nil {
		out.PdpEndEpoch = info.PdpEndEpoch.String()
	}

	if proven, perr := fwssView.ProvenThisPeriod(contract.EthCallOpts(ctx), big.NewInt(id)); perr == nil {
		out.ProvenThisPeriod = &proven
	}

	if info.PdpRailId == nil || info.PdpRailId.Sign() == 0 {
		return
	}

	paymentAddr, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return
	}
	payments, err := filecoinpayment.NewPayments(paymentAddr, eclient)
	if err != nil {
		return
	}

	rail, err := payments.GetRail(contract.EthCallOpts(ctx), info.PdpRailId)
	if err != nil {
		log.Warnw("PDPDataSetPayments: GetRail", "rail", info.PdpRailId, "error", err)
		return
	}
	if rail.LockupPeriod != nil {
		out.LockupPeriod = rail.LockupPeriod.String()
	}
	if rail.PaymentRate != nil {
		out.PaymentRate = rail.PaymentRate.String()
	}

	acct, err := payments.GetAccountInfoIfSettled(contract.EthCallOpts(ctx), rail.Token, info.Payer)
	if err != nil {
		log.Warnw("PDPDataSetPayments: GetAccountInfoIfSettled", "error", err)
		return
	}
	if acct.AvailableFunds != nil {
		out.AvailableFunds = formatUsdfc(acct.AvailableFunds)
	}
	if acct.FundedUntilEpoch != nil {
		out.FundedUntilEpoch = acct.FundedUntilEpoch.String()
	}

	if acct.FundedUntilEpoch != nil && rail.LockupPeriod != nil && rail.LockupPeriod.Sign() > 0 {
		head := big.NewInt(out.HeadEpoch)
		remainingEpochs := new(big.Int).Sub(acct.FundedUntilEpoch, head)
		if remainingEpochs.Sign() < 0 {
			remainingEpochs = big.NewInt(0)
		}
		paymentsLeft := new(big.Int).Div(remainingEpochs, rail.LockupPeriod)
		v := paymentsLeft.Int64()
		out.PaymentsRemaining = &v
	}
}

func (a *WebRPC) dataSetInteractions(ctx context.Context, id int64) ([]PDPDataSetInteraction, error) {
	type txRow struct {
		TxHash     string         `db:"tx_hash"`
		SendReason sql.NullString `db:"send_reason"`
		SendTime   time.Time      `db:"send_time"`
		Success    sql.NullBool   `db:"send_success"`
	}

	// Cap piece-add hashes — joining every historical add against message_sends_eth
	// with LOWER(TRIM(...)) was scanning huge sets (26s+). Prefer create/challenge
	// plus a small recent sample.
	// Restrict to send_success=TRUE so the planner can use the existing partial
	// idx_message_sends_eth_signed_hash_norm (reorg-check index). Dataset-linked
	// hashes are from broadcast sends, so this matches what we want to show.
	var txs []txRow
	err := a.Deps.DB.Select(ctx, &txs, `
		WITH hashes AS (
			SELECT LOWER(TRIM(BOTH FROM create_message_hash)) AS h
			FROM pdp_data_sets
			WHERE id = $1 AND create_message_hash IS NOT NULL AND create_message_hash != ''
			UNION
			SELECT LOWER(TRIM(BOTH FROM challenge_request_msg_hash))
			FROM pdp_data_sets
			WHERE id = $1 AND challenge_request_msg_hash IS NOT NULL AND challenge_request_msg_hash != ''
			UNION
			SELECT LOWER(TRIM(BOTH FROM add_message_hash))
			FROM (
				SELECT add_message_hash
				FROM pdp_data_set_pieces
				WHERE data_set = $1 AND add_message_hash IS NOT NULL
				ORDER BY piece_id DESC
				LIMIT 15
			) recent_pieces
			UNION
			SELECT LOWER(TRIM(BOTH FROM add_message_hash))
			FROM (
				SELECT DISTINCT add_message_hash
				FROM pdp_data_set_piece_adds
				WHERE data_set = $1 AND add_message_hash IS NOT NULL
				LIMIT 15
			) recent_adds
		)
		SELECT mse.signed_hash AS tx_hash,
		       mse.send_reason,
		       mse.send_time,
		       mse.send_success
		FROM hashes h
		JOIN message_sends_eth mse
		  ON mse.send_success = TRUE
		 AND mse.signed_hash IS NOT NULL
		 AND LOWER(TRIM(BOTH FROM mse.signed_hash)) = h.h
		ORDER BY mse.send_time DESC
		LIMIT 10`, id)
	if err != nil {
		return nil, xerrors.Errorf("interaction txs: %w", err)
	}

	out := make([]PDPDataSetInteraction, 0, 15)
	for _, tx := range txs {
		kind := "eth-tx"
		if tx.SendReason.Valid && tx.SendReason.String != "" {
			kind = tx.SendReason.String
		}
		inter := PDPDataSetInteraction{
			Kind:      kind,
			TxHash:    tx.TxHash,
			Timestamp: tx.SendTime,
		}
		if tx.Success.Valid {
			v := tx.Success.Bool
			inter.Success = &v
		}
		out = append(out, inter)
	}

	type proveRow struct {
		TaskID  int64     `db:"task_id"`
		Result  bool      `db:"result"`
		Err     string    `db:"err"`
		WorkEnd time.Time `db:"work_end"`
	}
	var proves []proveRow
	// pdp_prove_tasks only exists while the prove task is in-flight (CASCADE delete on
	// harmony_task completion). Do NOT join it to harmony_task_history in one statement:
	// ORDER BY h.work_end LIMIT N makes the planner scan all of history probing for
	// matches (minutes when the dataset has no in-flight prove rows).
	var proveTaskIDs []int64
	err = a.Deps.DB.Select(ctx, &proveTaskIDs, `
		SELECT task_id FROM pdp_prove_tasks WHERE data_set = $1`, id)
	if err != nil {
		return nil, xerrors.Errorf("prove task ids: %w", err)
	}
	if len(proveTaskIDs) > 0 {
		err = a.Deps.DB.Select(ctx, &proves, `
			SELECT h.task_id,
			       (h.result AND (h.err IS NULL OR h.err = '')) AS result,
			       COALESCE(h.err, '') AS err,
			       h.work_end
			FROM harmony_task_history h
			WHERE h.task_id = ANY($1)
			ORDER BY h.work_end DESC
			LIMIT 10`, proveTaskIDs)
		if err != nil {
			return nil, xerrors.Errorf("prove interactions: %w", err)
		}
	}
	if len(proves) == 0 {
		// Completed proves: recover dataset from err text ("dataset <id>") over a
		// name+time window that can use harmony_task_history work_end/name indexes.
		// Filter in Go — ILIKE '%dataset N%' can't use those indexes well either.
		var recent []proveRow
		err = a.Deps.DB.Select(ctx, &recent, `
			SELECT h.task_id,
			       (h.result AND (h.err IS NULL OR h.err = '')) AS result,
			       COALESCE(h.err, '') AS err,
			       h.work_end
			FROM harmony_task_history h
			WHERE h.name IN ('PDPv0_Prove', 'PDPProve')
			  AND h.work_end > CURRENT_TIMESTAMP - INTERVAL '14 days'
			  AND h.err IS NOT NULL AND h.err <> ''
			ORDER BY h.work_end DESC
			LIMIT 500`)
		if err != nil {
			return nil, xerrors.Errorf("prove history interactions: %w", err)
		}
		for _, p := range recent {
			dsID, ok := parseDataSetIDFromErr(p.Err)
			if !ok || dsID != id {
				continue
			}
			proves = append(proves, p)
			if len(proves) >= 10 {
				break
			}
		}
	}
	for _, p := range proves {
		succ := p.Result
		tid := p.TaskID
		out = append(out, PDPDataSetInteraction{
			Kind:      "prove",
			TaskID:    &tid,
			Success:   &succ,
			Err:       cleanHistoryErr(p.Err),
			Timestamp: p.WorkEnd,
		})
	}

	// Sort newest first and cap.
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out, nil
}

func (a *WebRPC) fwssView(ctx context.Context) (*FWSS.FilecoinWarmStorageServiceStateView, error) {
	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return nil, err
	}
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, eclient)
	if err != nil {
		return nil, err
	}
	return FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, eclient)
}

func (a *WebRPC) clientDataSetIDs(ctx context.Context, client common.Address, offset, limit int) ([]int64, error) {
	view, err := a.fwssView(ctx)
	if err != nil {
		return nil, xerrors.Errorf("FWSS view: %w", err)
	}
	infos, err := view.GetClientDataSets(contract.EthCallOpts(ctx), client, big.NewInt(int64(offset)), big.NewInt(int64(limit)))
	if err != nil {
		return nil, xerrors.Errorf("GetClientDataSets: %w", err)
	}
	ids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.DataSetId != nil {
			ids = append(ids, info.DataSetId.Int64())
		}
	}
	return ids, nil
}

func (a *WebRPC) clientDataSetTotal(ctx context.Context, client common.Address) (int, error) {
	view, err := a.fwssView(ctx)
	if err != nil {
		return 0, err
	}
	n, err := view.GetClientDataSetsLength(contract.EthCallOpts(ctx), client)
	if err != nil {
		return 0, err
	}
	if n == nil {
		return 0, nil
	}
	return int(n.Int64()), nil
}

func summaryFromStatsRow(row pdpDataSetStatsRow, head int64) PDPDataSetSummary {
	s := PDPDataSetSummary{
		ID:                        row.ID,
		ObjectCount:               row.ObjectCount,
		SizeBytes:                 row.SizeBytes,
		ConsecutiveProveFailures:  row.ConsecutiveProveFailures,
		ProveAtEpoch:              nullInt64Ptr(row.ProveAtEpoch),
		ChallengeWindow:           nullInt64Ptr(row.ChallengeWindow),
		UnrecoverableFailureEpoch: nullInt64Ptr(row.UnrecoverableFailureEpoch),
	}
	if row.FirstUploadAt.Valid {
		t := row.FirstUploadAt.Time
		s.FirstUploadAt = &t
	}
	s.ProvingStatus = provingStatusLabel(row.ProveAtEpoch, row.ChallengeWindow, row.ConsecutiveProveFailures, row.UnrecoverableFailureEpoch, head)
	return s
}

func provingStatusLabel(proveAt, window sql.NullInt64, consecutive int, unrecoverable sql.NullInt64, head int64) string {
	if unrecoverable.Valid {
		return "unrecoverable"
	}
	if consecutive > 0 {
		return "failing"
	}
	if !proveAt.Valid {
		return "uninit"
	}
	start := proveAt.Int64
	w := int64(0)
	if window.Valid {
		w = window.Int64
	}
	deadline := start + w
	if head < start {
		return "scheduled"
	}
	if head <= deadline {
		return "in-window"
	}
	return "overdue"
}

func looksLikeEthAddress(s string) bool {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	return common.IsHexAddress(s)
}
