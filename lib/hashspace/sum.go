package hashspace

import (
	"math"
	"os"
	"path/filepath"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/fs2"
)

// sizesForRanges returns one byte count per range. A folder with a single
// range uses its used counter. Several ranges are summed with fs2, which is
// the rebalance helper and not the write path.
func sizesForRanges(dir string, ranges []HashRange, folderUsed int64) ([]int64, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	if len(ranges) == 1 {
		return []int64{folderUsed}, nil
	}
	out := make([]int64, len(ranges))
	for i, r := range ranges {
		n, err := sumInterval(dir, r.Start, r.End)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

func sumInterval(dir, start, end string) (int64, error) {
	low, high := start, end
	if start != "" && start == end {
		low, high = "", ""
	}
	res, err := fs2.SumFileSizesInterval(dir, low, high, 0)
	if err != nil {
		return 0, err
	}
	n := res.Bytes
	if stringHashInInterval(layoutFile, low, high) {
		info, statErr := os.Stat(filepath.Join(dir, layoutFile))
		if statErr != nil && !os.IsNotExist(statErr) {
			return 0, statErr
		}
		if statErr == nil && info.Mode().IsRegular() {
			if info.Size() < 0 {
				return 0, xerrors.Errorf("negative layout size in %s", dir)
			}
			sz := uint64(info.Size())
			if n < sz {
				return 0, xerrors.Errorf("range sum does not cover layout.json in %s", dir)
			}
			n -= sz
		}
	}
	if n > uint64(math.MaxInt64) {
		return 0, xerrors.Errorf("range sum overflows")
	}
	return int64(n), nil
}

func stringHashInInterval(hash, low, high string) bool {
	if low != "" && high != "" && low > high {
		return hash > low || hash <= high
	}
	if low != "" && hash <= low {
		return false
	}
	if high != "" && hash > high {
		return false
	}
	return true
}
