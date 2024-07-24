package itests

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/cmd/curio/rpc"
	"github.com/filecoin-project/curio/cmd/curio/tasks"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/market/lmrpc"
	"github.com/filecoin-project/curio/tasks/seal"
	mockSeal "github.com/filecoin-project/curio/tasks/seal/mocks"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	markettypes "github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"
	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/messagepool"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet/key"
	"github.com/filecoin-project/lotus/gateway"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/journal/fsjournal"
	"github.com/filecoin-project/lotus/lib/lazy"
	"github.com/filecoin-project/lotus/node"
	lconfig "github.com/filecoin-project/lotus/node/config"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/node/repo"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
	"github.com/gbrlsnchs/jwt/v3"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/minio/blake2b-simd"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/snadrus/must"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

const tmax = 100
const FilecoinPrecision = uint64(1_000_000_000_000_000_000)
const pastOffset = 10000000 * time.Second
const TestNetworkVersion = network.Version22

var initBalance = big.Mul(big.NewInt(100000000), types.NewInt(FilecoinPrecision))

var log = logging.Logger("itest")
var ActorDebugging = true

func init() {
	_ = logging.SetLogLevel("*", "INFO")
	_ = os.Setenv("RUST_LOG", "DEBUG")

	chain.BootstrapPeerThreshold = 1
	messagepool.HeadChangeCoalesceMinDelay = time.Microsecond
	messagepool.HeadChangeCoalesceMaxDelay = 2 * time.Microsecond
	messagepool.HeadChangeCoalesceMergeInterval = 100 * time.Nanosecond

	// These values mimic the values set in the builtin-actors when configured to use the "testing" network. Specifically:
	// - All proof types.
	// - 2k minimum power.
	// - "small" verified deals.
	// - short precommit
	//
	// See:
	// - https://github.com/filecoin-project/builtin-actors/blob/0502c0722225ee58d1e6641431b4f9356cb2d18e/actors/runtime/src/runtime/policy.rs#L235
	// - https://github.com/filecoin-project/builtin-actors/blob/0502c0722225ee58d1e6641431b4f9356cb2d18e/actors/runtime/build.rs#L17-L45
	policy.SetProviderCollateralSupplyTarget(big.Zero(), big.NewInt(1))
	policy.SetConsensusMinerMinPower(abi.NewStoragePower(2048))
	policy.SetMinVerifiedDealSize(abi.NewStoragePower(256))
	policy.SetPreCommitChallengeDelay(10)
	policy.SetSupportedProofTypes(
		abi.RegisteredSealProof_StackedDrg2KiBV1,
		abi.RegisteredSealProof_StackedDrg8MiBV1,
		abi.RegisteredSealProof_StackedDrg512MiBV1,
		abi.RegisteredSealProof_StackedDrg32GiBV1,
		abi.RegisteredSealProof_StackedDrg64GiBV1,
	)
	policy.SetProviderCollateralSupplyTarget(big.NewInt(0), big.NewInt(100))

	//lbuild.InsecurePoStValidation = true

	if err := os.Setenv("BELLMAN_NO_GPU", "1"); err != nil {
		panic(fmt.Sprintf("failed to set BELLMAN_NO_GPU env variable: %s", err))
	}

	if err := os.Setenv("LOTUS_DISABLE_WATCHDOG", "1"); err != nil {
		panic(fmt.Sprintf("failed to set LOTUS_DISABLE_WATCHDOG env variable: %s", err))
	}
}

type pipeline struct {
	miner      address.Address
	sdr        *seal.SDRTask
	treed      *seal.TreeDTask
	treerc     *seal.TreeRCTask
	sectorSize abi.SectorSize
	remote     *paths.Remote
	local      *paths.Local
}

// TestFullNode represents a full node enrolled in an Ensemble.
type TestFullNode struct {
	v1api.FullNode
	EthSubRouter *gateway.EthSubHandler
	ListenAddr   multiaddr.Multiaddr
	ListenURL    string
	DefaultKey   *key.Key
	Stop         node.StopFunc
}

