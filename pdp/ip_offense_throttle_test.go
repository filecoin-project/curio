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
