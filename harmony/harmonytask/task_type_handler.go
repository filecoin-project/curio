package harmonytask

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var log = logging.Logger("harmonytask")

type taskTypeHandler struct {
	TaskInterface
	TaskTypeDetails
	TaskEngine *TaskEngine
	Count      atomic.Int32
	Bid        bool
}

func (h *taskTypeHandler) AddTask(extra func(TaskID, *harmonydb.Tx) (bool, error)) {
	var tID TaskID
	retryWait := time.Millisecond * 100
retryAddTask:
	_, err := h.TaskEngine.db.BeginTransaction(h.TaskEngine.ctx, func(tx *harmonydb.Tx) (bool, error) {
		// create taskID (from DB)
		err := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) 
          VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, h.Name, h.TaskEngine.ownerID).Scan(&tID)
		if err != nil {
			return false, fmt.Errorf("could not insert into harmonyTask: %w", err)
		}
		return extra(tID, tx)
	})

	if err != nil {
		if harmonydb.IsErrUniqueContraint(err) {
			log.Debugf("addtask(%s) saw unique constraint, so it's added already.", h.Name)
			return
		}
		if harmonydb.IsErrSerialization(err) {
			time.Sleep(retryWait)
			retryWait *= 2
			goto retryAddTask
		}
		log.Errorw("Could not add task. AddTasFunc failed", "error", err, "type", h.Name)
		return
	}
}

const (
	WorkSourcePoller   = "poller"
	WorkSourceRecover  = "recovered"
	WorkSourceIAmBored = "bored"
)

// considerWork is called to attempt to start work on a task-id of this task type.
// It presumes single-threaded calling, so there should not be a multi-threaded re-entry.
// The only caller should be the one work poller thread. This does spin off other threads,
// but those should not considerWork. Work completing may lower the resource numbers
// unexpectedly, but that will not invalidate work being already able to fit.
func (h *taskTypeHandler) considerWork(from string, ids []TaskID) (workAccepted, liveBids bool) {
	if len(ids) == 0 {
		return true, false // stop looking for takers
	}

	// 1. Can we do any more of this task type?
	// NOTE: 0 is the default value, so this way people don't need to worry about
	// this setting unless they want to limit the number of tasks of this type.
	if h.Max > 0 && int(h.Count.Load()) >= h.Max {
		log.Debugw("did not accept task", "name", h.Name, "reason", "at max already")
		return false, false
	}

	// 2. Can we do any more work? From here onward, we presume the resource
	// story will not change, so single-threaded calling is best.
	err := h.AssertMachineHasCapacity()
	if err != nil {
		log.Debugw("did not accept task", "name", h.Name, "reason", "at capacity already: "+err.Error())
		return false, false
	}

	return h.canAcceptPart(from, ids)
}
func (h *taskTypeHandler) canAcceptPart(from string, ids []TaskID) (workAccepted, liveBids bool) {
	sch := &SchedulingInfo{h.TaskEngine, from}

	// 3. Bid?
	if h.Bid {
		return h.bidLogic(from, ids, sch)
	}

	// 3b. Or run directly?
	tID, err := h.TaskInterface.(FastTask).CanAccept(ids, sch)
	if err != nil {
		log.Error(err)
		return false, false
	}
	if tID == nil {
		log.Infow("did not accept task", "task_id", ids[0], "reason", "CanAccept() refused", "name", h.Name)
		return false, false
	}
	return h.startTask(from, tID, ids), false
}

