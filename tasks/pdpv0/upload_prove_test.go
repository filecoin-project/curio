package pdpv0

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/fxamacker/cbor/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/boxo/ipld/unixfs"
	"github.com/ipfs/go-cid"
	ipldformat "github.com/ipfs/go-ipld-format"
	car "github.com/ipld/go-car"
	"github.com/ipld/go-car/util"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"

	"github.com/filecoin-project/curio/cuhttp"
	curiodeps "github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/hashfolder"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/piecestore"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/pdp/contract"
)

// TestUploadThenProve uploads a piece, places it, and generates a possession proof.
// A public piece lands in open-pieces; a restricted piece lands only in acl-pieces.
func TestUploadThenProve(t *testing.T) {
	t.Run("public", func(t *testing.T) {
		h := newUploadProveHarness(t)
		raw := bytes.Repeat([]byte{0xab}, 1024)
		runUploadProve(t, h, raw, 0 /* public */, true, http.StatusCreated)
	})
	t.Run("acl", func(t *testing.T) {
		h := newUploadProveHarness(t)
		fee := feePayload(t)
		sum, err := mh.Sum(fee, mh.SHA2_256, -1)
		require.NoError(t, err)
		leafCID := cid.NewCidV1(cid.Raw, sum)
		fsn := unixfs.NewFSNode(unixfs.TFile)
		fsn.AddBlockSize(uint64(len(fee)))
		fsBytes, err := fsn.GetBytes()
		require.NoError(t, err)
		nd := new(merkledag.ProtoNode)
		require.NoError(t, nd.SetCidBuilder(merkledag.V1CidPrefix()))
		nd.SetData(fsBytes)
		require.NoError(t, nd.AddRawLink("", &ipldformat.Link{Name: "", Size: uint64(len(fee)), Cid: leafCID}))
		var buf bytes.Buffer
		require.NoError(t, car.WriteHeader(&car.CarHeader{Roots: []cid.Cid{nd.Cid()}, Version: 1}, &buf))
		require.NoError(t, util.LdWrite(&buf, nd.Cid().Bytes(), nd.RawData()))
		require.NoError(t, util.LdWrite(&buf, leafCID.Bytes(), fee))
		raw := buf.Bytes()
		runUploadProve(t, h, raw, 1 /* restricted */, false, http.StatusCreated)
	})
	t.Run("acl rejects piece without outer CAR", func(t *testing.T) {
		h := newUploadProveHarness(t)
		runUploadProve(t, h, feePayload(t), 1 /* restricted */, false, http.StatusBadRequest)
	})
}

type uploadProveHarness struct {
	ctx     context.Context
	db      *harmonydb.DB
	hs      *hashfolder.Cluster
	cpr     *cachedreader.CachedPieceReader
	pieceIO piecestore.PieceIO
	srv     *httptest.Server
}

