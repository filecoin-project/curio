package pdp

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	"github.com/minio/sha256-simd"
	"github.com/samber/lo"
	"golang.org/x/crypto/sha3"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/commcidv2"
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
	ethClient *ethclient.Client
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

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient *ethclient.Client, fil ProveTaskChainApi, sender *message.SenderETH, cpr *cachedreader.CachedPieceReader, idx *indexstore.IndexStore) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		cpr:       cpr,
		fil:       fil,
		idx:       idx,
	}

	// ProveTasks are created on pdp_proof_sets entries where
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
				var proofSets []struct {
					ID int64 `db:"id"`
				}

				err := tx.Select(&proofSets, `
                    SELECT p.id
                    FROM pdp_proof_set p
                    INNER JOIN message_waits_eth mw on mw.signed_tx_hash = p.challenge_request_msg_hash
                    WHERE p.challenge_request_msg_hash IS NOT NULL AND mw.tx_success = TRUE AND p.prove_at_epoch < $1 
                    LIMIT 2
                `, apply.Height())
				if err != nil {
					return false, xerrors.Errorf("failed to select proof sets: %w", err)
				}

				if len(proofSets) == 0 {
					// No proof sets to process
					return false, nil
				}

				// Determine if there might be more proof sets to process
				more = len(proofSets) > 1

				// Process the first proof set
				todo := proofSets[0]

				// Insert a new task into pdp_prove_tasks
				affected, err := tx.Exec(`
                    INSERT INTO pdp_prove_tasks (proofset, task_id)
                    VALUES ($1, $2) ON CONFLICT DO NOTHING
                `, todo.ID, id)
				if err != nil {
					return false, xerrors.Errorf("failed to insert into pdp_prove_tasks: %w", err)
				}
				if affected == 0 {
					return false, nil
				}

				// Update pdp_proof_sets to set next_challenge_possible = FALSE
				affected, err = tx.Exec(`
                    UPDATE pdp_proof_set
                    SET challenge_request_msg_hash = NULL
                    WHERE id = $1 AND challenge_request_msg_hash IS NOT NULL
                `, todo.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
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

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Retrieve proof set and challenge epoch for the task
	var proofSetID int64

	err = p.db.QueryRow(context.Background(), `
        SELECT proofset
        FROM pdp_prove_tasks
        WHERE task_id = $1
    `, taskID).Scan(&proofSetID)
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
	challengeEpoch, err := pdpVerifier.GetNextChallengeEpoch(callOpts, big.NewInt(proofSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get next challenge epoch: %w", err)
	}

	seed, err := p.fil.StateGetRandomnessDigestFromBeacon(ctx, abi.ChainEpoch(challengeEpoch.Int64()), chainTypes.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain randomness from beacon for pdp prove: %w", err)
	}

	proofs, err := p.GenerateProofs(ctx, pdpVerifier, proofSetID, seed, contract.NumChallenges)
	if err != nil {
		return false, xerrors.Errorf("failed to generate proofs: %w", err)
	}

	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := abiData.Pack("provePossession", big.NewInt(proofSetID), proofs)
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

		log.Infof("PDP Prove Task: proofSetID: %d, taskID: %d, proofs: %s", proofSetID, taskID, proofStr)
	} */

	// If gas used is 0 fee is maximized
	gasFee := big.NewInt(0)
	proofFee, err := pdpVerifier.CalculateProofFee(callOpts, big.NewInt(proofSetID), gasFee)
	if err != nil {
		return false, xerrors.Errorf("failed to calculate proof fee: %w", err)
	}

	// Add 2x buffer for certainty
	proofFee = new(big.Int).Mul(proofFee, big.NewInt(3))

	// Get the sender address for this proofset
	owner, _, err := pdpVerifier.GetProofSetOwner(callOpts, big.NewInt(proofSetID))
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
		proofFee,
		0,
		nil,
		data,
	)

	log.Infow("PDP Prove Task",
		"proofSetID", proofSetID,
		"taskID", taskID,
		"proofs", proofs,
		"data", hex.EncodeToString(data),
		"gasFeeEstimate", gasFee,
		"proofFee initial", proofFee.Div(proofFee, big.NewInt(3)),
		"proofFee 3x", proofFee,
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

	// Remove the roots previously scheduled for deletion
	err = p.cleanupDeletedRoots(ctx, proofSetID, pdpVerifier)
	if err != nil {
		return false, xerrors.Errorf("failed to cleanup deleted roots: %w", err)
	}

	log.Infow("PDP Prove Task: transaction sent", "txHash", txHash, "proofSetID", proofSetID, "taskID", taskID)

	// Task completed successfully
	return true, nil
}

