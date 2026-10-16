package taskhelp

import (
	"time"
)

type RetryWaitFunc func(retries int) time.Duration

// RETRY_WAIT_EXP_MAX is the longest delay RetryWaitExp returns.
const RETRY_WAIT_EXP_MAX = 20 * time.Minute

// RetryWaitLinear returns a function that calculates a linearly increasing duration based on retries.
func RetryWaitLinear(initial, increment time.Duration) RetryWaitFunc {
	return func(retries int) time.Duration {
		return initial + time.Duration(retries)*increment
	}
}

// RetryWaitExp returns a function that calculates an exponentially increasing
// duration based on retries. The result never exceeds RETRY_WAIT_EXP_MAX.
// A non-finite result, which a large retry count produces, is also that cap.
func RetryWaitExp(initial time.Duration) RetryWaitFunc {
	return func(retries int) time.Duration {
		if initial == 0 {
			return 0
		}
		wait := initial << retries
		if wait == 0 { // overflow
			return RETRY_WAIT_EXP_MAX
		}
		return min(RETRY_WAIT_EXP_MAX, initial<<retries)
	}
}