func newUploadProveHarness(t *testing.T) *uploadProveHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	root := t.TempDir()
	storageDir := filepath.Join(root, "storage")
	require.NoError(t, os.MkdirAll(storageDir, 0o755))

	storageID := storiface.ID(uuid.New().String())
	meta := &storiface.LocalStorageMeta{
		ID:       storageID,
		Weight:   10,
		CanSeal:  true,
		CanStore: true,
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storageDir, "sectorstore.json"), mb, 0o644))

	bls := &paths.BasicLocalStorage{PathToJSON: filepath.Join(root, "storage.json")}
	index := paths.NewDBIndex(nil, db)
	localStore, err := paths.NewLocal(ctx, bls, index, "")
	require.NoError(t, err)
	require.NoError(t, localStore.OpenPath(ctx, storageDir))

	remote, err := paths.NewRemote(localStore, index, nil, 20, &paths.DefaultPartialFileHandler{})
	require.NoError(t, err)

	localPaths, err := localStore.Local(ctx)
	require.NoError(t, err)
	var drives []hashfolder.LocalDrive
	for _, p := range localPaths {
		if !p.CanStore {
			continue
		}
		drives = append(drives, hashfolder.LocalDrive{StorageID: string(p.ID), Root: p.LocalPath})
	}
	hs, err := hashfolder.NewCluster(ctx, db, drives, nil)
	require.NoError(t, err)
	require.NotNil(t, hs)
	t.Cleanup(func() { require.NoError(t, hs.Close()) })

	cpr := cachedreader.NewCachedPieceReader(
		db,
		pieceprovider.NewSectorReader(remote, index),
		pieceprovider.NewPieceParkReader(remote, index),
		nil,
	)
	cpr.SetOpenPieceReader(pieceprovider.NewOpenPieceReader(hs))
	cpr.SetACLPieceReader(pieceprovider.NewACLPieceReader(hs))

	cfg := config.DefaultCurioConfig()
	cfg.HTTP.DenylistServers = config.NewDynamic([]string{})

	deps := &curiodeps.Deps{
		DB:                db,
		Cfg:               cfg,
		CachedPieceReader: cpr,
		LocalStore:        localStore,
		Stor:              remote,
		Si:                index,
	}

	senderAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	_, err = db.Exec(ctx, `
		INSERT INTO eth_keys (address, private_key, role)
		VALUES ($1, $2, 'pdp')
		ON CONFLICT DO NOTHING`,
		senderAddr.Hex(), make([]byte, 32))
	require.NoError(t, err)

	// CI runs with --tags=debug (BuildDebug), which otherwise requires
	// CURIO_DEVNET_* env vars. Install process-wide test addresses instead.
	recordKeeper := common.HexToAddress("0x2222222222222222222222222222222222222222")
	contract.SetAddresses(contract.Addresses{
		PDPVerifier: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		FWSService:  recordKeeper,
	})

	var txSeq atomic.Uint64
	mockSender := uploadProveEthSender(func(_ context.Context, _ common.Address, _ *ethtypes.Transaction, _ string) (common.Hash, error) {
		n := txSeq.Add(1)
		return common.HexToHash(fmt.Sprintf("%064x", n)), nil
	})

	mux := chi.NewMux()
	_ = cuhttp.MountRetrievalPublicRoutes(ctx, mux, deps)

	svcCtx, svcCancel := context.WithCancel(ctx)
	t.Cleanup(svcCancel)
	pieceIO := piecestore.New(remote, localStore, index)
	require.NoError(t, pdp.MountRoutes(svcCtx, mux, pdp.MountDeps{
		DB:         db,
		PieceIO:    pieceIO,
		EthClient:  uploadProveEthClient{},
		EthSender:  mockSender,
		HashSpaces: hs,
		Local:      localStore,
	}, nil))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &uploadProveHarness{
		ctx:     ctx,
		db:      db,
		hs:      hs,
		cpr:     cpr,
		pieceIO: pieceIO,
		srv:     srv,
	}
}

