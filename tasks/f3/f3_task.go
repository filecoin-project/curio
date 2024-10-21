package f3

import (
	"context"
	"errors"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/jpillora/backoff"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-f3/gpbft"
	"github.com/filecoin-project/go-f3/manifest"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
)

const (
	// ParticipationCheckProgressMaxAttempts defines the maximum number of failed attempts
	// before we abandon the current lease and restart the participation process.
	//
	// The default backoff takes 12 attempts to reach a maximum delay of 1 minute.
	// Allowing for 13 failures results in approximately 2 minutes of backoff since
	// the lease was granted. Given a lease validity of up to 5 instances, this means
	// we would give up on checking the lease during its mid-validity period;
	// typically when we would try to renew the participation ticket. Hence, the value
	// to 13.
	ParticipationCheckProgressMaxAttempts = 13

	// ParticipationLeaseTerm is the number of instances the miner will attempt to lease from nodes.
	ParticipationLeaseTerm = 5
)

var log = logging.Logger("cf3")

type F3ParticipationAPI interface {
	F3GetOrRenewParticipationTicket(ctx context.Context, minerID address.Address, previous api.F3ParticipationTicket, instances uint64) (api.F3ParticipationTicket, error) //perm:sign
	F3Participate(ctx context.Context, ticket api.F3ParticipationTicket) (api.F3ParticipationLease, error)
	F3GetProgress(ctx context.Context) (gpbft.Instant, error)
	F3GetManifest(ctx context.Context) (*manifest.Manifest, error)
}

type F3Task struct {
	db  *harmonydb.DB
	api F3ParticipationAPI

	leaseTerm uint64

	actors map[dtypes.MinerAddress]bool
}

func NewF3Task(db *harmonydb.DB, api F3ParticipationAPI, actors map[dtypes.MinerAddress]bool) *F3Task {
	return &F3Task{
		db:        db,
		api:       api,
		leaseTerm: ParticipationLeaseTerm,

		actors: actors,
	}
}

func (f *F3Task) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var spID int64
	err = f.db.QueryRow(ctx, "SELECT sp_id FROM f3_tasks WHERE task_id = $1", taskID).Scan(&spID)
	if err != nil {
		return false, xerrors.Errorf("failed to get sp_id: %w", err)
	}

	maddr, err := address.NewIDAddress(uint64(spID))
	if err != nil {
		return false, xerrors.Errorf("failed to parse miner address: %w", err)
	}

	for {
		if !stillOwned() {
			return false, nil
		}

		var previousTicket []byte
		err = f.db.QueryRow(ctx, "SELECT previous_ticket FROM f3_tasks WHERE task_id = $1", taskID).Scan(&previousTicket)
		if err != nil {
			return false, xerrors.Errorf("failed to get previous ticket: %w", err)
		}

		// Ensure that calls are made on the same node (the first call will determine the node)
		ctx := deps.OnSingleNode(ctx)

		ticket, err := f.tryGetF3ParticipationTicket(ctx, stillOwned, maddr, previousTicket)
		if err != nil {
			return false, xerrors.Errorf("failed to get participation ticket: %w", err)
		}

		lease, participating, err := f.tryF3Participate(ctx, stillOwned, ticket)
		if err != nil {
			return false, xerrors.Errorf("failed to participate in F3: %w", err)
		}
		if !participating {
			return false, xerrors.Errorf("failed to participate in F3: not participating")
		}

		_, err = f.db.Exec(ctx, "UPDATE f3_tasks SET previous_ticket = $1 WHERE task_id = $2", ticket, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to update previous ticket: %w", err)
		}

		bo := &backoff.Backoff{
			Min:    1 * time.Second,
			Max:    1 * time.Minute,
			Factor: 1.5,
		}

		err = f.awaitLeaseExpiry(stillOwned, ctx, lease, bo)
		if err != nil {
			return false, xerrors.Errorf("failed to await lease expiry: %w", err)
		}
	}
}

func (f *F3Task) tryGetF3ParticipationTicket(ctx context.Context, stillOwned func() bool, participant address.Address, previousTicket []byte) (api.F3ParticipationTicket, error) {
	for stillOwned() {
		switch ticket, err := f.api.F3GetOrRenewParticipationTicket(ctx, participant, previousTicket, ParticipationLeaseTerm); {
		case ctx.Err() != nil:
			return api.F3ParticipationTicket{}, ctx.Err()
		case errors.Is(err, api.ErrF3Disabled):
			log.Errorw("Cannot participate in F3 as it is disabled.", "err", err)
			return api.F3ParticipationTicket{}, xerrors.Errorf("acquiring F3 participation ticket: %w", err)
		case err != nil:
			log.Errorw("Failed to acquire F3 participation ticket; retrying", "err", err)
			time.Sleep(1 * time.Second)
			continue
		default:
			log.Debug("Successfully acquired F3 participation ticket")
			return ticket, nil
		}
	}
	return api.F3ParticipationTicket{}, ctx.Err()
}

