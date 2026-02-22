package seal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simulateBuggyTriggerTimestamp reproduces what PostgreSQL does when a trigger
// writes `CURRENT_TIMESTAMP AT TIME ZONE 'UTC'` into a TIMESTAMPTZ column and
// the session timezone is not UTC.
//
// sessionOffsetSec is the session timezone's UTC offset in seconds
// (e.g. -18000 for America/New_York UTC-5).
func simulateBuggyTriggerTimestamp(actual time.Time, sessionOffsetSec int) time.Time {
	return actual.Add(time.Duration(-sessionOffsetSec) * time.Second)
}

func TestSlackEpochs(t *testing.T) {
	tests := []struct {
		name           string
		slack          time.Duration
		blockDelay     uint64
		expectedEpochs int64
	}{
		{
			name:           "6h slack with 30s blocks",
			slack:          6 * time.Hour,
			blockDelay:     30,
			expectedEpochs: 720,
		},
		{
			name:           "1h slack with 30s blocks",
			slack:          1 * time.Hour,
			blockDelay:     30,
			expectedEpochs: 120,
		},
		{
			name:           "rounds up fractional epochs",
			slack:          31 * time.Second,
			blockDelay:     30,
			expectedEpochs: 2,
		},
		{
			name:           "exact multiple",
			slack:          60 * time.Second,
			blockDelay:     30,
			expectedEpochs: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlackEpochs(tt.slack, tt.blockDelay)
			assert.Equal(t, tt.expectedEpochs, got)
		})
	}
}

func TestEvalBatchTimeout_FullCondition(t *testing.T) {
	now := time.Now()
	recentReady := now.Add(-1 * time.Minute) // not timed out
	farFuture := int64(999999)                // slack won't fire
	currentHeight := int64(1000)

	tests := []struct {
		name       string
		batchSize  int
		maxBatch   int
		shouldFire bool
	}{
		{
			name:       "batch full, fires immediately",
			batchSize:  10,
			maxBatch:   10,
			shouldFire: true,
		},
		{
			name:       "batch over max, fires immediately",
			batchSize:  15,
			maxBatch:   10,
			shouldFire: true,
		},
		{
			name:       "batch not full, does not fire",
			batchSize:  5,
			maxBatch:   10,
			shouldFire: false,
		},
		{
			name:       "maxBatch=0, full condition disabled",
			batchSize:  999,
			maxBatch:   0,
			shouldFire: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvalBatchTimeout(
				recentReady, 4*time.Hour,
				farFuture, 720, currentHeight,
				now, tt.batchSize, tt.maxBatch,
			)
			assert.Equal(t, tt.shouldFire, result.ShouldFire)
			if tt.shouldFire {
				assert.Contains(t, result.Reason, "FULL")
			}
		})
	}
}

func TestEvalBatchTimeout_FullTakesPriority(t *testing.T) {
	now := time.Now()
	oldReady := now.Add(-5 * time.Hour) // timeout would fire
	lowStartEpoch := int64(100)          // slack would fire

	result := EvalBatchTimeout(
		oldReady, 4*time.Hour,
		lowStartEpoch, 720, 1000,
		now, 10, 10,
	)
	require.True(t, result.ShouldFire)
	assert.Contains(t, result.Reason, "FULL", "FULL should take priority over slack and timeout")
}

func TestEvalBatchTimeout_SlackCondition(t *testing.T) {
	now := time.Now()
	timeout := 4 * time.Hour
	slackEpochs := int64(720)

	tests := []struct {
		name          string
		minStartEpoch int64
		currentHeight int64
		shouldFire    bool
	}{
		{
			name:          "start epoch well past slack",
			minStartEpoch: 1000,
			currentHeight: 1000,
			shouldFire:    true,
		},
		{
			name:          "start epoch exactly at slack boundary",
			minStartEpoch: 1720,
			currentHeight: 1000,
			shouldFire:    true,
		},
		{
			name:          "start epoch beyond slack",
			minStartEpoch: 1721,
			currentHeight: 1000,
			shouldFire:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EvalBatchTimeout(
				now.Add(time.Hour), timeout,
				tt.minStartEpoch, slackEpochs, tt.currentHeight,
				now, 1, 256,
			)
			assert.Equal(t, tt.shouldFire, result.ShouldFire)
			if tt.shouldFire {
				assert.Contains(t, result.Reason, "SLACK")
			}
		})
	}
}

