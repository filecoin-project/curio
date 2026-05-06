package deps

import (
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Chain JSON-RPC observability (Lotus full node client).
//
// Enable with:
//
//	CURIO_CHAIN_RPC_STATS=1
//
// Optional:
//
//	CURIO_CHAIN_RPC_CALLSITE=0   — count by RPC method name only (no stack walk)
//
// Every 10s logs aggregated counts for the previous window (then resets).

const chainRPCStatsInterval = 10 * time.Second

var (
	chainRPCStatsMu sync.Mutex
	chainRPCCounts  map[string]uint64
	chainRPCLogOnce sync.Once

	chainRPCStatsEnabledVal  bool
	chainRPCStatsEnabledOnce sync.Once

	chainRPCCallsiteEnabledVal  bool
	chainRPCCallsiteEnabledOnce sync.Once
)

func chainRPCStatsEnabled() bool {
	chainRPCStatsEnabledOnce.Do(func() {
		v := strings.TrimSpace(os.Getenv("CURIO_CHAIN_RPC_STATS"))
		on := v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		chainRPCStatsEnabledVal = on
	})
	return chainRPCStatsEnabledVal
}

func chainRPCCallsiteEnabled() bool {
	chainRPCCallsiteEnabledOnce.Do(func() {
		v := strings.TrimSpace(os.Getenv("CURIO_CHAIN_RPC_CALLSITE"))
		on := true
		switch {
		case v == "":
			on = true
		case v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "no"):
			on = false
		}
		chainRPCCallsiteEnabledVal = on
	})
	return chainRPCCallsiteEnabledVal
}

func noteChainRPC(method string) {
	if !chainRPCStatsEnabled() {
		return
	}
	chainRPCLogOnce.Do(func() {
		go chainRPCStatsLogLoop()
	})

	key := method
	if chainRPCCallsiteEnabled() {
		if site := chainRPCCallSite(); site != "" {
			key = method + "\x00" + site
		}
	}

	chainRPCStatsMu.Lock()
	if chainRPCCounts == nil {
		chainRPCCounts = make(map[string]uint64)
	}
	chainRPCCounts[key]++
	chainRPCStatsMu.Unlock()
}

func chainRPCCallSite() string {
	pc := make([]uintptr, 64)
	n := runtime.Callers(3, pc)
	frames := runtime.CallersFrames(pc[:n])
	for {
		f, more := frames.Next()
		if !more {
			break
		}
		file := f.File
		if strings.HasSuffix(file, "deps/chainrpc_stats.go") {
			continue
		}
		if strings.HasSuffix(file, "deps/apiinfo.go") {
			continue // JSON-RPC proxy + Retry closure
		}
		if strings.Contains(file, "reflect/") || strings.Contains(file, "runtime/") {
			continue
		}
		if strings.Contains(file, "github.com/filecoin-project/curio/") {
			return trimCurioPath(file) + ":" + itoa(f.Line)
		}
	}
	return ""
}

func trimCurioPath(file string) string {
	const p = "github.com/filecoin-project/curio/"
	if i := strings.Index(file, p); i >= 0 {
		return file[i+len(p):]
	}
	return file
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func chainRPCStatsLogLoop() {
	t := time.NewTicker(chainRPCStatsInterval)
	defer t.Stop()
	for range t.C {
		chainRPCStatsMu.Lock()
		snap := chainRPCCounts
		chainRPCCounts = nil
		chainRPCStatsMu.Unlock()

		if len(snap) == 0 {
			continue
		}

		type row struct {
			Method string
			Site   string
			Count  uint64
		}
		var rows []row
		for k, c := range snap {
			method, site := k, ""
			if i := strings.IndexByte(k, 0); i >= 0 {
				method = k[:i]
				site = k[i+1:]
			}
			rows = append(rows, row{Method: method, Site: site, Count: c})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Count != rows[j].Count {
				return rows[i].Count > rows[j].Count
			}
			return rows[i].Method < rows[j].Method
		})
		const maxRows = 60
		if len(rows) > maxRows {
			rows = rows[:maxRows]
		}
		clog.Infow("chain JSON-RPC stats (rolling "+chainRPCStatsInterval.String()+" window, set CURIO_CHAIN_RPC_STATS=0 to disable)",
			"top", rows)
	}
}
