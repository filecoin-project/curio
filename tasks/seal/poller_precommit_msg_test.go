package seal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrecommitBatchSQLExcludesFailedSectors(t *testing.T) {
	normalize := func(query string) string {
		return strings.ToLower(strings.Join(strings.Fields(query), " "))
	}

	candidateSQL := normalize(PRECOMMIT_BATCH_CANDIDATES_SQL)
	require.Contains(t, candidateSQL, "and p.failed = false")
	require.Less(t,
		strings.Index(candidateSQL, "and p.failed = false"),
		strings.Index(candidateSQL, "), numbered as"),
		"failed sectors must be removed before batches are numbered",
	)

	assignmentSQL := normalize(PRECOMMIT_BATCH_ASSIGN_SQL)
	require.Contains(t, assignmentSQL, "and failed = false")
	require.Contains(t, assignmentSQL, "and sector_number = any($4::bigint[])")
}
