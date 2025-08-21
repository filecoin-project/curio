package pdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

// Structures to represent database records
type DataSetPieceAdd struct {
	ID              string `db:"id"`
	Client          string `db:"client"`
	PieceCID2       string `db:"piece_cid_v2"` // pieceCIDV2
	DataSet         uint64 `db:"data_set_id"`
	PieceRef        int64  `db:"piece_ref"`
	AddMessageHash  string `db:"add_message_hash"`
	AddMessageIndex int64  `db:"add_message_index"`
}

// NewWatcherPieceAdd sets up the watcher for data set piece additions
func NewWatcherPieceAdd(db *harmonydb.DB, pcs *chainsched.CurioChainSched, ethClient *ethclient.Client) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetPieceAdds(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process pending data set piece adds: %s", err)
		}

		return nil
	}); err != nil {
		panic(err)
	}
}

// processPendingDataSetPieceAdds processes piece additions that have been confirmed on-chain
func processPendingDataSetPieceAdds(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	// Query for pdp_dataset_piece_adds entries where add_message_ok = TRUE
	var pieceAdds []DataSetPieceAdd

	err := db.Select(ctx, &pieceAdds, `
        SELECT id, client, piece_cid_v2, data_set_id, piece_ref, add_message_hash, add_message_index 
        FROM pdp_pipeline
        WHERE after_add_piece = TRUE AND after_add_piece_msg = FALSE
    `)
	if err != nil {
		return xerrors.Errorf("failed to select data set piece adds: %w", err)
	}

	if len(pieceAdds) == 0 {
		// No pending root adds
		return nil
	}

	// Process each piece addition
	for _, pieceAdd := range pieceAdds {
		err := processDataSetPieceAdd(ctx, db, pieceAdd, ethClient)
		if err != nil {
			log.Errorf("Failed to process piece add for tx %s: %s", pieceAdd.AddMessageHash, err)
			continue
		}
	}

	return nil
}

func processDataSetPieceAdd(ctx context.Context, db *harmonydb.DB, pieceAdd DataSetPieceAdd, ethClient *ethclient.Client) error {
	// Retrieve the tx_receipt from message_waits_eth
	var txReceiptJSON []byte
	var txSuccess bool
	err := db.QueryRow(ctx, `SELECT tx_success, tx_receipt FROM message_waits_eth WHERE signed_tx_hash = $1 
                                                       AND tx_success IS NOT NULL 
                                                       AND tx_receipt IS NOT NULL`, pieceAdd.AddMessageHash).Scan(&txSuccess, &txReceiptJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return xerrors.Errorf("tx hash %s is either missing from watch table or is not yet processed by watcher", pieceAdd.AddMessageHash)
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", pieceAdd.AddMessageHash, err)
	}

	// Unmarshal the tx_receipt JSON into types.Receipt
	var txReceipt types.Receipt
	err = json.Unmarshal(txReceiptJSON, &txReceipt)
	if err != nil {
		return xerrors.Errorf("failed to unmarshal tx_receipt for tx %s: %w", pieceAdd.AddMessageHash, err)
	}

	if !txSuccess {
		// This means msg failed, we should let the user know
		// TODO: Review if error would be in receipt
		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", pieceAdd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}
			_, err = tx.Exec(`DELETE FROM pdp_pipeline WHERE id = $1`, pieceAdd.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to clean up pdp pipeline: %w", err)
			}
			return true, nil
		})
		if err != nil {
			return xerrors.Errorf("failed to commit transaction: %w", err)
		}
		if !comm {
			return xerrors.Errorf("failed to commit transaction")
		}
		return nil
	}

	// Get the ABI from the contract metadata
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return fmt.Errorf("failed to get PDP ABI: %w", err)
	}

	// Get the event definition
	event, exists := pdpABI.Events["PiecesAdded"]
	if !exists {
		return fmt.Errorf("PiecesAdded event not found in ABI")
	}

	var pieceIds []uint64
	var pieceCids [][]byte
	eventFound := false

	pcid2, err := cid.Parse(pieceAdd.PieceCID2)
	if err != nil {
		return fmt.Errorf("failed to parse piece CID: %w", err)
	}

	parser, err := contract.NewPDPVerifierFilterer(contract.ContractAddresses().PDPVerifier, ethClient)
	if err != nil {
		return fmt.Errorf("failed to create PDPVerifierFilterer: %w", err)
	}

	// Iterate over the logs in the receipt
	for _, vLog := range txReceipt.Logs {
		// Check if the log corresponds to the PiecesAdded event
		if len(vLog.Topics) > 0 && vLog.Topics[0] == event.ID {
			// The setId is an indexed parameter in Topics[1], but we don't need it here
			// as we already have the dataset ID from the database

			parsed, err := parser.ParsePiecesAdded(*vLog)
			if err != nil {
				return fmt.Errorf("failed to parse event log: %w", err)
			}

			pieceIds = make([]uint64, len(parsed.PieceIds))
			for i := range parsed.PieceIds {
				pieceIds[i] = parsed.PieceIds[i].Uint64()
			}

			pieceCids = make([][]byte, len(parsed.PieceCids))
			for i := range parsed.PieceCids {
				pieceCids[i] = parsed.PieceCids[i].Data
			}

			eventFound = true
			// We found the event, so we can break the loop
			break
		}
	}

	if !eventFound {
		return fmt.Errorf("PiecesAdded event not found in receipt")
	}

	pieceId := pieceIds[pieceAdd.AddMessageIndex]
	pieceCid := pieceCids[pieceAdd.AddMessageIndex]

	apcid2, err := cid.Cast(pieceCid)
	if err != nil {
		return fmt.Errorf("failed to cast piece CID: %w", err)
	}

	if !apcid2.Equals(pcid2) {
		return fmt.Errorf("piece CID in event log does not match piece CID in message")
	}

	// Insert into message_waits_eth and pdp_dataset_pieces
	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Update data set for initialization upon first add
		_, err = tx.Exec(`
			UPDATE pdp_data_set SET init_ready = true
			WHERE id = $1 AND prev_challenge_request_epoch IS NULL AND challenge_request_msg_hash IS NULL AND prove_at_epoch IS NULL
			`, pieceAdd.DataSet)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_data_set: %w", err)
		}

		// Insert into pdp_dataset_piece
		n, err := tx.Exec(`
                  INSERT INTO pdp_dataset_piece (
                      data_set_id,
                      client,
                      piece_cid_v2,
                      piece,
                      piece_ref,
                      add_deal_id,
                      add_message_hash,
                      add_message_index
                  )
                  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
              `,
			pieceAdd.DataSet,
			pieceAdd.Client,
			pieceAdd.PieceCID2,
			pieceId,
			pieceAdd.PieceRef,
			pieceAdd.ID,
			pieceAdd.AddMessageHash,
			pieceAdd.AddMessageIndex,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to insert into pdp_dataset_piece: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows inserted for pdp_dataset_piece: %d", n)
		}

		n, err = tx.Exec(`UPDATE pdp_pipeline SET after_add_piece_msg = TRUE WHERE id = $1`, pieceAdd.ID)
		if err != nil {
			return false, xerrors.Errorf("failed to update pdp_pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("incorrect number of rows updated for pdp_pipeline: %d", n)
		}

		// Return true to commit the transaction
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("failed to save details to DB: %w", err)
	}

	if !comm {
		return xerrors.Errorf("failed to commit transaction")
	}

	return nil
}