// GetUnownedTasks returns a list of tasks that are not owned by any machine.
// It must decide how many tasks to bid on.
// What should happen when 10000 of something drops in to the queue?
// Do we spend minutes in bidding just to rebid?
//
//	That could break the healthy uptake in jobs and the bidding timers.
//
// Do we cache our bids?
//
//	That limits the usefulness of bidding as it cannot include capacity.
//
// So the remaining option is to universally limit the max number of items we care to bid on.
//   - A limit of 1 could be too low for big networks.
//   - A limit of "everyone's capacity for this task" is closer to right, but requires (slow) per-cycle pre-negotiation.
//   - A limit of "1 per bidder" will keep work flowing, but may take multiple bid rounds to fill bigger machines.
//
// A future adjustment could include more than 1x per machine if the "handful of behemoths" clusters suffer.
// Another solution is that "starving" machines could bid deeper into the queue.
func (h *taskTypeHandler) GetUnownedTasks() (ids []TaskID, err error) {
	var numBidders int
	err = h.TaskEngine.db.QueryRow(h.TaskEngine.ctx, `SELECT COUNT(*) FROM harmony_machine_details WHERE tasks LIKE $1`, "%,"+h.TaskTypeDetails.Name+",%").Scan(&numBidders)
	if err != nil {
		log.Error("Unable to read harmony_machine_details ", err)
		return nil, err
	}
	var unownedTasks []TaskID
	err = h.TaskEngine.db.Select(h.TaskEngine.ctx, &unownedTasks, `SELECT id
		FROM harmony_task
		WHERE owner_id IS NULL AND name=$1
		ORDER BY posted_time ASC
		LIMIT $2`, h.TaskTypeDetails.Name, numBidders)
	return unownedTasks, err
}

// bidLogic is called to attempt to start work on a task-id of this task type.
// It presumes single-threaded calling, so there should not be a multi-threaded re-entry.
// The only caller should be the one work poller thread.
func (h *taskTypeHandler) bidLogic(from string, ids []TaskID, sch *SchedulingInfo) (workAccepted, liveBids bool) {
	// __Accept_Phase__
	// accept everything we outbid people on.

	// Delete any "dead" bids
	_, err := h.TaskEngine.db.Exec(context.Background(),
		`DELETE FROM harmony_task_bid WHERE task_id in ($1) AND update_time < NOW() - (INTERVAL $2 seconds)`,
		ids, POLL_DURATION.Seconds()*3)
	if err != nil {
		log.Error(xerrors.Errorf("could not delete dead bids: %w", err))
		return false, false
	}

	// Collect our winnings
	var res []int
	err = h.TaskEngine.db.Select(context.Background(), &res,
		`SELECT t.task_id as task_id FROM 
		(SELECT task_id, MAX(bid) as max_bid FROM harmony_task_bid WHERE task_id in ($1) GROUP BY task_id) m
		JOIN harmony_task_bid t ON m.task_id=t.task_id AND t.bid=m.max_bid
	WHERE t.bidder = $2
	ORDER BY max_bid DESC`, ids, h.TaskEngine.ownerID)
	if err != nil {
		log.Error(xerrors.Errorf("could not select bids: %w", err))
		return false, false
	}
	var success bool
	for i, r := range res {
		err := h.AssertMachineHasCapacity()
		if err != nil {
			log.Debugw("did not accept task", "name", h.Name, "reason", "at capacity already: "+err.Error())
			// we can't accept any more tasks, so we should stop trying to bid on them
			_, err := h.TaskEngine.db.Exec(context.Background(), `UPDATE harmony_task_bid SET bid=0 WHERE task_id in ($1)`, res[i:])
			if err != nil {
				log.Error(xerrors.Errorf("could not update bid: %w", err))
			}
			return success, false
		}
		success = h.startTask(from, (*TaskID)(&r), ids)
	}

	// __Bid_Phase__
	// Place our bids IF we need the work.
	if success { // we removed one+ from the list, so lets get a full list.
		ids, err = h.GetUnownedTasks()
		if err != nil {
			log.Error(xerrors.Errorf("could not get unowned tasks: %w", err))
			return success, false
		}
	}

	// Get our bids.
	tb, err := h.TaskInterface.(BidTask).CanAccept(ids, sch)
	if err != nil {
		log.Error(xerrors.Errorf("could not bid: %w", err))
		return success, false
	}
	sort.Slice(tb, func(i, j int) bool {
		return tb[i].Bid > tb[j].Bid
	})
	// Bid up to our multiple of capacity.
	multiple := h.GetMultipleOfCapacity()
	if len(tb) < multiple {
		// We can't bid on all of them, so lets just bid on the best few.
		tb = tb[:multiple]
	}
	if len(tb) > 0 {
		liveBids = true
	}
	// Keep our bids current (even if we bid last time too).
	for _, t := range tb {
		// Claim our bid, even if 2nd best
		_, err := h.TaskEngine.db.Exec(context.Background(),
			`INSERT INTO harmony_task_bid (task_id, bidder, bid, update_time) VALUES ($1, $2, $3, NOW())`,
			t.TaskID, h.TaskEngine.ownerID, t.Bid)
		if err != nil {
			log.Error(xerrors.Errorf("could not update bid: %w", err))
			return success, liveBids
		}
	}
	return success, liveBids
}