func TestEvalBatchTimeout_TimeoutCondition(t *testing.T) {
	slackEpochs := int64(720)
	farFuture := int64(999999)
	currentHeight := int64(1000)
	timeout := 4 * time.Hour

	tests := []struct {
		name         string
		readyAge     time.Duration
		shouldFire   bool
		reasonSubstr string
	}{
		{name: "just became ready", readyAge: 1 * time.Minute, shouldFire: false},
		{name: "half timeout", readyAge: 2 * time.Hour, shouldFire: false},
		{name: "at timeout boundary", readyAge: 4 * time.Hour, shouldFire: true, reasonSubstr: "TIMEOUT"},
		{name: "well past timeout", readyAge: 6 * time.Hour, shouldFire: true, reasonSubstr: "TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			result := EvalBatchTimeout(
				now.Add(-tt.readyAge), timeout,
				farFuture, slackEpochs, currentHeight,
				now, 1, 256,
			)
			assert.Equal(t, tt.shouldFire, result.ShouldFire)
			if tt.shouldFire {
				assert.Contains(t, result.Reason, tt.reasonSubstr)
			} else {
				assert.Equal(t, "NONE", result.Reason)
			}
		})
	}
}

func TestEvalBatchTimeout_SlackTakesPriority(t *testing.T) {
	now := time.Now()
	result := EvalBatchTimeout(
		now.Add(-5*time.Hour), 4*time.Hour,
		1000, 720, 1000,
		now, 1, 256,
	)
	require.True(t, result.ShouldFire)
	assert.Contains(t, result.Reason, "SLACK", "slack should take priority over timeout")
}

func TestEvalBatchTimeout_NeitherConditionMet(t *testing.T) {
	now := time.Now()
	result := EvalBatchTimeout(
		now.Add(-1*time.Hour), 4*time.Hour,
		999999, 720, 1000,
		now, 1, 256,
	)
	assert.False(t, result.ShouldFire)
	assert.Equal(t, "NONE", result.Reason)
}

func TestBuggyTriggerTimestamp(t *testing.T) {
	timeout := 4 * time.Hour
	farFuture := int64(999999)

	actualReadyTime := time.Now().Add(-timeout)

	t.Run("correct timestamp fires on time", func(t *testing.T) {
		result := EvalBatchTimeout(
			actualReadyTime, timeout,
			farFuture, 720, 1000,
			time.Now(), 1, 256,
		)
		assert.True(t, result.ShouldFire)
	})

	t.Run("UTC-5 buggy timestamp delays by 5h", func(t *testing.T) {
		buggyReady := simulateBuggyTriggerTimestamp(actualReadyTime, -5*3600)

		assert.True(t, buggyReady.After(actualReadyTime))
		assert.Equal(t, 5*time.Hour, buggyReady.Sub(actualReadyTime).Round(time.Second))

		result := EvalBatchTimeout(
			buggyReady, timeout,
			farFuture, 720, 1000,
			time.Now(), 1, 256,
		)
		assert.False(t, result.ShouldFire,
			"batch should NOT fire: buggy timestamp shifts ready_at 5h into the future")

		resultLater := EvalBatchTimeout(
			buggyReady, timeout,
			farFuture, 720, 1000,
			time.Now().Add(5*time.Hour), 1, 256,
		)
		assert.True(t, resultLater.ShouldFire, "batch fires 5h late due to timezone bug")
	})

	t.Run("UTC+5 buggy timestamp fires 5h early", func(t *testing.T) {
		recentReady := time.Now().Add(-1 * time.Hour)
		buggyReady := simulateBuggyTriggerTimestamp(recentReady, 5*3600)

		result := EvalBatchTimeout(
			buggyReady, timeout,
			farFuture, 720, 1000,
			time.Now(), 1, 256,
		)
		assert.True(t, result.ShouldFire, "batch fires prematurely due to timezone bug")
	})

	t.Run("UTC session has no bug", func(t *testing.T) {
		correctReady := simulateBuggyTriggerTimestamp(actualReadyTime, 0)
		assert.Equal(t, actualReadyTime, correctReady)

		result := EvalBatchTimeout(
			correctReady, timeout,
			farFuture, 720, 1000,
			time.Now(), 1, 256,
		)
		assert.True(t, result.ShouldFire)
	})
}

