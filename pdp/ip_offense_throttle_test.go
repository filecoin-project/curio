package pdp

import (
	"net/http"
	"testing"
	"time"
)

func testIPOffenseThrottle() *IPOffenseThrottle {
	return NewIPOffenseThrottle(map[string]IPOffensePolicy{
		"test_offense": {
			Hits:   3,
			Window: time.Minute,
			Block:  2 * time.Minute,
			Reason: "too many test offenses",
		},
	})
}

func TestIPOffenseThrottleBlocksNoisyIP(t *testing.T) {
	throttle := testIPOffenseThrottle()
	req := &http.Request{RemoteAddr: "203.0.113.10:1234"}

	for i := 0; i < 2; i++ {
		blocked, retryAfter, _ := throttle.Record(req, "test_offense")
		if blocked || retryAfter != 0 {
			t.Fatalf("unexpected throttle at hit %d: blocked=%v retryAfter=%v", i+1, blocked, retryAfter)
		}
	}

	blocked, retryAfter, reason := throttle.Record(req, "test_offense")
	if !blocked || retryAfter != 2*time.Minute || reason != "too many test offenses" {
		t.Fatalf("expected block after limit, got blocked=%v retryAfter=%v reason=%q", blocked, retryAfter, reason)
	}

	blocked, retryAfter, _ = throttle.longestBlock(req)
	if !blocked || retryAfter <= 0 {
		t.Fatalf("expected blocked check to fail request, got blocked=%v retryAfter=%v", blocked, retryAfter)
	}
}

func TestIPOffenseThrottleDoesNotBlockOtherIPs(t *testing.T) {
	throttle := testIPOffenseThrottle()
	noisy := &http.Request{RemoteAddr: "203.0.113.10:1234"}
	other := &http.Request{RemoteAddr: "198.51.100.4:1234"}

	for i := 0; i < 3; i++ {
		throttle.Record(noisy, "test_offense")
	}

	blocked, _, _ := throttle.longestBlock(other)
	if blocked {
		t.Fatal("expected other IP to remain unblocked")
	}
}

func TestIPOffenseThrottleCleansExpiredStateOnAccess(t *testing.T) {
	throttle := testIPOffenseThrottle()
	req := &http.Request{RemoteAddr: "203.0.113.10:1234"}
	now := time.Now()

	throttle.mu.Lock()
	throttle.state["203.0.113.10"] = map[string]*ipOffenseState{
		"test_offense": {
			blockedUntil: now.Add(-time.Minute),
			windowStart:  now.Add(-3 * time.Minute),
		},
	}
	throttle.mu.Unlock()

	blocked, _, _ := throttle.longestBlock(req)
	if blocked {
		t.Fatal("expected expired block state to be cleaned on access")
	}

	throttle.mu.RLock()
	_, ok := throttle.state["203.0.113.10"]
	throttle.mu.RUnlock()
	if ok {
		t.Fatal("expected expired IP state to be removed")
	}
}

func TestIPOffenseThrottleCleanupAllRemovesStaleIPs(t *testing.T) {
	throttle := testIPOffenseThrottle()
	now := time.Now()

	throttle.mu.Lock()
	throttle.state["203.0.113.10"] = map[string]*ipOffenseState{
		"test_offense": {
			blockedUntil: now.Add(-time.Minute),
			windowStart:  now.Add(-3 * time.Minute),
		},
	}
	throttle.state["198.51.100.4"] = map[string]*ipOffenseState{
		"test_offense": {
			hits:         1,
			windowStart:  now,
			blockedUntil: now.Add(2 * time.Minute),
		},
	}
	throttle.mu.Unlock()

	throttle.cleanupAll()

	throttle.mu.RLock()
	defer throttle.mu.RUnlock()
	if _, ok := throttle.state["203.0.113.10"]; ok {
		t.Fatal("expected stale IP to be removed by periodic cleanup")
	}
	if _, ok := throttle.state["198.51.100.4"]; !ok {
		t.Fatal("expected active IP to remain after periodic cleanup")
	}
}
