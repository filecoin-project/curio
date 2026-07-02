package shouldtest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/curiostorage/shouldtest/config"
)

// TestCurioPDPBuilds ensures make curio-pdp compiles without CGO (skipped when SKIP_CURIO_PDP_BUILD is set).
func TestCurioPDPBuilds(t *testing.T) {
	if os.Getenv("SKIP_CURIO_PDP_BUILD") != "" {
		t.Skip("SKIP_CURIO_PDP_BUILD set")
	}

	root, err := config.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("make", "curio-pdp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "CGO_LDFLAGS_ALLOW=.*")
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make curio-pdp: %v\n%s", err, outBytes)
	}

	built := filepath.Join(root, "curio")
	if _, err := os.Stat(built); err != nil {
		t.Fatalf("expected ./curio after make curio-pdp: %v", err)
	}
}
