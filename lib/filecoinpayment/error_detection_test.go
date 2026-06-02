package filecoinpayment

import (
	"errors"
	"testing"
)

func TestIsRailInactiveOrSettledError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "RailInactiveOrSettled selector",
			err:      errors.New("execution reverted: 0x" + ErrSelectorRailInactiveOrSettled),
			expected: true,
		},
		{
			name:     "contract revert with unrelated selector",
			err:      errors.New("execution reverted: 0x96ed3e73"),
			expected: false,
		},
		{
			name:     "network error",
			err:      errors.New("connection timeout"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsRailInactiveOrSettledError(tc.err)
			if result != tc.expected {
				t.Errorf("IsRailInactiveOrSettledError(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}
