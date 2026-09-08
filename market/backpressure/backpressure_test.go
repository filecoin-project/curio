package backpressure

import (
	"testing"

	"github.com/filecoin-project/curio/deps/config"
)

func TestSDRQueueBackpressure(t *testing.T) {
	for _, test := range []struct {
		name            string
		limit           int
		bufferedSDR     int
		bufferedTrees   int
		bufferedPoRep   int
		waitDealSectors int
		want            bool
	}{
		{name: "zero limit empty queue", limit: 0, bufferedSDR: 0},
		{name: "zero limit nonempty queue", limit: 0, bufferedSDR: 100},
		{name: "positive limit below", limit: 8, bufferedSDR: 7},
		{name: "positive limit equal", limit: 8, bufferedSDR: 8},
		{name: "positive limit above", limit: 8, bufferedSDR: 9, want: true},
		{name: "zero limit preserves deal pressure", limit: 0, bufferedSDR: 100, waitDealSectors: 9, want: true},
		{name: "zero limit preserves tree pressure", limit: 0, bufferedSDR: 100, bufferedTrees: 9, want: true},
		{name: "zero limit preserves PoRep pressure", limit: 0, bufferedSDR: 100, bufferedPoRep: 9, want: true},
		{name: "other queues at limit", limit: 0, bufferedSDR: 100, waitDealSectors: 8, bufferedTrees: 8, bufferedPoRep: 8},
		{name: "negative limit unchanged", limit: -1, bufferedSDR: 0, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.CurioIngestConfig{
				MaxQueueSDR:        config.NewDynamic(test.limit),
				MaxQueueDealSector: config.NewDynamic(8),
				MaxQueueTrees:      config.NewDynamic(8),
				MaxQueuePoRep:      config.NewDynamic(8),
			}
			got := sdrQueueBackpressure(cfg, test.bufferedSDR, test.bufferedTrees, test.bufferedPoRep, test.waitDealSectors)
			if got != test.want {
				t.Fatalf("sdrQueueBackpressure() = %t, want %t", got, test.want)
			}
		})
	}
}
