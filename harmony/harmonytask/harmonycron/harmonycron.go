package harmonycron

import (
	"context"
	"regexp"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
)

type Cron struct {
	DB          *harmonydb.DB
	AddTaskFunc harmonytask.AddTaskFunc
	me          int
}

func New(db *harmonydb.DB, myMachineID int) *Cron {
	return &Cron{DB: db, me: myMachineID}
}

// At schedules a task to be run at a specific time.
// The task will be added to the harmony_task table with the given taskType.
// The task will be associated with the given sqlTable at the specified sqlRowID.
// Note: sqlTable must use the standard columns "id" and "task_id"
func (c *Cron) At(t time.Time, taskType, sqlTable string, sqlRowID int) {
	c.AddTaskFunc(func(tid harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		_, err := tx.Exec(`INSERT INTO harmony_cron (task_id, unixtime, task_type, sql_table, sql_row_id) VALUES ($1, $2, $3, $4, $5)`,
			tid, t.Unix(), taskType, sqlTable, sqlRowID)
		if harmonydb.IsErrUniqueContraint(err) {
			return false, nil
		}
		return true, err
	})
}

var validSQL = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func (c *Cron) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	var unixtime int64
	var taskType, sqlTable, sqlRowID string
	err = c.DB.QueryRow(context.Background(), `SELECT unixtime, task_type, sql_table, sql_row_id
	FROM harmony_cron WHERE task_id = $1`, taskID).
		Scan(&unixtime, &taskType, &sqlTable, &sqlRowID)
	if err != nil {
		return false, xerrors.Errorf("getting cron task: %w", err)
	}
	if !validSQL.MatchString(sqlTable) {
		return false, xerrors.Errorf("invalid sql table or column name")
	}
	time.Sleep(time.Until(time.Unix(unixtime, 0)))
	if !stillOwned() {
		return false, nil
	}
	_, err = c.DB.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) VALUES ($1) RETURNING id`, taskType, c.me, time.Now()).Scan(&taskID)
		if err != nil {
			return false, xerrors.Errorf("adding new task: %w", err)
		}
		_, err = tx.Exec(`SELECT update_ext_taskid($1, $2, $3)`, sqlTable, taskID, sqlRowID)
		if err != nil {
			return false, xerrors.Errorf("deleting cron task: %w", err)
		}
		return true, nil
	})
	if err != nil {
		return false, xerrors.Errorf("doing cron task: %w", err)
	}
	return true, nil
}

func (c *Cron) CanAccept(ids []harmonytask.TaskID, te *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	// TODO: avoid accepting if we are one of the busier nodes.
	return &ids[0], nil
}

func (c *Cron) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "Cron",
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 5 << 20,
			Gpu: 0,
		},
	}
}

func (c *Cron) Adder(f harmonytask.AddTaskFunc) {
	c.AddTaskFunc = f
}

var _ harmonytask.TaskInterface = &Cron{}