func TestBatchTimeoutWithSteadySectorArrival(t *testing.T) {
	timeout := 4 * time.Hour
	farFuture := int64(999999)

	firstSectorReady := time.Now().Add(-timeout)
	now := time.Now()

	t.Run("correct timestamps, batch fires at timeout", func(t *testing.T) {
		result := EvalBatchTimeout(
			firstSectorReady, timeout,
			farFuture, 720, 1000,
			now, 80, 256,
		)
		assert.True(t, result.ShouldFire)
	})

	t.Run("UTC-5 bug, batch fails to fire at timeout", func(t *testing.T) {
		buggyReady := simulateBuggyTriggerTimestamp(firstSectorReady, -5*3600)

		result := EvalBatchTimeout(
			buggyReady, timeout,
			farFuture, 720, 1000,
			now, 80, 256,
		)
		assert.False(t, result.ShouldFire)
	})

	t.Run("full batch fires despite buggy timestamp", func(t *testing.T) {
		buggyReady := simulateBuggyTriggerTimestamp(firstSectorReady, -5*3600)

		result := EvalBatchTimeout(
			buggyReady, timeout,
			farFuture, 720, 1000,
			now, 256, 256,
		)
		assert.True(t, result.ShouldFire, "FULL condition overrides timezone bug")
		assert.Contains(t, result.Reason, "FULL")
	})
}

func TestGroupBatchRows_Empty(t *testing.T) {
	assert.Nil(t, GroupBatchRows(nil))
}