func mockTipset(minerAddr address.Address, timestamp uint64) (*types.TipSet, error) {
	return types.NewTipSet([]*types.BlockHeader{{
		Miner:                 minerAddr,
		Height:                5,
		ParentStateRoot:       cid.MustParse("bafkqaaa"),
		Messages:              cid.MustParse("bafkqaaa"),
		ParentMessageReceipts: cid.MustParse("bafkqaaa"),
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeBLS},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeBLS},
		Timestamp:             timestamp,
	}})
}

func createInitSealer(t *testing.T, ctx context.Context, db *harmonydb.DB, rpcSecret string, dir string) pipeline {

	ctrl := gomock.NewController(t)
	sdrapi := mockSeal.NewMockSDRAPI(ctrl)
	var full v1api.FullNode

	maddr, err := address.NewFromString("t01000")
	require.NoError(t, err)

	head, err := mockTipset(maddr, 0)
	require.NoError(t, err)
	trand := blake2b.Sum256([]byte("make genesis mem random"))
	ticket := abi.Randomness(trand[:])

	sdrapi.EXPECT().ChainHead(gomock.Any()).Return(head, nil).MaxTimes(2)
	sdrapi.EXPECT().StateGetRandomnessFromTickets(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ticket, nil).MaxTimes(2)

	pip := pipeline{
		miner: maddr,
	}

	storageDir := path.Join(dir, "storage")

	strcfg := storiface.LocalStorageMeta{
		ID:       storiface.ID(uuid.New().String()),
		Weight:   10,
		CanSeal:  true,
		CanStore: true,
	}

	err = os.Mkdir(storageDir, 0755)
	require.NoError(t, err)
	b, err := json.MarshalIndent(strcfg, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(storageDir, "sectorstore.json"), b, 0644)
	require.NoError(t, err)

	localPaths := &paths.BasicLocalStorage{
		PathToJSON: path.Join(dir, "storage.json"),
	}

	de, err := journal.ParseDisabledEvents("")
	require.NoError(t, err)
	j, err := fsjournal.OpenFSJournalPath(path.Join(dir, "curioJournal"), de)
	require.NoError(t, err)

	go func() {
		<-ctx.Done()
		_ = j.Close()
	}()

	//al := alerting.NewAlertingSystem(j)
	si := paths.NewDBIndex(nil, db)
	sp := seal.NewPoller(db, full)
	ssize, err := units.RAMInBytes("2KiB")
	require.NoError(t, err)

	pip.sectorSize = abi.SectorSize(ssize)

	sa, err := deps.StorageAuth(rpcSecret)
	require.NoError(t, err)

	lstor, err := paths.NewLocal(ctx, localPaths, si, []string{"http://" + "127.0.0.1:12310" + "/remote"})
	require.NoError(t, err)
	//pip.local = lstor
	err = lstor.OpenPath(ctx, storageDir)
	require.NoError(t, err)
	err = localPaths.SetStorage(func(storageConfig *storiface.StorageConfig) {
		storageConfig.StoragePaths = append(storageConfig.StoragePaths, storiface.LocalPath{Path: storageDir})
	})
	require.NoError(t, err)

	rstor := paths.NewRemote(lstor, si, http.Header(sa), 10, &paths.DefaultPartialFileHandler{})
	//pip.remote = rstor
	slrLazy := lazy.MakeLazy(func() (*ffi.SealCalls, error) {
		return ffi.NewSealCalls(rstor, lstor, si), nil
	})
	pip.sdr = seal.NewSDRTask(sdrapi, db, sp, must.One(slrLazy.Val()), tmax)

	pip.treed = seal.NewTreeDTask(sp, db, must.One(slrLazy.Val()), tmax)
	pip.treerc = seal.NewTreeRCTask(sp, db, must.One(slrLazy.Val()), tmax)

	return pip
}

