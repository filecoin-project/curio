package lmrpc

import (
	"context"
	"errors"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/go-state-types/abi"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
	"net/url"
	"strconv"
	"time"
)

type refTracker struct {
	db *harmonydb.DB

	// lmrpcPieceProxyRoot is the root URL of BoostAdapter, eg http://10.0.0.2:32100
	lmrpcPieceProxyRoot url.URL
}

func newRefTracker(db *harmonydb.DB, lmrpcRoot url.URL) (*refTracker, error) {
	rt := &refTracker{
		db:                  db,
		lmrpcPieceProxyRoot: lmrpcRoot,
	}

	return rt, rt.init()
}

type pieceRef struct {
	RefID   int64  `db:"ref_id"`
	PieceID int64  `db:"piece_id"`
	DataURL string `db:"data_url"`
}

type DataUrlRef struct {
	DataURL string `db:"data_url"`
}

func (rt *refTracker) init() error {
	// Init looks at parked_piece_refs and for any piece that has a local data_url, it will check if it should still be there

	_, err := rt.db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (commit bool, err error) {
		var refs []pieceRef
		err = rt.db.Select(context.Background(), &refs, `SELECT ref_id, piece_id, data_url FROM parked_piece_refs where data_url like $1`, rt.lmrpcPieceProxyRoot.String()+"%")
		if err != nil {
			log.Errorf("failed to query parked_piece_refs: %s", err)
			return
		}

		if len(refs) == 0 {
			log.Infof("no piece refs to check")
			return
		}

		// Now we need to get all refs in places which can hold them. In total it shouldn't more than couple thousands on large setups
		// * sectors_sdr_initial_pieces -> data_url
		// * sectors_snap_initial_pieces -> data_url
		// * open_sector_pieces -> data_url
		// Any other piecerefs with an url pointing to us were created in our previous lives and should be removed

		// select from the 3 tables, LIKE pieceref:%
		var dataUrlsSDR, dataUrlsSnap, dataUrlsOpen []DataUrlRef

		err = rt.db.Select(context.Background(), &dataUrlsSDR, `SELECT data_url FROM sectors_sdr_initial_pieces where data_url like 'pieceref:%'`)
		if err != nil {
			return false, xerrors.Errorf("failed to query sectors_sdr_initial_pieces: %w", err)
		}
		err = rt.db.Select(context.Background(), &dataUrlsSnap, `SELECT data_url FROM sectors_snap_initial_pieces where data_url like 'pieceref:%'`)
		if err != nil {
			return false, xerrors.Errorf("failed to query sectors_snap_initial_pieces: %w", err)
		}
		err = rt.db.Select(context.Background(), &dataUrlsOpen, `SELECT data_url FROM open_sector_pieces where data_url like 'pieceref:%'`)
		if err != nil {
			return false, xerrors.Errorf("failed to query open_sector_pieces: %w", err)
		}

		refUrls := make(map[string]struct{}, len(dataUrlsSDR)+len(dataUrlsSnap)+len(dataUrlsOpen))
		for _, ref := range dataUrlsSDR {
			refUrls[ref.DataURL] = struct{}{}
		}
		for _, ref := range dataUrlsSnap {
			refUrls[ref.DataURL] = struct{}{}
		}
		for _, ref := range dataUrlsOpen {
			refUrls[ref.DataURL] = struct{}{}
		}

		// Now we have all the data_urls that are in use, filter refs to remove
		batch := &pgx.Batch{}
		var n int
		for _, ref := range refs {
			// ru = pieceref:[ref.RefID]
			ru := "pieceref:" + strconv.FormatInt(ref.RefID, 10)

			if _, ok := refUrls[ru]; ok {
				// This ref is still in use
				continue
			}
			// This ref should not exist
			n++
			batch.Queue(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, ref.RefID)
			log.Warnw("deleting orphaned piece ref", "ref_id", ref.RefID, "piece_id", ref.PieceID, "data_url", ref.DataURL)
		}

		br := tx.SendBatch(context.Background(), batch)
		defer br.Close()

		for i := 0; i < n; i++ {
			_, err = br.Exec()
			if err != nil {
				return false, xerrors.Errorf("failed to delete orphaned piece ref: %w", err)
			}
		}

		log.Infow("deleted orphaned piece refs", "count", n)

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return xerrors.Errorf("failed to init ref tracker: %w", err)
	}

	return nil
}