func (p *ProveTask) GenerateProofs(ctx context.Context, pdpService *contract.PDPVerifier, proofSetID int64, seed abi.Randomness, numChallenges int) ([]contract.PDPVerifierProof, error) {
	proofs := make([]contract.PDPVerifierProof, numChallenges)

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	totalLeafCount, err := pdpService.GetChallengeRange(callOpts, big.NewInt(proofSetID))
	if err != nil {
		return nil, xerrors.Errorf("failed to get proof set leaf count: %w", err)
	}
	totalLeaves := totalLeafCount.Uint64()

	challenges := lo.Times(numChallenges, func(i int) int64 {
		return generateChallengeIndex(seed, proofSetID, i, totalLeaves)
	})

	rootId, err := pdpService.FindRootIds(callOpts, big.NewInt(proofSetID), lo.Map(challenges, func(i int64, _ int) *big.Int { return big.NewInt(i) }))
	if err != nil {
		return nil, xerrors.Errorf("failed to find root IDs: %w", err)
	}

	for i := 0; i < numChallenges; i++ {
		root := rootId[i]

		proof, err := p.proveRoot(ctx, proofSetID, root.RootId.Int64(), root.Offset.Int64())
		if err != nil {
			return nil, xerrors.Errorf("failed to prove root %d (%d, %d, %d): %w", i, proofSetID, root.RootId.Int64(), root.Offset.Int64(), err)
		}

		proofs[i] = proof
	}

	return proofs, nil
}

func generateChallengeIndex(seed abi.Randomness, proofSetID int64, proofIndex int, totalLeaves uint64) int64 {
	// Create a buffer to hold the concatenated data (96 bytes: 32 bytes * 3)
	data := make([]byte, 0, 96)

	// Seed is a 32-byte big-endian representation

	data = append(data, seed...)

	// Convert proofSetID to 32-byte big-endian representation
	proofSetIDBigInt := big.NewInt(proofSetID)
	proofSetIDBytes := padTo32Bytes(proofSetIDBigInt.Bytes())
	data = append(data, proofSetIDBytes...)

	// Convert proofIndex to 8-byte big-endian representation
	proofIndexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(proofIndexBytes, uint64(proofIndex))
	data = append(data, proofIndexBytes...)

	// Compute the Keccak-256 hash
	hash := sha3.NewLegacyKeccak256()
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
		"proofSetID", proofSetID,
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

func (p *ProveTask) genSubrootMemtree(
	ctx context.Context,
	pieceCidV2 cid.Cid,
	challengedLeafIndex int64,
	savedLayer int,
) ([]byte, error) {
	// Calculate which snapshot node covers this challenged leaf
	leavesPerNode := int64(1) << savedLayer
	snapshotNodeIndex := challengedLeafIndex >> savedLayer
	startLeaf := snapshotNodeIndex << savedLayer

	// Convert tree-based leaf range to file-based offset/length
	offset := startLeaf * inputBytesPerLeaf
	length := leavesPerNode * inputBytesPerLeaf

	// Compute padded size to build Merkle tree (must match what BuildSha254Memtree expects)
	subrootSize := padreader.PaddedSize(uint64(length)).Padded()
	if subrootSize > proof.MaxMemtreeSize {
		return nil, xerrors.Errorf("subroot size exceeds maximum: %d", subrootSize)
	}

	// Get original file reader
	reader, reportedSize, err := p.cpr.GetSharedPieceReader(ctx, pieceCidV2)
	if err != nil {
		return nil, xerrors.Errorf("failed to get reader: %w", err)
	}
	defer reader.Close()

	if offset > int64(reportedSize) {
		// The entire requested range is beyond file size → pure padding
		// This should never happen
		//TODO: Maybe put a panic here?
		paddingOnly := nullreader.NewNullReader(abi.UnpaddedPieceSize(length))
		return proof.BuildSha254Memtree(paddingOnly, subrootSize.Unpadded())
	}

	_, err = reader.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, xerrors.Errorf("seek to offset %d failed: %w", offset, err)
	}

	// Read up to file limit
	var data io.Reader
	fileRemaining := int64(reportedSize) - offset
	if fileRemaining < length {
		data = io.MultiReader(io.LimitReader(reader, fileRemaining), nullreader.NewNullReader(abi.UnpaddedPieceSize(int64(subrootSize.Unpadded())-fileRemaining)))
	} else {
		data = io.LimitReader(reader, length)
	}

	// Build Merkle tree from padded input
	return proof.BuildSha254Memtree(data, subrootSize.Unpadded())
}

