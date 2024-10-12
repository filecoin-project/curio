package pdp

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/pipeline/lib/nullreader"
	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/minio/sha256-simd"
	"github.com/samber/lo"
	"golang.org/x/crypto/sha3"
	"golang.org/x/xerrors"
	"io"
	"math/big"
	"math/bits"
	"sort"
)

const LeafSize = proof.NODE_SIZE

type ProveTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH
	cpr       *cachedreader.CachedPieceReader

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH, cpr *cachedreader.CachedPieceReader) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		cpr:       cpr,
	}

	err := chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		for {
			more := false

			pt.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Select proof sets ready for proving
				var proofSets []struct {
					ID                 int64         `db:"id"`
					NextChallengeEpoch sql.NullInt64 `db:"next_challenge_epoch"`
				}

				err := tx.Select(&proofSets, `
                    SELECT id, next_challenge_epoch
                    FROM pdp_proof_sets
                    WHERE next_challenge_epoch IS NOT NULL
                      AND next_challenge_epoch <= $1
                      AND next_challenge_possible = TRUE
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
				more = len(proofSets) == 2

				// Process the first proof set
				todo := proofSets[0]

				// Insert a new task into pdp_prove_tasks
				affected, err := tx.Exec(`
                    INSERT INTO pdp_prove_tasks (proofset, challenge_epoch, task_id)
                    VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
                `, todo.ID, todo.NextChallengeEpoch.Int64, id)
				if err != nil {
					return false, xerrors.Errorf("failed to insert into pdp_prove_tasks: %w", err)
				}
				if affected == 0 {
					more = false
					return false, nil
				}

				// Update pdp_proof_sets to set next_challenge_possible = FALSE
				affected, err = tx.Exec(`
                    UPDATE pdp_proof_sets
                    SET next_challenge_possible = FALSE
                    WHERE id = $1 AND next_challenge_possible = TRUE
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
	pdpServiceAddress := pdpContracts.PDPService

	pdpService, err := contract.NewPDPService(pdpServiceAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPService contract at %s: %w", pdpServiceAddress.Hex(), err)
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	// Proof parameters
	challengeEpoch, err := pdpService.GetNextChallengeEpoch(callOpts, big.NewInt(proofSetID))
	if err != nil {
		return false, xerrors.Errorf("failed to get next challenge epoch: %w", err)
	}

	// Number of challenges to generate
	numChallenges := 5 // Application / pdp will specify this later

	proofs, err := p.GenerateProofs(ctx, pdpService, proofSetID, challengeEpoch, numChallenges)
	if err != nil {
		return false, xerrors.Errorf("failed to generate proofs: %w", err)
	}

	abiData, err := contract.PDPServiceMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPService ABI: %w", err)
	}

	data, err := abiData.Pack("provePossession", big.NewInt(proofSetID), proofs)
	if err != nil {
		return false, xerrors.Errorf("failed to pack data: %w", err)
	}

	// [ ["0x559e581f022bb4e4ec6e719e563bf0e026ad6de42e56c18714a2c692b1b88d7e", ["0x559e581f022bb4e4ec6e719e563bf0e026ad6de42e56c18714a2c692b1b88d7e"]] ]

	{
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
	}

	log.Infow("PDP Prove Task", "proofSetID", proofSetID, "taskID", taskID, "proofs", proofs, "data", hex.EncodeToString(data))

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	txEth := types.NewTransaction(
		0,
		contract.ContractAddresses().PDPService,
		big.NewInt(0),
		0,
		nil,
		data,
	)

	if !stillOwned() {
		// Task was abandoned, don't send the proofs
		return false, nil
	}

	fromAddress, err := p.getSenderAddress(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get sender address: %w", err)
	}

	reason := "pdp-prove"
	txHash, err := p.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		return false, xerrors.Errorf("failed to send transaction: %w", err)
	}

	// Start a transaction
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update pdp_prove_tasks with the transaction hash
		_, err := tx.Exec(`
					UPDATE pdp_prove_tasks
					SET message_eth_hash = $1
					WHERE task_id = $2
				`, txHash.Hex(), taskID)

		// Optionally, update pdp_proof_sets.next_challenge_epoch = NULL and next_challenge_possible = FALSE
		_, err = tx.Exec(`
            UPDATE pdp_proof_sets
            SET prev_challenge_epoch = $2, next_challenge_possible = FALSE
            WHERE id = $1
        `, proofSetID, challengeEpoch)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_proof_sets: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return false, err
	}

	// Task completed successfully
	return true, nil
}

