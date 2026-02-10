package build

import (
	"testing"
)

// verify version is set from BuildVersionArray, not hardcoded placeholder
func TestBuildVersionNotZero(t *testing.T) {
	if BuildVersion == "0.0.0" {
		t.Fatal("BuildVersion should not be 0.0.0")
	}
	if BuildVersion == "" {
		t.Fatal("BuildVersion should not be empty")
	}
}