// startTask is called to start 1 work task on a task-id of this task type.
// returns true if the task was started, false if it was not.
func (h *taskTypeHandler) startTask(from string, tID *TaskID, ids []TaskID) bool {
	releaseStorage := func() {
	}
	if h.TaskTypeDetails.Cost.Storage != nil {
		markComplete, err := h.TaskTypeDetails.Cost.Storage.Claim(int(*tID))
		if err != nil {
			log.Infow("did not accept task", "task_id", strconv.Itoa(int(*tID)), "reason", "storage claim failed", "name", h.Name, "error", err)

			if len(ids) > 1 {
				var tryAgain = make([]TaskID, 0, len(ids)-1)
				for _, id := range ids {
					if id != *tID {
						tryAgain = append(tryAgain, id)
					}
				}
				ids = tryAgain
				b, _ := h.canAcceptPart(from, ids)
				return b
			}

			return false
		}
		releaseStorage = func() {
			if err := markComplete(); err != nil {
				log.Errorw("Could not release storage", "error", err)
			}
		}
	}

	// if recovering we don't need to try to claim anything because those tasks are already claimed by us
	if from != WorkSourceRecover {
		// 4. Can we claim the work for our hostname?
		ct, err := h.TaskEngine.db.Exec(h.TaskEngine.ctx, "UPDATE harmony_task SET owner_id=$1 WHERE id=$2 AND owner_id IS NULL", h.TaskEngine.ownerID, *tID)
		if err != nil {
			log.Error(err)

			releaseStorage()
			return false
		}
		if ct == 0 {
			log.Infow("did not accept task", "task_id", strconv.Itoa(int(*tID)), "reason", "already Taken", "name", h.Name)
			releaseStorage()

			var tryAgain = make([]TaskID, 0, len(ids)-1)
			for _, id := range ids {
				if id != *tID {
					tryAgain = append(tryAgain, id)
				}
			}
			ids = tryAgain
			b, _ := h.considerWork(from, ids)
			return b
		}
	}

	h.Count.Add(1)
	go func() {
		log.Infow("Beginning work on Task", "id", *tID, "from", from, "name", h.Name)

		var done bool
		var doErr error
		workStart := time.Now()

		defer func() {
			if r := recover(); r != nil {
				stackSlice := make([]byte, 4092)
				sz := runtime.Stack(stackSlice, false)
				log.Error("Recovered from a serious error "+
					"while processing "+h.Name+" task "+strconv.Itoa(int(*tID))+": ", r,
					" Stack: ", string(stackSlice[:sz]))
			}
			h.Count.Add(-1)

			releaseStorage()
			h.recordCompletion(*tID, workStart, done, doErr)
			if done {
				for _, fs := range h.TaskEngine.follows[h.Name] { // Do we know of any follows for this task type?
					if _, err := fs.f(*tID, fs.h.AddTask); err != nil {
						log.Error("Could not follow", "error", err, "from", h.Name, "to", fs.name)
					}
				}
			}
		}()

		done, doErr = h.Do(*tID, func() bool {
			var owner int
			// Background here because we don't want GracefulRestart to block this save.
			err := h.TaskEngine.db.QueryRow(context.Background(),
				`SELECT owner_id FROM harmony_task WHERE id=$1`, *tID).Scan(&owner)
			if err != nil {
				log.Error("Cannot determine ownership: ", err)
				return false
			}
			return owner == h.TaskEngine.ownerID
		})
		if doErr != nil {
			log.Errorw("Do() returned error", "type", h.Name, "id", strconv.Itoa(int(*tID)), "error", doErr)
		}
	}()
	return true
}

