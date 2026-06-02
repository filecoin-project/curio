package shouldtest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/curiostorage/shouldtest"
	"github.com/curiostorage/shouldtest/config"
)

func libProofDir(t *testing.T) string {
	t.Helper()
	root, err := config.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "lib/proof")
}

func TestDependencyDirs_libProof(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go list integration in short mode")
	}

	moduleRoot, err := config.ModuleRoot()
	if err != nil {
		t.Fatal(err)
	}

	dirs, err := shouldtest.DependencyDirs([]string{"./lib/proof/..."}, []string{"debug", "fvm", "nosupraseal"}, moduleRoot)
	if err != nil {
		t.Fatal(err)
	}

	var foundProof, foundFFI bool
	for _, d := range dirs {
		if d == "lib/proof" || strings.HasPrefix(d, "lib/proof/") {
			foundProof = true
		}
		if strings.HasPrefix(d, "extern/filecoin-ffi") {
			foundFFI = true
		}
	}
	if !foundProof {
		t.Fatalf("expected lib/proof in deps, got %v", dirs)
	}
	if !foundFFI {
		t.Fatalf("expected extern/filecoin-ffi in deps, got %v", dirs)
	}
}

func TestLoadProfile_libProof(t *testing.T) {
	p, err := shouldtest.LoadProfile(libProofDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if p.CoverageFile != "test-lib-proof.out" {
		t.Fatalf("got %+v", p)
	}
}

func TestRunCI_skipWritesCoverage(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "output")
	if err := os.WriteFile(outPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_OUTPUT", outPath)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REF", "refs/heads/feature")
	base, err := exec.Command("git", "merge-base", "main", "HEAD").Output()
	if err != nil {
		t.Skip("need git merge-base with main:", err)
	}
	t.Setenv("GITHUB_BASE_SHA", strings.TrimSpace(string(base)))
	t.Setenv("GITHUB_HEAD_SHA", "HEAD")

	covDir := filepath.Join(dir, "coverage")
	repo, _, err := config.LoadFromModule()
	if err != nil {
		t.Fatal(err)
	}
	res, err := shouldtest.RunCI(shouldtest.CIConfig{
		ProfileDir:  libProofDir(t),
		CoverageDir: covDir,
		Repo:        repo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Run {
		t.Skip("workspace has matching changes; skip stub test")
	}
	if !res.SkippedOnCI {
		t.Fatal("expected coverage stub")
	}
	data, err := os.ReadFile(filepath.Join(covDir, "test-lib-proof.out"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "mode: atomic\n" {
		t.Fatalf("got %q", data)
	}
	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "run=false\n" {
		t.Fatalf("GITHUB_OUTPUT: %q", out)
	}
}
