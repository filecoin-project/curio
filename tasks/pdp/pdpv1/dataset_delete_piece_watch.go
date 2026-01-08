package pdpv1

import (
	"context"
	"database/sql"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

type DataSetPieceDelete struct {
	ID      string  `db:"id"`
	DataSet uint64  `db:"set_id"`
	Pieces  []int64 `db:"pieces"`
	Hash    string  `db:"tx_hash"`
}

func NewPDPv1WatcherPieceDelete(db *harmonydb.DB, pcs *chainsched.CurioChainSched, ethClient *ethclient.Client) {
	if err := pcs.AddHandler(chainsched.HandlerEntry{Fn: func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingDataSetPieceDeletes(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process PDPv1 pending data set creates: %s", err)
		}
		err = processPendingCleanup(ctx, db, ethClient)
		if err != nil {
			log.Errorf("Failed to process PDPv1 pending data set piece deletes: %s", err)
		}
		return nil
	}, Priority: chainsched.PriorityLate}); err != nil {
		panic(err)
	}
}

func processPendingDataSetPieceDeletes(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	var pendingDeletes []struct {
		ID        string       `db:"id"`
		DataSetID int64        `db:"set_id"`
		Pieces    []int64      `db:"pieces"`
		TxHash    string       `db:"tx_hash"`
		TxSuccess sql.NullBool `db:"tx_success"`
	}

	err := db.Select(ctx, &pendingDeletes, `SELECT
    												ppd.id,
    												ppd.set_id,
    												ppd.pieces,
    												ppd.tx_hash,
													mwe.tx_success
												FROM pdp_piece_delete ppd
												LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = ppd.tx_hash
												WHERE ppd.tx_hash IS NOT NULL
												  AND ppd.terminated = FALSE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending piece deletes: %w", err)
	}

	if len(pendingDeletes) == 0 {
		return nil
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, piece := range pendingDeletes {
		if !piece.TxSuccess.Valid {
			log.Debugf("for piece (%d:%d) tx %s not found in message_waits_eth", piece.DataSetID, piece.Pieces, piece.TxHash)
			continue
		}

		if !piece.TxSuccess.Bool {
			comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
				n, err := tx.Exec(`UPDATE market_mk20_deal
									SET pdp_v1 = jsonb_set(
													jsonb_set(pdp_v1, '{error}', to_jsonb($1::text), true),
													'{complete}', to_jsonb(true), true
												 )
									WHERE id = $2;`, "Transaction failed", piece.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
				}
				if n != 1 {
					return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
				}
				_, err = tx.Exec(`DELETE FROM pdp_piece_delete WHERE id = $1`, piece.ID)
				if err != nil {
					return false, xerrors.Errorf("failed to delete row from pdp_piece_delete: %w", err)
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

		removals, err := verifier.GetScheduledRemovals(&bind.CallOpts{Context: ctx}, big.NewInt(piece.DataSetID))
		if err != nil {
			return xerrors.Errorf("failed to get scheduled removals: %w", err)
		}

		for _, p := range piece.Pieces {
			contains := lo.Contains(removals, big.NewInt(p))
			if !contains {
				// Huston! we have a serious problem
				return xerrors.Errorf("piece %d of dataSet %d is not scheduled for removal", p, piece.DataSetID)
			}
		}

		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			n, err := tx.Exec(`UPDATE pdp_dataset_piece SET removed = TRUE, 
                         remove_deal_id = $1, 
                         remove_message_hash = $2 
                         WHERE data_set_id = $3 AND piece = ANY($4)`, piece.ID, piece.TxHash, piece.DataSetID, piece.Pieces)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_dataset_piece: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}

			n, err = tx.Exec(`UPDATE market_mk20_deal
							SET pdp_v1 = jsonb_set(pdp_v1, '{complete}', 'true'::jsonb, true)
							WHERE id = $1;`, piece.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update market_mk20_deal: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}

			n, err = tx.Exec(`UPDATE pdp_piece_delete SET terminated = TRUE WHERE id = $1`, piece.ID)
			if err != nil {
				return false, xerrors.Errorf("failed to update pdp_piece_delete: %w", err)
			}
			if n != 1 {
				return false, xerrors.Errorf("expected 1 row to be updated, got %d", n)
			}

			return true, nil
		}, harmonydb.OptionRetry())

		if err != nil {
			return xerrors.Errorf("failed to commit transaction: %w", err)
		}
		if !comm {
			return xerrors.Errorf("failed to commit transaction")
		}
	}

	return nil
}

func processPendingCleanup(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	var pending []struct {
		ID        string  `db:"id"`
		DataSetID int64   `db:"set_id"`
		Pieces    []int64 `db:"pieces"`
	}

	err := db.Select(ctx, &pending, `SELECT id, set_id, pieces FROM pdp_piece_delete WHERE terminated = TRUE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending piece deletes: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, p := range pending {

		var livePieces []int64
		var terminatedPieces []int64

		for _, piece := range p.Pieces {
			live, err := verifier.PieceLive(nil, big.NewInt(p.DataSetID), big.NewInt(piece))
			if err != nil {
				return xerrors.Errorf("failed to check if piece is live: %w", err)
			}
			if live {
				livePieces = append(livePieces, piece)
			} else {
				terminatedPieces = append(terminatedPieces, piece)
			}
		}

		if len(livePieces) > 0 {
			// Let's not delete anything that is still live
			_, err := db.Exec(ctx, `UPDATE pdp_piece_delete SET pieces = $2 
                        WHERE terminated = TRUE AND id = $1`, p.ID, livePieces)
			if err != nil {
				return xerrors.Errorf("failed to update pdp_piece_delete: %w", err)
			}
		}

		if len(terminatedPieces) > 0 {
			// Clean up the terminated pieces
			_, err := db.Exec(ctx, `INSERT INTO piece_cleanup (id, piece_cid_v2, pdp, sp_id, sector_number, piece_ref)
								SELECT p.add_deal_id, p.piece_cid_v2, TRUE, -1, -1, p.piece_ref
								FROM pdp_dataset_piece AS p
								WHERE p.data_set_id = $1
								  AND p.piece = ANY($2)
								ON CONFLICT (id, pdp) DO NOTHING;`, p.DataSetID, terminatedPieces)
			if err != nil {
				return xerrors.Errorf("failed to insert into piece_cleanup: %w", err)
			}
		}

		if len(livePieces) == 0 {
			_, err = db.Exec(ctx, `DELETE FROM pdp_piece_delete WHERE id = $1`, p.ID)
			if err != nil {
				return xerrors.Errorf("failed to delete row from pdp_piece_delete: %w", err)
			}
		}
	}
	return nil
}
