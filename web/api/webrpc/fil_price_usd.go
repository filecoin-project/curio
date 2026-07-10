package webrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const filUsdPriceTTL = 30 * time.Minute

var (
	filUsdPriceMu      sync.Mutex
	filUsdPriceCached  float64
	filUsdPriceCachedAt time.Time
)

type coinGeckoPriceResp map[string]map[string]float64

func filUsdPrice(ctx context.Context) (float64, error) {
	filUsdPriceMu.Lock()
	defer filUsdPriceMu.Unlock()

	if filUsdPriceCached > 0 && time.Since(filUsdPriceCachedAt) < filUsdPriceTTL {
		return filUsdPriceCached, nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		"https://api.coingecko.com/api/v3/simple/price?ids=filecoin&vs_currencies=usd", nil)
	if err != nil {
		return 0, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("coingecko status %d: %s", resp.StatusCode, string(body))
	}

	var parsed coinGeckoPriceResp
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, err
	}

	price := parsed["filecoin"]["usd"]
	if price <= 0 {
		return 0, fmt.Errorf("coingecko returned invalid FIL price")
	}

	filUsdPriceCached = price
	filUsdPriceCachedAt = time.Now()
	return price, nil
}

func expenseFilToUsdfc(ctx context.Context, expenseWei *big.Int) (string, string) {
	if expenseWei == nil || expenseWei.Sign() == 0 {
		return "0", ""
	}

	price, err := filUsdPrice(ctx)
	if err != nil {
		log.Debugw("PDPDashboardFinancial: FIL/USD price unavailable", "error", err)
		return "", "FIL/USD unavailable"
	}

	filRat := new(big.Rat).SetFrac(expenseWei, big.NewInt(1e18))
	filFloat, _ := filRat.Float64()
	usdfc := filFloat * price
	return formatUsdAmount(usdfc), fmt.Sprintf("≈ spot FIL ($%.2f)", price)
}

func formatUsdAmount(v float64) string {
	if v <= 0 {
		return "0"
	}
	if v >= 100 {
		return fmt.Sprintf("%.1f", v)
	}
	if v >= 10 {
		return fmt.Sprintf("%.2f", v)
	}
	return fmt.Sprintf("%.3f", v)
}
