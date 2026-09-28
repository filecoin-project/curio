// Package fs2 sums logical sizes of CID-named regular files under a directory tree.
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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Result describes the regular files successfully included in a range sum.
type Result struct {
	Bytes    int64
	Files    int64
	Vanished int64
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
	return Result{
		Bytes:    hi.Bytes + lo.Bytes,
		Files:    hi.Files + lo.Files,
		Vanished: hi.Vanished + lo.Vanished,
	}, nil
}

// ListHashesInterval returns up to limit regular-file hashes in (low, high],
// lexicographic order, strictly after `after`. An empty low or high leaves
// that side open. When low sorts after high the interval wraps. When low
// and high are equal and non-empty, the interval is the whole circle.
func ListHashesInterval(directory, low, high, after string, limit int) ([]string, error) {
	if err := checkSumArgs(directory, low, high, 0); err != nil {
		return nil, err
	}
	if strings.IndexByte(after, 0) >= 0 {
		return nil, fmt.Errorf("list hashes: bounds cannot contain NUL bytes")
	}
	if limit <= 0 {
		return nil, nil
	}
	if low != "" && low == high {
		low, high = "", ""
	}
	var out []string
	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hash := hashFromRel(rel)
		if d.IsDir() {
			if !subtreeInInterval(hash, low, high) || prefixAtOrBefore(hash, after) {
				return filepath.SkipDir
			}
			return nil
		}
		if !hashInInterval(hash, low, high) || (after != "" && hash <= after) {
			return nil
		}
		mode := d.Type()
		if mode != 0 && !mode.IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		out = append(out, hash)
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list hashes: %w", err)
	}
	return out, nil
}

func hashInInterval(hash, low, high string) bool {
	if low != "" && high != "" && low > high {
		return hashInRange(hash, low, "") || hashInRange(hash, "", high)
	}
	return hashInRange(hash, low, high)
}

func subtreeInInterval(prefix, low, high string) bool {
	if low != "" && high != "" && low > high {
		return subtreeCanMatch(prefix, low, "") || subtreeCanMatch(prefix, "", high)
	}
	return subtreeCanMatch(prefix, low, high)
}

// prefixAtOrBefore reports whether every hash under prefix sorts at or
// before after, so the directory cannot contain a later hash.
func prefixAtOrBefore(prefix, after string) bool {
	if after == "" || prefix == "" {
		return false
	}
	if strings.HasPrefix(after, prefix) {
		return false
	}
	return prefix < after
}

func addResult(a, b Result) (Result, error) {
	return Result{
		Bytes:    a.Bytes + b.Bytes,
		Files:    a.Files + b.Files,
		Vanished: a.Vanished + b.Vanished,
	}, nil
}
