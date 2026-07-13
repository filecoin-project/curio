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

	// FWSS / payment
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

	Interactions []PDPDataSetInteraction `json:"interactions"`
}

const pdpDataSetStatsByIDs = `
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
	WHERE ds.id = ANY($1)
	GROUP BY ds.id, ds.prove_at_epoch, ds.challenge_window,
	         ds.consecutive_prove_failures, ds.unrecoverable_proving_failure_epoch`

const pdpDataSetStatsPage = `
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
	GROUP BY ds.id, ds.prove_at_epoch, ds.challenge_window,
	         ds.consecutive_prove_failures, ds.unrecoverable_proving_failure_epoch
	ORDER BY ds.id DESC
	LIMIT $1 OFFSET $2`

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

func (a *WebRPC) PDPDataSetList(ctx context.Context, limit, offset int, filter string) (PDPDataSetListResult, error) {
	out := PDPDataSetListResult{Items: []PDPDataSetSummary{}}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	filter = strings.TrimSpace(filter)
	net, _ := a.NetSummary(ctx)
	head := net.Epoch

	if looksLikeEthAddress(filter) {
		ids, err := a.clientDataSetIDs(ctx, common.HexToAddress(filter), offset, limit)
		if err != nil {
			return out, err
		}
		if len(ids) == 0 {
			return out, nil
		}
		items, err := a.loadDataSetSummariesByIDs(ctx, ids, head)
		if err != nil {
			return out, err
		}
		out.Items = items
		if total, terr := a.clientDataSetTotal(ctx, common.HexToAddress(filter)); terr == nil {
			out.Total = total
		} else {
			out.Total = len(items)
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

	var rows []pdpDataSetStatsRow
	err = a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsPage, limit, offset)
	if err != nil {
		return out, xerrors.Errorf("list datasets: %w", err)
	}

	out.Items = make([]PDPDataSetSummary, 0, len(rows))
	for _, row := range rows {
		out.Items = append(out.Items, summaryFromStatsRow(row, head))
	}
	return out, nil
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
		AvailableFunds:            "—",
		FundedUntilEpoch:          "—",
		LockupPeriod:              "—",
		PaymentRate:               "—",
		Interactions:              []PDPDataSetInteraction{},
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

	a.enrichDataSetFWSS(ctx, detail)
	interactions, ierr := a.dataSetInteractions(ctx, id)
	if ierr != nil {
		log.Warnw("PDPDataSetDetail: interactions failed", "id", id, "error", ierr)
	} else {
		detail.Interactions = interactions
	}

	return detail, nil
}

func (a *WebRPC) enrichDataSetFWSS(ctx context.Context, detail *PDPDataSetDetail) {
	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return
	}
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, eclient)
	if err != nil {
		log.Warnw("PDPDataSetDetail: resolve FWSS view", "error", err)
		return
	}
	fwssView, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, eclient)
	if err != nil {
		return
	}

	info, err := fwssView.GetDataSet(contract.EthCallOpts(ctx), big.NewInt(detail.ID))
	if err != nil {
		log.Warnw("PDPDataSetDetail: GetDataSet", "id", detail.ID, "error", err)
		return
	}
	detail.Payer = info.Payer.Hex()
	detail.Payee = info.Payee.Hex()
	detail.ServiceProvider = info.ServiceProvider.Hex()
	if info.PdpRailId != nil {
		detail.PdpRailID = info.PdpRailId.String()
	}
	if info.PdpEndEpoch != nil {
		detail.PdpEndEpoch = info.PdpEndEpoch.String()
	}

	if proven, perr := fwssView.ProvenThisPeriod(contract.EthCallOpts(ctx), big.NewInt(detail.ID)); perr == nil {
		detail.ProvenThisPeriod = &proven
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
		log.Warnw("PDPDataSetDetail: GetRail", "rail", info.PdpRailId, "error", err)
		return
	}
	if rail.LockupPeriod != nil {
		detail.LockupPeriod = rail.LockupPeriod.String()
	}
	if rail.PaymentRate != nil {
		detail.PaymentRate = rail.PaymentRate.String()
	}

	acct, err := payments.GetAccountInfoIfSettled(contract.EthCallOpts(ctx), rail.Token, info.Payer)
	if err != nil {
		log.Warnw("PDPDataSetDetail: GetAccountInfoIfSettled", "error", err)
		return
	}
	if acct.AvailableFunds != nil {
		detail.AvailableFunds = formatUsdfc(acct.AvailableFunds)
	}
	if acct.FundedUntilEpoch != nil {
		detail.FundedUntilEpoch = acct.FundedUntilEpoch.String()
	}

	if acct.FundedUntilEpoch != nil && rail.LockupPeriod != nil && rail.LockupPeriod.Sign() > 0 {
		head := big.NewInt(detail.HeadEpoch)
		remainingEpochs := new(big.Int).Sub(acct.FundedUntilEpoch, head)
		if remainingEpochs.Sign() < 0 {
			remainingEpochs = big.NewInt(0)
		}
		paymentsLeft := new(big.Int).Div(remainingEpochs, rail.LockupPeriod)
		v := paymentsLeft.Int64()
		detail.PaymentsRemaining = &v
	}
}

