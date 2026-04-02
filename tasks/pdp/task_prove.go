package pdp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/oklog/ulid"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-keccak"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/mk20"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/pipeline/lib/nullreader"
)

const LeafSize = proof.NODE_SIZE

type ProveTask struct {
	db        *harmonydb.DB
	ethClient ethchain.EthClient
	sender    *message.SenderETH
	cpr       *cachedreader.CachedPieceReader
	fil       ProveTaskChainApi
	idx       *indexstore.IndexStore

	head atomic.Pointer[chainTypes.TipSet]

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type ProveTaskChainApi interface {
	StateGetRandomnessDigestFromBeacon(ctx context.Context, randEpoch abi.ChainEpoch, tsk chainTypes.TipSetKey) (abi.Randomness, error) //perm:read
	ChainHead(context.Context) (*chainTypes.TipSet, error)                                                                              //perm:read
}

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient ethchain.EthClient, fil ProveTaskChainApi, sender *message.SenderETH, cpr *cachedreader.CachedPieceReader, idx *indexstore.IndexStore) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		cpr:       cpr,
		fil:       fil,
		idx:       idx,
	}

	// ProveTasks are created on pdp_data_set entries where
	// challenge_request_msg_hash is not null (=not yet landed)

	err := chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		pt.head.Store(apply)

		for {
			more := false

			pt.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Select proof sets ready for proving
				var dataSets []struct {
					ID int64 `db:"id"`
				}

				err := tx.Select(&dataSets, `
                    SELECT p.id
                    FROM pdp_data_set p
                    INNER JOIN message_waits_eth mw on mw.signed_tx_hash = p.challenge_request_msg_hash
                    WHERE p.challenge_request_msg_hash IS NOT NULL AND mw.tx_success = TRUE AND p.prove_at_epoch < $1 
                    LIMIT 2
                `, apply.Height())
				if err != nil {
					return false, xerrors.Errorf("failed to select proof sets: %w", err)
				}

				if len(dataSets) == 0 {
					// No proof sets to process
					return false, nil
				}

				// Determine if there might be more proof sets to process
				more = len(dataSets) > 1

				// Process the first proof set
				todo := dataSets[0]

				// Insert a new task into pdp_proving_tasks
				affected, err := tx.Exec(`
                    INSERT INTO pdp_proving_tasks (data_set_id, task_id)
                    VALUES ($1, $2) ON CONFLICT DO NOTHING
                `, todo.ID, id)
				if err != nil {
					return false, xerrors.Errorf("failed to insert into pdp_proving_tasks: %w", err)
				}
				if affected == 0 {
					return false, nil
				}

				// Update pdp_data_set to set next_challenge_possible = FALSE
				affected, err = tx.Exec(`
                    UPDATE pdp_data_set
                    SET challenge_request_msg_hash = NULL
                    WHERE id = $1 AND challenge_request_msg_hash IS NOT NULL
                `, todo.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_data_set: %w", err)
				}
				if affected == 0 {
					more = false
					return false, nil
				}

				return true, nil
			})

			if !more {
				break
			}
		}

		return nil
	})
	if err != nil {
		// Handler registration failed
		panic(err)
	}

	return pt
}

