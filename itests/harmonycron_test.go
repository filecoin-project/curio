package itests

import (
	"context"
	"testing"
	"time"

	"github.com/snadrus/must"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp/harmonycron"
)

// TestHarmonyCron is Documentesting
func TestHarmonyCron(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbConfig := config.HarmonyDB{
		Hosts:    []string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
		Database: "yugabyte",
		Username: "yugabyte",
		Password: "yugabyte",
		Port:     "5433",
	}
	db := must.One(harmonydb.NewFromConfigWithITestID(t, dbConfig, harmonydb.ITestNewID()))
	hp := "localhost:1234"
	reg := must.One(resources.Register(db, hp))
	cron := harmonycron.New(db, reg.MachineID)

	// FutureTask is the task we want to run in the future.
	FutureTask := &FutureTask{db: db, Ch: make(chan string)}
	_ = must.One(harmonytask.New(db, []harmonytask.TaskInterface{cron, FutureTask}, hp, reg))
	// harmonycron lives in Deps, but we are shortcutting things here.
	// Above here is the setup `curio run`, below is the test /////////////////

	var taskValueForTesting string

	// A tale of PreviousTask and FutureTask  	/story
	// (p *PreviousTask) Do() {
	{
		taskValue := "abcde" // our future task needs this, so save it to that task's table
		var rowID int64
		// Pretend harmony_test is FutureTask's table
		err := db.QueryRow(ctx, "INSERT INTO harmony_test (options, result) VALUES ('FutureTaskTbl', $1) RETURNING id", taskValue).Scan(&rowID)
		require.NoError(t, err)
		// PreviousTask then schedules it sometime in the future. It will run AFTER this time.
		cron.At(time.Now().Add(3*time.Second), "FutureTask", "harmony_test", int(rowID))
		// Now, PreviousTask's machine could go down, & someone's FutureTask runner will pick it up.

		taskValueForTesting = taskValue // hack for the test. Nothing to see here.
	}
	require.Equal(t, taskValueForTesting, <-FutureTask.Ch)
}

type FutureTask struct {
	db *harmonydb.DB
	Ch chan string
}

func (*FutureTask) Adder(harmonytask.AddTaskFunc) {}
func (*FutureTask) CanAccept(tids []harmonytask.TaskID, te *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := tids[0]
	return &id, nil
}
func (w *FutureTask) Do(tID harmonytask.TaskID, stillMe func() bool) (bool, error) {
	var taskValue string
	err := w.db.QueryRow(context.Background(), "SELECT options FROM harmony_test WHERE task_id=$1", tID).Scan(&taskValue)
	if err != nil {
		return false, err
	}
	w.Ch <- taskValue
	return false, nil
}
func (*FutureTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "FutureTask",
		Cost: resources.Resources{}, // zeroes ok
	}
}
