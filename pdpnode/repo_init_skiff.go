//go:build maxboom

package pdpnode

import "os"

func ensureRepo(repoPath string) error {
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		return os.MkdirAll(repoPath, 0o755)
	}
	return nil
}
