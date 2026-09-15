package hashspacesolver

import (
	"bytes"
	"math/big"
)

// SliceSize returns how much of r's data lies in (startHash, endHash].
// startHash is the EndHash of the preceding range. Data is treated as
// uniform across the hash interval. The full range is SliceSize(r, start, r.EndHash).
func SliceSize(r Range, startHash, endHash []byte) int64 {
	if r.Size <= 0 || len(r.EndHash) == 0 {
		return 0
	}
	if bytes.Equal(endHash, r.EndHash) {
		return r.Size
	}
	if bytes.Equal(endHash, startHash) {
		return 0
	}

	total := interval(startHash, r.EndHash)
	part := interval(startHash, endHash)
	if total.Sign() == 0 {
		return 0
	}
	n := new(big.Int).Mul(big.NewInt(r.Size), part)
	return n.Div(n, total).Int64()
}

// StartHash returns the start of ranges[i] (the previous range's EndHash).
func StartHash(ranges []Range, i int) []byte {
	n := len(ranges)
	if n == 0 || i < 0 || i >= n {
		return nil
	}
	return cloneHash(ranges[(i-1+n)%n].EndHash)
}

func splitHash(r Range, startHash []byte, prefixSize int64) []byte {
	return splitHashBound(r, startHash, prefixSize, false)
}

func splitHashMin(r Range, startHash []byte, prefixSize int64) []byte {
	return splitHashBound(r, startHash, prefixSize, true)
}

func splitHashBound(r Range, startHash []byte, prefixSize int64, ceil bool) []byte {
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
	return addHash(startHash, delta)
}

func interval(from, to []byte) *big.Int {
	n := hashLen(from, to)
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

func hashLen(a, b []byte) int {
	if len(a) > len(b) {
		return len(a)
	}
	return len(b)
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

func addHash(start []byte, delta *big.Int) []byte {
	n := len(start)
	if n == 0 {
		return nil
	}
	space := hashSpace(n)
	v := hashInt(start, n)
	v.Add(v, delta)
	v.Mod(v, space)
	return intHash(v, n)
}

func intHash(v *big.Int, n int) []byte {
	raw := v.Bytes()
	out := make([]byte, n)
	if len(raw) > n {
		copy(out, raw[len(raw)-n:])
		return out
	}
	copy(out[n-len(raw):], raw)
	return out
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
