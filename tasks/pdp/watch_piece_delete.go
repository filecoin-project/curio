package pdp

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ipfs/go-cid"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipni/go-libipni/ingest/schema"
	"github.com/ipni/go-libipni/maurl"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/chainsched"
	"github.com/filecoin-project/curio/market/indexstore"
	"github.com/filecoin-project/curio/market/ipni/ipniculib"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
)

func NewPieceDeleteWatcher(cfg *config.HTTPConfig, db *harmonydb.DB, ethClient *ethclient.Client, pcs *chainsched.CurioChainSched, idx *indexstore.IndexStore) {
	if err := pcs.AddHandler(func(ctx context.Context, revert, apply *chainTypes.TipSet) error {
		err := processPendingPieceDeletes(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending piece delete: %s", err)
		}

		err = processPendingCleanup(ctx, db, ethClient)
		if err != nil {
			log.Warnf("Failed to process pending piece cleanup: %s", err)
		}

		err = processIndexingAndIPNICleanup(ctx, db, cfg, idx)
		if err != nil {
			log.Warnf("Failed to process indexing and IPNI cleanup: %s", err)
		}

		return nil
	}); err != nil {
		panic(err)
	}
}

func processPendingPieceDeletes(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {

	var pendingDeletes []struct {
		DataSetID int64  `db:"data_set"`
		PieceID   int64  `db:"piece_id"`
		TxHash    string `db:"rm_message_hash"`
	}

	err := db.Select(ctx, &pendingDeletes, `SELECT data_set, piece_id, rm_message_hex FROM pdp_data_set_pieces WHERE rm_message_hash IS NOT NULL AND removed = FALSE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending piece deletes: %w", err)
	}

	for _, piece := range pendingDeletes {
		err = processPendingPieceDelete(ctx, db, ethClient, piece.DataSetID, piece.PieceID, piece.TxHash)
		if err != nil {
			return xerrors.Errorf("failed to process pending piece delete: %w", err)
		}
	}

	return nil
}

func processPendingPieceDelete(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, dataSetID, pieceID int64, txHex string) error {
	var success bool
	err := db.QueryRow(ctx, `
        SELECT tx_success
        FROM message_waits_eth
        WHERE signed_tx_hash = $1
        AND tx_success IS NOT NULL
    `, txHex).Scan(&success)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return xerrors.Errorf("failed to get tx_receipt for tx %s: %w", txHex, err)
	}

	if !success {
		return xerrors.Errorf("tx %s failed", txHex)
	}

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	removals, err := verifier.GetScheduledRemovals(&bind.CallOpts{Context: ctx}, big.NewInt(dataSetID))
	if err != nil {
		return xerrors.Errorf("failed to get scheduled removals: %w", err)
	}

	contains := lo.Contains(removals, big.NewInt(pieceID))
	if !contains {
		// Huston! we have a serious problem
		return xerrors.Errorf("piece %d is not scheduled for removal", pieceID)
	}

	n, err := db.Exec(ctx, `UPDATE pdp_data_set_pieces 
								SET removed = TRUE 
								WHERE data_set = $1 
								  AND piece_id = $2 
								  AND rm_message_hash = $3
								  AND removed = FALSE`, dataSetID, pieceID)
	if err != nil {
		return xerrors.Errorf("failed to update pdp_data_set_pieces: %w", err)
	}

	if n != 1 {
		return xerrors.Errorf("expected to update 1 row but updated %d", n)
	}

	return nil
}

func processPendingCleanup(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) error {
	var pieces []struct {
		DataSetID int64 `db:"data_set"`
		PieceID   int64 `db:"piece_id"`
	}

	err := db.Select(ctx, &pieces, `SELECT data_set, piece_id FROM pdp_data_set_pieces WHERE removed = TRUE`)
	if err != nil {
		return xerrors.Errorf("failed to select pending piece deletes: %w", err)
	}

	if len(pieces) == 0 {
		return nil
	}

	log.Infof("Cleaning up %d pieces", len(pieces))

	pdpAddress := contract.ContractAddresses().PDPVerifier

	verifier, err := contract.NewPDPVerifier(pdpAddress, ethClient)
	if err != nil {
		return xerrors.Errorf("failed to instantiate PDPVerifier contract: %w", err)
	}

	for _, piece := range pieces {
		live, err := verifier.PieceLive(nil, big.NewInt(piece.DataSetID), big.NewInt(piece.PieceID))
		if err != nil {
			return xerrors.Errorf("failed to check if piece is live: %w", err)
		}

		if !live {
			_, err := db.Exec(ctx, `DELETE FROM pdp_data_set_pieces WHERE data_set = $1 AND piece_id = $2`, piece.DataSetID, piece.PieceID)
			if err != nil {
				return xerrors.Errorf("failed to delete piece %d: %w", piece.PieceID, err)
			}
		}
	}

	return nil
}

func processIndexingAndIPNICleanup(ctx context.Context, db *harmonydb.DB, cfg *config.HTTPConfig, idx *indexstore.IndexStore) error {

	var pieces []struct {
		ID        int64  `db:"id"`
		PieceCID  string `db:"piece_cid"`
		PieceSize int64  `db:"piece_size"`
		PieceRef  int64  `db:"piece_ref"`
	}

	err := db.Select(ctx, &pieces, `SELECT 
    										pr.id,
    										pr.piece_cid,
       										pp.piece_size, 
       										pr.piece_ref 
										FROM pdp_piecerefs pr
										    JOIN parked_piece_refs ppr ON pr.piece_ref = ppr.ref_id
										    JOIN parked_pieces pp ON ppr.piece_id = pp.id 
										WHERE pr.data_set_refcount = 0`)
	if err != nil {
		return xerrors.Errorf("failed to select pending piece deletes: %w", err)
	}

	if len(pieces) == 0 {
		return nil
	}

	log.Infof("Cleaning up Indexing and IPNI for %d pieces", len(pieces))

	var peerID string
	var privKeyBytes []byte
	err = db.QueryRow(ctx, `SELECT priv_key, peer_id FROM ipni_peerid WHERE sp_id = -2`).Scan(&privKeyBytes, &peerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // IPNI not init yet
		}
		return xerrors.Errorf("failed to get private ipni-libp2p key: %w", err)
	}

	pkey, err := crypto.UnmarshalPrivateKey(privKeyBytes)
	if err != nil {
		return xerrors.Errorf("unmarshaling private key: %w", err)
	}

	for _, piece := range pieces {
		// Deleting Index
		pcid, err := cid.Parse(piece.PieceCID)
		if err != nil {
			return xerrors.Errorf("failed to parse piece CID: %w", err)
		}

		err = idx.RemoveIndexes(ctx, pcid)
		if err != nil {
			return xerrors.Errorf("failed to remove indexes for piece %s: %w", piece.PieceCID, err)
		}

		// Create RM ad
		_, err = db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var contextID []byte
			var metadata []byte
			var isRMAd bool

			err = tx.QueryRow(`SELECT 
    									context_id, 
    									metadata,
    									is_rm
									FROM ipni 
									WHERE piece_cid = $1 
									  AND piece_size = $2 
									ORDER BY order_number DESC LIMIT 1`, piece.PieceCID, piece.PieceSize).Scan(&contextID, &metadata, &isRMAd)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				return false, xerrors.Errorf("querying previous advertisement: %w", err)
			}

			if isRMAd {
				// Already removed, skip
				log.Infof("Skipping removal ad for piece %s as last ad for this piece requested removal", piece.PieceCID)
				return false, nil
			}

			var prev string
			err = tx.QueryRow(`SELECT head FROM ipni_head WHERE provider = $1`, peerID).Scan(&prev)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return false, xerrors.Errorf("querying previous head: %w", err)
			}

			adv := schema.Advertisement{
				Provider:  peerID,
				ContextID: contextID,
				Metadata:  metadata,
				IsRm:      true,
			}

			{
				u, err := url.Parse(fmt.Sprintf("https://%s:443", cfg.DomainName))
				if err != nil {
					return false, xerrors.Errorf("parsing announce address domain: %w", err)
				}
				if build.BuildType != build.BuildMainnet && build.BuildType != build.BuildCalibnet {
					ls := strings.Split(cfg.ListenAddress, ":")
					u, err = url.Parse(fmt.Sprintf("http://%s:%s", cfg.DomainName, ls[1]))
					if err != nil {
						return false, xerrors.Errorf("parsing announce address domain: %w", err)
					}
				}

				addr, err := maurl.FromURL(u)
				if err != nil {
					return false, xerrors.Errorf("converting URL to multiaddr: %w", err)
				}

				log.Infow("Announcing piece removal to IPNI", "piece", piece.PieceCID, "provider", peerID, "addr", addr.String())

				adv.Addresses = append(adv.Addresses, addr.String())
			}

			if prev != "" {
				prevCID, err := cid.Parse(prev)
				if err != nil {
					return false, xerrors.Errorf("parsing previous CID: %w", err)
				}

				adv.PreviousID = cidlink.Link{Cid: prevCID}
			}

			err = adv.Sign(pkey)
			if err != nil {
				return false, xerrors.Errorf("signing the advertisement: %w", err)
			}

			err = adv.Validate()
			if err != nil {
				return false, xerrors.Errorf("validating the advertisement: %w", err)
			}

			adNode, err := adv.ToNode()
			if err != nil {
				return false, xerrors.Errorf("converting advertisement to node: %w", err)
			}

			ad, err := ipniculib.NodeToLink(adNode, schema.Linkproto)
			if err != nil {
				return false, xerrors.Errorf("converting advertisement to link: %w", err)
			}

			_, err = tx.Exec(`SELECT insert_ad_and_update_head($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				ad.(cidlink.Link).Cid.String(), adv.ContextID, piece.PieceCID, piece.PieceSize, adv.IsRm, adv.Provider, strings.Join(adv.Addresses, "|"),
				adv.Signature, adv.Entries.String())

			if err != nil {
				return false, xerrors.Errorf("adding advertisement to the database: %w", err)
			}

			// Let's drop the PDP piece ref
			_, err = tx.Exec(`DELETE FROM pdp_piecerefs WHERE id = $1`, piece.PieceRef)
			if err != nil {
				return false, xerrors.Errorf("failed to delete PDP piece ref %d: %w", piece.PieceRef, err)
			}

			_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, piece.PieceRef)
			if err != nil {
				return false, xerrors.Errorf("failed to delete parked piece ref %d: %w", piece.PieceRef, err)
			}

			return true, nil

		}, harmonydb.OptionRetry())

		if err != nil {
			return xerrors.Errorf("failed to create IPNI removal ad for piece %s: %w", piece.PieceCID, err)
		}
	}

	return nil
}
