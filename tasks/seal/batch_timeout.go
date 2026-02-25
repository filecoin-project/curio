package seal

import (
	"fmt"
	"math"
	"time"
)

// BatchEvalResult holds the outcome of a batch-firing evaluation.
type BatchEvalResult struct {
	ShouldFire bool
	Reason     string
}

// EvalBatchTimeout decides whether a precommit/commit batch should fire.
// Conditions checked in priority order:
//
//   - Full:    batchSize >= maxBatch (batch can't grow further)
//   - Slack:   (minStartEpoch - slackEpochs) <= currentHeight
//   - Timeout: now >= earliestReady + timeout
func EvalBatchTimeout(
	earliestReady time.Time,
	timeout time.Duration,
	minStartEpoch int64,
	slackEpochs int64,
	currentHeight int64,
	now time.Time,
	batchSize int,
	maxBatch int,
) BatchEvalResult {
	if maxBatch > 0 && batchSize >= maxBatch {
		return BatchEvalResult{
			ShouldFire: true,
			Reason:     fmt.Sprintf("FULL (%d/%d sectors)", batchSize, maxBatch),
		}
	}

	condSlack := (minStartEpoch - slackEpochs) <= currentHeight
	condTimeout := !now.Before(earliestReady.Add(timeout))

	if condSlack {
		return BatchEvalResult{
			ShouldFire: true,
			Reason:     fmt.Sprintf("SLACK (min start epoch: %d)", minStartEpoch),
		}
	}
	if condTimeout {
		return BatchEvalResult{
			ShouldFire: true,
			Reason:     fmt.Sprintf("TIMEOUT (earliest_ready_at: %s)", earliestReady.UTC().Format(time.RFC3339)),
		}
	}
	return BatchEvalResult{ShouldFire: false, Reason: "NONE"}
}

// SlackEpochs converts a wall-clock slack duration to a chain epoch count.
func SlackEpochs(slack time.Duration, blockDelaySecs uint64) int64 {
	return int64(math.Ceil(slack.Seconds() / float64(blockDelaySecs)))
}

// ClampMaxBatch returns the effective max batch size. If configured is positive
// and below the protocol cap, it is used; otherwise the protocol cap applies.
// Used by both precommit and commit batchers.
func ClampMaxBatch(configured, protocolMax int) int {
	if configured > 0 && configured < protocolMax {
		return configured
	}
	return protocolMax
}

// BatchRow represents a single sector row returned by the batch candidate query.
// Both precommit and commit candidate queries alias their columns to these names.
type BatchRow struct {
	SpID         int64     `db:"sp_id"`
	SectorNumber int64     `db:"sector_number"`
	StartEpoch   int64     `db:"start_epoch"`
	ReadyAt      time.Time `db:"ready_at"`
	RegSealProof int64     `db:"reg_seal_proof"`
	BatchIndex   int64     `db:"batch_index"`
}

// BatchCandidate is a grouped batch of sectors eligible for on-chain submission.
type BatchCandidate struct {
	SpID          int64
	RegSealProof  int64
	SectorNums    []int64
	MinStartEpoch int64
	EarliestReady time.Time
}

// GroupBatchRows aggregates sorted BatchRows into BatchCandidates.
// Input rows MUST be ordered by (sp_id, reg_seal_proof, batch_index).
func GroupBatchRows(rows []BatchRow) []BatchCandidate {
	if len(rows) == 0 {
		return nil
	}

	var batches []BatchCandidate
	cur := BatchCandidate{
		SpID:          rows[0].SpID,
		RegSealProof:  rows[0].RegSealProof,
		MinStartEpoch: rows[0].StartEpoch,
		EarliestReady: rows[0].ReadyAt,
	}
	curIdx := rows[0].BatchIndex

	for _, r := range rows {
		sameGroup := r.SpID == cur.SpID &&
			r.RegSealProof == cur.RegSealProof &&
			r.BatchIndex == curIdx

		if !sameGroup {
			batches = append(batches, cur)
			cur = BatchCandidate{
				SpID:          r.SpID,
				RegSealProof:  r.RegSealProof,
				MinStartEpoch: r.StartEpoch,
				EarliestReady: r.ReadyAt,
			}
			curIdx = r.BatchIndex
		}

		cur.SectorNums = append(cur.SectorNums, r.SectorNumber)
		if r.StartEpoch < cur.MinStartEpoch {
			cur.MinStartEpoch = r.StartEpoch
		}
		if r.ReadyAt.Before(cur.EarliestReady) {
			cur.EarliestReady = r.ReadyAt
		}
	}
	batches = append(batches, cur)

	return batches
}
