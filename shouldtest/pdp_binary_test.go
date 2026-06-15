package shouldtest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/curiostorage/shouldtest/config"
)

// TestPDPBinaryBuilds ensures cmd/pdp compiles (skipped when SKIP_PDP_BUILD is set).
func TestPDPBinaryBuilds(t *testing.T) {
	if os.Getenv("SKIP_PDP_BUILD") != "" {
		t.Skip("SKIP_PDP_BUILD set")
	}

	root, err := config.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "pdp")
	cmd := exec.Command("go", "build", "-tags", "cunative nosupraseal pdp", "-o", out, "./cmd/pdp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_LDFLAGS_ALLOW=.*", "CGO_ENABLED=0")
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/pdp: %v\n%s", err, outBytes)
	}
}
