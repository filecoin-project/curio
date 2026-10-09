package seal

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSDRSectorDiagnosticLookupContext(t *testing.T) {
	for _, mode := range []string{"success", "error", "cancelled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer stop()
			}
			boom := errors.New("synthetic lookup error")
			var queryCtx context.Context
			sid, err := lookupSDRSectorID(ctx, func(c context.Context, sp, sector *uint64) error {
				queryCtx = c
				if d, ok := c.Deadline(); !ok || time.Until(d) > 5*time.Second {
					t.Fatal("unbounded diagnostic lookup")
				}
				if e := c.Err(); e != nil {
					return e
				}
				if mode == "error" {
					return boom
				}
				*sp = 1000
				*sector = 2000
				return nil
			})
			if mode == "success" {
				if err != nil || sid == nil || sid.Miner != 1000 || sid.Number != 2000 {
					t.Fatalf("sector: %v %v", sid, err)
				}
			} else if err == nil || sid != nil {
				t.Fatalf("error fabricated sector: %v %v", sid, err)
			}
			if queryCtx.Err() == nil {
				t.Fatal("lookup child context not cancelled on return")
			}
		})
	}
}
