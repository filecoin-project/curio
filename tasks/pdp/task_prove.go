package pdp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"math/bits"
	"sort"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/minio/sha256-simd"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/crypto/sha3"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/proof"
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

	head atomic.Pointer[chainTypes.TipSet]

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

type ProveTaskChainApi interface {
	StateGetRandomnessDigestFromBeacon(ctx context.Context, randEpoch abi.ChainEpoch, tsk chainTypes.TipSetKey) (abi.Randomness, error) //perm:read
	ChainHead(context.Context) (*chainTypes.TipSet, error)                                                                              //perm:read
}

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient *ethclient.Client, fil ProveTaskChainApi, sender *message.SenderETH, cpr *cachedreader.CachedPieceReader) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
		cpr:       cpr,
		fil:       fil,
	}

	// ProveTasks are created on pdp_data_sets entries where
	// challenge_request_msg_hash is not null (=not yet landed)

	err := chainSched.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		if apply == nil {
			return nil
		}

		pt.head.Store(apply)

		for {
			more := false

			pt.addFunc.Val(ctx)(func(id harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Select data sets ready for proving
				var dataSets []struct {
					ID int64 `db:"id"`
				}

				currentHeight := apply.Height() - 1 // -1 to delay by a block to reduce chance for `premature proof` due to reorgs
				err := tx.Select(&dataSets, `
                    SELECT p.id
                    FROM pdp_data_sets p
                    INNER JOIN message_waits_eth mw on mw.signed_tx_hash = p.challenge_request_msg_hash
                    WHERE p.challenge_request_msg_hash IS NOT NULL
                      AND mw.tx_success = TRUE
                      AND p.prove_at_epoch < $1
                      AND p.terminated_at_epoch IS NULL
                      AND (p.next_prove_attempt_at IS NULL OR p.next_prove_attempt_at <= $1)
                    LIMIT 2
                `, currentHeight)
				if err != nil {
					return false, xerrors.Errorf("failed to select data sets: %w", err)
				}

				if len(dataSets) == 0 {
					// No data sets to process
					return false, nil
				}

				// Determine if there might be more data sets to process
				more = len(dataSets) > 1

				// Process the first data set
				todo := dataSets[0]

				// Insert a new task into pdpv0_prove_tasks
				affected, err := tx.Exec(`
                    INSERT INTO pdp_prove_tasks (data_set, task_id)
                    VALUES ($1, $2) ON CONFLICT DO NOTHING
                `, todo.ID, id)
				if err != nil {
					return false, xerrors.Errorf("failed to insert into pdp_prove_tasks: %w", err)
				}
				if affected == 0 {
					return false, nil
				}

				// Update pdp_data_sets to set next_challenge_possible = FALSE
				affected, err = tx.Exec(`
                    UPDATE pdp_data_sets
                    SET challenge_request_msg_hash = NULL
                    WHERE id = $1 AND challenge_request_msg_hash IS NOT NULL
                `, todo.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to update pdp_data_sets: %w", err)
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

func (p *ProveTask) disableProving(ctx context.Context, dataSetId int64) error {
	// cleanup all proving related columns
	// set init_ready to false so that next new piece enables proving
	//
	// This creates a bit of an edge case when piece deletions, additions and proving happen in the same time window:
	// - data set gets used, pieces are added and proven
	// - all pieces are deleted from it
	// - nextProvingPeriod gets called on an empty dataset
	// - a new piece gets added, it sets `init_ready = TRUE`, but it is true already
	// - prove task fires, detects that proving set is empty, challenge epoch is 0 (as the proving set is empty),
	// 		proving gets disabled
	// Now the dataset won't get proven until one more piece gets added to set `init_ready = TRUE`.
	// Better pattern here would be to react to events emitted in our messages from the transactions we send to PDPVerifier.
	// As ordering can get even more tricky if you consider that transactions are sent async.
	_, err := p.db.Exec(ctx, `
		UPDATE pdp_data_sets
		SET challenge_request_msg_hash = NULL, prove_at_epoch = NULL, init_ready = FALSE,
			prev_challenge_request_epoch = NULL
		WHERE id = $1
		`, dataSetId)
	if err != nil {
		return xerrors.Errorf("failed set values disabling proving: %w", err)
	}
	return nil
}

func (p *ProveTask) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Retrieve data set and challenge epoch for the task
	var dataSetId int64

	err = p.db.QueryRow(ctx, `
        SELECT data_set
        FROM pdp_prove_tasks
        WHERE task_id = $1
    `, taskID).Scan(&dataSetId)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details: %w", err)
	}

	defer func() {
		if err != nil {
			log.Errorw("Proof submission failed", "dataSetId", dataSetId, "error", err)
			err = fmt.Errorf("failed to submit possesion proof for dataset %d: %w", dataSetId, err)
		}
	}()

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
	challengeEpoch, err := pdpVerifier.GetNextChallengeEpoch(callOpts, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to get next challenge epoch: %w", err)
	}

	if challengeEpoch.Sign() == 0 { // if challengeEpoch is 0 (NO_CHALLENGE_SCHEDULED), we need to disable proving
		log.Infow("disabling proving", "dataSetId", dataSetId, "taskID", taskID, "reason", "no challenge epoch")
		err = p.disableProving(ctx, dataSetId)
		if err != nil {
			return false, xerrors.Errorf("failed to disable proving (caused by no challenge epoch): %w", err)
		}
		return true, nil
	}

	// Check if challengeEpoch is in the future
	// This can happen when Curio has been down and the local database state is out of sync with the chain.
	// The database may have an old prove_at_epoch, but GetNextChallengeEpoch returns the next proving window
	// which has rolled forward past the missed window.
	ts := p.head.Load()
	if ts == nil {
		// Try to get current head
		ts, err = p.fil.ChainHead(ctx)
		if err != nil {
			return false, xerrors.Errorf("failed to get chain head: %w", err)
		}
	}

	if challengeEpoch.Int64() > int64(ts.Height()) {
		log.Infow("challenge epoch is in the future, resetting to next proving period",
			"dataSetId", dataSetId, "challengeEpoch", challengeEpoch, "currentHeight", ts.Height())
		if err := ResetDatasetToNextPP(ctx, p.db, dataSetId); err != nil {
			return false, xerrors.Errorf("failed to reset to next proving period: %w", err)
		}
		return true, nil
	}

	totalLeafCount, err := pdpVerifier.GetChallengeRange(callOpts, big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to get data set leaf count: %w", err)
	}
	if !totalLeafCount.IsUint64() {
		return false, xerrors.Errorf("total leaf count is not uint64")
	}
	totalLeaves := totalLeafCount.Uint64()
	if totalLeaves == 0 {
		log.Infow("disabling proving", "dataSetId", dataSetId, "taskID", taskID, "reason", "no leaves")
		err = p.disableProving(ctx, dataSetId)
		if err != nil {
			return false, xerrors.Errorf("failed to disable proving (caused by no leaves): %w", err)
		}
		return true, nil
	}

	seed, err := p.fil.StateGetRandomnessDigestFromBeacon(ctx, abi.ChainEpoch(challengeEpoch.Int64()), chainTypes.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain randomness from beacon for pdp prove: %w", err)
	}

	proofs, err := p.GenerateProofs(ctx, pdpVerifier, dataSetId, seed, totalLeaves, contract.NumChallenges)
	if err != nil {
		return false, xerrors.Errorf("failed to generate proofs: %w", err)
	}

	abiData, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return false, xerrors.Errorf("failed to get PDPVerifier ABI: %w", err)
	}

	data, err := abiData.Pack("provePossession", big.NewInt(dataSetId), proofs)
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

		log.Infof("PDP Prove Task: dataSetId: %d, taskID: %d, proofs: %s", dataSetId, taskID, proofStr)
	} */

	pdpVerifierRaw := contract.PDPVerifierRaw{Contract: pdpVerifier}

	calcProofFeeResult := make([]any, 0)
	err = pdpVerifierRaw.Call(callOpts, &calcProofFeeResult, "calculateProofFee", big.NewInt(dataSetId))
	if err != nil {
		return false, xerrors.Errorf("failed to calculate proof fee: %w", err)
	}

	if len(calcProofFeeResult) == 0 {
		return false, xerrors.Errorf("failed to calculate proof fee: wrong number of return values")
	}
	if calcProofFeeResult[0] == nil {
		return false, xerrors.Errorf("failed to calculate proof fee: nil return value")
	}
	if calcProofFeeResult[0].(*big.Int) == nil {
		return false, xerrors.Errorf("failed to calculate proof fee: nil *big.Int return value")
	}
	proofFee := calcProofFeeResult[0].(*big.Int)

	// Add 2x buffer for certainty
	proofFee = new(big.Int).Mul(proofFee, big.NewInt(3))

	// Get the sender address for this data set
	owner, _, err := pdpVerifier.GetDataSetStorageProvider(callOpts, big.NewInt(dataSetId))
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

	log.Debugw("PDP Prove Task (verbose)",
		"dataSetId", dataSetId,
		"taskID", taskID,
		"proofs", proofLogs,
		"data", hex.EncodeToString(data),
		"proofFee initial", new(big.Int).Div(proofFee, big.NewInt(3)),
		"proofFee 3x", proofFee,
		"txEth", txEth,
	)

	log.Infow("PDP Prove Task",
		"dataSetId", dataSetId,
		"taskID", taskID,
		"proofCount", len(proofs),
		"dataLen", len(data),
		"proofFee", proofFee,
	)

	if !stillOwned() {
		// Task was abandoned, don't send the proofs
		return false, nil
	}

	reason := "pdp-prove"
	txHash, err := p.sender.Send(ctx, fromAddress, txEth, reason)
	if err != nil {
		// Get current height for error handling
		ts, heightErr := p.fil.ChainHead(ctx)
		if heightErr != nil {
			// Can't get chain height, fall back to cached head
			ts = p.head.Load()
		}
		if ts == nil {
			// No chain state available, let harmony retry
			return false, xerrors.Errorf("failed to send transaction (no chain state): %w", err)
		}
		currentHeight := int64(ts.Height())

		done, handleErr := HandleProvingSendError(ctx, p.db, dataSetId, currentHeight, err)
		if done {
			return true, nil
		}
		return false, xerrors.Errorf("failed to send transaction: %w", handleErr)
	}

	// Success, reset any accumulated failure count
	if resetErr := ResetProvingFailures(ctx, p.db, dataSetId); resetErr != nil {
		log.Warnw("Failed to reset proving failures after success", "error", resetErr, "dataSetId", dataSetId)
	}

	log.Infow("PDP Prove Task: transaction sent", "txHash", txHash, "dataSetId", dataSetId, "taskID", taskID)

	// Task completed successfully
	return true, nil
}

