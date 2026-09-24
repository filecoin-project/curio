//go:build linux || darwin

package hashspace

import (
	"math"

	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
)

func filesystemCapacity(root string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(root, &st); err != nil {
		return 0, xerrors.Errorf("statfs %s: %w", root, err)
	}
	if st.Bsize <= 0 {
		return 0, xerrors.Errorf("statfs %s: invalid block size %d", root, st.Bsize)
	}
	bsize := uint64(st.Bsize)
	if bsize != 0 && st.Blocks > math.MaxUint64/bsize {
		return 0, xerrors.Errorf("statfs %s: capacity overflows", root)
	}
	total := st.Blocks * bsize
	if total > uint64(math.MaxInt64) {
		return math.MaxInt64, nil
	}
	return int64(total), nil
}
