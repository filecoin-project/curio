package pdpv0

import (
	"context"
	"errors"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/pdp/contract"
)

func TestSaveCacheScheduleStopsWhenAddTaskDoesNotRunCallback(t *testing.T) {
	done := make(chan struct{})

	go func() {
		defer close(done)
		task := &TaskPDPSaveCache{}
		_ = task.schedule(context.Background(), func(func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) {})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("schedule did not stop after AddTask declined to run its callback")
	}
}

func zeroCacheLayer(t *testing.T, paddedSize abi.PaddedPieceSize) (cid.Cid, []indexstore.NodeDigest) {
	t.Helper()
	pcid, err := commcid.PieceCidV2FromV1(zerocomm.ZeroPieceCommitment(paddedSize.Unpadded()), uint64(paddedSize.Unpadded()))
	require.NoError(t, err)
	sectionRoot, err := commcid.CIDToPieceCommitmentV1(zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(PaddedReadSize).Unpadded()))
	require.NoError(t, err)
	nodes := make([]indexstore.NodeDigest, uint64(paddedSize)/PaddedReadSize)
	for i := range nodes {
		nodes[i] = indexstore.NodeDigest{
			Layer: commp.SnapshotLayerIndex(PaddedReadSize),
			Index: int64(i),
			Hash:  [32]byte(sectionRoot),
		}
	}
	return pcid, nodes
}

func TestValidatePDPCacheLayer64GiB(t *testing.T) {
	pcid, nodes := zeroCacheLayer(t, 64<<30)
	layer := commp.SnapshotLayerIndex(PaddedReadSize)
	require.Len(t, nodes, 16384)
	require.NoError(t, validatePDPCacheLayer(pcid, layer, nodes))
	require.Error(t, validatePDPCacheLayer(pcid, layer, nil))
	require.Error(t, validatePDPCacheLayer(pcid, layer, nodes[:len(nodes)/2]))
	require.Error(t, validatePDPCacheLayer(pcid, layer+1, nodes))

	for _, tc := range []struct {
		name   string
		mutate func([]indexstore.NodeDigest)
	}{
		{"duplicate index", func(n []indexstore.NodeDigest) { n[1].Index = 0 }},
		{"wrong node layer", func(n []indexstore.NodeDigest) { n[1].Layer++ }},
		{"corrupt hash", func(n []indexstore.NodeDigest) { n[1].Hash[0] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := append([]indexstore.NodeDigest(nil), nodes...)
			tc.mutate(invalid)
			require.Error(t, validatePDPCacheLayer(pcid, layer, invalid))
		})
	}
}

type saveCacheTestStore struct {
	nodes        []indexstore.NodeDigest
	layerReads   int
	writes       int
	truncateSave bool
	layerError   error
	missingOnce  bool
}

func (s *saveCacheTestStore) GetPDPLayerIndex(context.Context, cid.Cid) (bool, int, error) {
	if s.missingOnce {
		return false, 0, nil
	}
	return len(s.nodes) > 0, commp.SnapshotLayerIndex(PaddedReadSize), nil
}

func (s *saveCacheTestStore) GetPDPLayer(context.Context, cid.Cid, int) ([]indexstore.NodeDigest, error) {
	s.layerReads++
	if s.missingOnce {
		// Cache becomes available after the first failed proof's validation.
		s.missingOnce = false
		return nil, nil
	}
	return append([]indexstore.NodeDigest(nil), s.nodes...), s.layerError
}

func (s *saveCacheTestStore) GetPDPNode(_ context.Context, _ cid.Cid, _ int, index int64) (bool, *indexstore.NodeDigest, error) {
	for _, node := range s.nodes {
		if node.Index == index {
			return true, &node, nil
		}
	}
	return false, nil, nil
}

func (s *saveCacheTestStore) AddPDPLayer(_ context.Context, _ cid.Cid, nodes []indexstore.NodeDigest) error {
	s.writes++
	s.nodes = append([]indexstore.NodeDigest(nil), nodes...)
	if s.truncateSave {
		s.nodes = s.nodes[:len(s.nodes)/2]
	}
	return nil
}

type saveCacheTestReader struct {
	rawSize uint64
	opens   int
	bytes   int64
	err     error
	lastCID cid.Cid
}

func (r *saveCacheTestReader) GetSharedPieceReader(_ context.Context, pieceCID cid.Cid, _ bool) (storiface.Reader, uint64, error) {
	r.opens++
	r.lastCID = pieceCID
	if r.err != nil {
		return nil, 0, r.err
	}
	return &saveCacheTestSection{SectionReader: io.NewSectionReader(r, 0, int64(r.rawSize))}, r.rawSize, nil
}

