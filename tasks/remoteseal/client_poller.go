package remoteseal

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/promise"
)

const (
	pollerClientPoll = iota
	pollerClientFetch
	pollerClientC1Exchange
	pollerClientCleanup

	numClientPollers
)

const rsealClientPollerInterval = 10 * time.Second

// RSealClientPoller watches rseal_client_pipeline and creates tasks
// for poll, C1 exchange, and cleanup stages.
type RSealClientPoller struct {
	db *harmonydb.DB

	pollers [numClientPollers]promise.Promise[harmonytask.AddTaskFunc]
}

func NewRSealClientPoller(db *harmonydb.DB) *RSealClientPoller {
	return &RSealClientPoller{
		db: db,
	}
}

type clientPollTask struct {
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_number"`

	// client pipeline state
	AfterSDR        bool `db:"after_sdr"`
	AfterTreeD      bool `db:"after_tree_d"`
	AfterTreeC      bool `db:"after_tree_c"`
	AfterTreeR      bool `db:"after_tree_r"`
	AfterFetch      bool `db:"after_fetch"`
	AfterC1Exchange bool `db:"after_c1_exchange"`
	AfterCleanup    bool `db:"after_cleanup"`
	Failed          bool `db:"failed"`

	TaskIDSDR        *int64 `db:"task_id_sdr"`
	TaskIDTreeD      *int64 `db:"task_id_tree_d"`
	TaskIDTreeC      *int64 `db:"task_id_tree_c"`
	TaskIDTreeR      *int64 `db:"task_id_tree_r"`
	TaskIDFetch      *int64 `db:"task_id_fetch"`
	TaskIDC1Exchange *int64 `db:"task_id_c1_exchange"`
	TaskIDCleanup    *int64 `db:"task_id_cleanup"`

	// from sectors_sdr_pipeline
	AfterPrecommitMsgSuccess bool   `db:"after_precommit_msg_success"`
	SeedEpoch                *int64 `db:"seed_epoch"`
	AfterPoRep               bool   `db:"after_porep"`
}

// RunPoller starts the polling loop for the client-side remote seal pipeline.
func (p *RSealClientPoller) RunPoller(ctx context.Context) {
	ticker := time.NewTicker(rsealClientPollerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				log.Errorw("rseal client polling failed", "error", err)
			}
		}
	}
}

func (p *RSealClientPoller) poll(ctx context.Context) error {
	var tasks []clientPollTask

	err := p.db.Select(ctx, &tasks, `
		SELECT
			c.sp_id,
			c.sector_number,
			c.after_sdr,
			c.after_tree_d,
			c.after_tree_c,
			c.after_tree_r,
			c.after_fetch,
			c.after_c1_exchange,
			c.after_cleanup,
			c.failed,
			c.task_id_sdr,
			c.task_id_tree_d,
			c.task_id_tree_c,
			c.task_id_tree_r,
			c.task_id_fetch,
			c.task_id_c1_exchange,
			c.task_id_cleanup,
			COALESCE(s.after_precommit_msg_success, FALSE) AS after_precommit_msg_success,
			s.seed_epoch,
			COALESCE(s.after_porep, FALSE) AS after_porep
		FROM rseal_client_pipeline c
		JOIN sectors_sdr_pipeline s ON c.sp_id = s.sp_id AND c.sector_number = s.sector_number
		WHERE c.after_cleanup != TRUE OR c.after_c1_exchange != TRUE OR c.after_fetch != TRUE`)
	if err != nil {
		return xerrors.Errorf("querying rseal_client_pipeline: %w", err)
	}

	for _, task := range tasks {
		if task.Failed {
			continue
		}

		p.pollClientPoll(ctx, task)
		p.pollClientFetch(ctx, task)
		p.pollClientC1Exchange(ctx, task)
		p.pollClientCleanup(ctx, task)
	}

	return nil
}

// pollClientPoll creates RSealClientPoll tasks for sectors where SDR has not yet completed
// and no poll task is currently running. The poll task contacts the provider to check status.
func (p *RSealClientPoller) pollClientPoll(ctx context.Context, task clientPollTask) {
	// Only poll if SDR is not yet done, no poll task is assigned (task_id_sdr is set by delegate and stays until complete notification)
	if !task.AfterSDR && task.TaskIDSDR == nil && p.pollers[pollerClientPoll].IsSet() {
		p.pollers[pollerClientPoll].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE rseal_client_pipeline SET task_id_sdr = $1
				WHERE sp_id = $2 AND sector_number = $3 AND after_sdr = FALSE AND task_id_sdr IS NULL`,
				id, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating rseal_client_pipeline for poll: %w", err)
			}
			if n != 1 {
				return false, nil
			}
			return true, nil
		})
	}
}

// pollClientFetch creates fetch tasks for sectors where SDR+trees have completed remotely
// but the sealed data and cache have not yet been downloaded to local storage.
func (p *RSealClientPoller) pollClientFetch(ctx context.Context, task clientPollTask) {
	if task.AfterSDR && !task.AfterFetch && task.TaskIDFetch == nil &&
		p.pollers[pollerClientFetch].IsSet() {

		p.pollers[pollerClientFetch].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE rseal_client_pipeline SET task_id_fetch = $1
				WHERE sp_id = $2 AND sector_number = $3
				AND after_sdr = TRUE AND after_fetch = FALSE AND task_id_fetch IS NULL`,
				id, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating rseal_client_pipeline for fetch: %w", err)
			}
			if n != 1 {
				return false, nil
			}
			return true, nil
		})
	}
}

// pollClientC1Exchange creates C1 exchange tasks for sectors that have completed SDR+trees
// on the provider, precommit has landed on chain, and seed is available.
func (p *RSealClientPoller) pollClientC1Exchange(ctx context.Context, task clientPollTask) {
	if task.AfterSDR && !task.AfterC1Exchange && task.TaskIDC1Exchange == nil &&
		task.AfterPrecommitMsgSuccess && task.SeedEpoch != nil &&
		p.pollers[pollerClientC1Exchange].IsSet() {

		p.pollers[pollerClientC1Exchange].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE rseal_client_pipeline SET task_id_c1_exchange = $1
				WHERE sp_id = $2 AND sector_number = $3
				AND after_sdr = TRUE AND after_c1_exchange = FALSE AND task_id_c1_exchange IS NULL`,
				id, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating rseal_client_pipeline for c1 exchange: %w", err)
			}
			if n != 1 {
				return false, nil
			}
			return true, nil
		})
	}
}

// pollClientCleanup creates cleanup tasks for sectors where PoRep is done
// and the provider has not yet been told to clean up.
func (p *RSealClientPoller) pollClientCleanup(ctx context.Context, task clientPollTask) {
	if task.AfterPoRep && !task.AfterCleanup && task.TaskIDCleanup == nil &&
		p.pollers[pollerClientCleanup].IsSet() {

		p.pollers[pollerClientCleanup].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
			n, err := tx.Exec(`UPDATE rseal_client_pipeline SET task_id_cleanup = $1
				WHERE sp_id = $2 AND sector_number = $3
				AND after_cleanup = FALSE AND task_id_cleanup IS NULL`,
				id, task.SpID, task.SectorNumber)
			if err != nil {
				return false, xerrors.Errorf("updating rseal_client_pipeline for cleanup: %w", err)
			}
			if n != 1 {
				return false, nil
			}
			return true, nil
		})
	}
}
