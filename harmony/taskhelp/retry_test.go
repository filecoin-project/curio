package taskhelp

import (
	"math"
	"testing"
	"time"
)

func TestRetryWaitExpCapsAt20Minutes(t *testing.T) {
	wait := RetryWaitExp(5*time.Second, 2)
	if got := wait(0); got != 5*time.Second {
		t.Fatalf("retry 0: got %s, want 5s", got)
	}
	// 5s * 2^7 = 640s, still under the cap. 2^8 crosses it.
	if got := wait(7); got != 640*time.Second {
		t.Fatalf("retry 7: got %s, want 640s", got)
	}
	if got := wait(8); got != RETRY_WAIT_EXP_MAX {
		t.Fatalf("retry 8: got %s, want %s", got, RETRY_WAIT_EXP_MAX)
	}
	if got := wait(100); got != RETRY_WAIT_EXP_MAX {
		t.Fatalf("retry 100: got %s, want %s", got, RETRY_WAIT_EXP_MAX)
	}
}

func TestRetryWaitExpCapsInitialAndNonFinite(t *testing.T) {
	if got := RetryWaitExp(time.Hour, 2)(0); got != RETRY_WAIT_EXP_MAX {
		t.Fatalf("initial above cap: got %s, want %s", got, RETRY_WAIT_EXP_MAX)
	}
	if got := RetryWaitExp(5*time.Second, math.NaN())(1); got != RETRY_WAIT_EXP_MAX {
		t.Fatalf("NaN factor: got %s, want %s", got, RETRY_WAIT_EXP_MAX)
	}
	if got := RetryWaitExp(5*time.Second, -2)(1); got != 0 {
		t.Fatalf("negative wait: got %s, want 0", got)
	}
}
