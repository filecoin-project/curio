// Package fs2 sums logical sizes of regular files under a directory tree.
// Each file's hash is its path relative to the root with separators removed,
// so ab/cdefg is the hash abcdefg. That is the "2,-" split: the first two
// characters are the directory and the rest is the file name.
//
// Bounds use the same half-open convention as hashspacesolver.Transfer:
// (low, high]. An empty low or high leaves that side open.
// SumFileSizesInterval also accepts a wrapped interval, where low sorts
// after high.
package fs2

import (
	"fmt"
	"strings"
)

// Result describes the regular files successfully included in a range sum.
type Result struct {
	Bytes    uint64
	Files    uint64
	Vanished uint64
}

func checkSumArgs(directory, low, high string, queueDepth uint32) error {
	if strings.IndexByte(directory, 0) >= 0 ||
		strings.IndexByte(low, 0) >= 0 ||
		strings.IndexByte(high, 0) >= 0 {
		return fmt.Errorf("sum file sizes: path and bounds cannot contain NUL bytes")
	}
	if queueDepth > 4096 {
		return fmt.Errorf("sum file sizes: queue depth %d exceeds 4096", queueDepth)
	}
	return nil
}

func hashInRange(hash, low, high string) bool {
	if low != "" && hash <= low {
		return false
	}
	if high != "" && hash > high {
		return false
	}
	return true
}

func subtreeCanMatch(prefix, low, high string) bool {
	if prefix == "" {
		return true
	}
	if high != "" && prefix > high {
		return false
	}
	if low == "" || prefix >= low {
		return true
	}
	return strings.HasPrefix(low, prefix)
}

func hashFromRel(rel string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' {
			return -1
		}
		return r
	}, rel)
}

// SumFileSizesInterval sums regular files whose concatenated path hash lies
// in the half-open interval (low, high]. When both bounds are set and low
// sorts after high, the interval wraps around the hash circle: hashes after
// low plus hashes up to high. Otherwise it matches SumFileSizesRange.
func SumFileSizesInterval(directory, low, high string, queueDepth uint32) (Result, error) {
	if low == "" || high == "" || low <= high {
		return SumFileSizesRange(directory, low, high, queueDepth)
	}
	hi, err := SumFileSizesRange(directory, low, "", queueDepth)
	if err != nil {
		return Result{}, err
	}
	lo, err := SumFileSizesRange(directory, "", high, queueDepth)
	if err != nil {
		return Result{}, err
	}
	return addResult(hi, lo)
}

func addResult(a, b Result) (Result, error) {
	if a.Bytes > ^uint64(0)-b.Bytes || a.Files > ^uint64(0)-b.Files || a.Vanished > ^uint64(0)-b.Vanished {
		return Result{}, fmt.Errorf("sum overflow")
	}
	return Result{
		Bytes:    a.Bytes + b.Bytes,
		Files:    a.Files + b.Files,
		Vanished: a.Vanished + b.Vanished,
	}, nil
}
