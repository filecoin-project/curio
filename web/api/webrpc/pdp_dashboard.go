package webrpc

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/lib/filecoinpayment"
	"github.com/filecoin-project/curio/pdp/contract"
)

var pdpExpenseSendReasons = []string{
	"pdp-mkdataset",
	"pdp-create-and-add",
	"pdp-addpieces",
	"pdp-delete-piece",
	"pdp-proving-init",
	"pdp-proving-period",
	"pdp-prove",
	"pdp-terminate-service",
	"pdp-terminate-data-set",
	"pdp-add-piece",
	"pdp-create-data-set",
	"pdp-remove-piece",
	"settleRail",
}

const pdpExpenseSendCountQuery = `
		SELECT COUNT(*)
		FROM message_sends_eth
		WHERE send_reason = ANY($1)
		  AND send_time > NOW() - INTERVAL '7 days'
		  AND send_success = TRUE
		  AND signed_hash IS NOT NULL`

// Fee sample from recent sends only (gas price is recent). Pre-normalize the
// hash so the wait join can use signed_tx_hash equality.
const pdpExpenseGasSampleQuery = `
		WITH candidates AS (
			SELECT lower(trim(both from signed_hash)) AS tx_hash
			FROM message_sends_eth
			WHERE send_reason = ANY($1)
			  AND send_time > NOW() - INTERVAL '7 days'
			  AND send_success = TRUE
			  AND signed_hash IS NOT NULL
			ORDER BY send_time DESC
			LIMIT 100
		)
		SELECT
			mwe.tx_receipt->>'gasUsed' AS gas_used,
			mwe.tx_receipt->>'effectiveGasPrice' AS gas_price
		FROM candidates c
		JOIN message_waits_eth mwe ON mwe.signed_tx_hash = c.tx_hash
		WHERE mwe.tx_status = 'confirmed'
		  AND mwe.tx_success = TRUE
		  AND mwe.tx_receipt IS NOT NULL
		LIMIT 50`

// Sum RailSettled.totalNetPayeeAmount in SQL (one row back). Decode uses
// 4×uint64 bit casts — Yugabyte rejects bit(256)::numeric.
const pdpIncome30dSumQuery = `
		WITH settles AS (
			SELECT lower(trim(both from signed_hash)) AS tx_hash
			FROM message_sends_eth
			WHERE send_reason = 'settleRail'
			  AND send_time > NOW() - INTERVAL '30 days'
			  AND send_success = TRUE
			  AND signed_hash IS NOT NULL
		)
		SELECT COALESCE(SUM(` + ethLogDataUint256Word1SQL + `), 0)::text
		FROM settles mse
		JOIN message_waits_eth mwe
		  ON mwe.signed_tx_hash = mse.tx_hash
		 AND mwe.tx_status = 'confirmed'
		 AND mwe.tx_success = TRUE
		 AND mwe.tx_receipt IS NOT NULL
		CROSS JOIN LATERAL (
			SELECT e.value
			FROM jsonb_array_elements(COALESCE(mwe.tx_receipt->'logs', '[]'::jsonb)) AS e(value)
			WHERE e.value->'topics'->>0 = $2
			  AND lower(e.value->>'address') = lower($1)
		) log`

