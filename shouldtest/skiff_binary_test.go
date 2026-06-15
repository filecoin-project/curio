package shouldtest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/curiostorage/shouldtest/config"
)

// TestSkiffBinaryBuilds ensures cmd/skiff compiles (skipped when SKIP_SKIFF_BUILD is set).
func TestSkiffBinaryBuilds(t *testing.T) {
	if os.Getenv("SKIP_SKIFF_BUILD") != "" {
		t.Skip("SKIP_SKIFF_BUILD set")
	}

	root, err := config.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "skiff")
	cmd := exec.Command("go", "build", "-tags", "cunative nosupraseal skiff", "-o", out, "./cmd/skiff")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_LDFLAGS_ALLOW=.*", "CGO_ENABLED=0")
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/skiff: %v\n%s", err, outBytes)
	}
}
