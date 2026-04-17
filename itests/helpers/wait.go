package helpers

import (
	"context"
	"time"

	"golang.org/x/xerrors"
)

var ErrWaitTimeout = xerrors.New("timeout waiting for condition")

func WaitForCondition(ctx context.Context, timeout time.Duration, pollInterval time.Duration, check func() (bool, error)) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	tout := time.NewTimer(timeout)
	defer tout.Stop()
	for {
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
		case <-tout.C:
			return ErrWaitTimeout
		case <-ticker.C:
		}
	}
}
