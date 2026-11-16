package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	failLimitWindow = 15 * time.Minute
	failLimitMax    = 8
)

// failLimiter tracks failed password checks per remote IP in-process.
type failLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	now  func() time.Time
}

func newFailLimiter() *failLimiter {
	return &failLimiter{
		hits: map[string][]time.Time{},
		now:  time.Now,
	}
}

// blocked reports whether ip has exhausted the failure budget.
func (f *failLimiter) blocked(ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneLocked(ip)
	return len(f.hits[ip]) >= failLimitMax
}

// record adds a failure for ip.
func (f *failLimiter) record(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pruneLocked(ip)
	f.hits[ip] = append(f.hits[ip], f.now())
}

// clear removes failures for ip after a successful password check.
func (f *failLimiter) clear(ip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.hits, ip)
}

// pruneLocked drops timestamps outside the window. Caller holds f.mu.
func (f *failLimiter) pruneLocked(ip string) {
	cutoff := f.now().Add(-failLimitWindow)
	kept := f.hits[ip][:0]
	for _, t := range f.hits[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(f.hits, ip)
		return
	}
	f.hits[ip] = kept
}

// clientIP returns a stable remote address key for rate limiting.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
