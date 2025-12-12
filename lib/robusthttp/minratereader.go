package robusthttp

import (
	"io"
	"time"

	"golang.org/x/xerrors"
)

type RateEnforcingReader struct {
	r io.Reader

	readError error

	rc *RateCounter

	bytesTransferredSnap int64
	lastSpeedCheck       time.Time
	windowDuration       time.Duration
}

func NewRateEnforcingReader(r io.Reader, rc *RateCounter, windowDuration time.Duration) *RateEnforcingReader {
	return &RateEnforcingReader{
		r:              r,
		rc:             rc,
		windowDuration: windowDuration,
	}
}

func (rer *RateEnforcingReader) Read(p []byte) (int, error) {
	if rer.readError != nil {
		return 0, rer.readError
	}

	now := time.Now()

	if !rer.lastSpeedCheck.IsZero() && now.Sub(rer.lastSpeedCheck) >= rer.windowDuration {
		elapsedTime := now.Sub(rer.lastSpeedCheck)

		checkErr := rer.rc.Check(func() error {
			ctrTransferred := rer.rc.transferred.Load()
			transferredInWindow := ctrTransferred - rer.bytesTransferredSnap

			rer.bytesTransferredSnap = ctrTransferred
			rer.lastSpeedCheck = now

			transferSpeedMbps := float64(transferredInWindow*8) / 1e6 / elapsedTime.Seconds()

			return rer.rc.rateFunc(transferSpeedMbps, rer.rc.transfers.Load(), rer.rc.globalTransfers.Load())
		})

		if checkErr != nil {
			rer.readError = xerrors.Errorf("read rate over past %s is too slow: %w", rer.windowDuration, checkErr)
			return 0, rer.readError
		}
	} else if rer.lastSpeedCheck.IsZero() {
		// Initialize last speed check time and transferred bytes snapshot
		rer.lastSpeedCheck = now
		rer.bytesTransferredSnap = rer.rc.transferred.Load()
	}

	// Set read deadline
	if w, ok := rer.r.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = w.SetReadDeadline(now.Add(rer.windowDuration * 2))
	}

	n, err := rer.r.Read(p)
	rer.rc.transferred.Add(int64(n))
	return n, err
}

func (rer *RateEnforcingReader) ReadError() error {
	return rer.readError
}

func (rer *RateEnforcingReader) Done() {
	if rer.readError == nil {
		rer.rc.Release()
	}
}