func (rt *refTracker) addPieceEntry(ctx context.Context, db *harmonydb.DB, conf *config.CurioConfig, deal lpiece.PieceDealInfo, pieceSize abi.UnpaddedPieceSize, dataUrl url.URL, ssize abi.SectorSize) (int64, bool, func(), error) {
	var refID int64
	var pieceWasCreated bool

	for {
		var backpressureWait bool

		comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			// BACKPRESSURE
			wait, err := maybeApplyBackpressure(tx, conf.Ingest, ssize)
			if err != nil {
				return false, xerrors.Errorf("backpressure checks: %w", err)
			}
			if wait {
				backpressureWait = true
				return false, nil
			}

			var pieceID int64
			// Attempt to select the piece ID first
			err = tx.QueryRow(`SELECT id FROM parked_pieces WHERE piece_cid = $1`, deal.PieceCID().String()).Scan(&pieceID)

			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// Piece does not exist, attempt to insert
					err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size)
							VALUES ($1, $2, $3)
							ON CONFLICT (piece_cid) DO NOTHING
							RETURNING id`, deal.PieceCID().String(), int64(pieceSize.Padded()), int64(pieceSize)).Scan(&pieceID)
					if err != nil {
						return false, xerrors.Errorf("inserting new parked piece and getting id: %w", err)
					}
					pieceWasCreated = true // New piece was created
				} else {
					// Some other error occurred during select
					return false, xerrors.Errorf("checking existing parked piece: %w", err)
				}
			} else {
				pieceWasCreated = false // Piece already exists, no new piece was created
			}

			// Add parked_piece_ref
			err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url)
        			VALUES ($1, $2) RETURNING ref_id`, pieceID, dataUrl.String()).Scan(&refID)
			if err != nil {
				return false, xerrors.Errorf("inserting parked piece ref: %w", err)
			}

			// If everything went well, commit the transaction
			return true, nil // This will commit the transaction
		}, harmonydb.OptionRetry())
		if err != nil {
			return refID, false, nil, xerrors.Errorf("inserting parked piece: %w", err)
		}
		if !comm {
			if backpressureWait {
				// Backpressure was applied, wait and try again
				select {
				case <-time.After(backpressureWaitTime):
				case <-ctx.Done():
					return refID, false, nil, xerrors.Errorf("context done while waiting for backpressure: %w", ctx.Err())
				}
				continue
			}

			return refID, false, nil, xerrors.Errorf("piece tx didn't commit")
		}

		break
	}

	return refID, pieceWasCreated, func() {
		_, err := db.BeginTransaction(context.Background(), func(tx *harmonydb.Tx) (commit bool, err error) {
			refUrl := "pieceref:" + strconv.FormatInt(refID, 10)

			var dataUrls []DataUrlRef
			err = rt.db.Select(context.Background(), &dataUrls, `SELECT data_url FROM sectors_sdr_initial_pieces where data_url = $1`, refUrl)
			if err != nil {
				return false, xerrors.Errorf("failed to query sectors_sdr_initial_pieces: %w", err)
			}
			if len(dataUrls) > 0 {
				// have a ref, keep piece ref
				return false, nil
			}

			err = rt.db.Select(context.Background(), &dataUrls, `SELECT data_url FROM sectors_snap_initial_pieces where data_url = $1`, refUrl)
			if err != nil {
				return false, xerrors.Errorf("failed to query sectors_snap_initial_pieces: %w", err)
			}
			if len(dataUrls) > 0 {
				return false, nil
			}

			err = rt.db.Select(context.Background(), &dataUrls, `SELECT data_url FROM open_sector_pieces where data_url = $1`, refUrl)
			if err != nil {
				return false, xerrors.Errorf("failed to query open_sector_pieces: %w", err)
			}
			if len(dataUrls) > 0 {
				return false, nil
			}

			// No ref, delete piece ref
			log.Warnw("deleting orphaned piece ref", "ref_id", refID)
			_, err = tx.Exec(`DELETE FROM parked_piece_refs WHERE ref_id = $1`, refID)
			if err != nil {
				return false, xerrors.Errorf("failed to delete piece ref: %w", err)
			}

			return true, nil
		}, harmonydb.OptionRetry())
		if err != nil {
			log.Errorw("failed to perform pieceref cleanup", "ref_id", refID, "err", err)
		}
	}, nil
}
