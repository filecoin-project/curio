package paymentstatus

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
)

func railView(settled, lockup, end int64) filecoinpayment.PaymentsRailView {
	view := filecoinpayment.PaymentsRailView{
		SettledUpTo:  big.NewInt(settled),
		LockupPeriod: big.NewInt(lockup),
	}
	if end > 0 {
		view.EndEpoch = big.NewInt(end)
	}
	return view
}

func TestClassify_FundedRail(t *testing.T) {
	view := railView(1000, builtin.EpochsInDay*30, 0)
	snap := Classify(1100, view, "0xabc", DeletePipelineOverlay{}, ProvingOverlay{})
	require.Equal(t, StatusOK, snap.Status)
	require.Empty(t, snap.Reason)
}

func TestClassify_UnpaidGrace(t *testing.T) {
	lockup := int64(builtin.EpochsInDay * 30)
	view := railView(1000, lockup, 0)
	current := uint64(1000 + lockup + 1)
	snap := Classify(current, view, "0xabc", DeletePipelineOverlay{}, ProvingOverlay{})
	require.Equal(t, StatusGrace, snap.Status)
	require.Equal(t, ReasonUnpaidGrace, snap.Reason)
	require.NotNil(t, snap.GraceStartEpoch)
	require.Equal(t, int64(1000+lockup), *snap.GraceStartEpoch)
	require.NotNil(t, snap.ProjectedDeleteEpoch)
	require.Equal(t, int64(1000+lockup+TemporaryDefaultGraceInEpochs+lockup), *snap.ProjectedDeleteEpoch)
}

func TestClassify_PastAutoTerminateThreshold(t *testing.T) {
	lockup := int64(builtin.EpochsInDay * 30)
	view := railView(1000, lockup, 0)
	current := uint64(1000 + lockup + TemporaryDefaultGraceInEpochs + 1)
	snap := Classify(current, view, "0xabc", DeletePipelineOverlay{}, ProvingOverlay{})
	require.Equal(t, StatusGrace, snap.Status)
	require.Equal(t, ReasonPaymentDefault, snap.Reason)
}

func TestClassify_DeletePipelineWithEpoch(t *testing.T) {
	epoch := int64(5000)
	view := railView(1000, builtin.EpochsInDay*30, 0)
	snap := Classify(6000, view, "0xabc", DeletePipelineOverlay{
		InPipeline:              true,
		AfterTerminateService:   true,
		ServiceTerminationEpoch: &epoch,
		DeletionAllowed:         true,
	}, ProvingOverlay{})
	require.Equal(t, StatusPendingDelete, snap.Status)
	require.Equal(t, ReasonPaymentDefault, snap.Reason)
	require.Equal(t, epoch, *snap.ProjectedDeleteEpoch)
}

func TestClassify_ClientRequestedTerminationPendingEpoch(t *testing.T) {
	view := railView(1000, builtin.EpochsInDay*30, 0)
	snap := Classify(6000, view, "0xabc", DeletePipelineOverlay{
		InPipeline:            true,
		ClientRequested:       true,
		AfterTerminateService: true,
	}, ProvingOverlay{})
	require.Equal(t, StatusTerminating, snap.Status)
	require.Equal(t, ReasonClientRequested, snap.Reason)
	require.True(t, snap.DeleteDatePending)
}

func TestClassify_ProvingFailureReason(t *testing.T) {
	epoch := int64(7000)
	failure := int64(6500)
	view := railView(1000, builtin.EpochsInDay*30, 0)
	snap := Classify(8000, view, "0xabc", DeletePipelineOverlay{
		InPipeline:              true,
		ServiceTerminationEpoch: &epoch,
	}, ProvingOverlay{UnrecoverableFailureEpoch: &failure})
	require.Equal(t, ReasonProvingFailure, snap.Reason)
}

func TestEstimatedDeleteEpoch(t *testing.T) {
	lockup := int64(builtin.EpochsInDay * 30)
	got := EstimatedDeleteEpoch(big.NewInt(100), big.NewInt(lockup))
	require.NotNil(t, got)
	require.Equal(t, int64(100+lockup+TemporaryDefaultGraceInEpochs+lockup), *got)
}

func TestIsAtRisk_PastProjectedDelete(t *testing.T) {
	epoch := int64(5000)
	snap := Snapshot{
		Status:               StatusPendingDelete,
		ProjectedDeleteEpoch: &epoch,
	}
	require.False(t, IsAtRisk(snap, 6000))
	require.True(t, IsAtRisk(snap, 5000))
	require.True(t, IsAtRisk(snap, 4999))
}

func TestIsAtRisk_TerminatingWithoutDeleteEpoch(t *testing.T) {
	snap := Snapshot{
		Status:            StatusTerminating,
		DeleteDatePending: true,
	}
	require.True(t, IsAtRisk(snap, 9000))
}

func TestIsAtRisk_GraceBeforeProjectedDelete(t *testing.T) {
	lockup := int64(builtin.EpochsInDay * 30)
	view := railView(1000, lockup, 0)
	current := uint64(1000 + lockup + 1)
	snap := Classify(current, view, "0xabc", DeletePipelineOverlay{}, ProvingOverlay{})
	require.True(t, IsAtRisk(snap, current))

	past := uint64(*snap.ProjectedDeleteEpoch + 1)
	require.False(t, IsAtRisk(snap, past))
}
