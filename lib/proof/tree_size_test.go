package proof

import "testing"

func TestNodeLevel(t *testing.T) {
	tests := []struct {
		leaves   int64
		arity    int64
		expected int
	}{
		{0, 2, 0},
		{1, 2, 1},
		{2, 2, 2},
		{3, 2, 3},
		{4, 2, 3},
		{5, 2, 4},
		{8, 2, 4},
		{16, 2, 5},
		{1, 3, 1},
		{3, 3, 2},
		{4, 3, 3},
		{9, 3, 3},
		{10, 3, 4},
		{27, 3, 4},
		{28, 3, 5},
		{100, 10, 3},
		{1000, 10, 4},
	}

	for _, test := range tests {
		result := NodeLevel(test.leaves, test.arity)
		if result != test.expected {
			t.Errorf("NodeLevel(%d, %d) = %d; expected %d", test.leaves, test.arity, result, test.expected)
		}
	}
}
