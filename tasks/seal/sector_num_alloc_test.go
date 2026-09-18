package seal

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSectorStateLockUsesPreservingWriteConflict(t *testing.T) {
	required := []string{
		"ON CONFLICT (sp_id) DO UPDATE",
		"SET allocated = sectors_allocated_numbers.allocated",
		"RETURNING sp_id",
	}
	for _, fragment := range required {
		if !strings.Contains(lockSectorStateSQL, fragment) {
			t.Fatalf("sector state lock SQL is missing %q", fragment)
		}
	}
	if strings.Contains(lockSectorStateSQL, "SET allocated = EXCLUDED.allocated") {
		t.Fatal("sector state lock must not reset an existing allocation bitfield")
	}
}

func TestSectorStateLockHonorsCanceledContextBeforeQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := LockSectorState(ctx, nil, 1000)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lock error = %v, want context cancellation", err)
	}
}
