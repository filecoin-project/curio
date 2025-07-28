package proofshare

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/tasks/seal"
	"github.com/filecoin-project/curio/tasks/snap"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("proofshare")

// --- Metrics ---

var (
	trBuckets = []float64{0.05, 0.2, 0.5, 1, 5, 15, 45} // seconds

	queueCountGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "curio_psvc_proofshare_queue_count",
		Help: "Current proofshare request queue count",
	})
	adderCommitCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "curio_psvc_proofshare_adder_commits_total",
		Help: "Total number of successful task additions scheduled by Adder",
	})

	needAsksGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "curio_psvc_proofshare_need_asks",
		Help: "Number of asks still needed in current Do loop iteration",
	})
	toRequestGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "curio_psvc_proofshare_to_request_remaining",
		Help: "Remaining requests to fulfill for high-water mark",
	})

	newlyAddedCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "curio_psvc_proofshare_newly_added_total",
		Help: "Total number of new work requests inserted locally",
	})

	createAsksDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "curio_psvc_proofshare_create_asks_seconds",
		Help:    "Duration of create asks inner loop",
		Buckets: trBuckets,
	})
)

func init() {
	_ = prometheus.Register(queueCountGauge)
	_ = prometheus.Register(adderCommitCounter)
	_ = prometheus.Register(needAsksGauge)
	_ = prometheus.Register(toRequestGauge)
	_ = prometheus.Register(newlyAddedCounter)
	_ = prometheus.Register(createAsksDuration)
}

var ProofRequestPollInterval = time.Second * 3
var BoredBeforeToStart = time.Second * 7
var RequestQueueLowWaterMark = 5
var RequestQueueHighWaterMark = 8

type TaskRequestProofs struct {
	db          *harmonydb.DB
	chain       api.FullNode
	paramsReady func() (bool, error)
}

func NewTaskRequestProofs(db *harmonydb.DB, chain api.FullNode, paramck func() (bool, error)) *TaskRequestProofs {
	return &TaskRequestProofs{
		db:          db,
		chain:       chain,
		paramsReady: paramck,
	}
}