func runUploadProve(t *testing.T, h *uploadProveHarness, raw []byte, acl int, expectPublicRetrieve bool, wantAdd int) {
	t.Helper()
	ctx := h.ctx

	recordKeeper := common.HexToAddress("0x2222222222222222222222222222222222222222")
	createBody, err := json.Marshal(map[string]string{"recordKeeper": recordKeeper.Hex()})
	require.NoError(t, err)
	createRes, err := http.Post(h.srv.URL+"/pdp/data-sets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createRes.StatusCode, uploadProveHTTPBody(t, createRes))
	createLoc := createRes.Header.Get("Location")
	require.NoError(t, createRes.Body.Close())
	require.Contains(t, createLoc, "/pdp/data-sets/created/")
	createTxHash := strings.TrimPrefix(createLoc, "/pdp/data-sets/created/")

	dataSetID := int64(4242 + acl) // distinct IDs across subtests sharing a DB is fine; each harness has its own DB
	_, err = h.db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready, ipfs_indexing, acl)
		VALUES ($1, $2, 'public', 100, 10, FALSE, FALSE, $3)`,
		dataSetID, createTxHash, acl)
	require.NoError(t, err)

	cp := &commp.Calc{}
	_, err = cp.Write(raw)
	require.NoError(t, err)
	digest, padded, err := cp.Digest()
	require.NoError(t, err)
	pieceCidV1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	pieceCidV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(len(raw)))
	require.NoError(t, err)

	postBody, err := json.Marshal(map[string]string{"pieceCid": pieceCidV2.String()})
	require.NoError(t, err)
	postRes, err := http.Post(h.srv.URL+"/pdp/piece", "application/json", bytes.NewReader(postBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, postRes.StatusCode, uploadProveHTTPBody(t, postRes))
	uploadLoc := postRes.Header.Get("Location")
	require.NoError(t, postRes.Body.Close())

	putReq, err := http.NewRequest(http.MethodPut, h.srv.URL+uploadLoc, bytes.NewReader(raw))
	require.NoError(t, err)
	putRes, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, putRes.StatusCode, uploadProveHTTPBody(t, putRes))
	require.NoError(t, putRes.Body.Close())

	var parkedID, pieceRef, pdpPieceRefID int64
	var parkedComplete bool
	err = h.db.QueryRow(ctx, `
		SELECT pr.id, pr.piece_ref, pp.id, pp.complete
		FROM pdp_piecerefs pr
		JOIN parked_piece_refs ppr ON ppr.ref_id = pr.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pr.piece_cid = $1`, pieceCidV1.String()).Scan(&pdpPieceRefID, &pieceRef, &parkedID, &parkedComplete)
	require.NoError(t, err)
	require.True(t, parkedComplete)

	addBody, err := json.Marshal(map[string]any{
		"pieces": []map[string]any{{
			"pieceCid": pieceCidV2.String(),
			"subPieces": []map[string]string{{
				"subPieceCid": pieceCidV2.String(),
			}},
		}},
	})
	require.NoError(t, err)
	addRes, err := http.Post(
		h.srv.URL+"/pdp/data-sets/"+strconv.FormatInt(dataSetID, 10)+"/pieces",
		"application/json",
		bytes.NewReader(addBody),
	)
	require.NoError(t, err)
	addText := uploadProveHTTPBody(t, addRes)
	require.Equal(t, wantAdd, addRes.StatusCode, addText)
	if wantAdd != http.StatusCreated {
		require.Contains(t, addText, "encryption envelope")
		inACL, err := h.hs.ACL.HasFile(ctx, pieceCidV2)
		require.NoError(t, err)
		require.False(t, inACL, "a piece without an outer CAR must not be stored in acl-pieces")
		return
	}
	addLoc := addRes.Header.Get("Location")
	addTxHash := addLoc[strings.LastIndex(addLoc, "/")+1:]

	const pieceID int64 = 7
	_, err = h.db.Exec(ctx, `
		INSERT INTO pdp_data_set_pieces (
			data_set, piece, add_message_hash, add_message_index, piece_id,
			sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref
		) VALUES ($1, $2, $3, 0, $4, $5, 0, $6, $7)`,
		dataSetID, pieceCidV1.String(), addTxHash, pieceID,
		pieceCidV1.String(), int64(padded), pdpPieceRefID)
	require.NoError(t, err)

	inOpen, err := h.hs.Public.HasFile(ctx, pieceCidV2)
	require.NoError(t, err)
	inACL, err := h.hs.ACL.HasFile(ctx, pieceCidV2)
	require.NoError(t, err)
	if acl == 1 {
		require.False(t, inOpen, "restricted piece must not remain in open-pieces")
		require.True(t, inACL, "restricted piece must be in acl-pieces")
	} else {
		require.True(t, inOpen, "public piece must be in open-pieces")
		require.False(t, inACL, "public piece must not be in acl-pieces")
	}

	_, parkErr := h.pieceIO.PieceReader(ctx, storiface.PieceNumber(parkedID))
	require.Error(t, parkErr, "piece-park copy must be removed after sole-PDP placement")

	task := &ProveTask{db: h.db, cpr: h.cpr}
	proofOut, err := task.provePiece(ctx, dataSetID, pieceID, 0)
	require.NoError(t, err)
	require.NotEmpty(t, proofOut.Leaf)
	require.NotEmpty(t, proofOut.Proof)

	getPiece, err := http.Get(h.srv.URL + "/piece/" + pieceCidV1.String())
	require.NoError(t, err)
	body := uploadProveHTTPBody(t, getPiece)
	if expectPublicRetrieve {
		require.Equal(t, http.StatusOK, getPiece.StatusCode, body)
		require.Equal(t, raw, []byte(body))
	} else {
		require.NotEqual(t, http.StatusOK, getPiece.StatusCode, "acl-only piece must not be served on /piece/")
		require.NotEqual(t, string(raw), body)
	}
}

// feePayload is a bare FEE envelope. An encrypted piece must wrap this in a CAR.
func feePayload(t *testing.T) []byte {
	t.Helper()
	meta := map[string]any{
		"foc/v":    uint64(1),
		"foc/own":  "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		"foc/ks":   "0x00112233445566778899aabbccddeeff",
		"foc/salt": "0x000102030405060708090a0b0c0d0e0f",
	}
	protected, err := cbor.Marshal(map[int64]any{
		1:      int64(-65793),
		16:     "application/vnd.filecoin-encryption+cose",
		5:      []byte{1, 2, 3, 4, 5, 6, 7},
		-1:     uint64(262144),
		-65792: meta,
	})
	require.NoError(t, err)
	body, err := cbor.Marshal([]any{protected, map[int64]any{}, nil})
	require.NoError(t, err)
	tagged, err := cbor.Marshal(cbor.RawTag{Number: 16, Content: body})
	require.NoError(t, err)
	// Pad ciphertext so the piece is large enough for a multi-leaf memtree.
	return append(tagged, bytes.Repeat([]byte{0xcd}, 1024)...)
}

func uploadProveHTTPBody(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	_ = res.Body.Close()
	return string(b)
}

type uploadProveEthSender func(ctx context.Context, from common.Address, tx *ethtypes.Transaction, reason string) (common.Hash, error)

func (f uploadProveEthSender) Send(ctx context.Context, from common.Address, tx *ethtypes.Transaction, reason string) (common.Hash, error) {
	return f(ctx, from, tx, reason)
}

type uploadProveEthClient struct{}

var _ ethchain.EthClient = uploadProveEthClient{}

func (uploadProveEthClient) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (uploadProveEthClient) BalanceAtHash(context.Context, common.Address, common.Hash) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (uploadProveEthClient) BlobBaseFee(context.Context) (*big.Int, error) { return big.NewInt(0), nil }
func (uploadProveEthClient) BlockByHash(context.Context, common.Hash) (*ethtypes.Block, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) BlockByNumber(context.Context, *big.Int) (*ethtypes.Block, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) BlockNumber(context.Context) (uint64, error) { return 0, nil }
func (uploadProveEthClient) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return make([]byte, 32), nil
}
func (uploadProveEthClient) CallContractAtHash(context.Context, ethereum.CallMsg, common.Hash) ([]byte, error) {
	return make([]byte, 32), nil
}
func (uploadProveEthClient) ChainID(context.Context) (*big.Int, error) { return big.NewInt(1), nil }
func (uploadProveEthClient) Close()                                    {}
func (uploadProveEthClient) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return []byte{0x1}, nil
}
func (uploadProveEthClient) CodeAtHash(context.Context, common.Address, common.Hash) ([]byte, error) {
	return []byte{0x1}, nil
}
func (uploadProveEthClient) EstimateGas(context.Context, ethereum.CallMsg) (uint64, error) {
	return 21000, nil
}
func (uploadProveEthClient) EstimateGasAtBlock(context.Context, ethereum.CallMsg, *big.Int) (uint64, error) {
	return 21000, nil
}
func (uploadProveEthClient) EstimateGasAtBlockHash(context.Context, ethereum.CallMsg, common.Hash) (uint64, error) {
	return 21000, nil
}
func (uploadProveEthClient) FeeHistory(context.Context, uint64, *big.Int, []float64) (*ethereum.FeeHistory, error) {
	return &ethereum.FeeHistory{}, nil
}
func (uploadProveEthClient) FilterLogs(context.Context, ethereum.FilterQuery) ([]ethtypes.Log, error) {
	return nil, nil
}
func (uploadProveEthClient) HeaderByHash(context.Context, common.Hash) (*ethtypes.Header, error) {
	return &ethtypes.Header{Number: big.NewInt(1), BaseFee: big.NewInt(1)}, nil
}
func (uploadProveEthClient) HeaderByNumber(context.Context, *big.Int) (*ethtypes.Header, error) {
	return &ethtypes.Header{Number: big.NewInt(1), BaseFee: big.NewInt(1)}, nil
}
func (uploadProveEthClient) NetworkID(context.Context) (*big.Int, error) { return big.NewInt(1), nil }
func (uploadProveEthClient) NonceAt(context.Context, common.Address, *big.Int) (uint64, error) {
	return 0, nil
}
func (uploadProveEthClient) NonceAtHash(context.Context, common.Address, common.Hash) (uint64, error) {
	return 0, nil
}
func (uploadProveEthClient) PeerCount(context.Context) (uint64, error) { return 0, nil }
func (uploadProveEthClient) PendingBalanceAt(context.Context, common.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (uploadProveEthClient) PendingCallContract(context.Context, ethereum.CallMsg) ([]byte, error) {
	return make([]byte, 32), nil
}
func (uploadProveEthClient) PendingCodeAt(context.Context, common.Address) ([]byte, error) {
	return []byte{0x1}, nil
}
func (uploadProveEthClient) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return 0, nil
}
func (uploadProveEthClient) PendingStorageAt(context.Context, common.Address, common.Hash) ([]byte, error) {
	return make([]byte, 32), nil
}
func (uploadProveEthClient) PendingTransactionCount(context.Context) (uint, error) { return 0, nil }
func (uploadProveEthClient) SendRawTransactionSync(context.Context, []byte, *time.Duration) (*ethtypes.Receipt, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) SendTransaction(context.Context, *ethtypes.Transaction) error { return nil }
func (uploadProveEthClient) SendTransactionSync(context.Context, *ethtypes.Transaction, *time.Duration) (*ethtypes.Receipt, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) StorageAt(context.Context, common.Address, common.Hash, *big.Int) ([]byte, error) {
	return make([]byte, 32), nil
}
func (uploadProveEthClient) StorageAtHash(context.Context, common.Address, common.Hash, common.Hash) ([]byte, error) {
	return make([]byte, 32), nil
}
func (uploadProveEthClient) SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- ethtypes.Log) (ethereum.Subscription, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) SubscribeNewHead(context.Context, chan<- *ethtypes.Header) (ethereum.Subscription, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) SubscribeTransactionReceipts(context.Context, *ethereum.TransactionReceiptsQuery, chan<- []*ethtypes.Receipt) (ethereum.Subscription, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) SuggestGasPrice(context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}
func (uploadProveEthClient) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}
func (uploadProveEthClient) SyncProgress(context.Context) (*ethereum.SyncProgress, error) {
	return nil, nil
}
func (uploadProveEthClient) TransactionByHash(context.Context, common.Hash) (*ethtypes.Transaction, bool, error) {
	return nil, false, ethereum.NotFound
}
func (uploadProveEthClient) TransactionCount(context.Context, common.Hash) (uint, error) {
	return 0, nil
}
func (uploadProveEthClient) TransactionInBlock(context.Context, common.Hash, uint) (*ethtypes.Transaction, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) TransactionReceipt(context.Context, common.Hash) (*ethtypes.Receipt, error) {
	return nil, ethereum.NotFound
}
func (uploadProveEthClient) TransactionSender(context.Context, *ethtypes.Transaction, common.Hash, uint) (common.Address, error) {
	return common.Address{}, ethereum.NotFound
}