func (p *ProveTask) GenerateProofs(ctx context.Context, pdpService *contract.PDPVerifier, dataSetId int64, seed abi.Randomness, totalLeaves uint64, numChallenges int) ([]contract.IPDPTypesProof, error) {
	proofs := make([]contract.IPDPTypesProof, numChallenges)

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	challenges := lo.Times(numChallenges, func(i int) int64 {
		return generateChallengeIndex(seed, dataSetId, i, totalLeaves)
	})

	pieceId, err := pdpService.FindPieceIds(callOpts, big.NewInt(dataSetId), lo.Map(challenges, func(i int64, _ int) *big.Int { return big.NewInt(i) }))
	if err != nil {
		return nil, xerrors.Errorf("failed to find piece IDs: %w", err)
	}

	for i := 0; i < numChallenges; i++ {
		piece := pieceId[i]

		proof, err := p.provePiece(ctx, dataSetId, piece.PieceId.Int64(), piece.Offset.Int64())
		if err != nil {
			return nil, xerrors.Errorf("failed to prove piece %d (%d, %d, %d): %w", i, dataSetId, piece.PieceId.Int64(), piece.Offset.Int64(), err)
		}

		proofs[i] = proof
	}

	return proofs, nil
}

func generateChallengeIndex(seed abi.Randomness, dataSetId int64, proofIndex int, totalLeaves uint64) int64 {
	// Create a buffer to hold the concatenated data (96 bytes: 32 bytes * 3)
	data := make([]byte, 0, 96)

	// Seed is a 32-byte big-endian representation

	data = append(data, seed...)

	// Convert dataSetId to 32-byte big-endian representation
	dataSetIdBigInt := big.NewInt(dataSetId)
	dataSetIdBytes := padTo32Bytes(dataSetIdBigInt.Bytes())
	data = append(data, dataSetIdBytes...)

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
		"dataSetId", dataSetId,
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

func (p *ProveTask) genSubPieceMemtree(ctx context.Context, subPieceCid string, subPieceSize abi.PaddedPieceSize) ([]byte, error) {
	subPieceCidObj, err := cid.Parse(subPieceCid)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse subPiece CID: %w", err)
	}

	if subPieceSize > proof.MaxMemtreeSize {
		return nil, xerrors.Errorf("subPiece size exceeds maximum: %d", subPieceSize)
	}

	subPieceReader, unssize, err := p.cpr.GetSharedPieceReader(ctx, subPieceCidObj)
	if err != nil {
		return nil, xerrors.Errorf("failed to get subPiece reader: %w", err)
	}

	var r io.Reader = subPieceReader

	if unssize.Padded() > subPieceSize {
		return nil, xerrors.Errorf("subPiece size mismatch: %d > %d", unssize.Padded(), subPieceSize)
	} else if unssize.Padded() < subPieceSize {
		// pad with zeros
		r = io.MultiReader(r, nullreader.NewNullReader(abi.UnpaddedPieceSize(subPieceSize-unssize.Padded())))
	}

	defer func() {
		_ = subPieceReader.Close()
	}()

	return proof.BuildSha254Memtree(r, subPieceSize.Unpadded())
}

