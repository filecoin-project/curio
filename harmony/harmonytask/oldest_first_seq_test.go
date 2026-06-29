package harmonytask

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask/treehelper"
)

type stubTaskSource map[string][]task

func (s stubTaskSource) GetTasks(taskName string) []task {
	return s[taskName]
}

func TestOldestFirstSeq_unrelatedTypes(t *testing.T) {
	pref := treehelper.FindPreferredTaskRunOrder(map[string][]string{
		"a": {},
		"b": {},
	})
	handlers := map[string]*taskTypeHandler{
		"a": {TaskTypeDetails: TaskTypeDetails{Name: "a"}},
		"b": {TaskTypeDetails: TaskTypeDetails{Name: "b"}},
	}
	t0 := time.Unix(100, 0)
	t1 := t0.Add(time.Hour)
	ts := stubTaskSource{
		"a": {{ID: 1, PostedTime: t1}}, // younger
		"b": {{ID: 2, PostedTime: t0}}, // older
	}
	seq := oldestFirstSeq(handlers, ts, pref)
	require.Len(t, seq, 2)
	require.Equal(t, "b", seq[0].Name)
	require.Equal(t, "a", seq[1].Name)
}

func TestOldestFirstSeq_pipelineDownstreamOrder(t *testing.T) {
	pref := treehelper.FindPreferredTaskRunOrder(map[string][]string{
		"ch": {},
		"md": {"ch"},
		"rt": {"md"},
	})
	handlers := map[string]*taskTypeHandler{
		"ch": {TaskTypeDetails: TaskTypeDetails{Name: "ch"}},
		"md": {TaskTypeDetails: TaskTypeDetails{Name: "md"}},
		"rt": {TaskTypeDetails: TaskTypeDetails{Name: "rt"}},
	}
	t0 := time.Unix(100, 0)
	t1 := t0.Add(time.Minute)
	t2 := t0.Add(time.Hour)
	ts := stubTaskSource{
		"ch": {{ID: 1, PostedTime: t0}},
		"md": {{ID: 2, PostedTime: t1}},
		"rt": {{ID: 3, PostedTime: t2}},
	}
	seq := oldestFirstSeq(handlers, ts, pref)
	require.Len(t, seq, 3)
	require.Equal(t, "rt", seq[0].Name)
	require.Equal(t, "md", seq[1].Name)
	require.Equal(t, "ch", seq[2].Name)
}