func (r *saveCacheTestReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(r.rawSize) {
		return 0, io.EOF
	}
	n := min(len(p), int(int64(r.rawSize)-off))
	clear(p[:n])
	r.bytes += int64(n)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

type saveCacheTestSection struct {
	*io.SectionReader
}

func (*saveCacheTestSection) Close() error { return nil }

func insertSaveCachePiece(t *testing.T, db *harmonydb.DB, pcid cid.Cid, taskID harmonytask.TaskID) int64 {
	t.Helper()
	pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcid)
	require.NoError(t, err)
	fixture := newNotifyUploadFixture("save-cache")
	fixture.pieceCID = pcidV1.String()
	insertParkedPiece(t, db, &fixture, true, false, nil)
	_, err = db.Exec(t.Context(), `UPDATE parked_pieces SET piece_raw_size = $1, piece_padded_size = $2 WHERE id = $3`, rawSize, abi.UnpaddedPieceSize(rawSize).Padded(), fixture.parkedPiece)
	require.NoError(t, err)
	var id int64
	err = db.QueryRow(t.Context(), `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, needs_save_cache, save_cache_task_id)
		VALUES ($1, $2, $3, TRUE, $4) RETURNING id`, fixture.service, fixture.pieceCID, fixture.pieceRef, taskID).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestSaveCacheValidatesBeforeCompletion(t *testing.T) {
	pcid, valid := zeroCacheLayer(t, 64<<20)

	for i, tc := range []struct {
		name         string
		initial      []indexstore.NodeDigest
		truncateSave bool
		wantWrite    bool
		wantError    bool
	}{
		{name: "reuse valid", initial: valid},
		{name: "build missing", wantWrite: true},
		{name: "repair partial", initial: valid[:len(valid)/2], wantWrite: true},
		{name: "repair corrupt", initial: valid, wantWrite: true},
		{name: "reject incomplete write", truncateSave: true, wantWrite: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			taskID := harmonytask.TaskID(700 + i)
			id := insertSaveCachePiece(t, db, pcid, taskID)
			store := &saveCacheTestStore{nodes: append([]indexstore.NodeDigest(nil), tc.initial...), truncateSave: tc.truncateSave}
			if tc.name == "repair corrupt" {
				store.nodes[1].Hash[0] ^= 1
			}
			reader := &saveCacheTestReader{rawSize: uint64(abi.PaddedPieceSize(64 << 20).Unpadded())}
			task := NewTaskPDPSaveCache(db, reader, store)
			done, err := task.Do(t.Context(), taskID, func() bool { return true })
			if tc.wantError {
				require.Error(t, err)
				require.False(t, done)
			} else {
				require.NoError(t, err)
				require.True(t, done)
				require.NoError(t, validatePDPCacheLayer(pcid, commp.SnapshotLayerIndex(PaddedReadSize), store.nodes))
			}
			if tc.wantWrite {
				require.Equal(t, 1, store.writes)
				require.Equal(t, int64(reader.rawSize), reader.bytes)
			} else {
				require.Zero(t, store.writes)
				require.Zero(t, reader.opens)
			}
			var needsCache, taskCleared bool
			err = db.QueryRow(t.Context(), `SELECT needs_save_cache, save_cache_task_id IS NULL FROM pdp_piecerefs WHERE id = $1`, id).Scan(&needsCache, &taskCleared)
			require.NoError(t, err)
			require.Equal(t, tc.wantError, needsCache)
			require.Equal(t, !tc.wantError, taskCleared)
		})
	}
}

func TestSaveCacheSkips32MiBPaddedPiece(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	pcid, _ := zeroCacheLayer(t, 32<<20)
	const taskID = harmonytask.TaskID(750)
	id := insertSaveCachePiece(t, db, pcid, taskID)
	store := &saveCacheTestStore{}
	reader := &saveCacheTestReader{rawSize: uint64(abi.PaddedPieceSize(32 << 20).Unpadded())}
	task := NewTaskPDPSaveCache(db, reader, store)
	done, err := task.Do(t.Context(), taskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, done)
	require.Zero(t, store.layerReads)
	require.Zero(t, store.writes)
	require.Zero(t, reader.opens)
	var needsCache, taskCleared bool
	err = db.QueryRow(t.Context(), `SELECT needs_save_cache, save_cache_task_id IS NULL FROM pdp_piecerefs WHERE id = $1`, id).Scan(&needsCache, &taskCleared)
	require.NoError(t, err)
	require.False(t, needsCache)
	require.True(t, taskCleared)
}

