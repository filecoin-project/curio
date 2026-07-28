package webrpc

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const pdpFinancialCacheTTL = 15 * time.Minute

var (
	pdpFinancialCacheMu sync.RWMutex
	pdpFinancialCache   struct {
		at   time.Time
		data PDPDashboardFinancial
		ok   bool
	}
	pdpFinancialSF singleflight.Group
)

type PDPDashboardFinancial struct {
	Income30dUsdfc      string `json:"income30dUsdfc"`
	IncomeSubtitle      string `json:"incomeSubtitle"`
	AccruedUnsettled    string `json:"accruedUnsettledUsdfc"`
	Expense30dFil       string `json:"expense30dFil"`
	Expense30dUsdfc     string `json:"expense30dUsdfc"`
	Expense30dUsdfcNote string `json:"expense30dUsdfcNote"`
}

func (a *WebRPC) PDPDashboardFinancial(ctx context.Context) (PDPDashboardFinancial, error) {
	pdpFinancialCacheMu.RLock()
	if pdpFinancialCache.ok && time.Since(pdpFinancialCache.at) < pdpFinancialCacheTTL {
		out := pdpFinancialCache.data
		pdpFinancialCacheMu.RUnlock()
		return out, nil
	}
	pdpFinancialCacheMu.RUnlock()

	v, err, _ := pdpFinancialSF.Do("pdp-financial", func() (any, error) {
		pdpFinancialCacheMu.RLock()
		if pdpFinancialCache.ok && time.Since(pdpFinancialCache.at) < pdpFinancialCacheTTL {
			out := pdpFinancialCache.data
			pdpFinancialCacheMu.RUnlock()
			return out, nil
		}
		pdpFinancialCacheMu.RUnlock()

		// Detach from the browser RPC context so navigation does not cancel slow DB work.
		computeCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		out := a.computePDPDashboardFinancial(computeCtx)

		// Cache whenever expense succeeded. Income may still be "…" on a
		// transient failure; keeping expense cached avoids re-running the
		// ~30s gas sample on every poll.
		cacheable := out.Expense30dFil != "" && out.Expense30dFil != "…"
		pdpFinancialCacheMu.Lock()
		if cacheable {
			pdpFinancialCache.data = out
			pdpFinancialCache.at = time.Now()
			pdpFinancialCache.ok = true
		} else {
			pdpFinancialCache.ok = false
		}
		pdpFinancialCacheMu.Unlock()
		return out, nil
	})
	if err != nil {
		return PDPDashboardFinancial{}, err
	}
	return v.(PDPDashboardFinancial), nil
}

func (a *WebRPC) computePDPDashboardFinancial(ctx context.Context) PDPDashboardFinancial {
	out := PDPDashboardFinancial{
		IncomeSubtitle: "settled · 30 d",
	}

	var expenseWei *big.Int
	var expenseCount int
	var income, accrued *big.Int
	var expenseErr, incomeErr error
	var expenseMs, incomeMs int64

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		expenseWei, expenseCount, expenseErr = a.pdpExpenseGas30d(ctx)
		expenseMs = time.Since(start).Milliseconds()
	}()
	go func() {
		defer wg.Done()
		start := time.Now()
		income, accrued, incomeErr = a.pdpIncome30d(ctx)
		incomeMs = time.Since(start).Milliseconds()
	}()
	wg.Wait()

	log.Infow("PDPDashboardFinancial timings",
		"expenseMs", expenseMs,
		"incomeMs", incomeMs,
		"expenseTxCount", expenseCount,
		"expenseErr", expenseErr,
		"incomeErr", incomeErr,
	)

	if expenseErr != nil {
		log.Warnw("PDPDashboardFinancial: expense query failed", "error", expenseErr)
		out.Expense30dFil = "…"
	} else {
		out.Expense30dFil = formatFilFromWei(expenseWei)
		usdfc, priceNote := expenseFilToUsdfc(ctx, expenseWei)
		out.Expense30dUsdfc = usdfc
		out.Expense30dUsdfcNote = expenseEstimateNote(expenseCount, priceNote)
	}

	if incomeErr != nil {
		log.Warnw("PDPDashboardFinancial: income query failed", "error", incomeErr)
		out.Income30dUsdfc = "…"
		out.AccruedUnsettled = "…"
		return out
	}
	out.Income30dUsdfc = formatUsdfc(income)
	out.AccruedUnsettled = formatUsdfc(accrued)
	if income.Sign() == 0 {
		out.IncomeSubtitle = "no settlements in period"
	}

	return out
}

func expenseEstimateNote(txCount int, priceNote string) string {
	note := fmt.Sprintf("estimated · ~%d txs", txCount)
	if priceNote != "" {
		return note + " · " + priceNote
	}
	return note
}
