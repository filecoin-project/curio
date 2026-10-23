package hashfolder

import (
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

// sumInterval sums the piece files in dir whose hash lies in (start, end].
// start == end, both set, is the whole circle. It is shared by range sizing
// and overlap repair.
func sumInterval(dir, start, end string) (int64, error) {
	low, high := start, end
	if start != "" && start == end {
		low, high = "", ""
	}
	res, err := fs2.SumFileSizesInterval(dir, low, high, 0)
	if err != nil {
		return 0, err
	}
	return res.Bytes, nil
}
