// Package fs2 sums logical sizes of CID-named regular files under a directory tree.
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
	Bytes    int64
	Files    int64
	Vanished int64
}

func checkSumArgs(directory, low, high string, queueDepth uint32) error {
	if strings.IndexByte(directory, 0) >= 0 {
		return fmt.Errorf("sum file sizes: directory cannot contain a NUL byte")
	}
	if !cidCharsOnly(low) || !cidCharsOnly(high) {
		return fmt.Errorf("sum file sizes: bounds must contain only base32 CID characters")
	}
	if queueDepth > 4096 {
		return fmt.Errorf("sum file sizes: queue depth %d exceeds 4096", queueDepth)
	}
	return nil
}

// cidCharsOnly reports whether s is empty or uses the CIDv1 base32 alphabet (a-z, 2-7).
func cidCharsOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
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

const pathMax = 4096

func joinHash(prefix, name string) (string, error) {
	if len(prefix)+len(name) >= pathMax {
		return "", fmt.Errorf("hash path exceeds PATH_MAX: %s%s", prefix, name)
	}
	return prefix + name, nil
}

func joinPath(dir, name string) (string, error) {
	extra := 0
	if len(dir) > 0 && dir[len(dir)-1] != '/' {
		extra = 1
	}
	if len(dir)+extra+len(name) >= pathMax {
		return "", fmt.Errorf("path exceeds PATH_MAX under %s: %s", dir, name)
	}
	if extra == 1 {
		return dir + "/" + name, nil
	}
	return dir + name, nil
}

func hashFromRel(rel string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' {
			return -1
		}
		return r
	}, rel)
}
