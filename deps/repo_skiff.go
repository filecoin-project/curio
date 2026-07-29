//go:build skiff

package deps

import "os"

func ensureCurioRepo(repoPath string) error {
	return os.MkdirAll(repoPath, 0o755)
}
