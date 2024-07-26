/*
	 Package usertaskmgt provides a way to wrap tasks with a URL that can be called to assign the task to a worker.
		Timeline

- UrlTask accepts everything
- once accepted, UrlTask.Do() finds who should own the task and updates the DB:
  - harmony_task_user.owner_id & expiration_time
  - harmony_task releases the task (without err)

- The poller will see the task & call CanAccept()
  - CanAccept() will see the owner_id and call the deeper canaccept() if it's us.
  - If it's not us, check the expiration time and release the task by deleting the row.

- The task will be done by the worker who was told to do it, or eventually reassigned.

Pitfalls:
- If the user's URL is down, the task will be stuck in the DB.
- Turnaround time is slowed by the additional trip through the poller.
- Full task resources are claimed by the URL runner, so the task needs a full capacity.
*/
package usertaskmgt

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
)

var log = logging.Logger("userTaskMgt")

func WrapTasks(tasks []harmonytask.TaskInterface, UserScheduler []config.UserSchedule, db *harmonydb.DB, hostAndPort string) {
	m := lo.SliceToMap(UserScheduler, func(s config.UserSchedule) (string, *config.UserSchedule) {
		_, err := url.Parse(s.URL)
		if err != nil {
			log.Errorf("Invalid UserScheduleUrl: %s. Expected: taskName,url", s)
			return "", nil
		}
		return s.TaskName, &s
	})
	for i, task := range tasks {
		if s, ok := m[task.TypeDetails().Name]; ok {
			tasks[i] = &UrlTask{
				TaskInterface:       task,
				UserScheduleUrl:     s.URL,
				name:                task.TypeDetails().Name,
				db:                  db,
				hostAndPort:         hostAndPort,
				haltOnSchedulerDown: s.HaltOnSchedulerDown,
			}
		}
	}
}

type UrlTask struct {
	harmonytask.TaskInterface
	db                  *harmonydb.DB
	UserScheduleUrl     string
	name                string
	hostAndPort         string
	haltOnSchedulerDown bool
}

// CanAccept should accept all IF no harmony_task_user row exists, ELSE
// if us, try CanAccept() until expiration hits.
func (t *UrlTask) CanAccept(tids []harmonytask.TaskID, te *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := tids[0]
	var owner string
	var expiration int64
	var ignoreUserScheduler bool
	err := t.db.QueryRow(context.Background(), `SELECT 
	COALESCE(owner,''), 
	COALESCE(expiration, 0), 
	COALESCE(ignore_userscheduler,false) 
	from harmony_task_user WHERE task_id=$1`, id).Scan(&owner, &expiration, &ignoreUserScheduler)
	if err != nil {
		return nil, xerrors.Errorf("could not get owner: %w", err)
	}
	if owner != "" {
		if owner == t.hostAndPort || ignoreUserScheduler {
			return t.TaskInterface.CanAccept(tids, te)
		}
		if expiration < time.Now().Unix() {
			_, err = t.db.Exec(context.Background(), `DELETE FROM harmony_task_user WHERE task_id=$1`, id)
			if err != nil {
				return nil, xerrors.Errorf("could not delete from harmony_task_user: %w", err)
			}
		}
	}
	return &id, nil
}

var client = &http.Client{Timeout: time.Second * 10}

func (t *UrlTask) Do(id harmonytask.TaskID, stillMe func() bool) (b bool, err error) {
	defer func() {
		if err != harmonytask.ErrReturnToPoolPlease && !t.haltOnSchedulerDown {
			log.Error("Proceeding without user scheduler service running (as configured)")
			log.Error(err)
			t.db.Exec(context.Background(),
				`INSERT INTO harmony_task_user (task_id, owner, expiration, ignore_userscheduler) 
			VALUES ($1, '-', 0, true)`, id)
			err = harmonytask.ErrReturnToPoolPlease
		}
	}()
	var owner string
	err = t.db.QueryRow(context.Background(), `SELECT COALESCE(owner,'') FROM harmony_task_user WHERE task_id=$1`, id).Scan(&owner)
	if err != nil {
		return false, xerrors.Errorf("could not get owner: %w", err)
	}
	if owner == t.hostAndPort {
		return t.TaskInterface.Do(id, stillMe)
	}
	var workerList []string
	err = t.db.Select(context.Background(), &workerList, `SELECT host_and_port 
	FROM harmony_machines m JOIN harmony_machine_details d ON d.machine_id=m.id 
	WHERE tasks LIKE $1`, "%,"+t.name+",%")
	if err != nil {
		return false, xerrors.Errorf("could not get worker list: %w", err)
	}

	resp, err := client.Post(t.UserScheduleUrl, "application/json", bytes.NewReader([]byte(`
	{
		"task_type": "`+t.name+`",
		"task_id": `+strconv.Itoa(int(id))+`,
		"workers": [`+strings.Join(workerList, ",")+`], 
	}
	`)))
	if err != nil {
		return false, xerrors.Errorf("could not call user defined URL: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return false, xerrors.Errorf("User defined URL returned non-200 status code: %d", resp.StatusCode)
	}
	var respData struct {
		Worker  string
		Timeout int
	}
	defer resp.Body.Close()
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		return false, xerrors.Errorf("could not decode user defined URL response: %w", err)
	}

	// If it's us, we cannot shortcut because we don't have CanAccept's 2nd arg.

	expires := time.Now().Add(time.Second * time.Duration(respData.Timeout))
	_, err = t.db.Exec(context.Background(), `INSERT INTO harmony_task_user (task_id, owner, expiration) VALUES ($1,$2)`, id, respData.Worker, expires)
	if err != nil {
		return false, xerrors.Errorf("could not insert into harmony_task_user: %w", err)
	}

	return false, harmonytask.ErrReturnToPoolPlease
}
