package pdp

import (
	"context"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/go-state-types/abi"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
	"math/big"
)

// ProofSet represents a record from pdp_proof_sets table
type ProofSet struct {
	ID int64 `db:"id"`
}

// NewWatcherNextChallengeEpoch creates and starts the watcher for next_challenge_epoch updates
func NewWatcherNextChallengeEpoch(
	db *harmonydb.DB,
	ethClient *ethclient.Client,
	pcs *chainsched.CurioChainSched,
) error {
	callOpts := &bind.CallOpts{
		Context: context.Background(),
	}
	pdpServiceAddress := contract.ContractAddresses().PDPService
	pdpService, err := contract.NewPDPService(pdpServiceAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPService contract at %s: %w", pdpServiceAddress.Hex(), err)
	}
	cf, err := pdpService.GetChallengeFinality(callOpts)
	if err != nil {
		return xerrors.Errorf("failed to get challenge finality: %w", err)
	}
	challengeFinality := abi.ChainEpoch(cf.Uint64())

	return pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		minInterestingPrevChallengeEpoch := apply.Height() - challengeFinality
		err := processNullNextChallengeEpochs(ctx, minInterestingPrevChallengeEpoch, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process null next_challenge_epochs: %v", err)
		}
		return nil
	})
}

func processNullNextChallengeEpochs(ctx context.Context, minHeight abi.ChainEpoch, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_proof_sets entries where next_challenge_epoch IS NULL
	var proofSets []ProofSet

	err := db.Select(ctx, &proofSets, `
        SELECT id
        FROM pdp_proof_sets
        WHERE next_challenge_possible = FALSE
          AND (prev_challenge_epoch IS NULL OR prev_challenge_epoch < $1)
    `, minHeight)
	if err != nil {
		return xerrors.Errorf("failed to select proof sets with null next_challenge_epoch: %w", err)
	}

	if len(proofSets) == 0 {
		// No proof sets to process
		return nil
	}

	// Instantiate the PDPService contract instance
	pdpContracts := contract.ContractAddresses()
	pdpServiceAddress := pdpContracts.PDPService

	pdpService, err := contract.NewPDPService(pdpServiceAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPService contract at %s: %w", pdpServiceAddress.Hex(), err)
	}

	// Prepare call options (use latest block)
	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	// Process each proof set
	for _, ps := range proofSets {
		err := updateNextChallengeEpoch(ctx, db, pdpService, callOpts, ps)
		if err != nil {
			log.Warnf("Failed to update next_challenge_epoch for proof set %d: %v", ps.ID, err)
			continue
		}
	}

	return nil
}

func updateNextChallengeEpoch(
	ctx context.Context,
	db *harmonydb.DB,
	pdpService *contract.PDPService,
	callOpts *bind.CallOpts,
	ps ProofSet,
) error {
	// Call getNextChallengeEpoch(setID) on the PDPService contract
	nextChallengeEpochBigInt, err := pdpService.GetNextChallengeEpoch(callOpts, big.NewInt(ps.ID))
	if err != nil {
		return xerrors.Errorf("failed to get nextChallengeEpoch for proof set %d: %w", ps.ID, err)
	}

	// Check if nextChallengeEpoch is 0, which might indicate that it's uninitialized
	if nextChallengeEpochBigInt.Cmp(big.NewInt(0)) == 0 {
		return nil
	}

	// Update the database with the retrieved nextChallengeEpoch
	_, err = db.Exec(ctx, `
        UPDATE pdp_proof_sets
        SET next_challenge_epoch = $1, next_challenge_possible = TRUE
        WHERE id = $2 AND next_challenge_possible = FALSE
    `, nextChallengeEpochBigInt.Int64(), ps.ID)
	if err != nil {
		return xerrors.Errorf("failed to update next_challenge_epoch for proof set %d: %w", ps.ID, err)
	}

	log.Infof("Updated next_challenge_epoch for proof set %d to %d", ps.ID, nextChallengeEpochBigInt.Int64())
	return nil
}