func GenerateProofToRootFromSnapshot(
	snapshotLayer int,
	snapshotIndex int64,
	snapshotHash [32]byte,
	snapshotNodes []indexstore.NodeDigest,
) ([][32]byte, [32]byte, error) {
	snapMap := make(map[int64][32]byte)
	for _, n := range snapshotNodes {
		if n.Layer != snapshotLayer {
			continue // ignore other layers if present
		}
		snapMap[n.Index] = n.Hash
	}

	proof := make([][32]byte, 0)
	currentHash := snapshotHash
	currentIndex := snapshotIndex
	hasher := sha256.New()

	for level := snapshotLayer + 1; ; level++ {
		siblingIndex := currentIndex ^ 1

		siblingHash, exists := snapMap[siblingIndex]
		if !exists {
			// Padding if sibling missing
			siblingHash = currentHash
		}

		// Add sibling to proof
		proof = append(proof, siblingHash)

		// Compute parent
		hasher.Reset()
		if currentIndex%2 == 0 {
			hasher.Write(currentHash[:])
			hasher.Write(siblingHash[:])
		} else {
			hasher.Write(siblingHash[:])
			hasher.Write(currentHash[:])
		}
		sum := hasher.Sum(nil)
		var parent [32]byte
		copy(parent[:], sum)
		parent[31] &= 0x3F

		currentHash = parent
		currentIndex = currentIndex >> 1

		// stop when we reach the root (single node at level)
		if len(snapMap) <= 1 && currentIndex == 0 {
			break
		}
	}

	return proof, currentHash, nil
}

func (p *ProveTask) proveRoot(ctx context.Context, proofSetID int64, rootId int64, challengedLeaf int64) (contract.PDPVerifierProof, error) {
	//const arity = 2

	rootChallengeOffset := challengedLeaf * LeafSize

	var pieceCid string

	err := p.db.QueryRow(context.Background(), `SELECT piece_cid_v2 FROM pdp_proofset_root WHERE proofset = $1 AND root_id = $2`, proofSetID, rootId).Scan(&pieceCid)
	if err != nil {
		return contract.PDPVerifierProof{}, xerrors.Errorf("failed to get root and subroot: %w", err)
	}

	pcid, err := cid.Parse(pieceCid)
	if err != nil {
		return contract.PDPVerifierProof{}, xerrors.Errorf("failed to parse piece CID: %w", err)
	}

	pi, err := mk20.GetPieceInfo(pcid)
	if err != nil {
		return contract.PDPVerifierProof{}, xerrors.Errorf("failed to get piece info: %w", err)
	}

	var out contract.PDPVerifierProof
	var rootDigest [32]byte

	// If piece is less than 100 MiB, let's generate proof directly without using cache
	if pi.RawSize < MinSizeForCache {
		// Get original file reader
		reader, _, err := p.cpr.GetSharedPieceReader(ctx, pcid)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to get piece reader: %w", err)
		}
		defer reader.Close()

		// Build Merkle tree from padded input
		memTree, err := proof.BuildSha254Memtree(reader, pi.Size.Unpadded())
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to build memtree: %w", err)
		}
		log.Debugw("proveRoot", "rootChallengeOffset", rootChallengeOffset, "challengedLeaf", challengedLeaf)

		mProof, err := proof.MemtreeProof(memTree, challengedLeaf)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to generate memtree proof: %w", err)
		}

		out = contract.PDPVerifierProof{
			Leaf:  mProof.Leaf,
			Proof: mProof.Proof,
		}

		rootDigest = mProof.Root
	} else {
		layerIdx := snapshotLayerIndex(pi.RawSize)
		cacheIdx := challengedLeaf >> layerIdx

		has, node, err := p.idx.GetPDPNode(ctx, pcid, cacheIdx)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to get node: %w", err)
		}

		if !has {
			// TODO: Trigger a Layer save task here and figure out if we should proceed or not
			// TODO: Proceeding from here can cause memory issue for big pieces, we will need to generate proof using some other lib
			panic("implement me")
		}

		log.Debugw("proveRoot", "rootChallengeOffset", rootChallengeOffset, "challengedLeaf", challengedLeaf, "layerIdx", layerIdx, "cacheIdx", cacheIdx, "node", node)

		if node.Layer != layerIdx {
			return contract.PDPVerifierProof{}, xerrors.Errorf("node layer mismatch: %d != %d", node.Layer, layerIdx)
		}

		// build subroot memtree
		memtree, err := p.genSubrootMemtree(ctx, pcid, challengedLeaf, layerIdx)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to generate subroot memtree: %w", err)
		}

		/*
			type RawMerkleProof struct {
				Leaf  [32]byte
				Proof [][32]byte
				Root  [32]byte
			}
		*/
		subTreeProof, err := proof.MemtreeProof(memtree, challengedLeaf)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to generate sub tree proof: %w", err)
		}
		log.Debugw("subTreeProof", "subrootProof", subTreeProof)

		// Verify root of proof
		if subTreeProof.Root != node.Hash {
			return contract.PDPVerifierProof{}, xerrors.Errorf("subroot root mismatch: %x != %x", subTreeProof.Root, node.Hash)
		}

		// Fetch full cached layer from DB
		layerNodes, err := p.idx.GetPDPLayer(ctx, pcid)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to get layer nodes: %w", err)
		}

		proofs, rd, err := GenerateProofToRootFromSnapshot(node.Layer, node.Index, node.Hash, layerNodes)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to generate proof to root: %w", err)
		}

		com, err := commcidv2.CommPFromPCidV2(pcid)
		if err != nil {
			return contract.PDPVerifierProof{}, xerrors.Errorf("failed to get piece commitment: %w", err)
		}

		// Verify proof with original root
		if [32]byte(com.Digest()) != rd {
			return contract.PDPVerifierProof{}, xerrors.Errorf("root digest mismatch: %x != %x", com.Digest(), rd)
		}

		out = contract.PDPVerifierProof{
			Leaf:  subTreeProof.Leaf,
			Proof: append([][32]byte{subTreeProof.Root}, proofs...),
		}

		rootDigest = rd
	}

	if !Verify(out, rootDigest, uint64(challengedLeaf)) {
		return contract.PDPVerifierProof{}, xerrors.Errorf("proof verification failed")
	}

	// Return the completed proof
	return out, nil
}

