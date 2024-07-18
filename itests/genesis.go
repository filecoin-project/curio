package itests

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/fastparamfetch"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/tasks/winning"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/lotus/api/v1api"
	bstore "github.com/filecoin-project/lotus/blockstore"
	lbuild "github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/system"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/consensus"
	"github.com/filecoin-project/lotus/chain/gen"
	genesis2 "github.com/filecoin-project/lotus/chain/gen/genesis"
	lrand "github.com/filecoin-project/lotus/chain/rand"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	"github.com/filecoin-project/lotus/genesis"
	"github.com/filecoin-project/lotus/journal"
	"github.com/filecoin-project/lotus/lib/result"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/modules/dtypes"
	"github.com/filecoin-project/lotus/storage/sealer/ffiwrapper"
	builtin0 "github.com/filecoin-project/specs-actors/actors/builtin"
	verifreg0 "github.com/filecoin-project/specs-actors/actors/builtin/verifreg"
	adt0 "github.com/filecoin-project/specs-actors/actors/util/adt"
	runtime7 "github.com/filecoin-project/specs-actors/v7/actors/runtime"
	"github.com/ipfs/boxo/blockservice"
	"github.com/ipfs/boxo/exchange/offline"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipfs/go-libipfs/blocks"
	"github.com/ipld/go-car"
	"github.com/multiformats/go-multihash"
	"github.com/snadrus/must"
	"github.com/stretchr/testify/require"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
)

type GenesisBootstrap struct {
	Genesis *types.BlockHeader
}

func MakeGenesisMem(out io.Writer, template genesis.Template) func(bs dtypes.ChainBlockstore, syscalls vm.SyscallBuilder, j journal.Journal) modules.Genesis {
	return func(bs dtypes.ChainBlockstore, syscalls vm.SyscallBuilder, j journal.Journal) modules.Genesis {
		return func() (*types.BlockHeader, error) {
			log.Warn("Generating new random genesis block, note that this SHOULD NOT happen unless you are setting up new network")
			b, err := MakeGenesisBlock(context.TODO(), j, bs, syscalls, template)
			if err != nil {
				return nil, xerrors.Errorf("make genesis block failed: %w", err)
			}
			offl := offline.Exchange(bs)
			blkserv := blockservice.New(bs, offl)
			dserv := merkledag.NewDAGService(blkserv)

			if err := car.WriteCarWithWalker(context.TODO(), dserv, []cid.Cid{b.Genesis.Cid()}, out, gen.CarWalkFunc); err != nil {
				return nil, xerrors.Errorf("failed to write car file: %w", err)
			}

			return b.Genesis, nil
		}
	}
}