func (p *ProveTask) GenerateProofs(ctx context.Context, pdpService *contract.PDPService, proofSetID int64, seed *big.Int, numChallenges int) ([]contract.PDPServiceProof, error) {
	proofs := make([]contract.PDPServiceProof, numChallenges)

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	totalLeafCount, err := pdpService.GetProofSetLeafCount(callOpts, big.NewInt(proofSetID))
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

func generateChallengeIndex(seed *big.Int, proofSetID int64, proofIndex int, totalLeaves uint64) int64 {
	// Create a buffer to hold the concatenated data (96 bytes: 32 bytes * 3)
	data := make([]byte, 0, 96)

	// Convert seed to 32-byte big-endian representation
	seedBytes := padTo32Bytes(seed.Bytes())
	data = append(data, seedBytes...)

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
	log.Infow("generateChallengeIndex",
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

func (p *ProveTask) genSubrootMemtree(ctx context.Context, subrootCid string, subrootSize abi.PaddedPieceSize) ([]byte, error) {
	subrootCidObj, err := cid.Parse(subrootCid)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse subroot CID: %w", err)
	}

	if subrootSize > proof.MaxMemtreeSize {
		return nil, xerrors.Errorf("subroot size exceeds maximum: %d", subrootSize)
	}

	subrootReader, unssize, err := p.cpr.GetSharedPieceReader(ctx, subrootCidObj)
	if err != nil {
		return nil, xerrors.Errorf("failed to get subroot reader: %w", err)
	}

	var r io.Reader = subrootReader

	if unssize.Padded() > subrootSize {
		return nil, xerrors.Errorf("subroot size mismatch: %d > %d", unssize.Padded(), subrootSize)
	} else if unssize.Padded() < subrootSize {
		// pad with zeros
		r = io.MultiReader(r, nullreader.NewNullReader(abi.UnpaddedPieceSize(subrootSize-unssize.Padded())))
	}

	defer subrootReader.Close()

	return proof.BuildSha254Memtree(r, subrootSize.Unpadded())
}

func (p *ProveTask) proveRoot(ctx context.Context, proofSetID int64, rootId int64, challengedLeaf int64) (contract.PDPServiceProof, error) {
	const arity = 2

	rootChallengeOffset := challengedLeaf * LeafSize

	// Retrieve the root and subroot
	type subrootMeta struct {
		Root          string `db:"root"`
		Subroot       string `db:"subroot"`
		SubrootOffset int64  `db:"subroot_offset"` // padded offset
		SubrootSize   int64  `db:"subroot_size"`   // padded piece size
	}

	var subroots []subrootMeta

	err := p.db.Select(context.Background(), &subroots, `
			SELECT root, subroot, subroot_offset, subroot_size
			FROM pdp_proofset_roots
			WHERE proofset = $1 AND root_id = $2
			ORDER BY subroot_offset ASC
		`, proofSetID, rootId)
	if err != nil {
		return contract.PDPServiceProof{}, xerrors.Errorf("failed to get root and subroot: %w", err)
	}

	// find first subroot with subroot_offset >= rootChallengeOffset
	challSubRoot, challSubrootIdx, ok := lo.FindLastIndexOf(subroots, func(subroot subrootMeta) bool {
		return subroot.SubrootOffset < rootChallengeOffset
	})
	if !ok {
		return contract.PDPServiceProof{}, xerrors.New("no subroot found")
	}

	// build subroot memtree
	memtree, err := p.genSubrootMemtree(ctx, challSubRoot.Subroot, abi.PaddedPieceSize(challSubRoot.SubrootSize))
	if err != nil {
		return contract.PDPServiceProof{}, xerrors.Errorf("failed to generate subroot memtree: %w", err)
	}

	subrootChallengedLeaf := challengedLeaf - (challSubRoot.SubrootOffset / LeafSize)
	log.Infow("subrootChallengedLeaf", "subrootChallengedLeaf", subrootChallengedLeaf, "challengedLeaf", challengedLeaf, "subrootOffsetLs", challSubRoot.SubrootOffset/LeafSize)

	/*
		type RawMerkleProof struct {
			Leaf  [32]byte
			Proof [][32]byte
			Root  [32]byte
		}
	*/
	subrootProof, err := proof.MemtreeProof(memtree, subrootChallengedLeaf)
	pool.Put(memtree)
	if err != nil {
		return contract.PDPServiceProof{}, xerrors.Errorf("failed to generate subroot proof: %w", err)
	}
	log.Infow("subrootProof", "subrootProof", subrootProof)

	// build partial top-tree
	type treeElem struct {
		Level int // 1 == leaf, NODE_SIZE
		Hash  [LeafSize]byte
	}
	type elemIndex struct {
		Level      int
		ElemOffset int64 // offset in terms of nodes at the current level
	}

	partialTree := map[elemIndex]treeElem{}
	var subrootsSize abi.PaddedPieceSize

	// 1. prefill the partial tree
	for _, subroot := range subroots {
		subrootsSize += abi.PaddedPieceSize(subroot.SubrootSize)

		unsCid, err := cid.Parse(subroot.Subroot)
		if err != nil {
			return contract.PDPServiceProof{}, xerrors.Errorf("failed to parse subroot CID: %w", err)
		}

		commp, err := commcid.CIDToPieceCommitmentV1(unsCid)
		if err != nil {
			return contract.PDPServiceProof{}, xerrors.Errorf("failed to convert CID to piece commitment: %w", err)
		}

		var comm [LeafSize]byte
		copy(comm[:], commp)

		level := proof.NodeLevel(subroot.SubrootSize/LeafSize, arity)
		offset := (subroot.SubrootOffset / LeafSize) >> uint(level-1)
		partialTree[elemIndex{Level: level, ElemOffset: offset}] = treeElem{
			Level: level,
			Hash:  comm,
		}
	}

	rootSize := nextPowerOfTwo(subrootsSize)
	rootLevel := proof.NodeLevel(int64(rootSize/LeafSize), arity)

	// 2. build the partial tree
	// we do the build from the right side of the tree - elements are sorted by size, so only elements on the right side can have missing siblings

	isRight := func(offset int64) bool {
		return offset&1 == 1
	}

	for i := len(subroots) - 1; i >= 0; i-- {
		subroot := subroots[i]
		level := proof.NodeLevel(subroot.SubrootSize/LeafSize, arity)
		offset := (subroot.SubrootOffset / LeafSize) >> uint(level-1)
		firstSubroot := i == 0

		curElem := partialTree[elemIndex{Level: level, ElemOffset: offset}]

		log.Infow("processing partialtree subroot", "curElem", curElem, "level", level, "offset", offset, "subroot", subroot.SubrootOffset, "subrootSz", subroot.SubrootSize)

		for !isRight(offset) {
			// find the rightSibling
			siblingIndex := elemIndex{Level: level, ElemOffset: offset + 1}
			rightSibling, ok := partialTree[siblingIndex]
			if !ok {
				// if we're processing the first subroot branch, AND we've ran out of right siblings, we're done
				if firstSubroot {
					break
				}

				// create a zero rightSibling
				rightSibling = treeElem{
					Level: level,
					Hash:  zerocomm.PieceComms[level-zerocomm.Skip-1],
				}
				log.Infow("rightSibling zero", "rightSibling", rightSibling, "siblingIndex", siblingIndex, "level", level, "offset", offset)
				partialTree[siblingIndex] = rightSibling
			}

			// compute the parent
			parent := proof.ComputeBinShaParent(curElem.Hash, rightSibling.Hash)
			parentLevel := level + 1
			parentOffset := offset / arity

			partialTree[elemIndex{Level: parentLevel, ElemOffset: parentOffset}] = treeElem{
				Level: parentLevel,
				Hash:  parent,
			}

			// move to the parent
			level = parentLevel
			offset = parentOffset
		}
	}

	{
		var partialTreeList []elemIndex
		for k := range partialTree {
			partialTreeList = append(partialTreeList, k)
		}
		sort.Slice(partialTreeList, func(i, j int) bool {
			if partialTreeList[i].Level != partialTreeList[j].Level {
				return partialTreeList[i].Level < partialTreeList[j].Level
			}
			return partialTreeList[i].ElemOffset < partialTreeList[j].ElemOffset
		})
		log.Infow("partialTree", "partialTree", partialTreeList)
	}

	challLevel := proof.NodeLevel(challSubRoot.SubrootSize/LeafSize, arity)
	challOffset := (challSubRoot.SubrootOffset / LeafSize) >> uint(challLevel-1)

	log.Infow("challSubRoot", "challSubRoot", challSubrootIdx, "challLevel", challLevel, "challOffset", challOffset)

	challSubtreeLeaf := partialTree[elemIndex{Level: challLevel, ElemOffset: challOffset}]
	if challSubtreeLeaf.Hash != subrootProof.Root {
		return contract.PDPServiceProof{}, xerrors.Errorf("subtree root doesn't match partial tree leaf, %x != %x", challSubtreeLeaf.Hash, subrootProof.Root)
	}

	var out contract.PDPServiceProof
	copy(out.Leaf[:], subrootProof.Leaf[:])
	out.Proof = append(out.Proof, subrootProof.Proof...)

	currentLevel := challLevel
	currentOffset := challOffset

	for currentLevel < rootLevel {
		siblingOffset := currentOffset ^ 1

		// Retrieve sibling hash from partialTree or use zero hash
		siblingIndex := elemIndex{Level: currentLevel, ElemOffset: siblingOffset}
		siblingElem, ok := partialTree[siblingIndex]
		if !ok {
			return contract.PDPServiceProof{}, xerrors.Errorf("missing sibling at level %d, offset %d", currentLevel, siblingOffset)
		}

		log.Infow("siblingElem", "siblingElem", siblingElem, "siblingIndex", siblingIndex, "currentLevel", currentLevel, "currentOffset", currentOffset, "siblingOffset", siblingOffset)

		// Append the sibling's hash to the proof
		out.Proof = append(out.Proof, siblingElem.Hash)

		// Move up to the parent node
		currentOffset = currentOffset / arity
		currentLevel++
	}

	log.Infow("proof complete", "proof", out)

	rootCid, err := cid.Parse(subroots[0].Root)
	if err != nil {
		return contract.PDPServiceProof{}, xerrors.Errorf("failed to parse root CID: %w", err)
	}
	commRoot, err := commcid.CIDToPieceCommitmentV1(rootCid)
	if err != nil {
		return contract.PDPServiceProof{}, xerrors.Errorf("failed to convert CID to piece commitment: %w", err)
	}
	var cr [LeafSize]byte
	copy(cr[:], commRoot)

	if !Verify(out, cr, uint64(challengedLeaf)) {
		return contract.PDPServiceProof{}, xerrors.Errorf("proof verification failed")
	}

	// Return the completed proof
	return out, nil
}

func (p *ProveTask) getSenderAddress(ctx context.Context) (common.Address, error) {
	// todo do based on proofset

	var addressStr string
	err := p.db.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp' LIMIT 1`).Scan(&addressStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Address{}, errors.New("no sender address with role 'pdp' found")
		}
		return common.Address{}, err
	}
	address := common.HexToAddress(addressStr)
	return address, nil
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
			Ram: 128 << 20, // 128 MB
		},
		MaxFailures: 500,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

func nextPowerOfTwo(n abi.PaddedPieceSize) abi.PaddedPieceSize {
	lz := bits.LeadingZeros64(uint64(n - 1))
	return 1 << (64 - lz)
}

func Verify(proof contract.PDPServiceProof, root [32]byte, position uint64) bool {
	computedHash := proof.Leaf

	for i := 0; i < len(proof.Proof); i++ {
		sibling := proof.Proof[i]

		if position%2 == 0 {
			log.Infow("Verify", "position", position, "left-c", hex.EncodeToString(computedHash[:]), "right-s", hex.EncodeToString(sibling[:]), "ouh", hex.EncodeToString(shabytes(append(computedHash[:], sibling[:]...))[:]))
			// If position is even, current node is on the left
			computedHash = sha256.Sum256(append(computedHash[:], sibling[:]...))
		} else {
			log.Infow("Verify", "position", position, "left-s", hex.EncodeToString(sibling[:]), "right-c", hex.EncodeToString(computedHash[:]), "ouh", hex.EncodeToString(shabytes(append(sibling[:], computedHash[:]...))[:]))
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