func (p *ProveTask) getSenderAddress(ctx context.Context, match common.Address) (common.Address, error) {
	var addressStr string
	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' AND address = $1 LIMIT 1`, match.Hex()).Scan(&addressStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Address{}, errors.New("no sender address with role 'pdp' found")
		}
		return common.Address{}, err
	}
	address := common.HexToAddress(addressStr)
	return address, nil
}

func (p *ProveTask) cleanupDeletedRoots(ctx context.Context, proofSetID int64, pdpVerifier *contract.PDPVerifier) error {

	removals, err := pdpVerifier.GetScheduledRemovals(nil, big.NewInt(proofSetID))
	if err != nil {
		return xerrors.Errorf("failed to get scheduled removals: %w", err)
	}

	// Execute cleanup in a transaction
	ok, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {

		for _, removeID := range removals {
			log.Debugw("cleanupDeletedRoots", "removeID", removeID)
			// Get the pdp_pieceref ID for the root before deleting
			var pdpPieceRefID int64
			err := tx.QueryRow(`
                SELECT pdp_pieceref 
                FROM pdp_proofset_roots 
                WHERE proofset = $1 AND root_id = $2
            `, proofSetID, removeID.Int64()).Scan(&pdpPieceRefID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					// Root already deleted, skip
					continue
				}
				return false, xerrors.Errorf("failed to get piece ref for root %d: %w", removeID, err)
			}

			// Delete the parked piece ref, this will cascade to the pdp piece ref too
			_, err = tx.Exec(`
				DELETE FROM parked_piece_refs
				WHERE ref_id = $1
			`, pdpPieceRefID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete parked piece ref %d: %w", pdpPieceRefID, err)
			}

			// Delete the root entry
			_, err = tx.Exec(`
                DELETE FROM pdp_proofset_roots 
                WHERE proofset = $1 AND root_id = $2
            `, proofSetID, removeID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete root %d: %w", removeID, err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("failed to cleanup deleted roots: %w", err)
	}
	if !ok {
		return xerrors.Errorf("database delete not committed")
	}

	return nil
}

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	id := ids[0]
	return &id, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PDPProve",
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

func Verify(proof contract.PDPVerifierProof, root [32]byte, position uint64) bool {
	computedHash := proof.Leaf

	for i := 0; i < len(proof.Proof); i++ {
		sibling := proof.Proof[i]

		if position%2 == 0 {
			log.Debugw("Verify", "position", position, "left-c", hex.EncodeToString(computedHash[:]), "right-s", hex.EncodeToString(sibling[:]), "out", hex.EncodeToString(shabytes(append(computedHash[:], sibling[:]...))[:]))
			// If position is even, current node is on the left
			computedHash = sha256.Sum256(append(computedHash[:], sibling[:]...))
		} else {
			log.Debugw("Verify", "position", position, "left-s", hex.EncodeToString(sibling[:]), "right-c", hex.EncodeToString(computedHash[:]), "out", hex.EncodeToString(shabytes(append(sibling[:], computedHash[:]...))[:]))
			// If position is odd, current node is on the right
			computedHash = sha256.Sum256(append(sibling[:], computedHash[:]...))
		}
		computedHash[31] &= 0x3F // set top bits to 00

		// Move up to the parent node
		position /= 2
	}

	// Compare the reconstructed root with the expected root
	return computedHash == root
}

func shabytes(in []byte) []byte {
	out := sha256.Sum256(in)
	return out[:]
}

var _ = harmonytask.Reg(&ProveTask{})
var _ harmonytask.TaskInterface = &ProveTask{}
