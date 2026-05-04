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

func TestCommitIDPrefix(t *testing.T) {
	saved := CurrentCommit
	t.Cleanup(func() { CurrentCommit = saved })

	CurrentCommit = "+git_abcdef12345678_2024-01-01T00:00:00Z"
	if got := CommitIDPrefix(); got != "abcdef1" {
		t.Fatalf("got %q want abcdef1", got)
	}
	CurrentCommit = "+git_abcd_2024-01-01T00:00:00Z"
	if got := CommitIDPrefix(); got != "abcd" {
		t.Fatalf("got %q want abcd", got)
	}
	CurrentCommit = ""
	if got := CommitIDPrefix(); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}
