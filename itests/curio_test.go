package itests

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/itests/helpers"
	"github.com/filecoin-project/curio/tasks/seal"

	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

func TestCurioHappyPath(t *testing.T) {
	ctx := t.Context()

	full, _, db, maddr := helpers.BootstrapNetworkWithNewMiner(t, ctx, "2KiB")

	baseCfg, err := helpers.SetBaseConfigWithDefaults(t, ctx, db)
	require.NoError(t, err)

	temp := os.TempDir()
	dir, err := os.MkdirTemp(temp, "curio-happy-test")
	require.NoError(t, err)
	defer func() {
		_ = os.Remove(dir)
	}()

	helpers.StartCurioHarnessWithCleanup(ctx, t, dir, db, helpers.NewIndexStore(ctx, t, config.DefaultCurioConfig()), full, baseCfg.Apis.StorageRPCSecret, helpers.CurioHarnessOptions{
		LogLevels: []helpers.CurioLogLevel{
			{Subsystem: "harmonytask", Level: "DEBUG"},
			{Subsystem: "cu/seal", Level: "DEBUG"},
		},
	})

	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)

	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)

	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)

	wpt := mi.WindowPoStProofType
	spt, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, false)
	require.NoError(t, err)

	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		num, err := seal.AllocateSectorNumbers(ctx, full, tx, maddr, 1)
		if err != nil {
			return false, err
		}
		require.Len(t, num, 1)

		for _, n := range num {
			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", mid, n, spt)
			if err != nil {
				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
			}
		}

		return true, nil
	})

	require.NoError(t, err)
	require.True(t, comm)

	spt, err = miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, true)
	require.NoError(t, err)

	comm, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		num, err := seal.AllocateSectorNumbers(ctx, full, tx, maddr, 1)
		if err != nil {
			return false, err
		}
		require.Len(t, num, 1)

		for _, n := range num {
			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", mid, n, spt)
			if err != nil {
				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
			}
		}

		return true, nil
	})
	require.NoError(t, err)
	require.True(t, comm)

	// TODO: add DDO deal, f05 deal 2 MiB each in the sector

	var pollTask []struct {
		SpID                int64                   `db:"sp_id"`
		SectorNumber        int64                   `db:"sector_number"`
		RegisteredSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`

		TicketEpoch *int64 `db:"ticket_epoch"`

		TaskSDR  *int64 `db:"task_id_sdr"`
		AfterSDR bool   `db:"after_sdr"`

		TaskTreeD  *int64 `db:"task_id_tree_d"`
		AfterTreeD bool   `db:"after_tree_d"`

		TaskTreeC  *int64 `db:"task_id_tree_c"`
		AfterTreeC bool   `db:"after_tree_c"`

		TaskTreeR  *int64 `db:"task_id_tree_r"`
		AfterTreeR bool   `db:"after_tree_r"`

		TaskSynth  *int64 `db:"task_id_synth"`
		AfterSynth bool   `db:"after_synth"`

		PreCommitReadyAt *time.Time `db:"precommit_ready_at"`

		TaskPrecommitMsg  *int64 `db:"task_id_precommit_msg"`
		AfterPrecommitMsg bool   `db:"after_precommit_msg"`

		AfterPrecommitMsgSuccess bool   `db:"after_precommit_msg_success"`
		SeedEpoch                *int64 `db:"seed_epoch"`

		TaskPoRep  *int64 `db:"task_id_porep"`
		PoRepProof []byte `db:"porep_proof"`
		AfterPoRep bool   `db:"after_porep"`

		TaskFinalize  *int64 `db:"task_id_finalize"`
		AfterFinalize bool   `db:"after_finalize"`

		TaskMoveStorage  *int64 `db:"task_id_move_storage"`
		AfterMoveStorage bool   `db:"after_move_storage"`

		CommitReadyAt *time.Time `db:"commit_ready_at"`

		TaskCommitMsg  *int64 `db:"task_id_commit_msg"`
		AfterCommitMsg bool   `db:"after_commit_msg"`

		AfterCommitMsgSuccess bool `db:"after_commit_msg_success"`

		Failed       bool   `db:"failed"`
		FailedReason string `db:"failed_reason"`

		StartEpoch sql.NullInt64 `db:"start_epoch"`
	}

	require.Eventuallyf(t, func() bool {
		h, err := full.ChainHead(ctx)
		require.NoError(t, err)
		t.Logf("head: %d", h.Height())

		err = db.Select(ctx, &pollTask, `
											SELECT 
												sp_id, 
												sector_number, 
												reg_seal_proof, 
												ticket_epoch,
												task_id_sdr, 
												after_sdr,
												task_id_tree_d, 
												after_tree_d,
												task_id_tree_c, 
												after_tree_c,
												task_id_tree_r, 
												after_tree_r,
												task_id_synth, 
												after_synth,
												precommit_ready_at,
												task_id_precommit_msg, 
												after_precommit_msg,
												after_precommit_msg_success, 
												seed_epoch,
												task_id_porep, 
												porep_proof, 
												after_porep,
												task_id_finalize, 
												after_finalize,
												task_id_move_storage, 
												after_move_storage,
												commit_ready_at,
												task_id_commit_msg, 
												after_commit_msg,
												after_commit_msg_success,
												failed, 
												failed_reason,
												start_epoch
											FROM 
												sectors_sdr_pipeline;`)
		require.NoError(t, err)
		for i, task := range pollTask {
			t.Logf("Task %d: SpID=%d, SectorNumber=%d, RegisteredSealProof=%d, TicketEpoch=%s, TaskSDR=%s, AfterSDR=%t, TaskTreeD=%s, AfterTreeD=%t, TaskTreeC=%s, AfterTreeC=%t, TaskTreeR=%s, AfterTreeR=%t, TaskSynth=%s, AfterSynth=%t, PreCommitReadyAt=%s, TaskPrecommitMsg=%s, AfterPrecommitMsg=%t, AfterPrecommitMsgSuccess=%t, SeedEpoch=%s, TaskPoRep=%s, AfterPoRep=%t, TaskFinalize=%s, AfterFinalize=%t, TaskMoveStorage=%s, AfterMoveStorage=%t, CommitReadyAt=%s, TaskCommitMsg=%s, AfterCommitMsg=%t, AfterCommitMsgSuccess=%t, Failed=%t, FailedReason=%s, StartEpoch=%s",
				i,
				task.SpID,
				task.SectorNumber,
				task.RegisteredSealProof,
				helpers.Int64PtrOrNA(task.TicketEpoch),
				helpers.Int64PtrOrNA(task.TaskSDR),
				task.AfterSDR,
				helpers.Int64PtrOrNA(task.TaskTreeD),
				task.AfterTreeD,
				helpers.Int64PtrOrNA(task.TaskTreeC),
				task.AfterTreeC,
				helpers.Int64PtrOrNA(task.TaskTreeR),
				task.AfterTreeR,
				helpers.Int64PtrOrNA(task.TaskSynth),
				task.AfterSynth,
				helpers.TimePtrOrNA(task.PreCommitReadyAt),
				helpers.Int64PtrOrNA(task.TaskPrecommitMsg),
				task.AfterPrecommitMsg,
				task.AfterPrecommitMsgSuccess,
				helpers.Int64PtrOrNA(task.SeedEpoch),
				helpers.Int64PtrOrNA(task.TaskPoRep),
				task.AfterPoRep,
				helpers.Int64PtrOrNA(task.TaskFinalize),
				task.AfterFinalize,
				helpers.Int64PtrOrNA(task.TaskMoveStorage),
				task.AfterMoveStorage,
				helpers.TimePtrOrNA(task.CommitReadyAt),
				helpers.Int64PtrOrNA(task.TaskCommitMsg),
				task.AfterCommitMsg,
				task.AfterCommitMsgSuccess,
				task.Failed,
				task.FailedReason,
				helpers.NullInt64OrNA(task.StartEpoch),
			)
		}
		return pollTask[0].AfterCommitMsgSuccess && pollTask[1].AfterCommitMsgSuccess
	}, 10*time.Minute, 2*time.Second, "sector did not finish sealing in 10 minutes")

	require.Equal(t, pollTask[0].SectorNumber, int64(0))
	require.Equal(t, pollTask[0].SpID, int64(mid))
	require.Equal(t, pollTask[1].SectorNumber, int64(1))
	require.Equal(t, pollTask[1].SpID, int64(mid))
}