// Adder starts new requests when:
// - enabled
// - wallet not null
// - request_task_id null
// - request_queue len <= RequestQueueLowWaterMark
// - snap/porep tasks were bored recently
func (t *TaskRequestProofs) Adder(taskTx harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(ProofRequestPollInterval)

	log.Infow("TaskRequestProofs.Adder() started")

	go func() {
		for range ticker.C {
			// check if snap/porep tasks were bored recently
			porepWasBored := derefTime(seal.PorepLastBored.Load()).After(time.Now().Add(-BoredBeforeToStart))
			snapWasBored := derefTime(snap.ProveLastBored.Load()).After(time.Now().Add(-BoredBeforeToStart))
			if !porepWasBored && !snapWasBored {
				log.Infow("TaskRequestProofs.Adder() no snap/porep tasks were bored recently, skipping")
				continue
			}

			taskTx(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Get current state from proofshare_meta
				var enabled bool
				var wallet *string
				var requestTaskID *int64
				err := tx.QueryRow(`
					SELECT enabled, wallet, request_task_id 
					FROM proofshare_meta 
					WHERE singleton = true
				`).Scan(&enabled, &wallet, &requestTaskID)
				if err != nil {
					return false, err
				}

				// Check conditions to schedule request
				if !enabled || wallet == nil || requestTaskID != nil {
					return false, nil
				}

				// Count pending requests
				var queueCount int
				err = tx.QueryRow(`
					SELECT COUNT(*) 
					FROM proofshare_queue q
					LEFT JOIN harmony_task t ON t.id = q.compute_task_id
					WHERE q.compute_done = FALSE AND (q.compute_task_id IS NULL OR t.owner_id IS NULL)
				`).Scan(&queueCount)
				if err != nil {
					return false, err
				}

				queueCountGauge.Set(float64(queueCount))

				if queueCount > RequestQueueLowWaterMark {
					return false, nil
				}

				// Update request_task_id
				_, err = tx.Exec(`
					UPDATE proofshare_meta 
					SET request_task_id = $1
					WHERE singleton = true
				`, taskID)
				if err != nil {
					return false, err
				}

				// Successful commit
				adderCommitCounter.Inc()

				return true, nil
			})
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface.
func (t *TaskRequestProofs) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	rdy, err := t.paramsReady()
	if err != nil {
		return nil, xerrors.Errorf("failed to setup params: %w", err)
	}
	if !rdy {
		log.Infow("TaskRequestProofs.CanAccept() params not ready, not scheduling")
		return nil, nil
	}

	id := ids[0]
	return &id, nil
}

// Do implements harmonytask.TaskInterface.
func (t *TaskRequestProofs) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// This is a singleton task which runs essentially as long as it needs
	// until the queue gains high water mark.

	var queueCount int
	err = t.db.QueryRow(ctx, `
		SELECT COUNT(*) 
		FROM proofshare_queue q
		LEFT JOIN harmony_task t ON t.id = q.compute_task_id
		WHERE q.compute_done = FALSE AND (q.compute_task_id IS NULL OR t.owner_id IS NULL)
	`).Scan(&queueCount)
	if err != nil {
		return false, err
	}

	defer func() {
		if !done {
			return
		}

		_, err = t.db.Exec(ctx, `
			UPDATE proofshare_meta SET request_task_id = NULL WHERE singleton = true
		`)
		if err != nil {
			log.Errorw("failed to reset request_task_id", "error", err)
			done = false
			err = xerrors.Errorf("failed to reset request_task_id: %w", err)
		}
	}()

	log.Infow("checked queue count", "count", queueCount)

	toRequest := RequestQueueHighWaterMark - queueCount
	toRequestGauge.Set(float64(toRequest))
	if toRequest <= 0 {
		log.Infow("queue is at or above high water mark, nothing to request")
		return true, nil
	}

	log.Infow("starting proof request loop", "toRequest", toRequest)

	for {
		/////////////////////////
		// POLL

		var pshareMeta []struct {
			Wallet string `db:"wallet"`
			Price  string `db:"pprice"`
		}
		err = t.db.Select(ctx, &pshareMeta, `
			SELECT wallet, pprice 
			FROM proofshare_meta 
			WHERE singleton = true
		`)
		if err != nil {
			return false, err
		}
		if len(pshareMeta) != 1 {
			return false, xerrors.Errorf("expected 1 pshare meta, got %d", len(pshareMeta))
		}
		meta := pshareMeta[0]

		// Poll existing work requests from the remote service
		work, err := proofsvc.PollWork(meta.Wallet)
		if err != nil {
			return false, xerrors.Errorf("failed to poll work: %w", err)
		}

		log.Infow("polled work from service", "requests", len(work.Requests), "activeAsks", len(work.ActiveAsks))

		/////////////////////////
		// INSERT NEW WORK

		// Fetch existing service IDs from the database
		var existingServiceIDs []int64
		err = t.db.Select(ctx, &existingServiceIDs, `
			SELECT service_id 
			FROM proofshare_queue
		`)
		if err != nil {
			return false, xerrors.Errorf("failed to load existing service_ids: %w", err)
		}
		existingSet := make(map[int64]bool, len(existingServiceIDs))
		for _, sid := range existingServiceIDs {
			existingSet[sid] = true
		}

		log.Infow("loaded existing service IDs", "count", len(existingServiceIDs))

		// Insert new requests that don't exist locally yet
		newlyAdded := 0
		for _, r := range work.Requests {
			if !existingSet[r.WorkAskID] {
				if r.RequestCid == nil {
					return false, xerrors.Errorf("request data cannot be nil for work ask %d", r.WorkAskID)
				}
				requestCid := *r.RequestCid
				_, insertErr := t.db.Exec(ctx, `
					INSERT INTO proofshare_queue (
						service_id,
						obtained_at,
						request_cid,
						compute_done,
						submit_done
					)
					VALUES (
						$1,
						NOW(),
						$2,
						FALSE,
						FALSE
					) ON CONFLICT (request_cid) DO UPDATE SET
						service_id = $1,
						obtained_at = NOW(),
						compute_done = FALSE,
						submit_done = FALSE
					WHERE proofshare_queue.request_cid = $2
				`, r.WorkAskID, requestCid)
				if insertErr != nil {
					return false, xerrors.Errorf("failed to insert new request: %w", insertErr)
				}
				newlyAdded++
				log.Infow("inserted new request", "workAskID", r.WorkAskID)
			}
		}

		log.Infow("processed work requests", "newlyAdded", newlyAdded)

		// Subtract newly matched requests from the total needed
		toRequest -= newlyAdded

		/////////////////////////
		// CREATE NEW ASKS

		// If we still need more requests, create new work asks
		neededAsks := toRequest - len(work.ActiveAsks)
		log.Infow("checking if more asks needed", "neededAsks", neededAsks, "toRequest", toRequest, "activeAsks", len(work.ActiveAsks))
		needAsksGauge.Set(float64(neededAsks))

		startCreate := time.Now()
		for i := 0; i < neededAsks; i++ {
			price, err := types.BigFromString(meta.Price)
			if err != nil {
				return false, xerrors.Errorf("failed to parse price: %w", err)
			}

			// Convert wallet string to address
			walletAddr, err := address.NewFromString(meta.Wallet)
			if err != nil {
				return false, xerrors.Errorf("failed to parse wallet address: %w", err)
			}

			// Create address resolver
			resolver, err := proofsvc.NewAddressResolver(t.chain)
			if err != nil {
				return false, xerrors.Errorf("failed to create address resolver: %w", err)
			}

			askID, askErr := proofsvc.CreateWorkAsk(ctx, resolver, walletAddr, abi.TokenAmount(price))
			if askErr != nil {
				return false, xerrors.Errorf("failed to create work ask: %w", askErr)
			}
			log.Infow("created new work ask", "askID", askID)
			newlyAddedCounter.Inc()
		}
		if neededAsks > 0 {
			createAsksDuration.Observe(time.Since(startCreate).Seconds())
		}

		// If we've fulfilled our quota and there are no active asks, we're done
		if toRequest <= 0 && len(work.ActiveAsks) == 0 {
			log.Infow("request quota fulfilled and no active asks remaining")
			break
		}

		// Otherwise, wait before polling again
		log.Infow("waiting before next poll", "interval", ProofRequestPollInterval)
		time.Sleep(ProofRequestPollInterval)
	}

	return true, nil
}

// TypeDetails implements harmonytask.TaskInterface.
func (t *TaskRequestProofs) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "bg:PShareRequest",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 1 << 20,
		},
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10
		},
		MaxFailures: 0,
	}
}

var _ = harmonytask.Reg(&TaskRequestProofs{})

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