func (a *WebRPC) dataSetInteractions(ctx context.Context, id int64) ([]PDPDataSetInteraction, error) {
	type txRow struct {
		TxHash     string         `db:"tx_hash"`
		SendReason sql.NullString `db:"send_reason"`
		SendTime   time.Time      `db:"send_time"`
		Success    sql.NullBool   `db:"send_success"`
	}

	var txs []txRow
	err := a.Deps.DB.Select(ctx, &txs, `
		WITH hashes AS (
			SELECT create_message_hash AS h FROM pdp_data_sets WHERE id = $1 AND create_message_hash IS NOT NULL AND create_message_hash != ''
			UNION
			SELECT challenge_request_msg_hash FROM pdp_data_sets WHERE id = $1 AND challenge_request_msg_hash IS NOT NULL AND challenge_request_msg_hash != ''
			UNION
			SELECT add_message_hash FROM pdp_data_set_pieces WHERE data_set = $1 AND add_message_hash IS NOT NULL
			UNION
			SELECT add_message_hash FROM pdp_data_set_piece_adds WHERE data_set = $1 AND add_message_hash IS NOT NULL
		)
		SELECT lower(trim(both from mse.signed_hash)) AS tx_hash,
		       mse.send_reason,
		       mse.send_time,
		       mse.send_success
		FROM hashes h
		JOIN message_sends_eth mse ON lower(trim(both from mse.signed_hash)) = lower(trim(both from h.h))
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
	err = a.Deps.DB.Select(ctx, &proves, `
		SELECT h.task_id, h.result, COALESCE(h.err, '') AS err, h.work_end
		FROM pdp_prove_tasks pt
		JOIN harmony_task_history h ON h.task_id = pt.task_id
		WHERE pt.data_set = $1
		ORDER BY h.work_end DESC
		LIMIT 10`, id)
	if err != nil {
		return nil, xerrors.Errorf("prove interactions: %w", err)
	}
	for _, p := range proves {
		succ := p.Result
		tid := p.TaskID
		out = append(out, PDPDataSetInteraction{
			Kind:      "prove",
			TaskID:    &tid,
			Success:   &succ,
			Err:       p.Err,
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

func (a *WebRPC) loadDataSetSummariesByIDs(ctx context.Context, ids []int64, head int64) ([]PDPDataSetSummary, error) {
	if len(ids) == 0 {
		return []PDPDataSetSummary{}, nil
	}
	var rows []pdpDataSetStatsRow
	err := a.Deps.DB.Select(ctx, &rows, pdpDataSetStatsByIDs, ids)
	if err != nil {
		return nil, xerrors.Errorf("load dataset summaries: %w", err)
	}
	byID := make(map[int64]PDPDataSetSummary, len(rows))
	for _, row := range rows {
		byID[row.ID] = summaryFromStatsRow(row, head)
	}
	// Preserve FWSS order; include missing local rows as stubs.
	out := make([]PDPDataSetSummary, 0, len(ids))
	for _, id := range ids {
		if s, ok := byID[id]; ok {
			out = append(out, s)
			continue
		}
		out = append(out, PDPDataSetSummary{
			ID:            id,
			ProvingStatus: "unknown",
		})
	}
	return out, nil
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