func MakeGenesisBlock(ctx context.Context, j journal.Journal, bs bstore.Blockstore, sys vm.SyscallBuilder, template genesis.Template) (*GenesisBootstrap, error) {
	if j == nil {
		j = journal.NilJournal()
	}
	st, keyIDs, err := genesis2.MakeInitialStateTree(ctx, bs, template)
	if err != nil {
		return nil, xerrors.Errorf("make initial state tree failed: %w", err)
	}

	// Set up the Ethereum Address Manager
	if err = genesis2.SetupEAM(ctx, st, template.NetworkVersion); err != nil {
		return nil, xerrors.Errorf("failed to setup EAM: %w", err)
	}

	stateroot, err := st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("flush state tree failed: %w", err)
	}

	// temp chainstore
	cs := store.NewChainStore(bs, bs, datastore.NewMapDatastore(), nil, j)

	// Verify PreSealed Data
	stateroot, err = VerifyPreSealedData(ctx, cs, sys, stateroot, template, keyIDs, template.NetworkVersion)
	if err != nil {
		return nil, xerrors.Errorf("failed to verify presealed data: %w", err)
	}

	// setup Storage Miners
	stateroot, err = genesis2.SetupStorageMiners(ctx, cs, sys, stateroot, template.Miners, template.NetworkVersion, false)
	if err != nil {
		return nil, xerrors.Errorf("setup miners failed: %w", err)
	}

	st, err = state.LoadStateTree(st.Store, stateroot)
	if err != nil {
		return nil, xerrors.Errorf("failed to load updated state tree: %w", err)
	}

	// Set up Eth null addresses.
	if _, err := genesis2.SetupEthNullAddresses(ctx, st, template.NetworkVersion); err != nil {
		return nil, xerrors.Errorf("failed to set up Eth null addresses: %w", err)
	}

	stateroot, err = st.Flush(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to flush state tree: %w", err)
	}

	store := adt.WrapStore(ctx, cbor.NewCborStore(bs))
	emptyroot, err := adt0.MakeEmptyArray(store).Root()
	if err != nil {
		return nil, xerrors.Errorf("amt build failed: %w", err)
	}

	mm := &types.MsgMeta{
		BlsMessages:   emptyroot,
		SecpkMessages: emptyroot,
	}
	mmb, err := mm.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing msgmeta failed: %w", err)
	}
	if err := bs.Put(ctx, mmb); err != nil {
		return nil, xerrors.Errorf("putting msgmeta block to blockstore: %w", err)
	}

	log.Infof("Empty Genesis root: %s", emptyroot)

	tickBuf := make([]byte, 32)
	_, _ = rand.Read(tickBuf)
	genesisticket := &types.Ticket{
		VRFProof: tickBuf,
	}

	filecoinGenesisCid, err := cid.Decode("bafyreiaqpwbbyjo4a42saasj36kkrpv4tsherf2e7bvezkert2a7dhonoi")
	if err != nil {
		return nil, xerrors.Errorf("failed to decode filecoin genesis block CID: %w", err)
	}

	if !expectedCid().Equals(filecoinGenesisCid) {
		return nil, xerrors.Errorf("expectedCid != filecoinGenesisCid")
	}

	gblk, err := getGenesisBlock()
	if err != nil {
		return nil, xerrors.Errorf("failed to construct filecoin genesis block: %w", err)
	}

	if !filecoinGenesisCid.Equals(gblk.Cid()) {
		return nil, xerrors.Errorf("filecoinGenesisCid != gblk.Cid")
	}

	if err := bs.Put(ctx, gblk); err != nil {
		return nil, xerrors.Errorf("failed writing filecoin genesis block to blockstore: %w", err)
	}

	b := &types.BlockHeader{
		Miner:                 system.Address,
		Ticket:                genesisticket,
		Parents:               []cid.Cid{filecoinGenesisCid},
		Height:                0,
		ParentWeight:          types.NewInt(0),
		ParentStateRoot:       stateroot,
		Messages:              mmb.Cid(),
		ParentMessageReceipts: emptyroot,
		BLSAggregate:          nil,
		BlockSig:              nil,
		Timestamp:             template.Timestamp,
		ElectionProof:         new(types.ElectionProof),
		BeaconEntries: []types.BeaconEntry{
			{
				Round: 0,
				Data:  make([]byte, 32),
			},
		},
		ParentBaseFee: abi.NewTokenAmount(lbuild.InitialBaseFee),
	}

	sb, err := b.ToStorageBlock()
	if err != nil {
		return nil, xerrors.Errorf("serializing block header failed: %w", err)
	}

	if err := bs.Put(ctx, sb); err != nil {
		return nil, xerrors.Errorf("putting header to blockstore: %w", err)
	}

	return &GenesisBootstrap{
		Genesis: b,
	}, nil
}

var _ lrand.Rand = new(fakeRand)

// TODO: copied from actors test harness, deduplicate or remove from here
type fakeRand struct{}

func (fr *fakeRand) GetChainRandomness(ctx context.Context, randEpoch abi.ChainEpoch) ([32]byte, error) {
	out := make([]byte, 32)
	_, _ = mrand.New(mrand.NewSource(int64(randEpoch * 1000))).Read(out) //nolint
	return *(*[32]byte)(out), nil
}

func (fr *fakeRand) GetBeaconRandomness(ctx context.Context, randEpoch abi.ChainEpoch) ([32]byte, error) {
	out := make([]byte, 32)
	_, _ = mrand.New(mrand.NewSource(int64(randEpoch))).Read(out) //nolint
	return *(*[32]byte)(out), nil
}