func (h *taskTypeHandler) recordCompletion(tID TaskID, workStart time.Time, done bool, doErr error) {
	workEnd := time.Now()
	retryWait := time.Millisecond * 100
retryRecordCompletion:
	cm, err := h.TaskEngine.db.BeginTransaction(h.TaskEngine.ctx, func(tx *harmonydb.Tx) (bool, error) {
		var postedTime time.Time
		err := tx.QueryRow(`SELECT posted_time FROM harmony_task WHERE id=$1`, tID).Scan(&postedTime)

		if err != nil {
			return false, fmt.Errorf("could not log completion: %w ", err)
		}
		result := "unspecified error"
		if done {
			_, err = tx.Exec("DELETE FROM harmony_task WHERE id=$1", tID)
			if err != nil {

				return false, fmt.Errorf("could not log completion: %w", err)
			}
			result = ""
			if doErr != nil {
				result = "non-failing error: " + doErr.Error()
			}
		} else {
			if doErr != nil {
				result = "error: " + doErr.Error()
			}
			var deleteTask bool
			if h.MaxFailures > 0 {
				ct := uint(0)
				err = tx.QueryRow(`SELECT count(*) FROM harmony_task_history 
				WHERE task_id=$1 AND result=FALSE`, tID).Scan(&ct)
				if err != nil {
					return false, fmt.Errorf("could not read task history: %w", err)
				}
				if ct >= h.MaxFailures {
					deleteTask = true
				}
			}
			if deleteTask {
				_, err = tx.Exec("DELETE FROM harmony_task WHERE id=$1", tID)
				if err != nil {
					return false, fmt.Errorf("could not delete failed job: %w", err)
				}
				// Note: Extra Info is left laying around for later review & clean-up
			} else {
				_, err := tx.Exec(`UPDATE harmony_task SET owner_id=NULL WHERE id=$1`, tID)
				if err != nil {
					return false, fmt.Errorf("could not disown failed task: %v %v", tID, err)
				}
			}
		}
		_, err = tx.Exec(`INSERT INTO harmony_task_history 
									 (task_id,   name, posted,    work_start, work_end, result, completed_by_host_and_port,      err)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, tID, h.Name, postedTime.UTC(), workStart.UTC(), workEnd.UTC(), done, h.TaskEngine.hostAndPort, result)
		if err != nil {
			return false, fmt.Errorf("could not write history: %w", err)
		}
		return true, nil
	})
	if err != nil {
		if harmonydb.IsErrSerialization(err) {
			time.Sleep(retryWait)
			retryWait *= 2
			goto retryRecordCompletion
		}
		log.Error("Could not record transaction: ", err)
		return
	}
	if !cm {
		log.Error("Committing the task records failed")
	}
}

func (h *taskTypeHandler) GetMultipleOfCapacity() int {
	r := h.TaskEngine.ResourcesAvailable()
	cpuMultiple := r.Cpu / h.Cost.Cpu
	ramMultiple := int(r.Ram / h.Cost.Ram)
	gpuMultiple := int(r.Gpu / h.Cost.Gpu)
	// Nah, storage is hard.
	//storageMultiple := h.TaskTypeDetails.Cost.Storage.GetMultipleOfCapacity()
	return min(cpuMultiple, ramMultiple, gpuMultiple)
}

func (h *taskTypeHandler) AssertMachineHasCapacity() error {
	r := h.TaskEngine.ResourcesAvailable()

	if r.Cpu-h.Cost.Cpu < 0 {
		return errors.New("Did not accept " + h.Name + " task: out of cpu")
	}
	if h.Cost.Ram > r.Ram {
		return errors.New("Did not accept " + h.Name + " task: out of RAM")
	}
	if r.Gpu-h.Cost.Gpu < 0 {
		return errors.New("Did not accept " + h.Name + " task: out of available GPU")
	}

	if h.TaskTypeDetails.Cost.Storage != nil {
		if !h.TaskTypeDetails.Cost.Storage.HasCapacity() {
			return errors.New("Did not accept " + h.Name + " task: out of available Storage")
		}
	}
	return nil
}
