//go:build integration

package seal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

// Use only an explicitly selected disposable loopback PostgreSQL instance.
// NewFromConfigWithITestID applies the real migrations in an isolated database.
func precommitTestDB(t *testing.T) *harmonydb.DB {
	t.Helper()
	port := os.Getenv("CURIO_PRECOMMIT_TEST_PORT")
	if port == "" {
		t.Skip("set CURIO_PRECOMMIT_TEST_PORT for a disposable loopback PostgreSQL instance")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	require.NotZero(t, n)
	db, err := harmonydb.NewFromConfigWithITestID(t, func(o *harmonydb.ItestOptions) {
		o.Hosts, o.Port = []string{"127.0.0.1"}, port
		o.Database, o.Username, o.Password = "postgres", "postgres", ""
	})
	require.NoError(t, err)
	return db
}

type batchPrecommitAPI struct {
	mockPrecommitAPI
	head       *types.TipSet
	depositErr error
}

func (a *batchPrecommitAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return a.head, nil
}

func (a *batchPrecommitAPI) StateMinerPreCommitDepositForPower(context.Context, address.Address, miner.SectorPreCommitInfo, types.TipSetKey) (big.Int, error) {
	return big.Zero(), a.depositErr
}

func (a *batchPrecommitAPI) StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error) {
	worker, err := address.NewIDAddress(1001)
	return api.MinerInfo{Worker: worker}, err
}

type batchPrecommitSender func(*types.Message) (cid.Cid, error)

func (s batchPrecommitSender) Send(_ context.Context, msg *types.Message, _ *api.MessageSendSpec, _ string) (cid.Cid, error) {
	return s(msg)
}

func precommitTestHead(t *testing.T) *types.TipSet {
	t.Helper()
	addr, err := address.NewIDAddress(1000)
	require.NoError(t, err)
	root := cid.MustParse("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	head, err := types.NewTipSet([]*types.BlockHeader{{
		Miner: addr, Height: 1000, Ticket: &types.Ticket{VRFProof: []byte{1}},
		ParentStateRoot: root, Messages: root, ParentMessageReceipts: root,
		BlockSig:     &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate: &crypto.Signature{Type: crypto.SigTypeSecp256k1}, ParentBaseFee: big.NewInt(100),
	}})
	require.NoError(t, err)
	return head
}

