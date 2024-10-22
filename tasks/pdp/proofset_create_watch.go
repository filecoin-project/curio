package pdp

import (
	"context"
	"encoding/json"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"
	chainTypes "github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
	"math/big"
)

type ProofSetCreate struct {
	CreateMessageHash string `db:"create_message_hash"`
	Service           string `db:"service"`
}

func NewWatcherCreate(db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingProofSetCreates(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending proof set creates: %v", err)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingProofSetCreates(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_proofset_creates entries where ok = TRUE and proofset_created = FALSE
	var proofSetCreates []ProofSetCreate

	err := db.Select(ctx, &proofSetCreates, `
        SELECT create_message_hash, service
        FROM pdp_proofset_creates
        WHERE ok = TRUE AND proofset_created = FALSE
    `)
	if err != nil {
		return xerrors.Errorf("failed to select proof set creates: %w", err)
	}

	if len(proofSetCreates) == 0 {
		// No pending proof set creates
		return nil
	}

	// Process each proof set create
	for _, psc := range proofSetCreates {
		err := processProofSetCreate(ctx, db, psc)
		if err != nil {
			log.Warnf("Failed to process proof set create for tx %s: %v", psc.CreateMessageHash, err)
			continue
		}
	}

	return nil
}

func processProofSetCreate(ctx context.Context, db *harmonydb.DB, psc ProofSetCreate) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	err := db.QueryRow(ctx, `
        SELECT tx_receipt
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
    `, psc.CreateMessageHash).Scan(&txReceiptJSON)
	if err != nil {
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", psc.CreateMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", psc.CreateMessageHash, err)
	}

	// Parse the logs to extract the proofSetId
	proofSetId, err := extractProofSetIdFromReceipt(&txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to extract proofSetId from receipt for tx %s: %w", psc.CreateMessageHash, err)
	}

	// Insert a new entry into pdp_proof_sets
	err = insertProofSet(ctx, db, psc.CreateMessageHash, proofSetId, psc.Service)
	if err != nil {
		return xerrors.Errorf("failed to insert proof set %d for tx %+v: %w", proofSetId, psc, err)
	}

	// Update pdp_proofset_creates to set proofset_created = TRUE
	_, err = db.Exec(ctx, `
        UPDATE pdp_proofset_creates
        SET proofset_created = TRUE
        WHERE create_message_hash = $1
    `, psc.CreateMessageHash)
	if err != nil {
		return xerrors.Errorf("failed to update proofset_creates for tx %s: %w", psc.CreateMessageHash, err)
	}

	return nil
}

func extractProofSetIdFromReceipt(receipt *types.Receipt) (uint64, error) {
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return 0, xerrors.Errorf("failed to get PDP ABI: %w", err)
	}

	event, exists := pdpABI.Events["ProofSetCreated"]
	if !exists {
		return 0, xerrors.Errorf("ProofSetCreated event not found in ABI")
	}

	for _, vLog := range receipt.Logs {
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			if len(vLog.Topics) < 2 {
				return 0, xerrors.Errorf("log does not contain setId topic")
			}

			setIdBigInt := new(big.Int).SetBytes(vLog.Topics[1].Bytes())
			return setIdBigInt.Uint64(), nil
		}
	}

	return 0, xerrors.Errorf("ProofSetCreated event not found in receipt")
}

func insertProofSet(ctx context.Context, db *harmonydb.DB, createMsg string, proofSetId uint64, service string) error {
	// Implement the insertion into pdp_proof_sets table
	// Adjust the SQL statement based on your table schema
	_, err := db.Exec(ctx, `
        INSERT INTO pdp_proof_sets (id, create_message_hash, service)
        VALUES ($1, $2, $3)
    `, proofSetId, createMsg, service)
	return err
}
