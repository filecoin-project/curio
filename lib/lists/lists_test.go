package lists

import (
	"fmt"
	"sort"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestUniqNoAlloc(t *testing.T) {
	tests := [][]int{ // note: sort() is already tested, so these tests are sorted.
		{1},
		{1, 2, 3, 4, 5},
		{1, 2, 2, 3, 4, 4, 5},
		{1, 1, 1, 1, 1, 1, 1},
		{1, 2, 2, 3},
		{1, 2, 2},
		{1, 1},
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
