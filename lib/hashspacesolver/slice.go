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

// checkRange rejects a negative size, an empty EndHash, or a StartHash
// whose length differs from EndHash. It does not check that the hashes
// tile a circle.
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

// onRange reports whether h is r.StartHash or lies on (r.StartHash, r.EndHash].
// A hash of a different length is not on the range. r should already pass checkRange.
func onRange(r Range, h []byte) bool {
	if len(h) != len(r.EndHash) {
		return false
	}
	return hashEq(h, r.StartHash) || pointInArc(r.StartHash, r.EndHash, h)
}

// rangeOffset is the hash distance from r.StartHash to h. h must lie on r.
// isEnd places r.EndHash at the far end of a full-circle range rather than
// at zero; pass true when h is the end of the window.
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
// h must lie on r. isEnd places r.EndHash at r.Size on a full-circle range;
// pass true when h is the end of the window. A non-positive r.Size yields 0.
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

// splitHash is the hash prefixSize bytes after r.StartHash, rounding the
// hash step down. prefixSize <= 0 returns StartHash; prefixSize >= Size
// returns EndHash. r must have equal-length hashes. A zero-length interval
// returns EndHash.
func splitHash(r Range, prefixSize int64) []byte {
	return splitHashBound(r, prefixSize, false)
}

// splitHashBound is the hash prefixSize bytes after r.StartHash. ceil rounds
// the hash step up so the prefix holds at least prefixSize bytes when the
// division is inexact; otherwise the step rounds down. prefixSize <= 0
// returns StartHash. prefixSize >= Size, or a zero-length interval, returns
// EndHash. r must have equal-length hashes. An empty StartHash returns nil.
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
	n := len(startHash)
	if n == 0 {
		return nil
	}
	space := hashModulus(n)
	v := hashInt(startHash, n)
	v.Add(v, delta)
	v.Mod(v, space)
	raw := v.Bytes()
	out := make([]byte, n)
	if len(raw) > n {
		copy(out, raw[len(raw)-n:])
		return out
	}
	copy(out[n-len(raw):], raw)
	return out
}

// interval is the forward hash distance from `from` to `to` on a circle of
// 2^(8*n) values, where n is the longer hash. Equal hashes are a full
// circle, not zero.
func interval(from, to []byte) *big.Int {
	n := len(from)
	if len(to) > n {
		n = len(to)
	}
	if n == 0 {
		return new(big.Int)
	}
	space := hashModulus(n)
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

// hashModulus is 2^(8*n), the number of distinct n-byte hashes.
// n is a hash width in bytes.
func hashModulus(n int) *big.Int {
	return new(big.Int).Lsh(big.NewInt(1), uint(8*n))
}

// hashInt reads the last n bytes of h as a big-endian integer, and
// left-pads when h is shorter. n is the hash width in bytes.
func hashInt(h []byte, n int) *big.Int {
	if len(h) >= n {
		return new(big.Int).SetBytes(h[len(h)-n:])
	}
	padded := make([]byte, n)
	copy(padded[n-len(h):], h)
	return new(big.Int).SetBytes(padded)
}

// cloneHash copies h. A nil hash stays nil.
func cloneHash(h []byte) []byte {
	if h == nil {
		return nil
	}
	out := make([]byte, len(h))
	copy(out, h)
	return out
}

// hashEq reports whether a and b are equal. A nil hash equals an empty one.
func hashEq(a, b []byte) bool {
	return bytes.Equal(a, b)
}
