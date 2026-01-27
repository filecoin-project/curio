package harmonydb

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/yugabyte/pgx/v5/pgconn"
)

// after exhausting retries, returned error should wrap the original.
func TestBackoffSerializationError_PreservesOriginal(t *testing.T) {
	// save and restore original backoffs
	orig := backoffs
	backoffs = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { backoffs = orig }()

	serializationErr := &pgconn.PgError{Code: pgerrcode.SerializationFailure}

	_, err := backoffForSerializationError(func() (int, error) {
		return 0, serializationErr
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	if !errors.Is(err, serializationErr) {
		// check for the telltale nil wrap
		if strings.Contains(err.Error(), "<nil>") {
			t.Fatalf("err := inside loop shadows return param, wraps nil instead of original")
		}
		t.Fatalf("original error not preserved: %v", err)
	}
}