func (p *ProveTask) provePiece(ctx context.Context, dataSetId int64, pieceId int64, challengedLeaf int64) (contract.IPDPTypesProof, error) {
	const arity = 2

	pieceChallengeOffset := challengedLeaf * LeafSize

	// Retrieve the piece and subpiece
	type subPieceMeta struct {
		Piece          string `db:"piece"`
		SubPiece       string `db:"sub_piece"`
		SubPieceOffset int64  `db:"sub_piece_offset"` // padded offset
		SubPieceSize   int64  `db:"sub_piece_size"`   // padded piece size
		Removed        bool   `db:"removed"`
	}

	var subPieces []subPieceMeta

	err := p.db.Select(context.Background(), &subPieces, `
			SELECT piece, sub_piece, sub_piece_offset, sub_piece_size, removed
			FROM pdp_data_set_pieces
			WHERE data_set = $1 AND piece_id = $2
			ORDER BY sub_piece_offset ASC
		`, dataSetId, pieceId)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to get piece and subPiece: %w", err)
	}

	// find first subpiece with subpiece_offset >= pieceChallengeOffset
	challSubPiece, challSubPieceIdx, ok := lo.FindLastIndexOf(subPieces, func(subPiece subPieceMeta) bool {
		return subPiece.SubPieceOffset <= pieceChallengeOffset
	})
	if !ok {
		return contract.IPDPTypesProof{}, xerrors.New("no subpiece found")
	}
	if challSubPiece.Removed {
		log.Errorw("using removed piece", "dataSetId", dataSetId, "pieceId", pieceId)
	}

	// build subpiece memtree
	memtree, err := p.genSubPieceMemtree(ctx, challSubPiece.SubPiece, abi.PaddedPieceSize(challSubPiece.SubPieceSize))
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to generate subPiece memtree: %w", err)
	}

	subPieceChallengedLeaf := challengedLeaf - (challSubPiece.SubPieceOffset / LeafSize)
	log.Debugw("subPieceChallengedLeaf", "subPieceChallengedLeaf", subPieceChallengedLeaf, "challengedLeaf", challengedLeaf, "subPieceOffsetLs", challSubPiece.SubPieceOffset/LeafSize)

	/*
		type RawMerkleProof struct {
			Leaf  [32]byte
			Proof [][32]byte
			Piece  [32]byte
		}
	*/
	subPieceProof, err := proof.MemtreeProof(memtree, subPieceChallengedLeaf)
	pool.Put(memtree)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to generate subPiece proof: %w", err)
	}
	log.Debugw("subPieceProof", "subPieceProof", subPieceProof)

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
	var subPiecesSize abi.PaddedPieceSize

	// 1. prefill the partial tree
	for _, subPiece := range subPieces {
		subPiecesSize += abi.PaddedPieceSize(subPiece.SubPieceSize)

		unsCid, err := cid.Parse(subPiece.SubPiece)
		if err != nil {
			return contract.IPDPTypesProof{}, xerrors.Errorf("failed to parse subPiece CID: %w", err)
		}

		commp, err := commcid.CIDToPieceCommitmentV1(unsCid)
		if err != nil {
			return contract.IPDPTypesProof{}, xerrors.Errorf("failed to convert CID to piece commitment: %w", err)
		}

		var comm [LeafSize]byte
		copy(comm[:], commp)

		level := proof.NodeLevel(subPiece.SubPieceSize/LeafSize, arity)
		offset := (subPiece.SubPieceOffset / LeafSize) >> uint(level-1)
		partialTree[elemIndex{Level: level, ElemOffset: offset}] = treeElem{
			Level: level,
			Hash:  comm,
		}
	}

	pieceSize := nextPowerOfTwo(subPiecesSize)
	rootLevel := proof.NodeLevel(int64(pieceSize/LeafSize), arity)

	// 2. build the partial tree
	// we do the build from the right side of the tree - elements are sorted by size, so only elements on the right side can have missing siblings

	isRight := func(offset int64) bool {
		return offset&1 == 1
	}

	for i := len(subPieces) - 1; i >= 0; i-- {
		subPiece := subPieces[i]
		level := proof.NodeLevel(subPiece.SubPieceSize/LeafSize, arity)
		offset := (subPiece.SubPieceOffset / LeafSize) >> uint(level-1)
		firstSubPiece := i == 0

		curElem := partialTree[elemIndex{Level: level, ElemOffset: offset}]

		log.Debugw("processing partialtree subPiece", "curElem", curElem, "level", level, "offset", offset, "subPiece", subPiece.SubPieceOffset, "subPieceSz", subPiece.SubPieceSize)

		for !isRight(offset) {
			// find the rightSibling
			siblingIndex := elemIndex{Level: level, ElemOffset: offset + 1}
			rightSibling, ok := partialTree[siblingIndex]
			if !ok {
				// if we're processing the first subpiece branch, AND we've ran out of right siblings, we're done
				if firstSubPiece {
					break
				}

				// create a zero rightSibling
				rightSibling = treeElem{
					Level: level,
					Hash:  zerocomm.PieceComms[level-zerocomm.Skip-1],
				}
				log.Debugw("rightSibling zero", "rightSibling", rightSibling, "siblingIndex", siblingIndex, "level", level, "offset", offset)
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
			curElem = partialTree[elemIndex{Level: level, ElemOffset: offset}]
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

	}

	challLevel := proof.NodeLevel(challSubPiece.SubPieceSize/LeafSize, arity)
	challOffset := (challSubPiece.SubPieceOffset / LeafSize) >> uint(challLevel-1)

	log.Debugw("challSubPiece", "challSubPiece", challSubPieceIdx, "challLevel", challLevel, "challOffset", challOffset)

	challSubtreeLeaf := partialTree[elemIndex{Level: challLevel, ElemOffset: challOffset}]
	if challSubtreeLeaf.Hash != subPieceProof.Root {
		return contract.IPDPTypesProof{}, xerrors.Errorf("subtree root doesn't match partial tree leaf, %x != %x", challSubtreeLeaf.Hash, subPieceProof.Root)
	}

	var out contract.IPDPTypesProof
	copy(out.Leaf[:], subPieceProof.Leaf[:])
	out.Proof = append(out.Proof, subPieceProof.Proof...)

	currentLevel := challLevel
	currentOffset := challOffset

	for currentLevel < rootLevel {
		siblingOffset := currentOffset ^ 1

		// Retrieve sibling hash from partialTree or use zero hash
		siblingIndex := elemIndex{Level: currentLevel, ElemOffset: siblingOffset}
		index := elemIndex{Level: currentLevel, ElemOffset: currentOffset}
		siblingElem, ok := partialTree[siblingIndex]
		if !ok {
			return contract.IPDPTypesProof{}, xerrors.Errorf("missing sibling at level %d, offset %d", currentLevel, siblingOffset)
		}
		elem, ok := partialTree[index]
		if !ok {
			return contract.IPDPTypesProof{}, xerrors.Errorf("missing element at level %d, offset %d", currentLevel, currentOffset)
		}
		if currentOffset < siblingOffset { // left
			log.Debugw("Proof", "position", index, "left-c", hex.EncodeToString(elem.Hash[:]), "right-s", hex.EncodeToString(siblingElem.Hash[:]), "out", hex.EncodeToString(shabytes(append(elem.Hash[:], siblingElem.Hash[:]...))[:]))
		} else { // right
			log.Debugw("Proof", "position", index, "left-s", hex.EncodeToString(siblingElem.Hash[:]), "right-c", hex.EncodeToString(elem.Hash[:]), "out", hex.EncodeToString(shabytes(append(siblingElem.Hash[:], elem.Hash[:]...))[:]))
		}

		// Append the sibling's hash to the proof
		out.Proof = append(out.Proof, siblingElem.Hash)

		// Move up to the parent node
		currentOffset = currentOffset / arity
		currentLevel++
	}

	log.Debugw("proof complete", "proof", out)

	pieceCid, err := cid.Parse(subPieces[0].Piece)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to parse piece CID: %w", err)
	}
	commPiece, err := commcid.CIDToPieceCommitmentV1(pieceCid)
	if err != nil {
		return contract.IPDPTypesProof{}, xerrors.Errorf("failed to convert CID to piece commitment: %w", err)
	}
	var cr [LeafSize]byte
	copy(cr[:], commPiece)

	if !Verify(out, cr, uint64(challengedLeaf)) {
		return contract.IPDPTypesProof{}, xerrors.Errorf("proof verification failed")
	}

	// Return the completed proof
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

func (p *ProveTask) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	id := ids[0]
	return &id, nil
}

func (p *ProveTask) TypeDetails() harmonytask.TaskTypeDetails {
	// RAM: Proving builds a memtree for one piece at a time (sequential, not parallel).
	// Peak RAM ≈ 3× piece size (unpadBuf + memtreeBuf during fr32.Pad).
	// 2 GiB covers an average piece size of up to ~680 MiB; max 1 GiB pieces may exceed this.
	const proveTaskRAM = 2 << 30 // 2 GiB

	return harmonytask.TaskTypeDetails{
		Name: "PDPv0_Prove",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: proveTaskRAM,
		},
		MaxFailures: 5,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

func nextPowerOfTwo(n abi.PaddedPieceSize) abi.PaddedPieceSize {
	lz := bits.LeadingZeros64(uint64(n - 1))
	return 1 << (64 - lz)
}

func Verify(proof contract.IPDPTypesProof, root [32]byte, position uint64) bool {
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

var (
	_                           = harmonytask.Reg(&ProveTask{})
	_ harmonytask.TaskInterface = &ProveTask{}
)
