package lists

import (
	"fmt"
	"sort"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// hasDuplicates checks if the slice has duplicate elements (for ordered types)
func hasDuplicates[T comparable](list []T) bool {
	seen := make(map[T]struct{})
	for _, v := range list {
		if _, ok := seen[v]; ok {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}

func TestUniqNoAlloc(t *testing.T) {
	tests := [][]int{ // note: sort() is already tested, so these tests are sorted.
		{1},
		{1, 2, 3, 4, 5},
		{1, 2, 2, 3, 4, 4, 5},
		{1, 1, 1, 1, 1, 1, 1},
		{1, 2, 2, 3},
		{1, 2, 2},
		{1, 1},
		{1, 1, 2, 2},
	}
	for i, test := range tests {
		expected := lo.Uniq(test)
		sort.Slice(expected, func(i, j int) bool {
			return test[i] < test[j]
		})
		actual := UniqNoAlloc(test)
		sortedActual := make([]int, len(actual))
		copy(sortedActual, actual)
		sort.Slice(sortedActual, func(i, j int) bool {
			return sortedActual[i] < sortedActual[j]
		})
		require.Equal(t, expected, sortedActual, fmt.Sprintf("test %d: %v", i, actual))
	}
}

// TestUniqNoAllocFuzz tries to find inputs that produce non-unique results
func TestUniqNoAllocFuzz(t *testing.T) {
	// Test various inputs that might expose bugs
	testCases := [][]int{
		{1, 1, 1, 2, 2, 2},
		{1, 2, 1, 2, 1, 2},
		{2, 2, 2, 1, 1, 1},
		{1, 2, 3, 1, 2, 3},
		{1, 1, 2, 2, 2, 3, 3, 3},
		{3, 2, 1, 3, 2, 1},
		{1, 1, 1, 1, 2},
		{1, 2, 2, 2, 2},
		{1, 1, 2, 2},
		{5, 4, 3, 2, 1, 1, 2, 3, 4, 5},
	}
	for i, input := range testCases {
		inputCopy := make([]int, len(input))
		copy(inputCopy, input)
		result := UniqNoAlloc(input)
		require.False(t, hasDuplicates(result), "test %d: input %v produced non-unique result %v", i, inputCopy, result)
	}
}