func TestSaveCacheMigrationCleanup32MiBPaddedBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		paddedSize abi.PaddedPieceSize
		rawSize    uint64
		wantCache  bool
	}{
		{name: "exact boundary", paddedSize: 32 << 20, rawSize: 33292288},
		{name: "one byte above", paddedSize: 64 << 20, rawSize: 33292289, wantCache: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			pcid, _ := zeroCacheLayer(t, tc.paddedSize)
			id := insertSaveCachePiece(t, db, pcid, 751)
			_, err = db.Exec(t.Context(), `UPDATE pdp_piecerefs SET save_cache_task_id = NULL WHERE id = $1`, id)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `
				UPDATE parked_pieces SET piece_raw_size = $1 WHERE id = (
					SELECT pprf.piece_id FROM parked_piece_refs pprf
					JOIN pdp_piecerefs pr ON pr.piece_ref = pprf.ref_id WHERE pr.id = $2
				)`, tc.rawSize, id)
			require.NoError(t, err)
			task := &TaskPDPSaveCache{db: db}
			require.NoError(t, task.scheduleMigrationCleanup(t.Context(), nil))
			var needsCache bool
			err = db.QueryRow(t.Context(), `SELECT needs_save_cache FROM pdp_piecerefs WHERE id = $1`, id).Scan(&needsCache)
			require.NoError(t, err)
			require.Equal(t, tc.wantCache, needsCache)
		})
	}
}

func TestProvePieceValidatesCacheOnlyAfterFailure(t *testing.T) {
	for i, tc := range []struct {
		name           string
		paddedSize     abi.PaddedPieceSize
		wantRepair     bool
		wantError      bool
		wantLayerReads int
	}{
		{name: "valid", paddedSize: 64 << 30, wantLayerReads: 1},
		{name: "missing", paddedSize: 64 << 30, wantRepair: true, wantError: true, wantLayerReads: 1},
		{name: "corrupt sibling", paddedSize: 64 << 30, wantRepair: true, wantError: true, wantLayerReads: 2},
		{name: "piece unavailable", paddedSize: 64 << 30, wantError: true, wantLayerReads: 1},
		{name: "cache unavailable", paddedSize: 64 << 30, wantError: true, wantLayerReads: 2},
		{name: "32 MiB without cache", paddedSize: 32 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pcid, valid := zeroCacheLayer(t, tc.paddedSize)
			pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcid)
			require.NoError(t, err)
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)
			id := insertSaveCachePiece(t, db, pcid, harmonytask.TaskID(800+i))
			_, err = db.Exec(t.Context(), `UPDATE pdp_piecerefs SET needs_save_cache = FALSE, save_cache_task_id = NULL WHERE id = $1`, id)
			require.NoError(t, err)
			dataSetID := 800 + i
			_, err = db.Exec(t.Context(), `INSERT INTO pdp_data_sets (id, create_message_hash, service) VALUES ($1, 'cache-test', 'public')`, dataSetID)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `INSERT INTO message_waits_eth (signed_tx_hash) VALUES ('cache-test') ON CONFLICT DO NOTHING`)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `
				INSERT INTO pdp_data_set_pieces (data_set, piece, add_message_hash, add_message_index, piece_id, sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref)
				VALUES ($1, $2, 'cache-test', 0, 0, $2, 0, $3, $4)`, dataSetID, pcidV1.String(), tc.paddedSize, id)
			require.NoError(t, err)
			store := &saveCacheTestStore{nodes: append([]indexstore.NodeDigest(nil), valid...)}
			reader := &saveCacheTestReader{rawSize: rawSize}
			switch tc.name {
			case "missing", "32 MiB without cache":
				store.nodes = nil
			case "corrupt sibling":
				store.nodes[len(store.nodes)-1].Hash[0] ^= 1
			case "piece unavailable":
				reader.err = errors.New("piece storage unavailable")
			case "cache unavailable":
				store.layerError = errors.New("cache storage unavailable")
			}
			task := &ProveTask{db: db, cpr: reader, idx: store}
			out, err := task.provePiece(t.Context(), int64(dataSetID), 0, 0)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Len(t, out.Proof, proof.NodeLevel(int64(tc.paddedSize)/LeafSize, 2)-1)
				root := out.Leaf
				for _, sibling := range out.Proof {
					root = proof.ComputeBinShaParent(root, sibling)
				}
				commitment, err := commcid.CIDToPieceCommitmentV1(pcidV1)
				require.NoError(t, err)
				require.Equal(t, [32]byte(commitment), root)
			}
			require.Equal(t, tc.wantLayerReads, store.layerReads)
			if tc.paddedSize == 32<<20 {
				require.Equal(t, 1, reader.opens)
				require.Equal(t, int64(rawSize), reader.bytes)
				require.Equal(t, pcid, reader.lastCID)
			} else {
				require.LessOrEqual(t, reader.bytes, int64(PaddedReadSize))
				if reader.opens > 0 {
					require.Equal(t, pcid, reader.lastCID)
				}
			}
			var needsCache bool
			err = db.QueryRow(t.Context(), `SELECT needs_save_cache FROM pdp_piecerefs WHERE id = $1`, id).Scan(&needsCache)
			require.NoError(t, err)
			require.Equal(t, tc.wantRepair, needsCache)
		})
	}
}

