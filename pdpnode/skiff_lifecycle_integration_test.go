package pdpnode

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
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"

	"github.com/filecoin-project/curio/cuhttp"
	curiodeps "github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/pdp"
	"github.com/filecoin-project/curio/pdp/contract"
)

// TestSkiffCreateAddRetrieveLifecycle covers the skiff public HTTP happy path:
// create a data set, upload and add a piece, then retrieve bytes via /piece/{cid}.
//
// Retrieval is mounted exactly as skiff does (cuhttp.MountRetrievalPublicRoutes /
// MountCommonPublicRoutes). ETH send/watch is mocked; YSQL + local storage are real.
func TestSkiffCreateAddRetrieveLifecycle(t *testing.T) {
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

	cpr := cachedreader.NewCachedPieceReader(
		db,
		pieceprovider.NewSectorReader(remote, index),
		pieceprovider.NewPieceParkReader(remote, index),
		nil,
	)

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
	mockSender := ethTxSenderFunc(func(_ context.Context, _ common.Address, _ *ethtypes.Transaction, _ string) (common.Hash, error) {
		n := txSeq.Add(1)
		return common.HexToHash(fmt.Sprintf("%064x", n)), nil
	})

	mux := chi.NewMux()
	_ = cuhttp.MountRetrievalPublicRoutes(ctx, mux, deps)

	svcCtx, svcCancel := context.WithCancel(ctx)
	t.Cleanup(svcCancel)
	require.NoError(t, pdp.MountRoutes(svcCtx, mux, pdp.MountDeps{
		DB:         db,
		LocalStore: localStore,
		EthClient:  stubEthClient{},
		EthSender:  mockSender,
	}, nil))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	createBody, err := json.Marshal(map[string]string{"recordKeeper": recordKeeper.Hex()})
	require.NoError(t, err)
	createRes, err := http.Post(srv.URL+"/pdp/data-sets", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, createRes.StatusCode, httpBody(t, createRes))
	createLoc := createRes.Header.Get("Location")
	require.NoError(t, createRes.Body.Close())
	require.Contains(t, createLoc, "/pdp/data-sets/created/")
	createTxHash := strings.TrimPrefix(createLoc, "/pdp/data-sets/created/")

	const dataSetID uint64 = 4242
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (id, create_message_hash, service, proving_period, challenge_window, init_ready)
		VALUES ($1, $2, 'public', 100, 10, FALSE)`,
		int64(dataSetID), createTxHash)
	require.NoError(t, err)

	raw := bytes.Repeat([]byte{0xab}, 1024)
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
	postRes, err := http.Post(srv.URL+"/pdp/piece", "application/json", bytes.NewReader(postBody))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, postRes.StatusCode, httpBody(t, postRes))
	uploadLoc := postRes.Header.Get("Location")
	require.NoError(t, postRes.Body.Close())

	putReq, err := http.NewRequest(http.MethodPut, srv.URL+uploadLoc, bytes.NewReader(raw))
	require.NoError(t, err)
	putRes, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, putRes.StatusCode, httpBody(t, putRes))
	require.NoError(t, putRes.Body.Close())

	var parkedID, pieceRef int64
	var uploadID string
	err = db.QueryRow(ctx, `
		SELECT pu.id, pu.piece_ref, pp.id
		FROM pdp_piece_uploads pu
		JOIN parked_piece_refs ppr ON ppr.ref_id = pu.piece_ref
		JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE pu.piece_cid = $1`, pieceCidV1.String()).Scan(&uploadID, &pieceRef, &parkedID)
	require.NoError(t, err)

	require.NoError(t, writeParkedPieceBytes(storageDir, parkedID, raw))
	_, err = db.Exec(ctx, `UPDATE parked_pieces SET complete = TRUE WHERE id = $1`, parkedID)
	require.NoError(t, err)

	var pdpPieceRefID int64
	err = db.QueryRow(ctx, `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at)
		VALUES ('public', $1, $2, NOW())
		RETURNING id`, pieceCidV1.String(), pieceRef).Scan(&pdpPieceRefID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `DELETE FROM pdp_piece_uploads WHERE id = $1`, uploadID)
	require.NoError(t, err)

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
		srv.URL+"/pdp/data-sets/"+strconv.FormatUint(dataSetID, 10)+"/pieces",
		"application/json",
		bytes.NewReader(addBody),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, addRes.StatusCode, httpBody(t, addRes))
	addLoc := addRes.Header.Get("Location")
	require.NoError(t, addRes.Body.Close())
	addTxHash := addLoc[strings.LastIndex(addLoc, "/")+1:]

	const pieceID uint64 = 7
	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_set_pieces (
			data_set, piece, add_message_hash, add_message_index, piece_id,
			sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref
		) VALUES ($1, $2, $3, 0, $4, $5, 0, $6, $7)`,
		int64(dataSetID), pieceCidV1.String(), addTxHash, int64(pieceID),
		pieceCidV1.String(), int64(padded), pdpPieceRefID)
	require.NoError(t, err)

	dsRes, err := http.Get(srv.URL + "/pdp/data-sets/" + strconv.FormatUint(dataSetID, 10))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, dsRes.StatusCode, httpBody(t, dsRes))
	require.NoError(t, dsRes.Body.Close())

	pieceMetaRes, err := http.Get(fmt.Sprintf("%s/pdp/data-sets/%d/pieces/%d", srv.URL, dataSetID, pieceID))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, pieceMetaRes.StatusCode, httpBody(t, pieceMetaRes))
	require.NoError(t, pieceMetaRes.Body.Close())

	findRes, err := http.Get(srv.URL + "/pdp/piece?pieceCid=" + pieceCidV2.String())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, findRes.StatusCode, httpBody(t, findRes))
	require.NoError(t, findRes.Body.Close())

	getPiece, err := http.Get(srv.URL + "/piece/" + pieceCidV1.String())
	require.NoError(t, err)
	body := httpBody(t, getPiece)
	require.Equal(t, http.StatusOK, getPiece.StatusCode, body)
	require.Equal(t, raw, []byte(body))
}

