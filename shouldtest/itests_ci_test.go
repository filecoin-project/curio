package shouldtest_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/curiostorage/shouldtest/config"
)

// TestItestsInCI ensures every itests/*_test.go file has a dedicated matrix
// entry in .github/workflows/ci.yml (test-all excludes the itests package).
func TestItestsInCI(t *testing.T) {
	root, err := config.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}

	ciYAML, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}

	inCI := map[string]struct{}{}
	re := regexp.MustCompile(`target:\s*"\./(itests/[^"]+_test\.go)"`)
	for _, m := range re.FindAllStringSubmatch(string(ciYAML), -1) {
		inCI[m[1]] = struct{}{}
	}

	entries, err := filepath.Glob(filepath.Join(root, "itests", "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}

	var missing []string
	for _, path := range entries {
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if _, ok := inCI[rel]; !ok {
			missing = append(missing, rel)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("itests not referenced in .github/workflows/ci.yml test matrix:\n  %s\nadd a matrix entry with target: \"./%s\"",
			strings.Join(missing, "\n  "), missing[0])
	}
}