type proofPiecesEthClient struct {
	ethchain.EthClient
	response []byte
}

func (c *proofPiecesEthClient) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return c.response, nil
}

func TestGenerateProofsContinuesAfterMissingCacheWithoutPartialResult(t *testing.T) {
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	pcid, valid := zeroCacheLayer(t, 64<<30)
	pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcid)
	require.NoError(t, err)
	id := insertSaveCachePiece(t, db, pcid, 900)
	_, err = db.Exec(t.Context(), `UPDATE pdp_piecerefs SET needs_save_cache = FALSE, save_cache_task_id = NULL WHERE id = $1`, id)
	require.NoError(t, err)
	const dataSetID = 900
	_, err = db.Exec(t.Context(), `INSERT INTO pdp_data_sets (id, create_message_hash, service) VALUES ($1, 'cache-test', 'public')`, dataSetID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO message_waits_eth (signed_tx_hash) VALUES ('cache-test') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `
		INSERT INTO pdp_data_set_pieces (data_set, piece, add_message_hash, add_message_index, piece_id, sub_piece, sub_piece_offset, sub_piece_size, pdp_pieceref)
		VALUES ($1, $2, 'cache-test', 0, 0, $2, 0, $3, $4)`, dataSetID, pcidV1.String(), 64<<30, id)
	require.NoError(t, err)

	pieces := make([]contract.IPDPTypesPieceIdAndOffset, 5)
	for i := range pieces {
		pieces[i] = contract.IPDPTypesPieceIdAndOffset{PieceId: big.NewInt(0), Offset: big.NewInt(int64(i))}
	}
	contractABI, err := contract.PDPVerifierMetaData.GetAbi()
	require.NoError(t, err)
	response, err := contractABI.Methods["findPieceIds"].Outputs.Pack(pieces)
	require.NoError(t, err)
	verifier, err := contract.NewPDPVerifier(common.Address{}, &proofPiecesEthClient{response: response})
	require.NoError(t, err)
	store := &saveCacheTestStore{nodes: valid, missingOnce: true}
	reader := &saveCacheTestReader{rawSize: rawSize}
	task := &ProveTask{db: db, cpr: reader, idx: store}
	seed := make(abi.Randomness, 32)

	proofs, err := task.GenerateProofs(t.Context(), verifier, dataSetID, seed, (64<<30)/LeafSize, len(pieces))
	require.ErrorContains(t, err, "failed to prove piece 0")
	require.ErrorContains(t, err, "no proving cache found")
	require.Nil(t, proofs)
	require.Equal(t, 4, reader.opens)
	var needsCache bool
	err = db.QueryRow(t.Context(), `SELECT needs_save_cache FROM pdp_piecerefs WHERE id = $1`, id).Scan(&needsCache)
	require.NoError(t, err)
	require.True(t, needsCache)

	proofs, err = task.GenerateProofs(t.Context(), verifier, dataSetID, seed, (64<<30)/LeafSize, len(pieces))
	require.NoError(t, err)
	require.Len(t, proofs, len(pieces))
	require.Equal(t, 9, reader.opens)
	commitment, err := commcid.CIDToPieceCommitmentV1(pcidV1)
	require.NoError(t, err)
	for i, generated := range proofs {
		require.True(t, proof.VerifyProof(generated.Leaf, generated.Proof, [32]byte(commitment), uint64(i)))
	}
}
