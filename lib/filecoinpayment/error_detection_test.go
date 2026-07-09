package filecoinpayment

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestIsSkippableSettleRailError(t *testing.T) {
	insufficientFundsSelector := paymentErrorSelectorForTest(t, "InsufficientFundsForSettlement")

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
			name:     "RailInactiveOrSettled selector in wrapped estimate error",
			err:      errors.New("failed to estimate gas: chain: message execution failed revert reason=[0x" + ErrSelectorRailInactiveOrSettled + "]"),
			expected: true,
		},
		{
			name:     "InsufficientFundsForSettlement is not skippable",
			err:      errors.New("execution reverted: 0x" + insufficientFundsSelector),
			expected: false,
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

	for _, name := range skippableSettleRailErrorNames {
		selector := paymentErrorSelectorForTest(t, name)
		tests = append(tests, struct {
			name     string
			err      error
			expected bool
		}{
			name:     name + " selector",
			err:      errors.New("execution reverted: 0x" + selector),
			expected: true,
		})
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isSkippableSettleRailError(tc.err)
			if result != tc.expected {
				t.Errorf("isSkippableSettleRailError(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

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

func paymentErrorSelectorForTest(t *testing.T, name string) string {
	t.Helper()

	parsed, err := PaymentsMetaData.GetAbi()
	if err != nil {
		t.Fatalf("failed to parse Payments ABI: %v", err)
	}

	e, ok := parsed.Errors[name]
	if !ok {
		t.Fatalf("Payments ABI missing %s error", name)
	}

	return hex.EncodeToString(e.ID[:4])
}
