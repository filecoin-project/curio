package harmonydb

import (
	"testing"
)

// TestTxExec_RetriesInsideTransaction documents that Tx.Exec incorrectly
// uses backoffForSerializationError to retry individual statements inside
// a transaction.
//
// Bug location: userfuncs.go lines 262-270
//
// The issue:
//   func (t *Tx) Exec(sql rawStringOnly, arguments ...any) (count int, err error) {
//       res, err := backoffForSerializationError(func() (pgconn.CommandTag, error) {
//           return t.Tx.Exec(t.ctx, string(sql), arguments...)
//       })
//       // ...
//   }
//
// Why this is wrong:
// When a serialization error occurs inside a transaction, PostgreSQL/YugabyteDB
// aborts the entire transaction. The transaction enters an error state where
// ALL subsequent statements will fail with "current transaction is aborted".
// You MUST rollback and retry the ENTIRE transaction, not individual statements.
//
// What happens:
// 1. Transaction starts
// 2. Statement 1 succeeds
// 3. Statement 2 gets serialization error -> transaction is now ABORTED
// 4. backoffForSerializationError retries statement 2
// 5. Retry fails with "current transaction is aborted"
// 6. All 7 retries fail the same way
// 7. Error is returned (and lost due to the shadowing bug above)
//
// Correct behavior:
// Transaction-level retry should be handled by the caller using OptionRetry()
// on BeginTransaction, not inside Tx.Exec. Tx.Exec should NOT have any retry.
//
// Note the inconsistency:
// - Tx.Exec has backoff (wrong)
// - Tx.Query has NO backoff (correct, but inconsistent)
// - Tx.QueryRow has NO backoff (correct, but inconsistent)
func TestTxExec_RetriesInsideTransaction(t *testing.T) {
	// this test documents the incorrect behavior by comparing Tx methods

	t.Log("Tx.Exec wraps with backoffForSerializationError (line 262-265)")
	t.Log("Tx.Query does NOT wrap with backoff (line 273-275)")
	t.Log("Tx.QueryRow does NOT wrap with backoff (line 279-280)")
	t.Log("")
	t.Log("This inconsistency shows Tx.Exec retry was added without understanding")
	t.Log("that serialization errors abort the entire transaction.")
	t.Log("")
	t.Log("The correct fix: remove backoffForSerializationError from Tx.Exec")
	t.Log("Transaction retry should use OptionRetry() on BeginTransaction instead.")

	// to demonstrate this with a real DB:
	// 1. start transaction
	// 2. exec statement that causes serialization error
	// 3. observe that Tx.Exec retries 7 times, all failing
	// 4. observe that each retry fails with "current transaction is aborted"
	//    (not the original serialization error)
}

// TestTxMethodsInconsistency shows the API inconsistency between Tx methods
func TestTxMethodsInconsistency(t *testing.T) {
	// Tx.Exec: HAS backoffForSerializationError (lines 262-270)
	// Tx.Query: NO backoff (lines 273-276)
	// Tx.QueryRow: NO backoff (lines 279-281)
	// Tx.Select: calls Tx.Query, so NO backoff (lines 284-291)

	t.Log("API inconsistency in Tx methods:")
	t.Log("  Tx.Exec    - has retry (WRONG)")
	t.Log("  Tx.Query   - no retry")
	t.Log("  Tx.QueryRow - no retry")
	t.Log("  Tx.Select  - no retry (calls Tx.Query)")
	t.Log("")
	t.Log("All Tx methods should have NO retry because:")
	t.Log("1. Serialization errors abort the transaction")
	t.Log("2. Retrying individual statements cannot recover")
	t.Log("3. Transaction-level retry is handled by OptionRetry()")
}