func VerifyPreSealedData(ctx context.Context, cs *store.ChainStore, sys vm.SyscallBuilder, stateroot cid.Cid, template genesis.Template, keyIDs map[address.Address]address.Address, nv network.Version) (cid.Cid, error) {
	verifNeeds := make(map[address.Address]abi.PaddedPieceSize)
	var sum abi.PaddedPieceSize

	csc := func(context.Context, abi.ChainEpoch, *state.StateTree) (abi.TokenAmount, error) {
		return big.Zero(), nil
	}

	vmopt := vm.VMOpts{
		StateBase:      stateroot,
		Epoch:          0,
		Rand:           &fakeRand{},
		Bstore:         cs.StateBlockstore(),
		Actors:         consensus.NewActorRegistry(),
		Syscalls:       mkFakedSigSyscalls(sys),
		CircSupplyCalc: csc,
		NetworkVersion: nv,
		BaseFee:        big.Zero(),
	}
	vm, err := vm.NewVM(ctx, &vmopt)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create VM: %w", err)
	}

	for mi, m := range template.Miners {
		for si, s := range m.Sectors {
			if s.Deal.Provider != m.ID {
				return cid.Undef, xerrors.Errorf("Sector %d in miner %d in template had mismatch in provider and miner ID: %s != %s", si, mi, s.Deal.Provider, m.ID)
			}

			amt := s.Deal.PieceSize
			verifNeeds[keyIDs[s.Deal.Client]] += amt
			sum += amt
		}
	}

	verifregRoot, err := address.NewIDAddress(80)
	if err != nil {
		return cid.Undef, err
	}

	verifier, err := address.NewIDAddress(81)
	if err != nil {
		return cid.Undef, err
	}

	// Note: This is brittle, if the methodNum / param changes, it could break things
	_, err = doExecValue(ctx, vm, verifreg.Address, verifregRoot, types.NewInt(0), builtin0.MethodsVerifiedRegistry.AddVerifier, mustEnc(&verifreg0.AddVerifierParams{

		Address:   verifier,
		Allowance: abi.NewStoragePower(int64(1024 * 1024)), // eh, close enough

	}))
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create verifier: %w", err)
	}

	for c, _ := range verifNeeds {
		// Note: This is brittle, if the methodNum / param changes, it could break things
		_, err := doExecValue(ctx, vm, verifreg.Address, verifier, types.NewInt(0), builtin0.MethodsVerifiedRegistry.AddVerifiedClient, mustEnc(&verifreg0.AddVerifiedClientParams{
			Address:   c,
			Allowance: abi.NewStoragePower(int64(1024 * 1024)),
		}))
		if err != nil {
			return cid.Undef, xerrors.Errorf("failed to add verified client: %w", err)
		}
	}

	st, err := vm.Flush(ctx)
	if err != nil {
		return cid.Cid{}, xerrors.Errorf("vm flush: %w", err)
	}

	return st, nil
}

type fakedSigSyscalls struct {
	runtime7.Syscalls
}

func (fss *fakedSigSyscalls) VerifySignature(signature crypto.Signature, signer address.Address, plaintext []byte) error {
	return nil
}

func mkFakedSigSyscalls(base vm.SyscallBuilder) vm.SyscallBuilder {
	return func(ctx context.Context, rt *vm.Runtime) runtime7.Syscalls {
		return &fakedSigSyscalls{
			base(ctx, rt),
		}
	}
}

func mustEnc(i cbg.CBORMarshaler) []byte {
	enc, err := actors.SerializeParams(i)
	if err != nil {
		panic(err) // ok
	}
	return enc
}

func doExecValue(ctx context.Context, vm vm.Interface, to, from address.Address, value types.BigInt, method abi.MethodNum, params []byte) ([]byte, error) {
	ret, err := vm.ApplyImplicitMessage(ctx, &types.Message{
		To:       to,
		From:     from,
		Method:   method,
		Params:   params,
		GasLimit: 1_000_000_000_000_000,
		Value:    value,
		Nonce:    0,
	})
	if err != nil {
		return nil, xerrors.Errorf("doExec apply message failed: %w", err)
	}

	if ret.ExitCode != 0 {
		return nil, xerrors.Errorf("failed to call method: %w", ret.ActorErr)
	}

	return ret.Return, nil
}

const genesisMultihashString = "1220107d821c25dc0735200249df94a8bebc9c8e489744f86a4ca8919e81f19dcd72"
const genesisBlockHex = "a5684461746574696d6573323031372d30352d30352030313a32373a3531674e6574776f726b6846696c65636f696e65546f6b656e6846696c65636f696e6c546f6b656e416d6f756e7473a36b546f74616c537570706c796d322c3030302c3030302c303030664d696e6572736d312c3430302c3030302c3030306c50726f746f636f6c4c616273a36b446576656c6f706d656e746b3330302c3030302c3030306b46756e6472616973696e676b3230302c3030302c3030306a466f756e646174696f6e6b3130302c3030302c303030674d657373616765784854686973206973207468652047656e6573697320426c6f636b206f66207468652046696c65636f696e20446563656e7472616c697a65642053746f72616765204e6574776f726b2e"

