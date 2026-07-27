package pdp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/httprate"
)

// OffenseBadDataSetAdd is recorded when addPieces targets a missing or
// terminated data set.
const OffenseBadDataSetAdd = "bad_dataset_add"

// IPOffensePolicy configures per-offense IP throttling.
type IPOffensePolicy struct {
	Hits   int
	Window time.Duration
	Block  time.Duration
	Reason string
}

// IPOffenseThrottle tracks repeated request offenses per client IP.
type IPOffenseThrottle struct {
	policies map[string]IPOffensePolicy

	mu    sync.RWMutex
	state map[string]map[string]*ipOffenseState
}

const ipOffenseCleanupInterval = 5 * time.Minute

type ipOffenseState struct {
	hits         int
	windowStart  time.Time
	blockedUntil time.Time
}

func NewIPOffenseThrottle(policies map[string]IPOffensePolicy) *IPOffenseThrottle {
	return &IPOffenseThrottle{
		policies: policies,
		state:    make(map[string]map[string]*ipOffenseState),
	}
}

// RunCleanup periodically removes expired offense state for all tracked IPs.
func (t *IPOffenseThrottle) RunCleanup(ctx context.Context) {
	ticker := time.NewTicker(ipOffenseCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.cleanupAll()
		}
	}
}

func offenseStateExpired(st *ipOffenseState, policy IPOffensePolicy, now time.Time) bool {
	if now.Before(st.blockedUntil) {
		return false
	}
	if st.hits > 0 && now.Sub(st.windowStart) < policy.Window*2 {
		return false
	}
	return true
}

// cleanupIPLocked removes expired offense entries for ip. Caller must hold t.mu.
func (t *IPOffenseThrottle) cleanupIPLocked(ip string, now time.Time) {
	offenses, ok := t.state[ip]
	if !ok {
		return
	}

	for offense, st := range offenses {
		policy, ok := t.policies[offense]
		if !ok || offenseStateExpired(st, policy, now) {
			delete(offenses, offense)
		}
	}
	if len(offenses) == 0 {
		delete(t.state, ip)
	}
}

func (t *IPOffenseThrottle) cleanupAll() {
	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	for ip := range t.state {
		t.cleanupIPLocked(ip, now)
	}
}

func defaultIPOffensePolicies() map[string]IPOffensePolicy {
	return map[string]IPOffensePolicy{
		OffenseBadDataSetAdd: {
			Hits:   5,
			Window: time.Minute,
			Block:  5 * time.Minute,
			Reason: "too many requests for unavailable data sets",
		},
	}
}

func clientIPFromRequest(r *http.Request) string {
	ip, err := httprate.KeyByRealIP(r)
	if err == nil && ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Middleware rejects requests from IPs that are temporarily blocked for any
// configured offense.
func (t *IPOffenseThrottle) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if blocked, retryAfter, reason := t.longestBlock(r); blocked {
			log.Warnw("PDP request throttled",
				"clientIP", clientIPFromRequest(r),
				"path", r.URL.Path,
				"retryAfter", retryAfter,
				"reason", reason)
			respondIPThrottled(w, retryAfter, reason)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Record increments the offense counter for the request IP. It returns whether
// the IP is now blocked and how long to wait before retrying.
func (t *IPOffenseThrottle) Record(r *http.Request, offense string) (blocked bool, retryAfter time.Duration, reason string) {
	policy, ok := t.policies[offense]
	if !ok {
		return false, 0, ""
	}

	ip := clientIPFromRequest(r)
	if ip == "" {
		return false, 0, policy.Reason
	}

	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.cleanupIPLocked(ip, now)

	offenses, ok := t.state[ip]
	if !ok {
		offenses = make(map[string]*ipOffenseState)
		t.state[ip] = offenses
	}

	st, ok := offenses[offense]
	if !ok {
		st = &ipOffenseState{windowStart: now}
		offenses[offense] = st
	}

	if now.Before(st.blockedUntil) {
		return true, time.Until(st.blockedUntil), policy.Reason
	}

	if now.Sub(st.windowStart) >= policy.Window {
		st.hits = 0
		st.windowStart = now
	}

	st.hits++
	if st.hits < policy.Hits {
		return false, 0, policy.Reason
	}

	st.blockedUntil = now.Add(policy.Block)
	st.hits = 0
	st.windowStart = now
	log.Warnw("throttling IP after repeated PDP offense",
		"clientIP", ip,
		"offense", offense,
		"retryAfter", policy.Block)
	return true, policy.Block, policy.Reason
}

func (t *IPOffenseThrottle) longestBlock(r *http.Request) (bool, time.Duration, string) {
	ip := clientIPFromRequest(r)
	if ip == "" {
		return false, 0, ""
	}

	now := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.cleanupIPLocked(ip, now)

	offenses, ok := t.state[ip]
	if !ok {
		return false, 0, ""
	}

	var (
		blocked    bool
		retryAfter time.Duration
		reason     string
	)

	for offense, st := range offenses {
		if !now.Before(st.blockedUntil) {
			continue
		}
		remaining := time.Until(st.blockedUntil)
		policy := t.policies[offense]
		if !blocked || remaining > retryAfter {
			blocked = true
			retryAfter = remaining
			reason = policy.Reason
		}
	}

	return blocked, retryAfter, reason
}

func respondIPThrottled(w http.ResponseWriter, retryAfter time.Duration, reason string) {
	seconds := int(retryAfter.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	if reason == "" {
		reason = "too many requests"
	}
	w.Header().Set("Retry-After", fmt.Sprint(seconds))
	http.Error(w, reason, http.StatusTooManyRequests)
}

func (p *PDPService) recordIPOffense(w http.ResponseWriter, r *http.Request, offense string) bool {
	blocked, retryAfter, reason := p.ipOffenseThrottle.Record(r, offense)
	if !blocked {
		return false
	}
	respondIPThrottled(w, retryAfter, reason)
	return true
}
