package pdpv0

import (
	"errors"
	"testing"
)

func TestIsUnrecoverableError(t *testing.T) {
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
			name:     "DataSetPaymentBeyondEndEpoch by selector",
			err:      errors.New("failed to estimate gas: execution reverted: 0xd7c45de5000000000000"),
			expected: true,
		},
		{
			name:     "DataSetPaymentAlreadyTerminated by selector",
			err:      errors.New("execution reverted: 0xe3f8fa35"),
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      errors.New("network timeout"),
			expected: false,
		},
		{
			name:     "contract revert is not termination",
			err:      errors.New("execution reverted: 0x96ed3e73"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsUnrecoverableError(tc.err)
			if result != tc.expected {
				t.Errorf("IsUnrecoverableError(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

func TestIsContractRevert(t *testing.T) {
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
			name:     "execution reverted",
			err:      errors.New("failed to estimate gas: execution reverted: 0x96ed3e73"),
			expected: true,
		},
		{
			name:     "vm execution error",
			err:      errors.New("vm execution error: something went wrong"),
			expected: true,
		},
		{
			name:     "filecoin evm exit code 33",
			err:      errors.New("message failed (exit=[33], revert reason=[...])"),
			expected: true,
		},
		{
			name:     "retcode 33",
			err:      errors.New("call failed with RetCode=33"),
			expected: true,
		},
		{
			name:     "network timeout is not revert",
			err:      errors.New("connection timeout"),
			expected: false,
		},
		{
			name:     "rpc error is not revert",
			err:      errors.New("rpc error: server unavailable"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsContractRevert(tc.err)
			if result != tc.expected {
				t.Errorf("IsContractRevert(%v) = %v, want %v", tc.err, result, tc.expected)
			}
		})
	}
}

func TestCalculateBackoffBlocks(t *testing.T) {
	tests := []struct {
		failures int
		expected int
	}{
		{0, 0},
		{1, 100},
		{2, 200},
		{3, 400},
		{4, 800},
		{5, 1600},
		{10, MaxBackoffBlocks}, // capped
	}

	for _, tc := range tests {
		result := CalculateBackoffBlocks(tc.failures)
		if result != tc.expected {
			t.Errorf("CalculateBackoffBlocks(%d) = %d, want %d", tc.failures, result, tc.expected)
		}
	}
}
