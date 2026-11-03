package webrpc

import (
	"testing"

	"github.com/filecoin-project/curio/pdp/paymentstatus"
)

func TestCountAtRiskByStatus(t *testing.T) {
	entries := []atRiskEntry{
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusGrace}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusTerminating}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusTerminating}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusPendingDelete}},
		{snap: paymentstatus.Snapshot{Status: paymentstatus.StatusOK}},
	}
	got := countAtRiskByStatus(entries)
	if got.Total != 5 || got.Grace != 1 || got.Terminating != 2 || got.PendingDelete != 1 {
		t.Fatalf("counts = %+v", got)
	}
}