type ethTxSenderFunc func(ctx context.Context, from common.Address, tx *ethtypes.Transaction, reason string) (common.Hash, error)

func (f ethTxSenderFunc) Send(ctx context.Context, from common.Address, tx *ethtypes.Transaction, reason string) (common.Hash, error) {
	return f(ctx, from, tx, reason)
}

func httpBody(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	_ = res.Body.Close()
	return string(b)
}

func writeParkedPieceBytes(storageDir string, pieceID int64, data []byte) error {
	path := filepath.Join(
		storageDir,
		storiface.FTPiece.String(),
		storiface.SectorName(storiface.PieceNumber(pieceID).Ref().ID),
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

type stubEthClient struct{}

var _ ethchain.EthClient = stubEthClient{}

func (stubEthClient) BalanceAt(context.Context, common.Address, *big.Int) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (stubEthClient) BalanceAtHash(context.Context, common.Address, common.Hash) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (stubEthClient) BlobBaseFee(context.Context) (*big.Int, error) { return big.NewInt(0), nil }
func (stubEthClient) BlockByHash(context.Context, common.Hash) (*ethtypes.Block, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) BlockByNumber(context.Context, *big.Int) (*ethtypes.Block, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) BlockNumber(context.Context) (uint64, error) { return 0, nil }
func (stubEthClient) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return make([]byte, 32), nil
}
func (stubEthClient) CallContractAtHash(context.Context, ethereum.CallMsg, common.Hash) ([]byte, error) {
	return make([]byte, 32), nil
}
func (stubEthClient) ChainID(context.Context) (*big.Int, error) { return big.NewInt(1), nil }
func (stubEthClient) Close()                                    {}
func (stubEthClient) CodeAt(context.Context, common.Address, *big.Int) ([]byte, error) {
	return []byte{0x1}, nil
}
func (stubEthClient) CodeAtHash(context.Context, common.Address, common.Hash) ([]byte, error) {
	return []byte{0x1}, nil
}
func (stubEthClient) EstimateGas(context.Context, ethereum.CallMsg) (uint64, error) {
	return 21000, nil
}
func (stubEthClient) EstimateGasAtBlock(context.Context, ethereum.CallMsg, *big.Int) (uint64, error) {
	return 21000, nil
}
func (stubEthClient) EstimateGasAtBlockHash(context.Context, ethereum.CallMsg, common.Hash) (uint64, error) {
	return 21000, nil
}
func (stubEthClient) FeeHistory(context.Context, uint64, *big.Int, []float64) (*ethereum.FeeHistory, error) {
	return &ethereum.FeeHistory{}, nil
}
func (stubEthClient) FilterLogs(context.Context, ethereum.FilterQuery) ([]ethtypes.Log, error) {
	return nil, nil
}
func (stubEthClient) HeaderByHash(context.Context, common.Hash) (*ethtypes.Header, error) {
	return &ethtypes.Header{Number: big.NewInt(1), BaseFee: big.NewInt(1)}, nil
}
func (stubEthClient) HeaderByNumber(context.Context, *big.Int) (*ethtypes.Header, error) {
	return &ethtypes.Header{Number: big.NewInt(1), BaseFee: big.NewInt(1)}, nil
}
func (stubEthClient) NetworkID(context.Context) (*big.Int, error) { return big.NewInt(1), nil }
func (stubEthClient) NonceAt(context.Context, common.Address, *big.Int) (uint64, error) {
	return 0, nil
}
func (stubEthClient) NonceAtHash(context.Context, common.Address, common.Hash) (uint64, error) {
	return 0, nil
}
func (stubEthClient) PeerCount(context.Context) (uint64, error) { return 0, nil }
func (stubEthClient) PendingBalanceAt(context.Context, common.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (stubEthClient) PendingCallContract(context.Context, ethereum.CallMsg) ([]byte, error) {
	return make([]byte, 32), nil
}
func (stubEthClient) PendingCodeAt(context.Context, common.Address) ([]byte, error) {
	return []byte{0x1}, nil
}
func (stubEthClient) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return 0, nil
}
func (stubEthClient) PendingStorageAt(context.Context, common.Address, common.Hash) ([]byte, error) {
	return make([]byte, 32), nil
}
func (stubEthClient) PendingTransactionCount(context.Context) (uint, error) { return 0, nil }
func (stubEthClient) SendRawTransactionSync(context.Context, []byte, *time.Duration) (*ethtypes.Receipt, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) SendTransaction(context.Context, *ethtypes.Transaction) error { return nil }
func (stubEthClient) SendTransactionSync(context.Context, *ethtypes.Transaction, *time.Duration) (*ethtypes.Receipt, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) StorageAt(context.Context, common.Address, common.Hash, *big.Int) ([]byte, error) {
	return make([]byte, 32), nil
}
func (stubEthClient) StorageAtHash(context.Context, common.Address, common.Hash, common.Hash) ([]byte, error) {
	return make([]byte, 32), nil
}
func (stubEthClient) SubscribeFilterLogs(context.Context, ethereum.FilterQuery, chan<- ethtypes.Log) (ethereum.Subscription, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) SubscribeNewHead(context.Context, chan<- *ethtypes.Header) (ethereum.Subscription, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) SubscribeTransactionReceipts(context.Context, *ethereum.TransactionReceiptsQuery, chan<- []*ethtypes.Receipt) (ethereum.Subscription, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) SuggestGasPrice(context.Context) (*big.Int, error)  { return big.NewInt(1), nil }
func (stubEthClient) SuggestGasTipCap(context.Context) (*big.Int, error) { return big.NewInt(1), nil }
func (stubEthClient) SyncProgress(context.Context) (*ethereum.SyncProgress, error) {
	return nil, nil
}
func (stubEthClient) TransactionByHash(context.Context, common.Hash) (*ethtypes.Transaction, bool, error) {
	return nil, false, ethereum.NotFound
}
func (stubEthClient) TransactionCount(context.Context, common.Hash) (uint, error) { return 0, nil }
func (stubEthClient) TransactionInBlock(context.Context, common.Hash, uint) (*ethtypes.Transaction, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) TransactionReceipt(context.Context, common.Hash) (*ethtypes.Receipt, error) {
	return nil, ethereum.NotFound
}
func (stubEthClient) TransactionSender(context.Context, *ethtypes.Transaction, common.Hash, uint) (common.Address, error) {
	return common.Address{}, ethereum.NotFound
}
