package helpers

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

var ErrWaitTimeout = xerrors.New("timeout waiting for condition")

func WaitForCondition(ctx context.Context, timeout time.Duration, pollInterval time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		done, err := check()
		if err != nil {
			return err
		}
		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	return ErrWaitTimeout
}
