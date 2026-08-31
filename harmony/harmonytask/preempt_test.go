package harmonytask

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/runregistry"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
)

func TestComputePreemptionPlanSkipsUninterruptible(t *testing.T) {
	victimMax := taskhelp.Max(8)
	protectedMax := taskhelp.Max(8)

	victim := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "Victim",
			Max:  victimMax,
			Cost: resources.Resources{Cpu: 2, Ram: 1 << 20},
		},
		running: runregistry.New(),
	}
	protected := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name:            "SendLike",
			Max:             protectedMax,
			Uninterruptible: true,
			Cost:            resources.Resources{Cpu: 2, Ram: 1 << 20},
		},
		running: runregistry.New(),
	}

	victim.running.Start(1, func() {})
	victimMax.Add(1)
	protected.running.Start(2, func() {})
	protectedMax.Add(1)

	// Total 4 CPU: both running tasks consume 2 each → available 0.
	// Needing 2 CPU can only be satisfied by preempting Victim; SendLike is skipped.
	e := &TaskEngine{
		cfg: taskEngineConfig{
			reg: &resources.Reg{
				Resources: resources.Resources{Cpu: 4, Ram: 4 << 20},
			},
		},
		handlers: []*taskTypeHandler{victim, protected},
	}

	plan := e.computePreemptionPlan(resources.Resources{Cpu: 2, Ram: 1 << 20})
	require.NotNil(t, plan)
	require.Len(t, plan.candidates, 1)
	require.Equal(t, TaskID(1), plan.candidates[0].taskID)
	require.Equal(t, "Victim", plan.candidates[0].handler.Name)
}

func TestComputePreemptionPlanSkipsTimeSensitive(t *testing.T) {
	max := taskhelp.Max(8)
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name:          "WinPostLike",
			Max:           max,
			TimeSensitive: true,
			Cost:          resources.Resources{Cpu: 2, Ram: 1 << 20},
		},
		running: runregistry.New(),
	}
	h.running.Start(7, func() {})
	max.Add(1)

	e := &TaskEngine{
		cfg: taskEngineConfig{
			reg: &resources.Reg{
				Resources: resources.Resources{Cpu: 2, Ram: 4 << 20},
			},
		},
		handlers: []*taskTypeHandler{h},
	}

	require.Nil(t, e.computePreemptionPlan(resources.Resources{Cpu: 2, Ram: 1 << 20}))
}

func TestComputePreemptionPlanNilWhenNoDeficit(t *testing.T) {
	e := &TaskEngine{
		cfg: taskEngineConfig{
			reg: &resources.Reg{
				Resources: resources.Resources{Cpu: 4, Ram: 8 << 20},
			},
		},
	}
	require.Nil(t, e.computePreemptionPlan(resources.Resources{Cpu: 1, Ram: 1 << 20}))
}

func TestExecutePreemptionDoesNotHangWhenVictimsIgnoreCancel(t *testing.T) {
	max := taskhelp.Max(8)
	h := &taskTypeHandler{
		TaskTypeDetails: TaskTypeDetails{
			Name: "Victim",
			Max:  max,
			Cost: resources.Resources{Cpu: 1, Ram: 1 << 20},
		},
		running: runregistry.New(),
	}
	h.running.Start(1, func() {})
	h.running.Start(2, func() {})
	max.Add(2)

	e := &TaskEngine{
		cfg: taskEngineConfig{
			reg: &resources.Reg{
				Resources: resources.Resources{Cpu: 2, Ram: 4 << 20},
			},
		},
		handlers: []*taskTypeHandler{h},
	}
	plan := e.computePreemptionPlan(resources.Resources{Cpu: 2, Ram: 2 << 20})
	require.NotNil(t, plan)
	require.GreaterOrEqual(t, len(plan.candidates), 2)

	oldTimeout := preemptTaskKillTimeout
	preemptTaskKillTimeout = 20 * time.Millisecond
	defer func() { preemptTaskKillTimeout = oldTimeout }()

	done := make(chan struct{})
	go func() {
		e.executePreemption(plan)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("executePreemption hung waiting for victims that never Finish")
	}
}
