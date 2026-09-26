//go:build linux || darwin

package hashspace

import (
	"math"

	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
)

func filesystemCapacity(root string) (int64, error) {
	total, _, err := filesystemStat(root)
	return total, err
}

func filesystemFree(root string) (int64, error) {
	_, free, err := filesystemStat(root)
	return free, err
}

// filesystemStat returns the total size and the bytes available to this
// process on the filesystem holding root.
func filesystemStat(root string) (int64, int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(root, &st); err != nil {
		return 0, 0, xerrors.Errorf("statfs %s: %w", root, err)
	}
	if st.Bsize <= 0 {
		return 0, 0, xerrors.Errorf("statfs %s: invalid block size %d", root, st.Bsize)
	}
	bsize := uint64(st.Bsize)
	return blocksToBytes(uint64(st.Blocks), bsize), blocksToBytes(uint64(st.Bavail), bsize), nil
}

func blocksToBytes(blocks, bsize uint64) int64 {
	if bsize != 0 && blocks > math.MaxUint64/bsize {
		return math.MaxInt64
	}
	n := blocks * bsize
	if n > uint64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(n)
}
