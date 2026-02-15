package remoteseal

import (
	"context"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/promise"
)

var log = logging.Logger("remoteseal")

const (
	pollerProvTicketFetch = iota
	pollerProvSDR
	pollerProvTreeD
	pollerProvTreeRC
	pollerProvNotifyClient
	pollerProvFinalize
	pollerProvCleanup

	numProviderPollers
)

const providerPollerInterval = 10 * time.Second

type RSealProviderPoller struct {
	db      *harmonydb.DB
	pollers [numProviderPollers]promise.Promise[harmonytask.AddTaskFunc]
}

func NewProviderPoller(db *harmonydb.DB) *RSealProviderPoller {
	return &RSealProviderPoller{
		db: db,
	}
}

type pollProviderTask struct {
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_number"`
	RegSealProof int   `db:"reg_seal_proof"`
	PartnerID    int64 `db:"partner_id"`

	// ticket
	TicketEpoch *int64 `db:"ticket_epoch"`

	// task IDs
	TaskIDSdr          *int64 `db:"task_id_sdr"`
	TaskIDTreeD        *int64 `db:"task_id_tree_d"`
	TaskIDTreeC        *int64 `db:"task_id_tree_c"`
	TaskIDTreeR        *int64 `db:"task_id_tree_r"`
	TaskIDNotifyClient *int64 `db:"task_id_notify_client"`
	TaskIDFinalize     *int64 `db:"task_id_finalize"`
	TaskIDCleanup      *int64 `db:"task_id_cleanup"`

	// after flags
	AfterSDR          bool `db:"after_sdr"`
	AfterTreeD        bool `db:"after_tree_d"`
	AfterTreeC        bool `db:"after_tree_c"`
	AfterTreeR        bool `db:"after_tree_r"`
	AfterNotifyClient bool `db:"after_notify_client"`
	AfterC1Supplied   bool `db:"after_c1_supplied"`
	AfterFinalize     bool `db:"after_finalize"`
	AfterCleanup      bool `db:"after_cleanup"`

	// cleanup
	CleanupRequested bool       `db:"cleanup_requested"`
	CleanupTimeout   *time.Time `db:"cleanup_timeout"`

	// failure
	Failed bool `db:"failed"`
}

func (sp *RSealProviderPoller) RunPoller(ctx context.Context) {
	ticker := time.NewTicker(providerPollerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := sp.poll(ctx); err != nil {
				log.Errorw("provider poller failed", "error", err)
			}
		}
	}
}

func (sp *RSealProviderPoller) poll(ctx context.Context) error {
	var tasks []pollProviderTask

	err := sp.db.Select(ctx, &tasks, `SELECT
		sp_id,
		sector_number,
		reg_seal_proof,
		partner_id,
		ticket_epoch,
		task_id_sdr,
		after_sdr,
		task_id_tree_d,
		after_tree_d,
		task_id_tree_c,
		after_tree_c,
		task_id_tree_r,
		after_tree_r,
		task_id_notify_client,
		after_notify_client,
		after_c1_supplied,
		task_id_finalize,
		after_finalize,
		cleanup_requested,
		cleanup_timeout,
		task_id_cleanup,
		after_cleanup,
		failed
	FROM rseal_provider_pipeline
	WHERE after_cleanup != TRUE AND failed != TRUE`)
	if err != nil {
		return xerrors.Errorf("selecting provider pipeline tasks: %w", err)
	}

	for _, task := range tasks {
		task := task

		if task.Failed {
			continue
		}

		// 1. Ticket fetch: ticket not yet obtained, no SDR task assigned
		//    We reuse the task_id_sdr column for the ticket fetch task.
		//    After ticket is fetched, task_id_sdr is cleared so the real SDR can be assigned.
		if task.TicketEpoch == nil && task.TaskIDSdr == nil {
			sp.pollStartTicketFetch(ctx, task)
			continue
		}

		// 2. SDR: ticket obtained, no SDR task running, SDR not done
		if !task.AfterSDR && task.TaskIDSdr == nil && task.TicketEpoch != nil {
			sp.pollStartSDR(ctx, task)
			continue
		}

		// 3. TreeD: SDR done, TreeD not done, no TreeD task running
		if task.AfterSDR && !task.AfterTreeD && task.TaskIDTreeD == nil {
			sp.pollStartTreeD(ctx, task)
			continue
		}

		// 4. TreeRC: TreeD done, TreeC/TreeR not done, no tasks running
		if task.AfterTreeD && !task.AfterTreeC && !task.AfterTreeR && task.TaskIDTreeC == nil && task.TaskIDTreeR == nil {
			sp.pollStartTreeRC(ctx, task)
			continue
		}

		// 5. NotifyClient: TreeR done, not yet notified, no notify task running
		if task.AfterTreeR && !task.AfterNotifyClient && task.TaskIDNotifyClient == nil {
			sp.pollStartNotifyClient(ctx, task)
			continue
		}

		// 6. Finalize: C1 supplied by client, not yet finalized, no finalize task running
		if task.AfterC1Supplied && !task.AfterFinalize && task.TaskIDFinalize == nil {
			sp.pollStartFinalize(ctx, task)
			continue
		}

		// 7. Cleanup: cleanup requested (or timeout reached), not yet cleaned, no cleanup task running
		if !task.AfterCleanup && task.TaskIDCleanup == nil {
			shouldCleanup := task.CleanupRequested ||
				(task.CleanupTimeout != nil && time.Now().After(*task.CleanupTimeout))
			if shouldCleanup {
				sp.pollStartCleanup(ctx, task)
				continue
			}
		}
	}

	return nil
}