func (n *Ensemble) genesisMinerInit() {
	t := n.t
	ctx := n.ctx

	sharedITestID := harmonydb.ITestNewID()
	dbConfig := config.HarmonyDB{
		Hosts:    []string{envElse("CURIO_HARMONYDB_HOSTS", "127.0.0.1")},
		Database: "yugabyte",
		Username: "yugabyte",
		Password: "yugabyte",
		Port:     "5433",
	}
	db, err := harmonydb.NewFromConfigWithITestID(t, dbConfig, sharedITestID)
	require.NoError(t, err)

	n.db = db

	workDir, err := os.MkdirTemp(os.TempDir(), "curio-test")
	require.NoError(t, err)

	n.workDir = workDir
	n.t.Cleanup(func() {
		_ = os.RemoveAll(workDir)
	})

	sk, err := io.ReadAll(io.LimitReader(rand.Reader, 32))
	require.NoError(t, err)

	rpcSecret := base64.StdEncoding.EncodeToString(sk)
	p := createInitSealer(t, ctx, db, rpcSecret, workDir)
	//n.remote = p.remote
	//n.local = p.local
	n.miner = p.miner

	ctrl := gomock.NewController(t)
	allocAPI := mockSeal.NewMockAllocAPI(ctrl)
	bret := bitfield.New()
	allocAPI.EXPECT().StateMinerAllocated(gomock.Any(), gomock.Any(), gomock.Any()).Return(&bret, nil).AnyTimes()

	num, err := seal.AllocateSectorNumbers(ctx, allocAPI, db, p.miner, 2, func(tx *harmonydb.Tx, numbers []abi.SectorNumber) (bool, error) {
		for _, n := range numbers {
			_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) values ($1, $2, $3)", 1000, n, abi.RegisteredSealProof_StackedDrg2KiBV1_1)
			if err != nil {
				return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
			}
		}
		return true, nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, len(num))

	// PreSeal sectors
	for i := 0; i < len(num); i++ {
		var tID harmonytask.TaskID
		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// create taskID (from DB)
			serr := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) 
          VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, p.sdr.TypeDetails().Name, 0).Scan(&tID)
			if serr != nil {
				return false, serr
			}
			n, serr := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_sdr = $1 WHERE sp_id = $2 AND sector_number = $3 AND task_id_sdr IS NULL`, tID, 1000, i)
			if serr != nil {
				return false, serr
			}
			if n != 1 {
				return false, xerrors.New("not 1 row")
			}
			return true, nil
		})
		require.NoError(t, err)
		require.True(t, comm)
		done, err := p.sdr.Do(tID, func() bool {
			return true
		})
		require.NoError(t, err)
		require.True(t, done)
		_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id=$1`, tID)
		require.NoError(t, err)

		comm, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// create taskID (from DB)
			serr := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) 
          VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, p.treed.TypeDetails().Name, 0).Scan(&tID)
			if serr != nil {
				return false, serr
			}
			n, serr := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_tree_d = $1 WHERE sp_id = $2 AND sector_number = $3 AND after_sdr = TRUE AND task_id_tree_d IS NULL`, tID, 1000, i)
			if serr != nil {
				return false, serr
			}
			if n != 1 {
				return false, xerrors.New("not 1 row")
			}
			return true, nil
		})
		require.NoError(t, err)
		require.True(t, comm)
		done, err = p.treed.Do(tID, func() bool {
			return true
		})
		require.NoError(t, err)
		require.True(t, done)
		_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id=$1`, tID)
		require.NoError(t, err)

		comm, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// create taskID (from DB)
			serr := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) 
          VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, p.treerc.TypeDetails().Name, 0).Scan(&tID)
			if serr != nil {
				return false, serr
			}
			n, serr := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_tree_c = $1, task_id_tree_r = $1 WHERE sp_id = $2 AND sector_number = $3 AND after_tree_d = TRUE AND task_id_tree_c IS NULL AND task_id_tree_r IS NULL`, tID, 1000, i)
			if serr != nil {
				return false, serr
			}
			if n != 1 {
				return false, xerrors.New("not 1 row")
			}
			return true, nil
		})
		require.NoError(t, err)
		require.True(t, comm)
		done, err = p.treerc.Do(tID, func() bool {
			return true
		})
		require.NoError(t, err)
		require.True(t, done)
		_, err = db.Exec(ctx, `DELETE FROM harmony_task WHERE id=$1`, tID)
		require.NoError(t, err)
	}

	pkey, _, err := ic.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)
	pid, err := peer.IDFromPrivateKey(pkey)
	require.NoError(t, err)

	ckey, err := key.GenerateKey(types.KTBLS)
	require.NoError(n.t, err)

	genacc := genesis.Actor{
		Type:    genesis.TAccount,
		Balance: initBalance,
		Meta:    (&genesis.AccountMeta{Owner: ckey.Address}).ActorMeta(),
	}

	n.genesis.accounts = append(n.genesis.accounts, genacc)

	var sealedSectors []*genesis.PreSeal
	for i := 0; i < len(num); i++ {
		var sectorDetails []struct {
			CommR string `db:"tree_r_cid"`
			CommD string `db:"tree_d_cid"`
		}
		err = db.Select(ctx, &sectorDetails, `SELECT tree_r_cid, tree_d_cid FROM sectors_sdr_pipeline WHERE sp_id = 1000 AND sector_Number = $1`, num[i])
		require.NoError(t, err)

		commR, err := cid.Parse(sectorDetails[0].CommR)
		require.NoError(t, err)
		commD, err := cid.Parse(sectorDetails[0].CommD)
		require.NoError(t, err)
		label, err := markettypes.NewLabelFromString(fmt.Sprintf("%d", i))
		require.NoError(t, err)

		proposal := markettypes.DealProposal{
			PieceCID:             commD,
			PieceSize:            abi.PaddedPieceSize(p.sectorSize),
			Client:               ckey.Address,
			Provider:             p.miner,
			Label:                label,
			StartEpoch:           0,
			EndEpoch:             9001,
			StoragePricePerEpoch: big.Zero(),
			ProviderCollateral:   big.Zero(),
			ClientCollateral:     big.Zero(),
		}

		sealedSectors = append(sealedSectors, &genesis.PreSeal{
			CommR:         commR,
			CommD:         commD,
			ProofType:     abi.RegisteredSealProof_StackedDrg2KiBV1_1,
			SectorID:      num[i],
			Deal:          proposal,
			DealClientKey: ckey.KeyInfo,
		})
	}

	n.genesis.miners = append(n.genesis.miners, genesis.Miner{
		ID:            p.miner,
		MarketBalance: big.Zero(),
		PowerBalance:  big.Zero(),
		SectorSize:    p.sectorSize,
		Sectors:       sealedSectors,
		PeerId:        pid,
	})

	_, err = db.Exec(ctx, `DELETE FROM sectors_sdr_pipeline`)
	require.NoError(t, err)
}

type genesys struct {
	version  network.Version
	miners   []genesis.Miner
	accounts []genesis.Actor
}

type Ensemble struct {
	t            *testing.T
	ctx          context.Context
	genesisBlock bytes.Buffer
	db           *harmonydb.DB
	mn           mocknet.Mocknet
	Full         *TestFullNode
	MinerAccount genesis.Actor
	workDir      string
	remote       *paths.Remote
	local        *paths.Local
	miner        address.Address

	genesis *genesys
}

func NewEnsemble(t *testing.T) *Ensemble {
	var full TestFullNode
	gen := genesys{
		version: network.Version22,
	}
	e := &Ensemble{
		t:       t,
		Full:    &full,
		ctx:     context.Background(),
		genesis: &gen,
	}
	e.genesisMinerInit()
	return e.Start()
}

// Start starts all enrolled nodes.
func (n *Ensemble) Start() *Ensemble {
	ctx := n.ctx
	n.Full.EthSubRouter = gateway.NewEthSubHandler()
	lapi.RunningNodeType = lapi.NodeFull
	rkey, err := key.GenerateKey(types.KTBLS)
	require.NoError(n.t, err)

	genacc := genesis.Actor{
		Type:    genesis.TAccount,
		Balance: initBalance,
		Meta:    (&genesis.AccountMeta{Owner: rkey.Address}).ActorMeta(),
	}

	//n.genesis.version = TestNetworkVersion

	n.genesis.accounts = append(n.genesis.accounts, genacc)

	minerAddr, err := key.GenerateKey(types.KTBLS)
	require.NoError(n.t, err)

	genacc = genesis.Actor{
		Type:    genesis.TAccount,
		Balance: initBalance,
		Meta:    (&genesis.AccountMeta{Owner: minerAddr.Address}).ActorMeta(),
	}
	n.genesis.accounts = append(n.genesis.accounts, genacc)
	n.genesis.miners[0].Owner = minerAddr.Address
	n.genesis.miners[0].Worker = minerAddr.Address

	gtempl := n.generateGenesis()
	n.mn = mocknet.New()

	repoPath := path.Join(n.workDir, "lotus")
	r, err := repo.NewFS(repoPath)
	require.NoError(n.t, err)
	require.NoError(n.t, r.Init(repo.FullNode))

	//setup config with options
	lr, err := r.Lock(repo.FullNode)
	require.NoError(n.t, err)
	c, err := lr.Config()
	require.NoError(n.t, err)

	cfg, ok := c.(*lconfig.FullNode)
	if !ok {
		n.t.Fatalf("invalid config from repo, got: %T", c)
	}
	cfg.Fevm.EnableEthRPC = true
	cfg.Events.MaxFilterHeightRange = math.MaxInt64
	cfg.Events.EnableActorEventsAPI = true
	cfg.Chainstore.EnableSplitstore = false
	err = lr.SetConfig(func(raw interface{}) {
		rcfg := raw.(*lconfig.FullNode)
		*rcfg = *cfg
	})
	require.NoError(n.t, err)

	err = lr.Close()
	require.NoError(n.t, err)

	var fapi v1api.FullNode

	opts := []node.Option{
		node.FullAPI(&fapi),
		node.Base(),
		node.Repo(r),
		//node.If(full.options.disableLibp2p, node.MockHost(n.mn)),
		node.Test(),

		// so that we subscribe to pubsub topics immediately
		node.Override(new(dtypes.Bootstrapper), dtypes.Bootstrapper(true)),

		// upgrades
		//node.Override(new(stmgr.UpgradeSchedule), stmgr.UpgradeSchedule{{
		//	Height:  1,
		//	Network: TestNetworkVersion,
		//}}),
	}

	opts = append(opts, node.Override(new(modules.Genesis), MakeGenesisMem(&n.genesisBlock, *gtempl)))

	// Are we mocking proofs?
	//if n.options.mockProofs {
	//	opts = append(opts,
	//		node.Override(new(storiface.Verifier), mock.MockVerifier),
	//		node.Override(new(storiface.Prover), mock.MockProver),
	//	)
	//}

	// Construct the full node.
	stop, err := node.New(ctx, opts...)
	n.Full.Stop = stop

	require.NoError(n.t, err)
	n.Full.FullNode = fapi

	addr, err := fapi.WalletImport(ctx, &rkey.KeyInfo)
	require.NoError(n.t, err)

	err = fapi.WalletSetDefault(ctx, addr)
	require.NoError(n.t, err)

	addr, err = fapi.WalletImport(ctx, &minerAddr.KeyInfo)
	require.NoError(n.t, err)

	//var rpcShutdownOnce sync.Once
	var stopOnce sync.Once
	var stopErr error

	stopFunc := stop
	stop = func(ctx context.Context) error {
		stopOnce.Do(func() {
			stopErr = stopFunc(ctx)
		})
		return stopErr
	}

	//rpcCloser := fullRpc(n.t, n.Full, api)
	////n.Full.FullNode = withRPC
	//n.Full.Stop = func(ctx2 context.Context) error {
	//	rpcShutdownOnce.Do(rpcCloser)
	//	return stop(ctx)
	//}
	//n.t.Cleanup(func() { rpcShutdownOnce.Do(rpcCloser) })

	n.t.Cleanup(func() {
		_ = stop(context.Background())
	})

	//go NewBlockMiner(ctx, n.t, api, n.db, n.remote, n.local, n.miner)

	return n
}

func (n *Ensemble) generateGenesis() *genesis.Template {
	var verifRoot = gen.DefaultVerifregRootkeyActor

	templ := &genesis.Template{
		NetworkVersion:   n.genesis.version,
		Accounts:         n.genesis.accounts,
		Miners:           n.genesis.miners,
		NetworkName:      "test",
		Timestamp:        uint64(time.Now().Unix() - int64(pastOffset.Seconds())),
		VerifregRootKey:  verifRoot,
		RemainderAccount: gen.DefaultRemainderAccountActor,
	}

	fmt.Println(*templ)

	return templ
}

type Closer func()

func CreateRPCServer(t *testing.T, handler http.Handler, listener net.Listener) (*httptest.Server, multiaddr.Multiaddr, Closer) {
	testServ := &httptest.Server{
		Listener: listener,
		Config: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 30 * time.Second,
		},
	}
	testServ.Start()

	addr := testServ.Listener.Addr()
	maddr, err := manet.FromNetAddr(addr)
	require.NoError(t, err)
	closer := func() {
		testServ.CloseClientConnections()
		testServ.Close()
	}

	return testServ, maddr, closer
}
func fullRpc(t *testing.T, f *TestFullNode, fullNode v1api.FullNode) Closer {
	handler, err := node.FullNodeHandler(fullNode, false)
	require.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv, maddr, rpcCloser := CreateRPCServer(t, handler, l)
	fmt.Printf("FULLNODE RPC ENV FOR CLI DEBUGGING `export FULLNODE_API_INFO=%s`\n", "ws://"+srv.Listener.Addr().String())
	sendItestdNotif("FULLNODE_API_INFO", t.Name(), "ws://"+srv.Listener.Addr().String())

	rpcOpts := []jsonrpc.Option{
		jsonrpc.WithClientHandler("Filecoin", f.EthSubRouter),
		jsonrpc.WithClientHandlerAlias("eth_subscription", "Filecoin.EthSubscription"),
	}

	cl, stop, err := client.NewFullNodeRPCV1(context.Background(), "ws://"+srv.Listener.Addr().String()+"/rpc/v1", nil, rpcOpts...)
	require.NoError(t, err)
	f.ListenAddr, f.ListenURL, f.FullNode = maddr, srv.URL, cl

	return func() { stop(); rpcCloser() }
}

type ItestdNotif struct {
	NodeType string // api env var name
	TestName string
	Api      string
}

func sendItestdNotif(nodeType, testName, apiAddr string) {
	td := os.Getenv("LOTUS_ITESTD")
	if td == "" {
		// not running
		return
	}

	notif := ItestdNotif{
		NodeType: nodeType,
		TestName: testName,
		Api:      apiAddr,
	}
	nb, err := json.Marshal(&notif)
	if err != nil {
		return
	}

	if _, err := http.Post(td, "application/json", bytes.NewReader(nb)); err != nil { // nolint:gosec
		return
	}
}

func createCliContext(dir string) (*cli.Context, error) {
	// Define flags for the command
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "listen",
			Usage:   "host address and port the worker api will listen on",
			Value:   "0.0.0.0:12300",
			EnvVars: []string{"LOTUS_WORKER_LISTEN"},
		},
		&cli.BoolFlag{
			Name:  "nosync",
			Usage: "don't check full-node sync status",
		},
		&cli.BoolFlag{
			Name:   "halt-after-init",
			Usage:  "only run init, then return",
			Hidden: true,
		},
		&cli.BoolFlag{
			Name:  "manage-fdlimit",
			Usage: "manage open file limit",
			Value: true,
		},
		&cli.StringFlag{
			Name:  "storage-json",
			Usage: "path to json file containing storage config",
			Value: "~/.curio/storage.json",
		},
		&cli.StringFlag{
			Name:  "journal",
			Usage: "path to journal files",
			Value: "~/.curio/",
		},
		&cli.StringSliceFlag{
			Name:    "layers",
			Aliases: []string{"l", "layer"},
			Usage:   "list of layers to be interpreted (atop defaults)",
		},
	}

	// Set up the command with flags
	command := &cli.Command{
		Name:  "simulate",
		Flags: flags,
		Action: func(c *cli.Context) error {
			fmt.Println("Listen address:", c.String("listen"))
			fmt.Println("No-sync:", c.Bool("nosync"))
			fmt.Println("Halt after init:", c.Bool("halt-after-init"))
			fmt.Println("Manage file limit:", c.Bool("manage-fdlimit"))
			fmt.Println("Storage config path:", c.String("storage-json"))
			fmt.Println("Journal path:", c.String("journal"))
			fmt.Println("Layers:", c.StringSlice("layers"))
			return nil
		},
	}

	// Create a FlagSet and populate it
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range flags {
		if err := f.Apply(set); err != nil {
			return nil, xerrors.Errorf("Error applying flag: %s\n", err)
		}
	}

	curioDir := path.Join(dir, "curio")
	cflag := fmt.Sprintf("--storage-json=%s", curioDir)

	storage := path.Join(dir, "storage.json")
	sflag := fmt.Sprintf("--journal=%s", storage)

	// Parse the flags with test values
	err := set.Parse([]string{"--listen=0.0.0.0:12345", "--nosync", "--manage-fdlimit", sflag, cflag, "--layers=seal"})
	if err != nil {
		return nil, xerrors.Errorf("Error setting flag: %s\n", err)
	}

	// Create a cli.Context from the FlagSet
	app := cli.NewApp()
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = command

	return ctx, nil
}

func ConstructCurioTest(ctx context.Context, t *testing.T, parent, dir string, db *harmonydb.DB, full v1api.FullNode, maddr address.Address, cfg *config.CurioConfig) (api.Curio, func(), jsonrpc.ClientCloser, <-chan struct{}) {
	ffiselect.IsTest = true

	cctx, err := createCliContext(dir)
	require.NoError(t, err)

	shutdownChan := make(chan struct{})

	{
		var ctxclose func()
		ctx, ctxclose = context.WithCancel(ctx)
		go func() {
			<-shutdownChan
			ctxclose()
		}()
	}

	dependencies := &deps.Deps{}
	dependencies.DB = db
	dependencies.Chain = full
	seal.SetDevnet(true)
	err = os.Setenv("CURIO_REPO_PATH", dir)
	require.NoError(t, err)
	err = dependencies.PopulateRemainingDeps(ctx, cctx, false)
	require.NoError(t, err)

	dependencies.Cfg.Subsystems.EnableWinningPost = true
	dependencies.Cfg.Subsystems.EnableWindowPost = true

	taskEngine, err := tasks.StartTasks(ctx, dependencies)
	require.NoError(t, err)

	dependencies.Cfg.Subsystems.BoostAdapters = []string{fmt.Sprintf("%s:127.0.0.1:32000", maddr)}
	err = lmrpc.ServeCurioMarketRPCFromConfig(dependencies.DB, dependencies.Chain, dependencies.Cfg)
	require.NoError(t, err)

	go func() {
		err = rpc.ListenAndServe(ctx, dependencies, shutdownChan) // Monitor for shutdown.
		require.NoError(t, err)
	}()

	finishCh := node.MonitorShutdown(shutdownChan)

	var machines []string
	err = db.Select(ctx, &machines, `select host_and_port from harmony_machines`)
	require.NoError(t, err)

	require.Len(t, machines, 1)
	laddr, err := net.ResolveTCPAddr("tcp", machines[0])
	require.NoError(t, err)

	ma, err := manet.FromNetAddr(laddr)
	require.NoError(t, err)

	var apiToken []byte
	{
		type jwtPayload struct {
			Allow []auth.Permission
		}

		p := jwtPayload{
			Allow: lapi.AllPermissions,
		}

		sk, err := base64.StdEncoding.DecodeString(cfg.Apis.StorageRPCSecret)
		require.NoError(t, err)

		apiToken, err = jwt.Sign(&p, jwt.NewHS256(sk))
		require.NoError(t, err)
	}

	ctoken := fmt.Sprintf("%s:%s", string(apiToken), ma)
	err = os.Setenv("CURIO_API_INFO", ctoken)
	require.NoError(t, err)

	capi, ccloser, err := rpc.GetCurioAPI(&cli.Context{})
	require.NoError(t, err)

	scfg := storiface.LocalStorageMeta{
		ID:         storiface.ID(uuid.New().String()),
		Weight:     10,
		CanSeal:    true,
		CanStore:   true,
		MaxStorage: 0,
		Groups:     []string{},
		AllowTo:    []string{},
	}

	err = capi.StorageInit(ctx, dir, scfg)
	require.NoError(t, err)

	err = capi.StorageAddLocal(ctx, dir)
	require.NoError(t, err)

	err = capi.StorageAddLocal(ctx, path.Join(parent, "storage"))
	require.NoError(t, err)

	slist, err := capi.StorageList(ctx)
	require.NoError(t, err)

	for k, v := range slist {
		log.Infof("STORAGE %s %s", k, v)
	}

	_ = logging.SetLogLevel("harmonytask", "DEBUG")

	return capi, taskEngine.GracefullyTerminate, ccloser, finishCh
}

func envElse(env, els string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return els
}
