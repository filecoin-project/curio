package paymentstatus

import (
	"math/big"

	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/lib/filecoinpayment"
)

const TemporaryDefaultGraceInEpochs = 30 * builtin.EpochsInDay

const (
	StatusOK            = "ok"
	StatusGrace         = "grace"
	StatusTerminating   = "terminating"
	StatusPendingDelete = "pending_delete"
)

const (
	ReasonUnpaidGrace     = "unpaid_grace"
	ReasonPaymentDefault  = "payment_default"
	ReasonClientRequested = "client_requested"
	ReasonProvingFailure  = "proving_failure"
)

type DeletePipelineOverlay struct {
	InPipeline              bool
	ClientRequested         bool
	AfterTerminateService   bool
	ServiceTerminationEpoch *int64
	DeletionAllowed         bool
	Terminated              bool
}

type ProvingOverlay struct {
	UnrecoverableFailureEpoch *int64
}

// Snapshot is the computed payment/deletion state for one dataset.
type Snapshot struct {
	DataSetID            int64
	Payer                string
	RailID               *int64
	SettledUpTo          *int64
	LockupPeriod         *int64
	RailEndEpoch         *int64
	PdpEndEpoch          *int64
	GraceStartEpoch      *int64
	ProjectedDeleteEpoch *int64
	Status               string
	Reason               string
	DeleteDatePending    bool
}

func bigIntEpoch(v *big.Int) *int64 {
	if v == nil || v.Sign() <= 0 {
		return nil
	}
	out := v.Int64()
	return &out
}

func addEpochs(base *big.Int, delta int64) *int64 {
	if base == nil {
		return nil
	}
	sum := new(big.Int).Add(base, big.NewInt(delta))
	if sum.Sign() <= 0 {
		return nil
	}
	out := sum.Int64()
	return &out
}

func graceStartEpoch(settledUpTo, lockupPeriod *big.Int) *int64 {
	if settledUpTo == nil || lockupPeriod == nil {
		return nil
	}
	return addEpochs(new(big.Int).Add(settledUpTo, lockupPeriod), 0)
}

func EstimatedDeleteEpoch(settledUpTo, lockupPeriod *big.Int) *int64 {
	if settledUpTo == nil || lockupPeriod == nil || lockupPeriod.Sign() <= 0 {
		return nil
	}
	threshold := new(big.Int).Add(settledUpTo, lockupPeriod)
	threshold.Add(threshold, big.NewInt(TemporaryDefaultGraceInEpochs))
	threshold.Add(threshold, lockupPeriod)
	if threshold.Sign() <= 0 {
		return nil
	}
	out := threshold.Int64()
	return &out
}

func railLive(view filecoinpayment.PaymentsRailView) bool {
	return view.EndEpoch == nil || view.EndEpoch.Sign() <= 0
}

func lockupExhausted(view filecoinpayment.PaymentsRailView, currentEpoch uint64) bool {
	if !railLive(view) || view.SettledUpTo == nil || view.LockupPeriod == nil {
		return false
	}
	threshold := new(big.Int).Add(view.SettledUpTo, view.LockupPeriod)
	return threshold.Uint64() <= currentEpoch
}

func pastAutoTerminateThreshold(view filecoinpayment.PaymentsRailView, currentEpoch uint64) bool {
	if !railLive(view) || view.SettledUpTo == nil || view.LockupPeriod == nil {
		return false
	}
	threshold := new(big.Int).Add(view.SettledUpTo, view.LockupPeriod)
	threshold.Add(threshold, big.NewInt(TemporaryDefaultGraceInEpochs))
	return threshold.Uint64() < currentEpoch
}

func deleteReason(deleteOverlay DeletePipelineOverlay, proving ProvingOverlay) string {
	if deleteOverlay.ClientRequested {
		return ReasonClientRequested
	}
	if proving.UnrecoverableFailureEpoch != nil {
		return ReasonProvingFailure
	}
	return ReasonPaymentDefault
}

// Classify derives UI/task status from rail view and local overlays.
func Classify(
	currentEpoch uint64,
	view filecoinpayment.PaymentsRailView,
	payer string,
	deleteOverlay DeletePipelineOverlay,
	proving ProvingOverlay,
) Snapshot {
	out := Snapshot{
		Payer:  payer,
		Status: StatusOK,
	}

	if view.SettledUpTo != nil {
		out.SettledUpTo = bigIntEpoch(view.SettledUpTo)
	}
	if view.LockupPeriod != nil {
		out.LockupPeriod = bigIntEpoch(view.LockupPeriod)
	}
	if view.EndEpoch != nil && view.EndEpoch.Sign() > 0 {
		out.RailEndEpoch = bigIntEpoch(view.EndEpoch)
	}

	out.GraceStartEpoch = graceStartEpoch(view.SettledUpTo, view.LockupPeriod)

	if deleteOverlay.InPipeline && !deleteOverlay.Terminated {
		out.Reason = deleteReason(deleteOverlay, proving)
		if deleteOverlay.ServiceTerminationEpoch != nil {
			out.PdpEndEpoch = deleteOverlay.ServiceTerminationEpoch
			out.ProjectedDeleteEpoch = deleteOverlay.ServiceTerminationEpoch
			if deleteOverlay.DeletionAllowed {
				out.Status = StatusPendingDelete
			} else {
				out.Status = StatusTerminating
			}
			return out
		}

		out.Status = StatusTerminating
		out.DeleteDatePending = true
		return out
	}

	if lockupExhausted(view, currentEpoch) && railLive(view) {
		out.Reason = ReasonUnpaidGrace
		out.Status = StatusGrace
		out.ProjectedDeleteEpoch = EstimatedDeleteEpoch(view.SettledUpTo, view.LockupPeriod)
		if pastAutoTerminateThreshold(view, currentEpoch) {
			out.Reason = ReasonPaymentDefault
		}
		return out
	}

	if !railLive(view) && view.EndEpoch != nil && view.EndEpoch.Sign() > 0 {
		out.Reason = ReasonPaymentDefault
		out.Status = StatusTerminating
		out.RailEndEpoch = bigIntEpoch(view.EndEpoch)
		out.ProjectedDeleteEpoch = bigIntEpoch(view.EndEpoch)
	}

	return out
}

func StoredRailView(settledUpTo, lockupPeriod, railEndEpoch *int64) filecoinpayment.PaymentsRailView {
	view := filecoinpayment.PaymentsRailView{}
	if settledUpTo != nil {
		view.SettledUpTo = big.NewInt(*settledUpTo)
	}
	if lockupPeriod != nil {
		view.LockupPeriod = big.NewInt(*lockupPeriod)
	}
	if railEndEpoch != nil {
		view.EndEpoch = big.NewInt(*railEndEpoch)
	}
	return view
}