// pollStartTicketFetch creates a ticket-fetch task. The task_id is stored in the
// task_id_sdr column temporarily. The RSealProviderTicket task fetches the ticket
// from the client, writes ticket_epoch/ticket_value, and clears task_id_sdr so
// that the real SDR task can be assigned next poll cycle.
func (sp *RSealProviderPoller) pollStartTicketFetch(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvTicketFetch].IsSet() {
		return
	}

	sp.pollers[pollerProvTicketFetch].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_sdr = $1
			WHERE sp_id = $2 AND sector_number = $3 AND ticket_epoch IS NULL AND task_id_sdr IS NULL`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update ticket fetch task: %w", err)
		}
		if n != 1 {
			return false, nil // someone else got it
		}

		return true, nil
	})
}

// pollStartSDR assigns an SDR task to a sector that has obtained its ticket.
// This uses the same SDR task type as the regular seal pipeline; the existing
// SDR task's Do() queries rseal_provider_pipeline via UNION ALL.
func (sp *RSealProviderPoller) pollStartSDR(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvSDR].IsSet() {
		return
	}

	sp.pollers[pollerProvSDR].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_sdr = $1
			WHERE sp_id = $2 AND sector_number = $3 AND task_id_sdr IS NULL AND ticket_epoch IS NOT NULL AND after_sdr = FALSE`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update sdr task: %w", err)
		}
		if n != 1 {
			return false, nil
		}

		return true, nil
	})
}

// pollStartTreeD assigns a TreeD task. Uses the same TreeD task type as the regular pipeline.
func (sp *RSealProviderPoller) pollStartTreeD(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvTreeD].IsSet() {
		return
	}

	sp.pollers[pollerProvTreeD].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_tree_d = $1
			WHERE sp_id = $2 AND sector_number = $3 AND after_sdr = TRUE AND task_id_tree_d IS NULL`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update tree_d task: %w", err)
		}
		if n != 1 {
			return false, nil
		}

		return true, nil
	})
}

// pollStartTreeRC assigns TreeC and TreeR tasks (same task handles both).
// Uses the same TreeRC task type as the regular pipeline.
func (sp *RSealProviderPoller) pollStartTreeRC(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvTreeRC].IsSet() {
		return
	}

	sp.pollers[pollerProvTreeRC].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_tree_c = $1, task_id_tree_r = $1
			WHERE sp_id = $2 AND sector_number = $3 AND after_tree_d = TRUE AND task_id_tree_c IS NULL AND task_id_tree_r IS NULL`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update tree_rc task: %w", err)
		}
		if n != 1 {
			return false, nil
		}

		return true, nil
	})
}

// pollStartNotifyClient creates a task to notify the client that SDR+trees are done.
func (sp *RSealProviderPoller) pollStartNotifyClient(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvNotifyClient].IsSet() {
		return
	}

	sp.pollers[pollerProvNotifyClient].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_notify_client = $1
			WHERE sp_id = $2 AND sector_number = $3 AND after_tree_r = TRUE AND task_id_notify_client IS NULL AND after_notify_client = FALSE`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update notify client task: %w", err)
		}
		if n != 1 {
			return false, nil
		}

		return true, nil
	})
}

// pollStartFinalize creates a task to drop SDR layers after the client has supplied C1.
func (sp *RSealProviderPoller) pollStartFinalize(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvFinalize].IsSet() {
		return
	}

	sp.pollers[pollerProvFinalize].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_finalize = $1
			WHERE sp_id = $2 AND sector_number = $3 AND after_c1_supplied = TRUE AND task_id_finalize IS NULL AND after_finalize = FALSE`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update finalize task: %w", err)
		}
		if n != 1 {
			return false, nil
		}

		return true, nil
	})
}

// pollStartCleanup creates a task to remove all sector data from storage.
func (sp *RSealProviderPoller) pollStartCleanup(ctx context.Context, task pollProviderTask) {
	if !sp.pollers[pollerProvCleanup].IsSet() {
		return
	}

	sp.pollers[pollerProvCleanup].Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
		n, err := tx.Exec(`UPDATE rseal_provider_pipeline SET task_id_cleanup = $1
			WHERE sp_id = $2 AND sector_number = $3 AND task_id_cleanup IS NULL AND after_cleanup = FALSE
			AND (cleanup_requested = TRUE OR (cleanup_timeout IS NOT NULL AND NOW() > cleanup_timeout))`,
			id, task.SpID, task.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("update cleanup task: %w", err)
		}
		if n != 1 {
			return false, nil
		}

		return true, nil
	})
}