func (p *ProveTask) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	// Retrieve proof set and challenge epoch for the task
	var dataSetID int64

	err = p.db.QueryRow(ctx, `
        SELECT data_set_id
        FROM pdp_proving_tasks
        WHERE task_id = $1
    `, taskID).Scan(&dataSetID)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details: %w", err)
	}

	pdpContracts := contract.ContractAddresses()
	pdpVerifierAddress := pdpContracts.PDPVerifier

	pdpVerifier, err := contract.NewPDPVerifier(pdpVerifierAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPVerifier contract at %s: %w", pdpVerifierAddress.Hex(), err)
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	// Proof parameters
	challengeEpoch, err := pdpVerifier.GetNextChallengeEpoch(callOpts, big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get next challenge epoch: %w", err)
	}

	seed, err := p.fil.StateGetRandomnessDigestFromBeacon(ctx, abi.ChainEpoch(challengeEpoch.Int64()), chainTypes.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain randomness from beacon for pdp prove: %w", err)
	}

	proofs, err := p.GenerateProofs(ctx, pdpVerifier, dataSetID, seed, contract.NumChallenges)
	if err != nil {
		return false, xerrors.Errorf("failed to generate proofs: %w", err)
	}

	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := abiData.Pack("provePossession", big.NewInt(dataSetID), proofs)
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	// [ ["0x559e581f022bb4e4ec6e719e563bf0e026ad6de42e56c18714a2c692b1b88d7e", ["0x559e581f022bb4e4ec6e719e563bf0e026ad6de42e56c18714a2c692b1b88d7e"]] ]

	/* {
		// format proofs for logging
		var proofStr string = "[ [\"0x"
		proofStr += hex.EncodeToString(proofs[0].Leaf[:])
		proofStr += "\", ["
		for i, proof := range proofs[0].Proof {
			if i > 0 {
				proofStr += ", "
			}
			proofStr += "\"0x"
			proofStr += hex.EncodeToString(proof[:])
			proofStr += "\""
		}

		proofStr += "] ] ]"

		log.Infof("PDP Prove Task: dataSetID: %d, taskID: %d, proofs: %s", dataSetID, taskID, proofStr)
	} */

	fee, err := pdpVerifier.CalculateProofFee(callOpts, big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to calculate proof fee: %w", err)
	}

	// Get the sender address for this dataset
	owner, _, err := pdpVerifier.GetDataSetStorageProvider(callOpts, big.NewInt(dataSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get owner: %w", err)
	}

	fromAddress, err := p.getSenderAddress(ctx, owner)
	if err != nil {
		return false, xerrors.Errorf("failed to get sender address: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	txEth := types.NewTransaction(
		0,
		pdpVerifierAddress,
		fee,
		0,
		nil,
		data,
	)

	// Prepare a temp struct for logging proofs as hex
	type proofLog struct {
		Leaf  string   `json:"leaf"`
		Proof []string `json:"proof"`
	}
	proofLogs := make([]proofLog, len(proofs))
	for i, pf := range proofs {
		leafHex := hex.EncodeToString(pf.Leaf[:])
		proofHex := make([]string, len(pf.Proof))
		for j, p := range pf.Proof {
			proofHex[j] = hex.EncodeToString(p[:])
		}
		proofLogs[i] = proofLog{
			Leaf:  leafHex,
			Proof: proofHex,
		}
	}

	log.Infow("PDP Prove Task",
		"dataSetID", dataSetID,
		"taskID", taskID,
		"proofs", proofLogs,
		"data", hex.EncodeToString(data),
		"proofFee initial", fee.Div(fee, big.NewInt(3)),
		"proofFee 3x", fee,
		"txEth", txEth,
	)

	if !stillOwned() {
		// Task was abandoned, don't send the proofs
		return false, nil
	}

	reason := "pdp-prove"
	txHash, err := p.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	log.Infow("PDP Prove Task: transaction sent", "txHash", txHash, "dataSetID", dataSetID, "taskID", taskID)

	// Task completed successfully
	return true, nil
}

func (p *ProveTask) GenerateProofs(ctx context.Context, pdpService *contract.PDPVerifier, dataSetID int64, seed abi.Randomness, numChallenges int) ([]contract.IPDPTypesProof, error) {
	proofs := make([]contract.IPDPTypesProof, numChallenges)

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	totalLeafCount, err := pdpService.GetChallengeRange(callOpts, big.NewInt(dataSetID))
	if err != nil {
		return nil, xerrors.Errorf("failed to get proof set leaf count: %w", err)
	}
	totalLeaves := totalLeafCount.Uint64()

	challenges := lo.Times(numChallenges, func(i int) int64 {
		return generateChallengeIndex(seed, dataSetID, i, totalLeaves)
	})

	pieceId, err := pdpService.FindPieceIds(callOpts, big.NewInt(dataSetID), lo.Map(challenges, func(i int64, _ int) *big.Int { return big.NewInt(i) }))
	if err != nil {
		return nil, xerrors.Errorf("failed to find root IDs: %w", err)
	}

	for i := 0; i < numChallenges; i++ {
		piece := pieceId[i]

		proof, err := p.proveRoot(ctx, dataSetID, piece.PieceId.Int64(), piece.Offset.Int64())
		if err != nil {
			return nil, xerrors.Errorf("failed to prove root %d (%d, %d, %d): %w", i, dataSetID, piece.PieceId.Int64(), piece.Offset.Int64(), err)
		}

		proofs[i] = proof
	}

	return proofs, nil
}

func generateChallengeIndex(seed abi.Randomness, dataSetID int64, proofIndex int, totalLeaves uint64) int64 {
	// Create a buffer to hold the concatenated data (96 bytes: 32 bytes * 3)
	data := make([]byte, 0, 96)

	// Seed is a 32-byte big-endian representation

	data = append(data, seed...)

	// Convert dataSetID to 32-byte big-endian representation
	dataSetIDBigInt := big.NewInt(dataSetID)
	dataSetIDBytes := padTo32Bytes(dataSetIDBigInt.Bytes())
	data = append(data, dataSetIDBytes...)

	// Convert proofIndex to 8-byte big-endian representation
	proofIndexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(proofIndexBytes, uint64(proofIndex))
	data = append(data, proofIndexBytes...)

	// Compute the Keccak-256 hash
	hash := keccak.NewLegacyKeccak256()
	hash.Write(data)
	hashBytes := hash.Sum(nil)

	// Convert hash to big.Int
	hashInt := new(big.Int).SetBytes(hashBytes)

	// Compute challenge index
	totalLeavesBigInt := new(big.Int).SetUint64(totalLeaves)
	challengeIndex := new(big.Int).Mod(hashInt, totalLeavesBigInt)

	// Log for debugging
	log.Debugw("generateChallengeIndex",
		"seed", seed,
		"dataSetID", dataSetID,
		"proofIndex", proofIndex,
		"totalLeaves", totalLeaves,
		"data", hex.EncodeToString(data),
		"hash", hex.EncodeToString(hashBytes),
		"hashInt", hashInt,
		"totalLeavesBigInt", totalLeavesBigInt,
		"challengeIndex", challengeIndex,
	)

	return challengeIndex.Int64()
}

// padTo32Bytes pads the input byte slice to 32 bytes with leading zeros
func padTo32Bytes(b []byte) []byte {
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	return padded
}

// cprPieceReader adapts CachedPieceReader to the proof.PieceReader interface.
type cprPieceReader struct {
	cpr *cachedreader.CachedPieceReader
}

func (r *cprPieceReader) GetPieceReader(ctx context.Context, pieceCid cid.Cid) (proof.SectionReadCloser, uint64, error) {
	rdr, size, err := r.cpr.GetSharedPieceReader(ctx, pieceCid, false)
	return rdr, size, err
}

// idxProofCache adapts IndexStore to the proof.ProofCache interface.
type idxProofCache struct {
	idx *indexstore.IndexStore
}

func (c *idxProofCache) GetLayerIndex(ctx context.Context, pieceCidV2 cid.Cid) (bool, int, error) {
	return c.idx.GetPDPLayerIndex(ctx, pieceCidV2)
}

func (c *idxProofCache) GetNode(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int, index int64) (bool, [32]byte, error) {
	has, node, err := c.idx.GetPDPNode(ctx, pieceCidV2, layerIdx, index)
	if err != nil || !has {
		return has, [32]byte{}, err
	}
	return true, node.Hash, nil
}

func (c *idxProofCache) GetLayer(ctx context.Context, pieceCidV2 cid.Cid, layerIdx int) ([]byte, error) {
	nodes, err := c.idx.GetPDPLayer(ctx, pieceCidV2, layerIdx)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(nodes)*proof.NODE_SIZE)
	for i, n := range nodes {
		copy(out[i*proof.NODE_SIZE:], n.Hash[:])
	}
	return out, nil
}

func (p *ProveTask) proveRoot(ctx context.Context, dataSetID int64, pieceID int64, challengedLeaf int64) (contract.IPDPTypesProof, error) {

	rootChallengeOffset := challengedLeaf * LeafSize

	var pieceCid, dealID string

	err := p.db.QueryRow(context.Background(), `SELECT piece_cid_v2, add_deal_id FROM pdp_dataset_piece WHERE data_set_id = $1 AND piece = $2`, dataSetID, pieceID).Scan(&pieceCid, &dealID)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to piece cid and deal id for the piece: %w", err)
	}

	pcid, err := cid.Parse(pieceCid)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to parse piece CID: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to get piece info: %w", err)
	}

	var mProof *proof.RawMerkleProof

	// Try cached proof for large pieces
	if pi.RawSize >= MinSizeForCache {
		cachedProof, cacheErr := proof.GenerateCachedProof(ctx, &cprPieceReader{cpr: p.cpr}, &idxProofCache{idx: p.idx}, pcid, challengedLeaf)
		if cacheErr != nil {
			log.Errorw("cached proof generation failed", "piece", pcid, "error", cacheErr)
			return contract.IPDPTypesProof{}, xerrors.Errorf("failed to generate cached proof: %w", cacheErr)
		}
		if cachedProof == nil {
			log.Warnw("no proving cache found, triggering save cache", "piece", pcid)
			if scErr := p.startSaveCache(ctx, dealID); scErr != nil {
				return contract.IPDPTypesProof{}, xerrors.Errorf("failed to start save cache task: %w", scErr)
			}
			return contract.IPDPTypesProof{}, xerrors.Errorf("no proving cache found")
		}
		mProof = cachedProof
	} else {
		reader, rawSize, err := p.cpr.GetSharedPieceReader(ctx, pcid, false)
		if err != nil {
			return contract.IPDPTypesProof{}, xerrors.Errorf("failed to get piece reader: %w", err)
		}
		defer func() {
			_ = reader.Close()
		}()

		if rawSize != pi.RawSize {
			return contract.IPDPTypesProof{}, xerrors.Errorf("piece size mismatch: expected %d, got %d", pi.RawSize, rawSize)
		}

		if padreader.PaddedSize(rawSize).Padded() != pi.Size {
			return contract.IPDPTypesProof{}, xerrors.Errorf("piece size mismatch: expected %d, got %d", pi.Size, padreader.PaddedSize(rawSize))
		}

		var r io.Reader = reader

		// Pad the reader to .Unpadded() bytes
		if rawSize < uint64(pi.Size.Unpadded()) {
			// pad with zeros
			r = io.MultiReader(r, nullreader.NewNullReader(abi.UnpaddedPieceSize(uint64(pi.Size.Unpadded())-rawSize)))
		}

		memTree, err := proof.BuildSha254Memtree(r, pi.Size.Unpadded())
		if err != nil {
			return contract.IPDPTypesProof{}, xerrors.Errorf("failed to build memtree: %w", err)
		}
		defer pool.Put(memTree)

		mProof, err = proof.MemtreeProof(memTree, challengedLeaf)
		if err != nil {
			return contract.IPDPTypesProof{}, xerrors.Errorf("failed to generate memtree proof: %w", err)
		}
	}

	log.Debugw("proveRoot", "rootChallengeOffset", rootChallengeOffset, "challengedLeaf", challengedLeaf)

	out := contract.IPDPTypesProof{
		Leaf:  mProof.Leaf,
		Proof: mProof.Proof,
	}

	if !proof.VerifyProof(out.Leaf, out.Proof, mProof.Root, uint64(challengedLeaf)) {
		return contract.IPDPTypesProof{}, xerrors.Errorf("proof verification failed")
	}

	return out, nil
}