func (f *F3Task) tryF3Participate(ctx context.Context, stillOwned func() bool, ticket api.F3ParticipationTicket) (api.F3ParticipationLease, bool, error) {
	for stillOwned() {
		switch lease, err := f.api.F3Participate(ctx, ticket); {
		case ctx.Err() != nil:
			return api.F3ParticipationLease{}, false, ctx.Err()
		case errors.Is(err, api.ErrF3Disabled):
			log.Errorw("Cannot participate in F3 as it is disabled.", "err", err)
			return api.F3ParticipationLease{}, false, xerrors.Errorf("attempting F3 participation with ticket: %w", err)
		case errors.Is(err, api.ErrF3ParticipationTicketExpired):
			log.Warnw("F3 participation ticket expired while attempting to participate. Acquiring a new ticket.", "err", err)
			return api.F3ParticipationLease{}, false, nil
		case errors.Is(err, api.ErrF3ParticipationTicketStartBeforeExisting):
			log.Warnw("F3 participation ticket starts before the existing lease. Acquiring a new ticket.", "err", err)
			return api.F3ParticipationLease{}, false, nil
		case errors.Is(err, api.ErrF3ParticipationTicketInvalid):
			log.Errorw("F3 participation ticket is not valid. Acquiring a new ticket after backoff.", "err", err)
			time.Sleep(1 * time.Second)
			return api.F3ParticipationLease{}, false, nil
		case errors.Is(err, api.ErrF3ParticipationIssuerMismatch):
			log.Warnw("Node is not the issuer of F3 participation ticket. Miner maybe load-balancing or node has changed. Retrying F3 participation after backoff.", "err", err)
			time.Sleep(1 * time.Second)
			continue
		case errors.Is(err, api.ErrF3NotReady):
			log.Warnw("F3 is not ready. Retrying F3 participation after backoff.", "err", err)
			time.Sleep(30 * time.Second)
			continue
		case err != nil:
			log.Errorw("Unexpected error while attempting F3 participation. Retrying after backoff", "err", err)
			return api.F3ParticipationLease{}, false, xerrors.Errorf("attempting F3 participation with ticket: %w", err)
		default:
			log.Infow("Successfully acquired F3 participation lease.",
				"issuer", lease.Issuer,
				"not-before", lease.FromInstance,
				"not-after", lease.ToInstance(),
			)
			return lease, true, nil
		}
	}
	return api.F3ParticipationLease{}, false, ctx.Err()
}

func (f *F3Task) awaitLeaseExpiry(stillOwned func() bool, ctx context.Context, lease api.F3ParticipationLease, backoff *backoff.Backoff) error {
	renewLeaseWithin := f.leaseTerm / 2
	for stillOwned() {
		manifest, err := f.api.F3GetManifest(ctx)
		switch {
		case errors.Is(err, api.ErrF3Disabled):
			log.Errorw("Cannot await F3 participation lease expiry as F3 is disabled.", "err", err)
			return xerrors.Errorf("awaiting F3 participation lease expiry: %w", err)
		case err != nil:
			if backoff.Attempt() > ParticipationCheckProgressMaxAttempts {
				log.Errorw("Too many failures while attempting to check F3  progress. Restarting participation.", "attempts", backoff.Attempt(), "err", err)
				return nil
			}
			log.Errorw("Failed to check F3 progress while awaiting lease expiry. Retrying after backoff.", "attempts", backoff.Attempt(), "backoff", backoff.Duration(), "err", err)
			time.Sleep(backoff.Duration())
		case manifest == nil || manifest.NetworkName != lease.Network:
			// If we got an unexpected manifest, or no manifest, go back to the
			// beginning and try to get another ticket. Switching from having a manifest
			// to having no manifest can theoretically happen if the lotus node reboots
			// and has no static manifest.
			return nil
		}
		switch progress, err := f.api.F3GetProgress(ctx); {
		case errors.Is(err, api.ErrF3Disabled):
			log.Errorw("Cannot await F3 participation lease expiry as F3 is disabled.", "err", err)
			return xerrors.Errorf("awaiting F3 participation lease expiry: %w", err)
		case err != nil:
			if backoff.Attempt() > ParticipationCheckProgressMaxAttempts {
				log.Errorw("Too many failures while attempting to check F3  progress. Restarting participation.", "attempts", backoff.Attempt(), "err", err)
				return nil
			}
			log.Errorw("Failed to check F3 progress while awaiting lease expiry. Retrying after backoff.", "attempts", backoff.Attempt(), "backoff", backoff.Duration(), "err", err)
			time.Sleep(backoff.Duration())
		case progress.ID+renewLeaseWithin >= lease.ToInstance():
			log.Infof("F3 progressed (%d) to within %d instances of lease expiry (%d). Renewing participation.", progress.ID, renewLeaseWithin, lease.ToInstance())
			return nil
		default:
			remainingInstanceLease := lease.ToInstance() - progress.ID
			waitTime := time.Duration(remainingInstanceLease-renewLeaseWithin) * manifest.CatchUpAlignment
			if waitTime == 0 {
				waitTime = 100 * time.Millisecond
			}
			log.Debugf("F3 participation lease is valid for further %d instances. Re-checking after %s.", remainingInstanceLease, waitTime)
			time.Sleep(waitTime)
		}
	}
	return ctx.Err()
}

func (f *F3Task) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (f *F3Task) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: taskhelp.BackgroundTask("F3Participate"),
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 10 << 20,
		},
		MaxFailures: 1,
	}
}

func (f *F3Task) Adder(taskFunc harmonytask.AddTaskFunc) {
	for minerAddress := range f.actors {
		mid, err := address.IDFromAddress(address.Address(minerAddress))
		if err != nil {
			log.Errorw("failed to parse miner address", "miner", minerAddress, "error", err)
			continue
		}

		taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
			n, err := tx.Exec("INSERT INTO f3_tasks (sp_id, task_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", mid, id)
			if err != nil {
				return false, err
			}

			return n > 0, nil
		})
	}
}

func (f *F3Task) GetSpid(db *harmonydb.DB, taskID int64) string {
	var spId string
	err := db.QueryRow(context.Background(), `SELECT sp_id FROM f3_tasks WHERE task_id = $1`, taskID).Scan(&spId)
	if err != nil {
		return ""
	}
	return spId
}

var _ = harmonytask.Reg(&F3Task{})
var _ harmonytask.TaskInterface = &F3Task{}
