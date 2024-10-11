package pdp

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/lib/promise"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/tasks/message"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/samber/lo"
	"golang.org/x/xerrors"
	"math/big"
)

const LeafSize = proof.NODE_SIZE

type ProveTask struct {
	db        *harmonydb.DB
	ethClient *ethclient.Client
	sender    *message.SenderETH

	addFunc promise.Promise[harmonytask.AddTaskFunc]
}

func NewProveTask(chainSched *chainsched.CurioChainSched, db *harmonydb.DB, ethClient *ethclient.Client, sender *message.SenderETH) *ProveTask {
	pt := &ProveTask{
		db:        db,
		ethClient: ethClient,
		sender:    sender,
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
	var challengeEpoch int64

	err = p.db.QueryRow(context.Background(), `
        SELECT proofset, challenge_epoch
        FROM pdp_prove_tasks
        WHERE task_id = $1
    `, taskID).Scan(&proofSetID, &challengeEpoch)
	if err != nil {
		return false, xerrors.Errorf("failed to get task details: %w", err)
	}

	pdpContracts := contract.ContractAddresses()
	pdpServiceAddress := pdpContracts.PDPService

	pdpService, err := contract.NewPDPService(pdpServiceAddress, p.ethClient)
	if err != nil {
		return false, xerrors.Errorf("failed to instantiate PDPService contract at %s: %w", pdpServiceAddress.Hex(), err)
	}

	// Proof parameters
	seed := big.NewInt(challengeEpoch)

	// Number of challenges to generate
	numChallenges := 20 // Application / pdp will specify this later

	proofs, err := p.GenerateProofs(ctx, pdpService, proofSetID, seed, numChallenges)
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
            SET next_challenge_epoch = NULL, next_challenge_possible = FALSE
            WHERE id = $1
        `, proofSetID)
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

		proof, err := p.proveRoot(proofSetID, root.RootId.Int64(), root.Offset.Int64())
		if err != nil {
			return nil, xerrors.Errorf("failed to prove root (%d, %d, %d): %w", proofSetID, root.RootId.Int64(), root.Offset.Int64(), err)
		}

		proofs[i] = proof
	}

	return proofs, nil
}

func generateChallengeIndex(seed *big.Int, proofSetID int64, proofIndex int, totalLeaves uint64) int64 {
	// Hash the seed, proofSetID, and proofIndex to get a deterministic challenge index
	data := append(seed.Bytes(), big.NewInt(proofSetID).Bytes()...)
	data = append(data, big.NewInt(int64(proofIndex)).Bytes()...)
	hash := crypto.Keccak256Hash(data)

	hashInt := new(big.Int).SetBytes(hash.Bytes())
	totalLeavesBigInt := new(big.Int).SetUint64(totalLeaves)
	challengeIndex := new(big.Int).Mod(hashInt, totalLeavesBigInt)

	return challengeIndex.Int64()
}

func (p *ProveTask) proveRoot(proofSetID int64, rootId int64, challengedLeaf int64) (contract.PDPServiceProof, error) {
	/*
		CREATE TABLE pdp_proofset_roots (
		    proofset BIGINT NOT NULL, -- pdp_proof_sets.id
		    root TEXT NOT NULL, -- root cid (piececid v2)

		    add_message_hash TEXT NOT NULL REFERENCES message_waits_eth(signed_tx_hash) ON DELETE CASCADE,
		    add_message_index BIGINT NOT NULL, -- index of root in the add message

		    root_id BIGINT NOT NULL, -- on-chain index of the root in the rootCids sub-array

		    -- aggregation roots (aggregated like pieces in filecoin sectors)
		    subroot TEXT NOT NULL, -- subroot cid (piececid v2), with no aggregation this == root
		    subroot_offset BIGINT NOT NULL, -- offset of the subroot in the root
		    -- note: size contained in subroot piececid v2

		    pdp_pieceref BIGINT NOT NULL, -- pdp_piecerefs.id

		    CONSTRAINT pdp_proofset_roots_root_id_unique PRIMARY KEY (proofset, root_id, subroot_offset),

		    FOREIGN KEY (proofset) REFERENCES pdp_proof_sets(id) ON DELETE CASCADE, -- cascade, if we drop a proofset, we no longer care about the roots
		    FOREIGN KEY (pdp_pieceref) REFERENCES pdp_piecerefs(id) ON DELETE SET NULL -- sets null on delete so that it's easy to notice and clean up
		);

	*/

	rootChallengeOffset := challengedLeaf * LeafSize

	// Retrieve the root and subroot
	type subrootMeta struct {
		Root          string `db:"root"`
		Subroot       string `db:"subroot"`
		SubrootOffset int64  `db:"subroot_offset"`
	}

	var subroots []subrootMeta

	err := p.db.Select(context.Background(), &subroots, `
			SELECT root, subroot, subroot_offset
			FROM pdp_proofset_roots
			WHERE proofset = $1 AND root_id = $2
			ORDER BY subroot_offset ASC
		`, proofSetID, rootId, rootChallengeOffset)
	if err != nil {
		return contract.PDPServiceProof{}, xerrors.Errorf("failed to get root and subroot: %w", err)
	}

	// find first subroot with subroot_offset >= rootChallengeOffset
	challRoot, challIdx, ok := lo.FindIndexOf(subroots, func(subroot subrootMeta) bool {
		return subroot.SubrootOffset >= rootChallengeOffset
	})
	if !ok {
		return contract.PDPServiceProof{}, xerrors.New("no subroot found")
	}

	_ = challIdx
	_ = challRoot

	panic("implement me")
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
		MaxFailures: 5,
	}
}

func (p *ProveTask) Adder(taskFunc harmonytask.AddTaskFunc) {
	p.addFunc.Set(taskFunc)
}

var _ = harmonytask.Reg(&ProveTask{})
var _ harmonytask.TaskInterface = &ProveTask{}
