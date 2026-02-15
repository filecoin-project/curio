package itests

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/docker/go-units"
	logging "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/tasks/seal"

	lapi "github.com/filecoin-project/lotus/api"
	miner2 "github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/cli/spcli/createminer"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestRemoteSealHappyPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = logging.SetLogLevel("*", "INFO")
	_ = logging.SetLogLevel("harmonytask", "DEBUG")
	_ = logging.SetLogLevel("cu/seal", "DEBUG")
	_ = logging.SetLogLevel("cu-http", "DEBUG")
	_ = logging.SetLogLevel("sealmarket", "DEBUG")
	_ = logging.SetLogLevel("remoteseal", "DEBUG")

	full, miner, ensemble := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(32),
		kit.ThroughRPC(),
	)
	ensemble.Start()
	blockTime := 100 * time.Millisecond
	ensemble.BeginMining(blockTime)

	full.WaitTillChain(ctx, kit.HeightAtLeast(15))

	_ = miner.LogSetLevel(ctx, "*", "ERROR")
	_ = full.LogSetLevel(ctx, "*", "ERROR")

	token, err := full.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)
	fapi := fmt.Sprintf("%s:%s", string(token), full.ListenAddr)

	sharedITestID := harmonydb.ITestNewID()
	t.Logf("sharedITestID: %s", sharedITestID)

	db, err := harmonydb.NewFromConfigWithITestID(t, sharedITestID)
	require.NoError(t, err)
	defer db.ITestDeleteAll()

	idxStore, err := indexstore.NewIndexStore([]string{testutils.EnvElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")}, 9042, config.DefaultCurioConfig())
	require.NoError(t, err)
	err = idxStore.Start(ctx, true)
	require.NoError(t, err)

	// Create miner
	addr := miner.OwnerKey.Address
	sectorSizeInt, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)
	maddr, err := createminer.CreateStorageMiner(ctx, full, addr, addr, addr, abi.SectorSize(sectorSizeInt), 0, 1.0)
	require.NoError(t, err)

	err = deps.CreateMinerConfig(ctx, full, db, []string{maddr.String()}, fapi)
	require.NoError(t, err)

	// Load base config
	baseCfg := config.DefaultCurioConfig()
	var baseText string
	err = db.QueryRow(ctx, "SELECT config FROM harmony_config WHERE title='base'").Scan(&baseText)
	require.NoError(t, err)
	_, err = deps.LoadConfigWithUpgrades(baseText, baseCfg)
	require.NoError(t, err)

	baseCfg.Batching.PreCommit.Timeout = time.Second
	baseCfg.Batching.Commit.Timeout = time.Second

	// Provider config: SDR + Trees + Remote Seal Provider + DealMarket (for HTTP)
	providerCfg := *baseCfg
	providerCfg.Subsystems.EnableSealSDR = true
	providerCfg.Subsystems.EnableSealSDRTrees = true
	providerCfg.Subsystems.EnableRemoteSealProvider = true
	providerCfg.Subsystems.EnableDealMarket = true

	// Client config: Remote Seal Client + PoRep + commit flow + DealMarket (for HTTP)
	clientCfg := *baseCfg
	clientCfg.Subsystems.EnableRemoteSealClient = true
	clientCfg.Subsystems.EnablePoRepProof = true
	clientCfg.Subsystems.EnableSendPrecommitMsg = true
	clientCfg.Subsystems.EnableSendCommitMsg = true
	clientCfg.Subsystems.EnableMoveStorage = true
	clientCfg.Subsystems.EnableDealMarket = true

	// Save configs
	cb, err := config.ConfigUpdate(&providerCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", string(cb))
	require.NoError(t, err)

	// Create temp dirs for provider and client
	providerDir, err := os.MkdirTemp("", "curio-provider-*")
	require.NoError(t, err)
	defer os.RemoveAll(providerDir)

	clientDir, err := os.MkdirTemp("", "curio-client-*")
	require.NoError(t, err)
	defer os.RemoveAll(clientDir)

	// Start provider instance
	t.Log("Starting provider instance...")
	providerAPI, providerTerm, providerCloser, providerFinish := ConstructCurioTest(ctx, t, providerDir, db, idxStore, full, maddr, &providerCfg)
	defer providerTerm()
	defer providerCloser()

	// Wait for provider machine to register
	time.Sleep(2 * time.Second)

	// Now start client instance (uses same DB, different temp dir)
	// We need a separate DB connection since ConstructCurioTest checks harmony_machines
	// and we now have the provider in there. Let's update the config for client.

	// Save the client config as a separate layer
	ccb, err := config.ConfigUpdate(&clientCfg, config.DefaultCurioConfig(), config.Commented(true), config.DefaultKeepUncommented(), config.NoEnv())
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO harmony_config (title, config) VALUES ($1, $2) ON CONFLICT (title) DO UPDATE SET config = $2`, "base", string(ccb))
	require.NoError(t, err)

	t.Log("Starting client instance...")
	clientAPI, clientTerm, clientCloser, clientFinish := ConstructCurioTest(ctx, t, clientDir, db, idxStore, full, maddr, &clientCfg)
	defer clientTerm()
	defer clientCloser()

	// Wait for both instances to settle
	time.Sleep(3 * time.Second)

	// Get provider's host_and_port from harmony_machines to build the provider URL
	var machines []struct {
		HostAndPort string `db:"host_and_port"`
	}
	err = db.Select(ctx, &machines, `SELECT host_and_port FROM harmony_machines ORDER BY id`)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(machines), 1, "expected at least 1 machine")
	t.Logf("Machines registered: %+v", machines)

	// For remote seal, we need the provider's HTTP endpoint.
	// In test, the HTTP server may not start because DealMarket deps may not be fully wired.
	// Instead, we'll directly insert the partner/provider DB rows to set up the relationship.
	// This tests the pipeline tasks without needing the HTTP setup flow.

	// Generate a test token
	tokenBytes := make([]byte, 32)
	_, err = rand.Read(tokenBytes)
	require.NoError(t, err)
	testToken := hex.EncodeToString(tokenBytes)

	// Insert partner on provider side
	var partnerID int64
	err = db.QueryRow(ctx, `INSERT INTO rseal_delegated_partners (partner_name, partner_url, partner_token, allowance_remaining, allowance_total)
		VALUES ($1, $2, $3, $4, $4) RETURNING id`,
		"test-client", "http://localhost:0", testToken, int64(100)).Scan(&partnerID)
	require.NoError(t, err)
	t.Logf("Created partner ID: %d with token: %s", partnerID, testToken[:8]+"...")

	// Insert provider on client side
	mid, err := address.IDFromAddress(maddr)
	require.NoError(t, err)

	// For the client provider entry, we need the provider's HTTP base URL.
	// Since HTTP servers may not be running in test, use the first machine's host_and_port
	// as a placeholder - the actual HTTP calls are handled by tasks that poll the DB.
	providerURL := fmt.Sprintf("http://%s", machines[0].HostAndPort)

	var providerID int64
	err = db.QueryRow(ctx, `INSERT INTO rseal_client_providers (sp_id, provider_url, provider_token, provider_name)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		int64(mid), providerURL, testToken, "test-provider").Scan(&providerID)
	require.NoError(t, err)
	t.Logf("Created provider ID: %d", providerID)

	// Get seal proof type
	mi, err := full.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	require.NoError(t, err)
	nv, err := full.StateNetworkVersion(ctx, types.EmptyTSK)
	require.NoError(t, err)
	wpt := mi.WindowPoStProofType
	spt, err := miner2.PreferredSealProofTypeFromWindowPoStType(nv, wpt, true)
	require.NoError(t, err)

	// Allocate a sector and insert into the pipeline
	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		nums, err := seal.AllocateSectorNumbers(ctx, full, tx, maddr, 1)
		if err != nil {
			return false, err
		}
		require.Len(t, nums, 1)

		sectorNum := nums[0]
		t.Logf("Allocated sector number: %d", sectorNum)

		// Insert into sectors_sdr_pipeline
		_, err = tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3)`,
			int64(mid), sectorNum, spt)
		if err != nil {
			return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
		}

		// Insert into rseal_client_pipeline to indicate this sector is remotely sealed
		_, err = tx.Exec(`INSERT INTO rseal_client_pipeline (sp_id, sector_number, provider_id, reg_seal_proof)
			VALUES ($1, $2, $3, $4)`,
			int64(mid), sectorNum, providerID, spt)
		if err != nil {
			return false, xerrors.Errorf("inserting into rseal_client_pipeline: %w", err)
		}

		// Also insert into rseal_provider_pipeline so the provider side picks it up
		_, err = tx.Exec(`INSERT INTO rseal_provider_pipeline (partner_id, sp_id, sector_number, reg_seal_proof)
			VALUES ($1, $2, $3, $4)`,
			partnerID, int64(mid), sectorNum, spt)
		if err != nil {
			return false, xerrors.Errorf("inserting into rseal_provider_pipeline: %w", err)
		}

		return true, nil
	})
	require.NoError(t, err)
	require.True(t, comm)

	t.Log("Sector pipeline entries created, waiting for sealing to complete...")

	// Poll for completion
	var pollTask []struct {
		SpID                     int64         `db:"sp_id"`
		SectorNumber             int64         `db:"sector_number"`
		AfterSDR                 bool          `db:"after_sdr"`
		AfterTreeD               bool          `db:"after_tree_d"`
		AfterTreeC               bool          `db:"after_tree_c"`
		AfterTreeR               bool          `db:"after_tree_r"`
		AfterSynth               bool          `db:"after_synth"`
		AfterPrecommitMsg        bool          `db:"after_precommit_msg"`
		AfterPrecommitMsgSuccess bool          `db:"after_precommit_msg_success"`
		AfterPoRep               bool          `db:"after_porep"`
		AfterFinalize            bool          `db:"after_finalize"`
		AfterMoveStorage         bool          `db:"after_move_storage"`
		AfterCommitMsg           bool          `db:"after_commit_msg"`
		AfterCommitMsgSuccess    bool          `db:"after_commit_msg_success"`
		Failed                   bool          `db:"failed"`
		FailedReason             string        `db:"failed_reason"`
		StartEpoch               sql.NullInt64 `db:"start_epoch"`
	}

	require.Eventuallyf(t, func() bool {
		h, err := full.ChainHead(ctx)
		require.NoError(t, err)
		t.Logf("head: %d", h.Height())

		err = db.Select(ctx, &pollTask, `SELECT sp_id, sector_number,
			after_sdr, after_tree_d, after_tree_c, after_tree_r, after_synth,
			after_precommit_msg, after_precommit_msg_success,
			after_porep, after_finalize, after_move_storage,
			after_commit_msg, after_commit_msg_success,
			failed, failed_reason, start_epoch
			FROM sectors_sdr_pipeline WHERE sp_id = $1`, int64(mid))
		require.NoError(t, err)

		for i, task := range pollTask {
			t.Logf("Task %d: sp=%d sector=%d sdr=%t treeD=%t treeC=%t treeR=%t synth=%t precommit=%t precommitOK=%t porep=%t finalize=%t move=%t commit=%t commitOK=%t failed=%t reason=%s",
				i, task.SpID, task.SectorNumber,
				task.AfterSDR, task.AfterTreeD, task.AfterTreeC, task.AfterTreeR, task.AfterSynth,
				task.AfterPrecommitMsg, task.AfterPrecommitMsgSuccess,
				task.AfterPoRep, task.AfterFinalize, task.AfterMoveStorage,
				task.AfterCommitMsg, task.AfterCommitMsgSuccess,
				task.Failed, task.FailedReason)
		}

		// Also log remote seal pipeline status
		var provPipeline []struct {
			SpID          int64  `db:"sp_id"`
			SectorNumber  int64  `db:"sector_number"`
			AfterSDR      bool   `db:"after_sdr"`
			AfterTreeR    bool   `db:"after_tree_r"`
			AfterNotify   bool   `db:"after_notify_client"`
			AfterC1       bool   `db:"after_c1_supplied"`
			AfterFinalize bool   `db:"after_finalize"`
			AfterCleanup  bool   `db:"after_cleanup"`
			Failed        bool   `db:"failed"`
			FailedMsg     string `db:"failed_reason_msg"`
		}
		_ = db.Select(ctx, &provPipeline, `SELECT sp_id, sector_number, after_sdr, after_tree_r, after_notify_client, after_c1_supplied, after_finalize, after_cleanup, failed, failed_reason_msg FROM rseal_provider_pipeline`)
		for _, pp := range provPipeline {
			t.Logf("ProvPipeline: sp=%d sector=%d sdr=%t treeR=%t notify=%t c1=%t finalize=%t cleanup=%t failed=%t msg=%s",
				pp.SpID, pp.SectorNumber, pp.AfterSDR, pp.AfterTreeR, pp.AfterNotify, pp.AfterC1, pp.AfterFinalize, pp.AfterCleanup, pp.Failed, pp.FailedMsg)
		}

		var clientPipeline []struct {
			SpID         int64  `db:"sp_id"`
			SectorNumber int64  `db:"sector_number"`
			AfterSDR     bool   `db:"after_sdr"`
			AfterTreeR   bool   `db:"after_tree_r"`
			AfterFetch   bool   `db:"after_fetch"`
			AfterC1      bool   `db:"after_c1_exchange"`
			AfterCleanup bool   `db:"after_cleanup"`
			Failed       bool   `db:"failed"`
			FailedMsg    string `db:"failed_reason_msg"`
		}
		_ = db.Select(ctx, &clientPipeline, `SELECT sp_id, sector_number, after_sdr, after_tree_r, after_fetch, after_c1_exchange, after_cleanup, failed, failed_reason_msg FROM rseal_client_pipeline`)
		for _, cp := range clientPipeline {
			t.Logf("ClientPipeline: sp=%d sector=%d sdr=%t treeR=%t fetch=%t c1=%t cleanup=%t failed=%t msg=%s",
				cp.SpID, cp.SectorNumber, cp.AfterSDR, cp.AfterTreeR, cp.AfterFetch, cp.AfterC1, cp.AfterCleanup, cp.Failed, cp.FailedMsg)
		}

		if len(pollTask) == 0 {
			return false
		}

		// Check if the sector completed the full pipeline
		for _, task := range pollTask {
			if task.Failed {
				t.Errorf("sector %d failed: %s", task.SectorNumber, task.FailedReason)
				return false
			}
			if !task.AfterCommitMsgSuccess {
				return false
			}
		}
		return true
	}, 15*time.Minute, 2*time.Second, "remote seal pipeline did not complete in 15 minutes")

	t.Log("Remote seal pipeline completed successfully!")

	_ = providerAPI.Shutdown(ctx)
	_ = clientAPI.Shutdown(ctx)
	<-providerFinish
	<-clientFinish
}
