//go:build skiff && (freebsd || openbsd)

package pdpnode

import (
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

func listMountPoints() ([]string, error) {
	stat, err := unix.Getmntinfo(unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var mounts []string

	for _, entry := range stat {
		mountPoint := filepath.Clean(entry.F_mntonname)
		if mountPoint == "" {
			continue
		}
		if _, ok := seen[mountPoint]; ok {
			continue
		}
		seen[mountPoint] = struct{}{}
		mounts = append(mounts, mountPoint)
	}

	sort.Strings(mounts)
	return mounts, nil
}
