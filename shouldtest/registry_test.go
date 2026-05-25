package shouldtest_test

import (
	"testing"

	"github.com/curiostorage/shouldtest/config"
	"github.com/curiostorage/shouldtest/registry"
)

// TestRepoCIRegistry ensures every package shouldtest.json is referenced in CI
// per .github/shouldtest.json registry settings.
func TestRepoCIRegistry(t *testing.T) {
	repo, root, err := config.LoadFromModule()
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(root, repo.RegistryConfig()); err != nil {
		t.Fatal(err)
	}
}
