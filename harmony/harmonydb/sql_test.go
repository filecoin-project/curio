package harmonydb

import (
	"strings"
	"testing"
)

// TestNoDuplicateMigrationDatePrefixes verifies that no two SQL migration files
// share the same YYYYMMDD date prefix. Migrations are applied in lexicographic
// order, so duplicate prefixes create ambiguous ordering.
func TestNoDuplicateMigrationDatePrefixes(t *testing.T) {
	entries, err := upgradeFS.ReadDir("sql")
	if err != nil {
		t.Fatalf("reading embedded sql directory: %v", err)
	}

	seen := make(map[string]string) // date prefix → first filename
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Date prefix is everything before the first '-'.
		prefix, _, ok := strings.Cut(name, "-")
		if !ok {
			continue
		}

		if prev, exists := seen[prefix]; exists {
			t.Errorf("duplicate date prefix %q:\n  %s\n  %s", prefix, prev, name)
		} else {
			seen[prefix] = name
		}
	}
}