var cidBuilder = cid.V1Builder{Codec: cid.DagCBOR, MhType: multihash.SHA2_256}

func expectedCid() cid.Cid {
	mh, err := multihash.FromHexString(genesisMultihashString)
	if err != nil {
		panic(err)
	}
	return cid.NewCidV1(cidBuilder.Codec, mh)
}

func getGenesisBlock() (blocks.Block, error) {
	genesisBlockData, err := hex.DecodeString(genesisBlockHex)
	if err != nil {
		return nil, err
	}

	genesisCid, err := cidBuilder.Sum(genesisBlockData)
	if err != nil {
		return nil, err
	}

	block, err := blocks.NewBlockWithCid(genesisBlockData, genesisCid)
	if err != nil {
		return nil, err
	}

	return block, nil
}

func NewBlockMiner(ctx context.Context, t *testing.T, full v1api.FullNode, db *harmonydb.DB, store *paths.Remote, local *paths.Local, maddr address.Address) {
	maddrs := make(map[dtypes.MinerAddress]bool)
	maddrs[dtypes.MinerAddress(maddr)] = true
	proofTypes := make(map[abi.RegisteredSealProof]bool)
	proofTypes[abi.RegisteredSealProof_StackedDrg2KiBV1_1] = true
	proofTypes[abi.RegisteredSealProof_StackedDrg2KiBV1] = true

	var fetchOnce sync.Once
	var fetchResult atomic.Pointer[result.Result[bool]]

	var asyncParams = func() func() (bool, error) {
		fetchOnce.Do(func() {
			go func() {
				for spt := range proofTypes {

					provingSize := uint64(must.One(spt.SectorSize()))
					err := fastparamfetch.GetParams(context.TODO(), lbuild.ParametersJSON(), lbuild.SrsJSON(), provingSize)

					if err != nil {
						log.Errorw("failed to fetch params", "error", err)
						fetchResult.Store(&result.Result[bool]{Value: false, Error: err})
						return
					}
				}

				fetchResult.Store(&result.Result[bool]{Value: true})
			}()
		})

		return func() (bool, error) {
			res := fetchResult.Load()
			if res == nil {
				return false, nil
			}
			return res.Value, res.Error
		}
	}

	fh := &paths.FetchHandler{Local: local, PfHandler: &paths.DefaultPartialFileHandler{}}
	//remoteHandler := func(w http.ResponseWriter, r *http.Request) {
	//	fh.ServeHTTP(w, r)
	//}

	srv := &http.Server{
		Handler:           fh,
		ReadHeaderTimeout: time.Minute * 3,
		//BaseContext: func(listener net.Listener) context.Context {
		//	ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "lotus-worker"))
		//	return ctx
		//},
		Addr: "127.0.0.1:12310",
	}

	go func() {
		serr := srv.ListenAndServe()
		require.NoError(t, serr)
		t.Cleanup(func() {
			_ = srv.Shutdown(context.Background())
		})
	}()

	winPoStTask := winning.NewWinPostTask(10000, db, store, ffiwrapper.ProofVerifier, asyncParams(), full, maddrs)
	addTask := func(extra func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {
		var tID harmonytask.TaskID
		_, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
			// create taskID (from DB)
			err := tx.QueryRow(`INSERT INTO harmony_task (name, added_by, posted_time) 
          VALUES ($1, $2, CURRENT_TIMESTAMP) RETURNING id`, winPoStTask.TypeDetails().Name, 100).Scan(&tID)
			if err != nil {
				return false, fmt.Errorf("could not insert into harmonyTask: %w", err)
			}
			return extra(tID, tx)
		})

		if err != nil {
			if harmonydb.IsErrUniqueContraint(err) {
				return
			}
			require.NoError(t, err)
		}
	}
	winPoStTask.Adder(addTask)

	for {
		select {
		case <-time.After(time.Second): // Find work periodically
		case <-ctx.Done(): ///////////////////// Graceful exit
			return
		}

		var unownedTasks []harmonytask.TaskID
		err := db.Select(ctx, &unownedTasks, `SELECT id 
			FROM harmony_task
			WHERE owner_id IS NULL AND name=$1
			ORDER BY update_time`, winPoStTask.TypeDetails().Name)
		if err != nil {
			log.Error("Unable to read work ", err)
			continue
		}
		if len(unownedTasks) > 0 {
			for _, id := range unownedTasks {
				done, err := winPoStTask.Do(id, func() bool { return true })
				require.NoError(t, err)
				require.True(t, done)
			}
		}
	}
}
