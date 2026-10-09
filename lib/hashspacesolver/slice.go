package hashspacesolver

import (
	"bytes"
	"math/big"
	"slices"

	"golang.org/x/xerrors"
)

// SliceSize returns how much of r's data lies in (start, end]. Both bounds
// must lie on r, with start no later than end. Data is treated as uniform
// across r, so the window is measured from r.StartHash:
// SliceSize(r, r.StartHash, r.EndHash) is r.Size, and a suffix ending at
// r.EndHash is r.Size minus the prefix before it.
func SliceSize(r Range, start, end []byte) (int64, error) {
	if err := checkRange(r); err != nil {
		return 0, err
	}
	if !onRange(r, start) || !onRange(r, end) {
		return 0, xerrors.Errorf("window (%x, %x] is not on range (%x, %x]", start, end, r.StartHash, r.EndHash)
	}
	if rangeOffset(r, start, false).Cmp(rangeOffset(r, end, true)) > 0 {
		return 0, xerrors.Errorf("window (%x, %x] runs backwards on range (%x, %x]", start, end, r.StartHash, r.EndHash)
	}
	return sliceSize(r, start, end), nil
}

// LinkStarts sets each range's StartHash to the EndHash of the range before
// it in hash order. It is for callers that only store end hashes.
func LinkStarts(ranges []Range) {
	order := make([]int, len(ranges))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return bytes.Compare(ranges[a].EndHash, ranges[b].EndHash)
	})
	for k, i := range order {
		prev := order[(k-1+len(order))%len(order)]
		ranges[i].StartHash = cloneHash(ranges[prev].EndHash)
	}
}

func checkRange(r Range) error {
	if r.Size < 0 {
		return xerrors.Errorf("range (%x, %x] has negative size", r.StartHash, r.EndHash)
	}
	if len(r.EndHash) == 0 {
		return xerrors.Errorf("range has empty EndHash")
	}
	if len(r.StartHash) != len(r.EndHash) {
		return xerrors.Errorf("range (%x, %x] StartHash length %d != EndHash length %d", r.StartHash, r.EndHash, len(r.StartHash), len(r.EndHash))
	}
	return nil
}

// checkTiling requires the ranges to cover the circle exactly once: in
// EndHash order, each range starts where the one before it ends.
func checkTiling(ranges []Range) error {
	order := make([]int, len(ranges))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return bytes.Compare(ranges[a].EndHash, ranges[b].EndHash)
	})
	for k, i := range order {
		prev := order[(k-1+len(order))%len(order)]
		if !hashEq(ranges[i].StartHash, ranges[prev].EndHash) {
			return xerrors.Errorf("range %d starts at %x, but the range before it ends at %x", i, ranges[i].StartHash, ranges[prev].EndHash)
		}
	}
	return nil
}

func onRange(r Range, h []byte) bool {
	if len(h) != len(r.EndHash) {
		return false
	}
	return hashEq(h, r.StartHash) || pointInArc(r.StartHash, r.EndHash, h)
}

// rangeOffset is the hash distance from r.StartHash to h. isEnd places
// r.EndHash at the far end of a full-circle range rather than at zero.
func rangeOffset(r Range, h []byte, isEnd bool) *big.Int {
	if isEnd && hashEq(h, r.EndHash) {
		return interval(r.StartHash, r.EndHash)
	}
	if hashEq(h, r.StartHash) {
		return new(big.Int)
	}
	return interval(r.StartHash, h)
}

// sliceSize is SliceSize without bounds checks; start and end must be on r.
func sliceSize(r Range, start, end []byte) int64 {
	return rangePos(r, end, true) - rangePos(r, start, false)
}

// rangePos returns how many of r's bytes lie in (r.StartHash, h].
func rangePos(r Range, h []byte, isEnd bool) int64 {
	if r.Size <= 0 {
		return 0
	}
	if isEnd && hashEq(h, r.EndHash) {
		return r.Size
	}
	if hashEq(h, r.StartHash) {
		return 0
	}
	total := interval(r.StartHash, r.EndHash)
	n := new(big.Int).Mul(big.NewInt(r.Size), interval(r.StartHash, h))
	return n.Div(n, total).Int64()
}

// Contains reports whether p lies in the half-open circle interval (start, end].
// The endpoint end is included and start is excluded. A start equal to end
// covers the whole circle, including that point. Callers pass equal-width hashes.
func Contains(start, end, p []byte) bool {
	if len(p) == 0 {
		return false
	}
	if bytes.Equal(start, end) {
		return true
	}
	if bytes.Compare(start, end) < 0 {
		return bytes.Compare(start, p) < 0 && bytes.Compare(p, end) <= 0
	}
	return bytes.Compare(start, p) < 0 || bytes.Compare(p, end) <= 0
}

func splitHash(r Range, prefixSize int64) []byte {
	return splitHashBound(r, prefixSize, false)
}

func splitHashBound(r Range, prefixSize int64, ceil bool) []byte {
	startHash := r.StartHash
	if prefixSize <= 0 {
		return cloneHash(startHash)
	}
	if prefixSize >= r.Size {
		return cloneHash(r.EndHash)
	}
	total := interval(startHash, r.EndHash)
	if total.Sign() == 0 {
		return cloneHash(r.EndHash)
	}
	delta := new(big.Int).Mul(total, big.NewInt(prefixSize))
	if ceil {
		delta.Add(delta, big.NewInt(r.Size-1))
	}
	delta.Div(delta, big.NewInt(r.Size))
	if delta.Cmp(total) >= 0 {
		return cloneHash(r.EndHash)
	}
	if delta.Sign() == 0 && ceil {
		delta.SetInt64(1)
	}
	return /* addHash */ func(start []byte, delta *big.Int) []byte {
		n := len(start)
		if n == 0 {
			return nil
		}
		space := hashSpace(n)
		v := hashInt(start, n)
		v.Add(v, delta)
		v.Mod(v, space)
		return /* intHash */ func(v *big.Int, n int) []byte {
			raw := v.Bytes()
			out := make([]byte, n)
			if len(raw) > n {
				copy(out, raw[len(raw)-n:])
				return out
			}
			copy(out[n-len(raw):], raw)
			return out
		}(v, n)
	}(startHash, delta)
}

func interval(from, to []byte) *big.Int {
	n := /* hashLen */ func(a, b []byte) int {
		if len(a) > len(b) {
			return len(a)
		}
		return len(b)
	}(from, to)
	if n == 0 {
		return new(big.Int)
	}
	space := hashSpace(n)
	a := hashInt(from, n)
	b := hashInt(to, n)
	if a.Cmp(b) == 0 {
		return space
	}
	d := new(big.Int).Sub(b, a)
	if d.Sign() < 0 {
		d.Add(d, space)
	}
	return d
}

func hashSpace(n int) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(8*n))
}

func hashInt(h []byte, n int) *big.Int {
	if len(h) >= n {
		return new(big.Int).SetBytes(h[len(h)-n:])
	}
	padded := make([]byte, n)
	copy(padded[n-len(h):], h)
	return new(big.Int).SetBytes(padded)
}

func cloneHash(h []byte) []byte {
	if h == nil {
		return nil
	}
	out := make([]byte, len(h))
	copy(out, h)
	return out
}

func hashEq(a, b []byte) bool {
	return bytes.Equal(a, b)
}
