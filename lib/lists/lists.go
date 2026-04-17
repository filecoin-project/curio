package lists

import (
	"bytes"
	"cmp"
	"sort"
)

// UniqNoAlloc removes duplicates from a slice, returning it resliced.
// No allocations are performed, but the list gets reordered and should not be used.
// Ordering is required.
func UniqNoAlloc[T cmp.Ordered](list []T) []T {
	return UniqNoAllocWithComparator(list, func(i, j int) bool {
		return list[i] < list[j]
	})
}

// UniqNoAllocWithComparator removes duplicates from a slice (of some kind of ordered type).
// The return is resliced and entries reordered, so the original list ref should not be used.
// Provide the less function to compare two elements.
func UniqNoAllocWithComparator[T any](list []T, less func(a, b int) bool) []T {
	if len(list) < 2 {
		return list
	}

	sort.Slice(list, less)

	// only 2 stack ints and the idx
	matchTarget := 0
	j := len(list) - 1

outer:
	for i := 1; i <= j; i++ { // i = index, j last unique's index
		if !less(matchTarget, i) && !less(i, matchTarget) { // equal: neither less than the other
			for !less(j-1, j) { // clean up the end so we copy only unique elements.
				j--
				if j <= i {
					break outer
				}
			}
			list[i] = list[j] // Grab a clean value from the end to replace the dup.
			j--
		} else {
			matchTarget = i // new values need cleanness testing against upcoming values.
		}
	}
	for j > 0 && !less(j-1, j) { // maybe clean the end.
		j--
	}
	return list[:max(j+1, 1)]
}

func UniqNoAllocByteArray[T ~[16]byte | ~[20]byte | ~[32]byte](list []T) []T {
	return UniqNoAllocWithComparator(list, func(i, j int) bool {
		for k := 0; k < len(list[i]); k++ {
			if list[i][k] < list[j][k] {
				return true
			}
			if list[i][k] > list[j][k] {
				return false
			}
		}
		return false
	})
}

func UniqNoAllocBytes[T ~[]byte](list []T) []T {
	return UniqNoAllocWithComparator(list, func(i, j int) bool {
		return bytes.Compare(list[i], list[j]) < 0
	})
}
