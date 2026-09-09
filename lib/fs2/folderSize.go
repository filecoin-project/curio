// Package fs2 sums logical sizes of regular files under a directory tree.
// Each file's hash is its path relative to the root with separators removed,
// so ab/cdefg is the hash abcdefg. Bounds use the same half-open convention
// as hashspacesolver.Transfer: (low, high]. An empty low or high leaves that
// side open.
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
