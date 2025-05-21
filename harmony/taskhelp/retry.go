package taskhelp

import (
	"math"
	"time"
)

type RetryWaitFunc func(retries int) time.Duration

// RetryWaitLinear returns a function that calculates a linearly increasing duration based on retries.
func RetryWaitLinear(initial, increment time.Duration) RetryWaitFunc {
	return func(retries int) time.Duration {
		return initial + time.Duration(retries)*increment
	}
}

// RetryWaitExp returns a function that calculates an exponentially increasing duration based on retries.
func RetryWaitExp(initial time.Duration, factor float64) RetryWaitFunc {
	return func(retries int) time.Duration {
		return time.Duration(float64(initial) * math.Pow(factor, float64(retries)))
	}
}