func (p *ProveTask) getSenderAddress(ctx context.Context, match common.Address) (common.Address, error) {
	var addressStr string
	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' AND address = $1 LIMIT 1`, match.Hex()).Scan(&addressStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.Address{}, errors.New("no sender address with role 'pdp' found")
		}
		return common.Address{}, err
	}
	address := common.HexToAddress(addressStr)
	return address, nil
}

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return []harmonytask.TaskID{}, nil
	}
	return ids, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPProve",
		MayFollow: []string{"PDPInitPP", "AggregatePDPDeal", "PDPProvingPeriod"},
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 256 << 20, // 256 MB
		},
		MaxFailures: 5,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&ProveTask{})
var _ harmonytask.TaskInterface = &ProveTask{}

func (p *ProveTask) startSaveCache(ctx context.Context, dealID string) error {
	id, err := ulid.Parse(dealID)
	if err != nil {
		return xerrors.Errorf("failed to parse deal ID: %w", err)
	}

	deal, err := mk20.DealFromDB(ctx, p.db, id)
	if err != nil {
		return xerrors.Errorf("failed to get deal from DB: %w", err)
	}

	pdp := deal.Products.PDPV1

	var refID int64
	err = p.db.QueryRow(ctx, `SELECT piece_ref FROM market_piece_deal WHERE id = $1 AND piece_ref IS NOT NULL`, id.String()).Scan(&refID)
	if err != nil {
		return xerrors.Errorf("failed to get piece ref: %w", err)
	}

	_, err = p.db.Exec(ctx, `INSERT INTO pdp_pipeline (
									id, client, piece_cid_v2, data_set_id, extra_data, piece_ref, 
                          			downloaded, deal_aggregation, aggr_index, aggregated, indexing, announce, announce_payload, after_commp, after_add_piece, after_add_piece_msg) 
								VALUES ($1, $2, $3, $4, $5, $6, TRUE, 0, 0, TRUE, FALSE, FALSE, FALSE, TRUE, TRUE, TRUE) ON CONFLICT(id, aggr_index) DO NOTHING`,
		id.String(), deal.Client, deal.Data.PieceCID.String(), *pdp.DataSetID, pdp.ExtraData, refID)

	if err != nil {
		return xerrors.Errorf("inserting piece in PDP pipeline: %w", err)
	}
	return nil
}