type ProvingActivityDay struct {
	Date    string `json:"date"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Faulted int    `json:"faulted"`
}

type PDPDashboardSummary struct {
	DataUnderProofBytes int64                `json:"dataUnderProofBytes"`
	DataSetCount        int                  `json:"dataSetCount"`
	PieceCount          int                  `json:"pieceCount"`
	ProvingSuccessRate  float64              `json:"provingSuccessRate"`
	ProvingSuccessCount int                  `json:"provingSuccessCount"`
	ProvingFailureCount int                  `json:"provingFailureCount"`
	FaultedPeriods30d   int                  `json:"faultedPeriods30d"`
	ProvingActivity     []ProvingActivityDay `json:"provingActivity"`
}

func (a *WebRPC) PDPDashboardSummary(ctx context.Context) (PDPDashboardSummary, error) {
	summary := PDPDashboardSummary{
		ProvingActivity: []ProvingActivityDay{},
	}

	var storage struct {
		Bytes      int64 `db:"bytes"`
		DataSets   int   `db:"data_sets"`
		PieceCount int   `db:"piece_count"`
	}
	err := a.Deps.DB.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(dsp.sub_piece_size), 0) AS bytes,
			COUNT(DISTINCT ds.id) AS data_sets,
			COUNT(*) AS piece_count
		FROM pdp_data_set_pieces dsp
		JOIN pdp_data_sets ds ON ds.id = dsp.data_set
		WHERE dsp.removed = FALSE
		  AND ds.unrecoverable_proving_failure_epoch IS NULL`).Scan(
		&storage.Bytes, &storage.DataSets, &storage.PieceCount)
	if err != nil {
		return summary, xerrors.Errorf("data under proof: %w", err)
	}
	summary.DataUnderProofBytes = storage.Bytes
	summary.DataSetCount = storage.DataSets
	summary.PieceCount = storage.PieceCount

	net, netErr := a.NetSummary(ctx)
	if netErr != nil {
		log.Warnw("PDPDashboardSummary: net summary failed, skipping faulted periods", "error", netErr)
	} else {
		epochs30d := int64(builtin.EpochsInDay * 30)
		if qerr := a.Deps.DB.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM pdp_data_sets
			WHERE unrecoverable_proving_failure_epoch IS NOT NULL
			  AND unrecoverable_proving_failure_epoch > $1`, net.Epoch-epochs30d).Scan(&summary.FaultedPeriods30d); qerr != nil {
			log.Warnw("PDPDashboardSummary: faulted periods query failed", "error", qerr)
		}
	}

	success, failed, activity, err := a.pdpProveStats30d(ctx)
	if err != nil {
		log.Warnw("PDPDashboardSummary: proving stats failed", "error", err)
	} else {
		summary.ProvingSuccessCount = success
		summary.ProvingFailureCount = failed
		total := success + failed
		if total > 0 {
			summary.ProvingSuccessRate = float64(success) / float64(total) * 100
		}
		summary.ProvingActivity = activity
	}

	if netErr == nil {
		faultedByDay, ferr := a.pdpFaultedActivity30d(ctx, net.Epoch)
		if ferr != nil {
			log.Warnw("PDPDashboardSummary: faulted activity failed", "error", ferr)
		} else {
			summary.ProvingActivity = mergeProvingActivity(summary.ProvingActivity, faultedByDay)
		}
	}

	return summary, nil
}

func mergeProvingActivity(taskDays []ProvingActivityDay, faultedByDay map[string]int) []ProvingActivityDay {
	byDate := make(map[string]*ProvingActivityDay, len(taskDays)+len(faultedByDay))
	order := []string{}

	addDay := func(date string) *ProvingActivityDay {
		if byDate[date] == nil {
			byDate[date] = &ProvingActivityDay{Date: date}
			order = append(order, date)
		}
		return byDate[date]
	}

	for _, day := range taskDays {
		entry := addDay(day.Date)
		entry.Success = day.Success
		entry.Failed = day.Failed
	}
	for date, count := range faultedByDay {
		addDay(date).Faulted = count
	}

	sort.Strings(order)
	out := make([]ProvingActivityDay, 0, len(order))
	for _, date := range order {
		out = append(out, *byDate[date])
	}
	return out
}

func (a *WebRPC) pdpFaultedActivity30d(ctx context.Context, headEpoch int64) (map[string]int, error) {
	epochs30d := int64(builtin.EpochsInDay * 30)
	cutoff := headEpoch - epochs30d

	var rows []struct {
		FailureEpoch int64 `db:"failure_epoch"`
	}
	err := a.Deps.DB.Select(ctx, &rows, `
		SELECT unrecoverable_proving_failure_epoch AS failure_epoch
		FROM pdp_data_sets
		WHERE unrecoverable_proving_failure_epoch IS NOT NULL
		  AND unrecoverable_proving_failure_epoch > $1`, cutoff)
	if err != nil {
		return nil, xerrors.Errorf("faulted datasets: %w", err)
	}

	blockDelay := time.Duration(build.BlockDelaySecs) * time.Second
	headTime := time.Now()
	byDay := make(map[string]int, len(rows))
	for _, row := range rows {
		delta := headEpoch - row.FailureEpoch
		if delta < 0 {
			delta = 0
		}
		day := headTime.Add(-time.Duration(delta) * blockDelay).Format("2006-01-02")
		byDay[day]++
	}
	return byDay, nil
}

func (a *WebRPC) pdpProveStats30d(ctx context.Context) (success int, failed int, activity []ProvingActivityDay, err error) {
	activity = []ProvingActivityDay{}

	// harmony_task_history is indexed on work_end + name; avoid joining the full
	// message_sends_eth/message_waits_eth tables on page load.
	var rows []struct {
		Day     time.Time `db:"day"`
		Success int       `db:"success"`
		Failed  int       `db:"failed"`
	}
	err = a.Deps.DB.Select(ctx, &rows, `
		SELECT
			DATE(work_end) AS day,
			COUNT(*) FILTER (WHERE succeeded) AS success,
			COUNT(*) FILTER (WHERE NOT succeeded) AS failed
		FROM (
			SELECT task_id, BOOL_OR(result) AS succeeded, MAX(work_end) AS work_end
			FROM harmony_task_history
			WHERE name IN ('PDPv0_Prove', 'PDPProve')
			  AND work_end > CURRENT_TIMESTAMP - INTERVAL '30 days'
			GROUP BY task_id
		) per_task
		GROUP BY DATE(work_end)
		ORDER BY 1`)
	if err != nil {
		return 0, 0, activity, xerrors.Errorf("proving activity: %w", err)
	}

	for _, row := range rows {
		activity = append(activity, ProvingActivityDay{
			Date:    row.Day.Format("2006-01-02"),
			Success: row.Success,
			Failed:  row.Failed,
		})
		success += row.Success
		failed += row.Failed
	}

	return success, failed, activity, nil
}

func (a *WebRPC) pdpExpenseGas30d(ctx context.Context) (total *big.Int, txCount int, err error) {
	var rows []receiptGasFields
	var count7d int
	var countErr, sampleErr error

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		countErr = a.Deps.DB.QueryRow(ctx, pdpExpenseSendCountQuery, pdpExpenseSendReasons).Scan(&count7d)
	}()
	go func() {
		defer wg.Done()
		sampleErr = a.Deps.DB.Select(ctx, &rows, pdpExpenseGasSampleQuery, pdpExpenseSendReasons)
	}()
	wg.Wait()

	if countErr != nil {
		return nil, 0, xerrors.Errorf("expense send count: %w", countErr)
	}
	// Extrapolate 7d volume to 30d (~4.286×) so we avoid a full 30d COUNT on 600k+ rows.
	txCount = int(float64(count7d) * (30.0 / 7.0))
	if count7d == 0 {
		return big.NewInt(0), 0, nil
	}
	if sampleErr != nil {
		return nil, txCount, xerrors.Errorf("expense gas sample: %w", sampleErr)
	}

	avgFee, err := avgReceiptGasFee(rows)
	if err != nil {
		return nil, txCount, err
	}
	if avgFee.Sign() == 0 {
		return big.NewInt(0), txCount, nil
	}

	total = new(big.Int).Mul(avgFee, big.NewInt(int64(txCount)))
	return total, txCount, nil
}

func avgReceiptGasFee(rows []receiptGasFields) (*big.Int, error) {
	if len(rows) == 0 {
		return big.NewInt(0), nil
	}
	sum := big.NewInt(0)
	ok := 0
	for _, row := range rows {
		fee, err := receiptGasFeeFromFields(row.GasUsed, row.EffectiveGasPrice)
		if err != nil {
			log.Warnw("PDPDashboardFinancial: skip sample receipt gas parse", "error", err)
			continue
		}
		sum.Add(sum, fee)
		ok++
	}
	if ok == 0 {
		return big.NewInt(0), nil
	}
	return new(big.Int).Div(sum, big.NewInt(int64(ok))), nil
}

func (a *WebRPC) pdpIncome30d(ctx context.Context) (settled *big.Int, accrued *big.Int, err error) {
	settled = big.NewInt(0)
	accrued = big.NewInt(0)

	paymentAddr, err := filecoinpayment.PaymentContractAddress()
	if err != nil {
		return settled, accrued, err
	}

	eclient, err := a.Deps.EthClient.Val()
	if err != nil {
		return settled, accrued, nil
	}

	payments, err := filecoinpayment.NewPayments(paymentAddr, eclient)
	if err != nil {
		return settled, accrued, xerrors.Errorf("payments contract: %w", err)
	}

	reg, regErr := a.FSRegistryStatus(ctx)
	if regErr == nil && reg != nil && reg.Active && reg.Payee != "" {
		payee := common.HexToAddress(reg.Payee)
		token, tokenErr := contract.USDFCAddress()
		if tokenErr == nil {
			acct, acctErr := payments.Accounts(&bind.CallOpts{Context: ctx}, token, payee)
			if acctErr == nil && acct.LockupCurrent != nil {
				accrued = acct.LockupCurrent
			}
		}
	}

	var settledStr string
	err = a.Deps.DB.QueryRow(ctx, pdpIncome30dSumQuery, paymentAddr.Hex(), railSettledTopic).Scan(&settledStr)
	if err != nil {
		return settled, accrued, xerrors.Errorf("settlement sum: %w", err)
	}
	settled, err = parseSQLNumericInt(settledStr)
	if err != nil {
		return settled, accrued, xerrors.Errorf("settlement sum parse: %w", err)
	}

	return settled, accrued, nil
}

func formatUsdfc(v *big.Int) string {
	if v == nil || v.Sign() == 0 {
		return "0"
	}
	// Reject corrupt decode results (larger than 1e30 USDFC in 18-decimal units).
	maxSane := new(big.Int).Exp(big.NewInt(10), big.NewInt(48), nil)
	if v.Cmp(maxSane) > 0 {
		return "—"
	}
	rat := new(big.Rat).SetFrac(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	f, _ := rat.Float64()
	if f >= 100 {
		return fmt.Sprintf("%.1f", f)
	}
	if f >= 10 {
		return fmt.Sprintf("%.2f", f)
	}
	return fmt.Sprintf("%.3f", f)
}

func formatFilFromWei(wei *big.Int) string {
	if wei == nil || wei.Sign() == 0 {
		return "0"
	}
	// 1 FIL = 1e18 wei on ETH side for gas; display as FIL with reasonable precision
	rat := new(big.Rat).SetFrac(wei, big.NewInt(1e18))
	f, _ := rat.Float64()
	if f >= 1 {
		return fmt.Sprintf("%.2f", f)
	}
	return fmt.Sprintf("%.4f", f)
}
