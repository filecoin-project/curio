package itests

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNoBuildFolder(t *testing.T) {
	// run on command line: go mod why github.com/filecoin-project/lotus/build
	res, err := exec.Command("go", "mod", "why", "github.com/filecoin-project/lotus/build").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run go mod why: %v", err)
	}
	if len(strings.Split(string(res), "\n")) > 2 {
		t.Fatalf("unexpected output: %s", res)
	}
}