func TestGroupBatchRows_SingleBatch(t *testing.T) {
	now := time.Now()
	rows := []BatchRow{
		{SpID: 1, SectorNumber: 10, StartEpoch: 100, ReadyAt: now.Add(-3 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 1, SectorNumber: 11, StartEpoch: 50, ReadyAt: now.Add(-2 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 1, SectorNumber: 12, StartEpoch: 200, ReadyAt: now.Add(-4 * time.Hour), RegSealProof: 8, BatchIndex: 0},
	}

	batches := GroupBatchRows(rows)
	require.Len(t, batches, 1)

	b := batches[0]
	assert.Equal(t, int64(1), b.SpID)
	assert.Equal(t, int64(8), b.RegSealProof)
	assert.Equal(t, []int64{10, 11, 12}, b.SectorNums)
	assert.Equal(t, int64(50), b.MinStartEpoch)
	assert.Equal(t, now.Add(-4*time.Hour), b.EarliestReady)
}

func TestGroupBatchRows_MultipleBatchesSameProvider(t *testing.T) {
	now := time.Now()
	rows := []BatchRow{
		{SpID: 1, SectorNumber: 10, StartEpoch: 100, ReadyAt: now.Add(-3 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 1, SectorNumber: 11, StartEpoch: 200, ReadyAt: now.Add(-2 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 1, SectorNumber: 12, StartEpoch: 300, ReadyAt: now.Add(-1 * time.Hour), RegSealProof: 8, BatchIndex: 1},
	}

	batches := GroupBatchRows(rows)
	require.Len(t, batches, 2)

	assert.Equal(t, []int64{10, 11}, batches[0].SectorNums)
	assert.Equal(t, int64(100), batches[0].MinStartEpoch)

	assert.Equal(t, []int64{12}, batches[1].SectorNums)
	assert.Equal(t, int64(300), batches[1].MinStartEpoch)
}

func TestGroupBatchRows_MultipleProviders(t *testing.T) {
	now := time.Now()
	rows := []BatchRow{
		{SpID: 1, SectorNumber: 10, StartEpoch: 100, ReadyAt: now.Add(-3 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 2, SectorNumber: 20, StartEpoch: 50, ReadyAt: now.Add(-5 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 2, SectorNumber: 21, StartEpoch: 60, ReadyAt: now.Add(-4 * time.Hour), RegSealProof: 8, BatchIndex: 0},
	}

	batches := GroupBatchRows(rows)
	require.Len(t, batches, 2)

	assert.Equal(t, int64(1), batches[0].SpID)
	assert.Equal(t, []int64{10}, batches[0].SectorNums)

	assert.Equal(t, int64(2), batches[1].SpID)
	assert.Equal(t, []int64{20, 21}, batches[1].SectorNums)
	assert.Equal(t, int64(50), batches[1].MinStartEpoch)
	assert.Equal(t, now.Add(-5*time.Hour), batches[1].EarliestReady)
}

func TestGroupBatchRows_IntegrationWithEval(t *testing.T) {
	now := time.Now()
	timeout := 4 * time.Hour
	farFuture := int64(999999)

	rows := []BatchRow{
		// Batch 0: old sector, fires on timeout
		{SpID: 1, SectorNumber: 10, StartEpoch: farFuture, ReadyAt: now.Add(-5 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		{SpID: 1, SectorNumber: 11, StartEpoch: farFuture, ReadyAt: now.Add(-4 * time.Hour), RegSealProof: 8, BatchIndex: 0},
		// Batch 1: recent sector, should NOT fire
		{SpID: 1, SectorNumber: 12, StartEpoch: farFuture, ReadyAt: now.Add(-1 * time.Hour), RegSealProof: 8, BatchIndex: 1},
	}

	batches := GroupBatchRows(rows)
	require.Len(t, batches, 2)

	result0 := EvalBatchTimeout(batches[0].EarliestReady, timeout, batches[0].MinStartEpoch, 720, 1000, now, len(batches[0].SectorNums), 256)
	assert.True(t, result0.ShouldFire)

	result1 := EvalBatchTimeout(batches[1].EarliestReady, timeout, batches[1].MinStartEpoch, 720, 1000, now, len(batches[1].SectorNums), 256)
	assert.False(t, result1.ShouldFire)
}

func TestClampMaxBatch(t *testing.T) {
	tests := []struct {
		name        string
		configured  int
		protocolMax int
		expected    int
	}{
		{
			name:        "zero uses protocol max",
			configured:  0,
			protocolMax: 256,
			expected:    256,
		},
		{
			name:        "negative uses protocol max",
			configured:  -1,
			protocolMax: 256,
			expected:    256,
		},
		{
			name:        "configured below protocol max is used",
			configured:  10,
			protocolMax: 256,
			expected:    10,
		},
		{
			name:        "configured equals protocol max uses protocol max",
			configured:  256,
			protocolMax: 256,
			expected:    256,
		},
		{
			name:        "configured above protocol max is capped",
			configured:  300,
			protocolMax: 256,
			expected:    256,
		},
		{
			name:        "configured=1 is valid minimum",
			configured:  1,
			protocolMax: 256,
			expected:    1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampMaxBatch(tt.configured, tt.protocolMax)
			assert.Equal(t, tt.expected, got)
		})
	}
}
