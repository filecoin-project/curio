package itests

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp/usertaskmgt"
)

func TestUserTaskMgt(t *testing.T) {
	gopath, err := exec.LookPath("go")
	require.NoError(t, err)
	exec.Command(gopath, "run", "cmd/userschedule").Run() // round-robin scheduler

	db := dbSetup(t)
	harmonytask.POLL_DURATION = 200 * time.Millisecond
	output := ""
	var instances []*ut
	for a := 0; a < 3; a++ { // make 3 "machines"
		inst := &ut{db: db, f: func() { output += strconv.Itoa(a) }}
		instances = append(instances, inst)

		host := "foo:" + strconv.Itoa(a)
		tasks := []harmonytask.TaskInterface{inst}
		usertaskmgt.WrapTasks(tasks, []config.UserSchedule{
			{TaskName: "foo", URL: "http://localhost:7654"},
		}, db, host)
		_, err := harmonytask.New(db, tasks, host)
		require.NoError(t, err)
	}

	for a := 0; a < 5; a++ { // schedule 5 tasks
		instances[0].myAddTask(func(tID harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			return true, nil
		})
	}
	require.Equal(t, "01201", output)
}

type ut struct {
	db        *harmonydb.DB
	f         func()
	myAddTask harmonytask.AddTaskFunc
}

func (u *ut) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "foo",
		Cost: resources.Resources{},
	}
}
func (u *ut) CanAccept(tids []harmonytask.TaskID, te *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &tids[0], nil
}
func (u *ut) Adder(f harmonytask.AddTaskFunc) {
	u.myAddTask = f
}
func (u *ut) Do(tID harmonytask.TaskID, _ func() bool) (bool, error) {
	u.f()
	time.Sleep(time.Second) // so there's no chance that a later task will finish first.
	return true, nil
}

func dbSetup(t *testing.T) *harmonydb.DB {
	sharedITestID := harmonydb.ITestNewID()
	dbConfig := config.HarmonyDB{
		Hosts:    []string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
		Database: "yugabyte",
		Username: "yugabyte",
		Password: "yugabyte",
		Port:     "5433",
	}
	db, err := harmonydb.NewFromConfigWithITestID(t, dbConfig, sharedITestID)
	require.NoError(t, err)
	return db
}