func TestPrecommitBatchIsolation(t *testing.T) {
	for _, scenario := range []string{"mixed", "all-invalid", "deposit-error", "failure-write-error", "send-error"} {
		t.Run(scenario, func(t *testing.T) {
			db, ctx := precommitTestDB(t), t.Context()
			exec := func(_ int, err error) {
				t.Helper()
				require.NoError(t, err)
			}
			const commitment = "bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4"
			exec(db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline
				(sp_id,sector_number,reg_seal_proof,ticket_epoch,tree_r_cid,tree_d_cid,task_id_precommit_msg)
				SELECT 1000,n,$1,900,$2,$2,77 FROM generate_series(1,5) n`,
				abi.RegisteredSealProof_StackedDrg32GiBV1_1, commitment))
			exec(db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET ticket_epoch=$1 WHERE sector_number=2`,
				1000-policy.MaxPreCommitRandomnessLookback-1))
			exec(db.Exec(ctx, `UPDATE sectors_sdr_pipeline SET failed=TRUE,failed_reason='previous' WHERE sector_number=5`))
			exec(db.Exec(ctx, `INSERT INTO sectors_sdr_initial_pieces
				(sp_id,sector_number,piece_index,piece_cid,piece_size,data_url,data_raw_size,data_delete_on_finalize,direct_start_epoch,direct_end_epoch)
				VALUES (1000,1,0,$1,128,'test',127,FALSE,999,2000000)`, commitment))
			// Same sector under another provider and another task must be untouched.
			exec(db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline (sp_id,sector_number,reg_seal_proof,task_id_precommit_msg)
				VALUES (2000,1,8,88),(1000,7,8,88)`))
			if scenario == "all-invalid" {
				exec(db.Exec(ctx, `DELETE FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number IN (3,4)`))
			}
			if scenario == "failure-write-error" {
				exec(db.Exec(ctx, `CREATE FUNCTION reject_failure() RETURNS trigger LANGUAGE plpgsql AS $$
					BEGIN RAISE EXCEPTION 'injected failure write error'; END $$;
					CREATE TRIGGER reject_failure BEFORE UPDATE ON sectors_sdr_pipeline
					FOR EACH ROW WHEN (NEW.failed) EXECUTE FUNCTION reject_failure()`))
			}
			testAPI := &batchPrecommitAPI{head: precommitTestHead(t)}
			if scenario == "deposit-error" {
				testAPI.depositErr = errors.New("injected deposit RPC error")
			}
			var sent []abi.SectorNumber
			calls := 0
			messageCID := cid.MustParse(commitment)
			task := &SubmitPrecommitTask{db: db, api: testAPI, feeCfg: &config.CurioFees{},
				sender: batchPrecommitSender(func(msg *types.Message) (cid.Cid, error) {
					calls++
					var params miner.PreCommitSectorBatchParams2
					require.NoError(t, params.UnmarshalCBOR(bytes.NewReader(msg.Params)))
					for _, sector := range params.Sectors {
						sent = append(sent, sector.SectorNumber)
					}
					if scenario == "send-error" {
						return cid.Undef, errors.New("injected send error")
					}
					// A row attached after message construction must not receive its CID.
					exec(db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline (sp_id,sector_number,reg_seal_proof,task_id_precommit_msg)
						VALUES (1000,6,8,77)`))
					return messageCID, nil
				}),
			}
			done, err := task.Do(ctx, 77, func() bool { return true })
			var healthyFailed int
			require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
				WHERE sp_id=1000 AND sector_number IN (3,4) AND failed`).Scan(&healthyFailed))
			require.Zero(t, healthyFailed, "one invalid sector must not fail valid batch members")
			if scenario == "mixed" || scenario == "all-invalid" {
				require.NoError(t, err)
				require.True(t, done)
			} else {
				require.Error(t, err)
				require.False(t, done)
			}
			var recorded []int64
			require.NoError(t, db.Select(ctx, &recorded, `SELECT sector_number FROM sectors_sdr_pipeline
				WHERE after_precommit_msg ORDER BY sector_number`))
			if scenario == "mixed" {
				require.Equal(t, []abi.SectorNumber{3, 4}, sent)
				require.Equal(t, []int64{3, 4}, recorded)
				var matching int
				require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
					WHERE precommit_msg_cid=$1 AND task_id_precommit_msg IS NULL`, messageCID.String()).Scan(&matching))
				require.Equal(t, 2, matching)
				require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM message_waits WHERE signed_message_cid=$1`, messageCID.String()).Scan(&matching))
				require.Equal(t, 1, matching)
			} else {
				require.Empty(t, recorded)
				var waits int
				require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM message_waits`).Scan(&waits))
				require.Zero(t, waits)
			}
			if scenario == "mixed" || scenario == "send-error" {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
			var protected int
			require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
				WHERE task_id_precommit_msg=88 AND NOT failed AND precommit_msg_cid IS NULL`).Scan(&protected))
			require.Equal(t, 2, protected)
			if scenario != "failure-write-error" {
				var failures int
				require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
					WHERE sp_id=1000 AND sector_number IN (1,2) AND failed
					AND task_id_precommit_msg IS NULL AND precommit_msg_cid IS NULL`).Scan(&failures))
				require.Equal(t, 2, failures)
			} else {
				var unchanged int
				require.NoError(t, db.QueryRow(ctx, `SELECT COUNT(*) FROM sectors_sdr_pipeline
					WHERE sp_id=1000 AND sector_number BETWEEN 1 AND 4 AND NOT failed
					AND task_id_precommit_msg=77`).Scan(&unchanged))
				require.Equal(t, 4, unchanged)
			}
			if scenario == "mixed" || scenario == "all-invalid" {
				var preserved bool
				require.NoError(t, db.QueryRow(ctx, `SELECT failed AND failed_reason='previous'
					AND task_id_precommit_msg IS NULL AND precommit_msg_cid IS NULL
					FROM sectors_sdr_pipeline WHERE sp_id=1000 AND sector_number=5`).Scan(&preserved))
				require.True(t, preserved)
			}
		})
	}
}

type precommitBatchPollerAPI struct {
	SealPollerAPI
	head *types.TipSet
}

func (a precommitBatchPollerAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return a.head, nil
}

func TestPrecommitBatchSkipsFailedCandidates(t *testing.T) {
	db, ctx := precommitTestDB(t), t.Context()
	_, err := db.Exec(ctx, `INSERT INTO sectors_sdr_pipeline
		(sp_id,sector_number,reg_seal_proof,ticket_epoch,after_synth,failed)
		VALUES (1000,1,8,1,TRUE,TRUE),(1000,2,8,2,TRUE,FALSE)`)
	require.NoError(t, err)
	poller := &SealPoller{db: db, api: precommitBatchPollerAPI{head: precommitTestHead(t)},
		cfg: pollerConfig{preCommit: preCommitBatchingConfig{MaxPreCommitBatch: 1}}}
	poller.pollers[pollerPrecommitMsg].Set(func(extra func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) { return extra(77, tx) })
		require.NoError(t, err)
		require.True(t, committed)
	})
	poller.pollStartBatchPrecommitMsg(ctx)
	var assigned []int64
	require.NoError(t, db.Select(ctx, &assigned, `SELECT sector_number FROM sectors_sdr_pipeline WHERE task_id_precommit_msg=77`))
	require.Equal(t, []int64{2}, assigned)
}
