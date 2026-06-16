//go:build skiff && darwin

package pdpnode

import (
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

func listMountPoints() ([]string, error) {
	count, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}

	buf := make([]unix.Statfs_t, count)
	count, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var mounts []string

	for i := 0; i < count; i++ {
		mountPoint := filepath.Clean(unix.ByteSliceToString(buf[i].Mntonname[:]))
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
