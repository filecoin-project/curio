package pdpv0

import (
	"errors"
	"testing"
)

type errorClassifier struct {
	name string
	fn   func(error) bool
}

var knownErrorClassifiers = []errorClassifier{
	{name: "unrecoverable", fn: IsUnrecoverableError},
	{name: "retry same proving period", fn: IsRetrySameProvingPeriodError},
	{name: "insufficient challenge delay", fn: IsInsufficientChallengeDelayError},
	{name: "skip current proving period", fn: IsSkipCurrentProvingPeriodError},
	{name: "next proving period already called", fn: IsNextProvingPeriodAlreadyCalledError},
	{name: "proving period not initialized", fn: IsProvingPeriodNotInitializedError},
	{name: "next proving period empty dataset", fn: IsNextProvingPeriodEmptyDatasetError},
	{name: "refresh proving state", fn: IsRefreshProvingStateError},
	{name: "unexpected proving invariant", fn: IsUnexpectedProvingInvariantError},
	{name: "FWSS proving not started", fn: IsFWSSProvingNotStartedError},
	{name: "operator attention", fn: IsOperatorAttentionProvingError},
	{name: "proof generation failure", fn: IsProofGenerationFailureError},
	{name: "PDPVerifier data set not found", fn: IsPDPVerifierDataSetNotFound},
}

func selectorRevert(selector string) error {
	return errors.New("failed to estimate gas: execution reverted: 0x" + selector + "0000000000000000")
}

func reasonRevert(reason string) error {
	return errors.New("failed to estimate gas: execution reverted: " + reason)
}

func TestKnownErrorClassifiers(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantClassifier string
	}{
		{
			name:           "FWSS DataSetPaymentBeyondEndEpoch is unrecoverable",
			err:            selectorRevert(contractErrorSelector(ErrFWSSDataSetPaymentBeyondEndEpoch)),
			wantClassifier: "unrecoverable",
		},
		{
			name:           "FWSS DataSetPaymentAlreadyTerminated is unrecoverable",
			err:            selectorRevert(contractErrorSelector(ErrFWSSDataSetPaymentAlreadyTerminated)),
			wantClassifier: "unrecoverable",
		},
		{
			name:           "PDPVerifier DataSetNotLive is unrecoverable",
			err:            selectorRevert(contractErrorSelector(ErrPDPVerifierDataSetNotLive)),
			wantClassifier: "unrecoverable",
		},
		{
			name:           "FWSS ChallengeWindowTooEarly retries same proving period",
			err:            selectorRevert(contractErrorSelector(ErrFWSSChallengeWindowTooEarly)),
			wantClassifier: "retry same proving period",
		},
		{
			name:           "PDPVerifier premature proof string retries same proving period",
			err:            reasonRevert(provingRevertPrematureProof),
			wantClassifier: "retry same proving period",
		},
		{
			name:           "PDPVerifier InsufficientChallengeDelay has its own category",
			err:            selectorRevert(contractErrorSelector(ErrPDPVerifierInsufficientChallengeDelay)),
			wantClassifier: "insufficient challenge delay",
		},
		{
			name:           "FWSS ProofAlreadySubmitted skips current proving period",
			err:            selectorRevert(contractErrorSelector(ErrFWSSProofAlreadySubmitted)),
			wantClassifier: "skip current proving period",
		},
		{
			name:           "FWSS ProvingPeriodPassed skips current proving period",
			err:            selectorRevert(contractErrorSelector(ErrFWSSProvingPeriodPassed)),
			wantClassifier: "skip current proving period",
		},
		{
			name:           "FWSS NextProvingPeriodAlreadyCalled is PP-only",
			err:            selectorRevert(contractErrorSelector(ErrFWSSNextProvingPeriodAlreadyCalled)),
			wantClassifier: "next proving period already called",
		},
		{
			name:           "FWSS ProvingPeriodNotInitialized is PP preflight reset",
			err:            selectorRevert(contractErrorSelector(ErrFWSSProvingPeriodNotInitialized)),
			wantClassifier: "proving period not initialized",
		},
		{
			name:           "PDPVerifier empty dataset string stops next proving period",
			err:            reasonRevert(provingRevertNoLeavesForProvingPeriod),
			wantClassifier: "next proving period empty dataset",
		},
		{
			name:           "PDPVerifier no challenge scheduled string skips current proving period",
			err:            reasonRevert(provingRevertNoChallengeScheduled),
			wantClassifier: "skip current proving period",
		},
		{
			name:           "FWSS InvalidChallengeEpoch refreshes proving state",
			err:            selectorRevert(contractErrorSelector(ErrFWSSInvalidChallengeEpoch)),
			wantClassifier: "refresh proving state",
		},
		{
			name:           "PDPVerifier leaf index string is unexpected invariant",
			err:            reasonRevert(provingRevertLeafIndexOutOfBounds),
			wantClassifier: "unexpected proving invariant",
		},
		{
			name:           "PDPVerifier ExcessiveChallengeDelay is unexpected invariant",
			err:            selectorRevert(contractErrorSelector(ErrPDPVerifierExcessiveChallengeDelay)),
			wantClassifier: "unexpected proving invariant",
		},
		{
			name:           "FWSS OnlyPDPVerifierAllowed is unexpected invariant",
			err:            selectorRevert(contractErrorSelector(ErrFWSSOnlyPDPVerifierAllowed)),
			wantClassifier: "unexpected proving invariant",
		},
		{
			name:           "FWSS InvalidChallengeCount is unexpected invariant",
			err:            selectorRevert(contractErrorSelector(ErrFWSSInvalidChallengeCount)),
			wantClassifier: "unexpected proving invariant",
		},
		{
			name:           "FWSS ProvingNotStarted has its own category",
			err:            selectorRevert(contractErrorSelector(ErrFWSSProvingNotStarted)),
			wantClassifier: "FWSS proving not started",
		},
		{
			name:           "PDPVerifier storage provider string needs operator attention",
			err:            reasonRevert(provingRevertOnlyStorageProviderCanProve),
			wantClassifier: "operator attention",
		},
		{
			name:           "PDPVerifier proof did not verify string is proof generation failure",
			err:            reasonRevert(provingRevertProofDidNotVerify),
			wantClassifier: "proof generation failure",
		},
		{
			name:           "PDPVerifier DataSetNotFound has its own category",
			err:            selectorRevert(contractErrorSelector(ErrPDPVerifierDataSetNotFound)),
			wantClassifier: "PDPVerifier data set not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, classifier := range knownErrorClassifiers {
				got := classifier.fn(tc.err)
				want := classifier.name == tc.wantClassifier
				if got != want {
					t.Errorf("%s(%v) = %v, want %v", classifier.name, tc.err, got, want)
				}
			}
		})
	}
}

func TestNilErrorsAreNotClassified(t *testing.T) {
	for _, classifier := range knownErrorClassifiers {
		if classifier.fn(nil) {
			t.Errorf("%s(nil) = true, want false", classifier.name)
		}
	}
	if IsContractRevert(nil) {
		t.Error("IsContractRevert(nil) = true, want false")
	}
}

func TestUnknownContractRevertIsOnlyGenericRevert(t *testing.T) {
	err := selectorRevert("deadbeef")

	for _, classifier := range knownErrorClassifiers {
		if classifier.fn(err) {
			t.Errorf("%s(%v) = true, want false", classifier.name, err)
		}
	}
	if !IsContractRevert(err) {
		t.Errorf("IsContractRevert(%v) = false, want true", err)
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
