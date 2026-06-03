package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/parkpiece"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/mk20"
)

const minSaveCacheSize = abi.PaddedPieceSize(uint64(32 * 1024 * 1024))
const serviceName = "public"
const storachaMigrationDataURL = "storacha-migration"

type importPiecesOutput struct {
	Count  int      `json:"count"`
	Pieces []string `json:"pieces"`
}

type storachaImportResult struct {
	Imported   bool
	PieceCIDV2 string
}

type storachaParkedPieceState struct {
	ID              int64
	UseStagedFile   bool
	AlreadyComplete bool
}

type storachaFinalRecoveryRow struct {
	ID         int64  `db:"id"`
	PieceCID   string `db:"piece_cid"`
	PaddedSize int64  `db:"piece_padded_size"`
	RawSize    int64  `db:"piece_raw_size"`
}

var importPiecesCmd = &cli.Command{
	Name:  "import-pieces",
	Usage: "Imports already existing pieces from storage to piece park system",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "source",
			Usage: "path to store the pieces from",
		},
		&cli.StringFlag{
			Name:  "target",
			Usage: "path to storage directory in Curio's attached permanent storage",
		},
		&cli.IntFlag{
			Name:  "batch-size",
			Usage: "number of pieces to move to permanent storage",
			Value: 20,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context

		/*
			How this migration works:

			The source directory has Storacha CAR files named by piece CID v2. Curio's
			piece park stores the same bytes under a parked_pieces id, so the final
			file under the target storage root becomes piece/s-t00-<parked_piece_id>.
			That means the migration must first learn or create the parked_pieces
			row before it knows the final filename.

			Every fresh source file is moved to target/storacha-staging first. That
			staging move is the first retry boundary: once the file leaves source, a
			later run can still find it and finish the DB/ref/final-file work through
			importStagedStorachaPiece.

			importStagedStorachaPiece owns the normal path:
			1. Parse the staged filename to get the piece info.
			2. In one DB transaction, create or reuse the parked_pieces row.
			3. If the row is incomplete and has a live harmony task, leave the staged
			   file alone because that task owns the normal download/write path.
			4. Otherwise, ensure exactly one storacha-migration parked_piece_refs row
			   and exactly one pdp_piecerefs row for that parked ref.
			5. After the transaction commits, move the staged file to
			   target/piece/s-t00-<id>, declare it in sector_location, then mark
			   parked_pieces.complete=true.

			recoverFinalStorachaPieces exists for the one point where staging can no
			longer help: if a previous run already renamed the file into target/piece
			and then crashed before declaring the file or marking the parked piece
			complete. At that point there is no staged filename left to process, so
			recovery starts from the DB row/ref and checks whether the final file is
			already present.
		*/

		if cctx.String("source") == "" || cctx.String("target") == "" {
			return fmt.Errorf("source and target both are required")
		}

		sourcePath, err := homedir.Expand(cctx.String("source"))
		if err != nil {
			return xerrors.Errorf("expanding source path: %w", err)
		}

		targetPath, err := homedir.Expand(cctx.String("target"))
		if err != nil {
			return xerrors.Errorf("expanding target path: %w", err)
		}
		targetPath, err = filepath.Abs(filepath.Clean(targetPath))
		if err != nil {
			return xerrors.Errorf("resolving target path: %w", err)
		}

		storageID, err := pieceStorageID(targetPath)
		if err != nil {
			return xerrors.Errorf("getting storage ID: %w", err)
		}

		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}

		si := paths.NewDBIndex(curioalerting.NewAlertingSystem(), db)

		out, err := runImportPieces(ctx, db, si, storageID, sourcePath, targetPath, cctx.Int("batch-size"))
		if err != nil {
			return err
		}

		err = PrintJson(out)
		if err != nil {
			return err
		}

		return nil
	},
}

func runImportPieces(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, sourcePath, targetPath string, batchSize int) (importPiecesOutput, error) {
	var out importPiecesOutput

	staging := filepath.Join(targetPath, "storacha-staging")
	existing, err := os.ReadDir(staging)
	if err != nil {
		if !os.IsNotExist(err) {
			return out, xerrors.Errorf("reading staging directory: %w", err)
		}

		err = os.MkdirAll(staging, 0755)
		if err != nil {
			return out, xerrors.Errorf("creating staging directory: %w", err)
		}
	}

	if len(existing) > 0 {
		// Staging is the retry boundary: a file here was already moved out of
		// source, but DB refs or final piece declaration may not be complete.
		for _, entry := range existing {
			if out.Count >= batchSize {
				break
			}
			if entry.IsDir() {
				continue
			}

			result, err := importStagedStorachaPiece(ctx, db, si, storageID, targetPath, filepath.Join(staging, entry.Name()))
			if err != nil {
				return out, err
			}
			if result.Imported {
				out.Pieces = append(out.Pieces, result.PieceCIDV2)
				out.Count++
			}
		}
	}

	// This covers the crash point after staging was renamed to the final
	// piece path, but before sector_location/parked_pieces was finalized.
	err = recoverFinalStorachaPieces(ctx, db, si, storageID, targetPath, &out, batchSize)
	if err != nil {
		return out, err
	}

	// Open the directory and read all .car files in it. No need to care about subdirectories
	entries, err := os.ReadDir(sourcePath)
	if err != nil {
		return out, xerrors.Errorf("reading directory: %w", err)
	}

	for _, entry := range entries {
		if out.Count >= batchSize {
			break
		}
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".car") {
			continue
		}

		_, ok, err := storachaPieceInfoFromFileName(entry.Name())
		if err != nil {
			return out, err
		}
		if !ok {
			continue
		}

		oldPath := filepath.Join(sourcePath, entry.Name())
		stagingPath := filepath.Join(staging, entry.Name())
		if _, err := os.Stat(stagingPath); err == nil {
			return out, xerrors.Errorf("staging file already exists: %s", stagingPath)
		} else if !os.IsNotExist(err) {
			return out, xerrors.Errorf("checking staging file: %w", err)
		}

		// New imports also go through staging, so the same recovery code can
		// resume after a crash at any later step.
		err = os.Rename(oldPath, stagingPath)
		if err != nil {
			return out, xerrors.Errorf("renaming old path: %w", err)
		}

		result, err := importStagedStorachaPiece(ctx, db, si, storageID, targetPath, stagingPath)
		if err != nil {
			return out, err
		}
		if result.Imported {
			out.Pieces = append(out.Pieces, result.PieceCIDV2)
			out.Count++
		}
	}

	return out, nil
}

// pieceStorageID maps the user-provided target directory back to Curio's
// storage id. The target flag is expected to point at the storage root that
// contains sectorstore.json; final piece files are stored under target/piece.
func pieceStorageID(targetPath string) (storiface.ID, error) {
	mb, err := os.ReadFile(filepath.Join(targetPath, paths.MetaFile))
	if err != nil {
		return "", xerrors.Errorf("reading storage metadata for %s: %w", targetPath, err)
	}

	var meta storiface.LocalStorageMeta
	if err := json.Unmarshal(mb, &meta); err != nil {
		return "", xerrors.Errorf("unmarshalling storage metadata for %s: %w", targetPath, err)
	}

	if meta.ID != ("") {
		return meta.ID, nil
	}

	return "", xerrors.Errorf("no storage ID found for %s", targetPath)
}

// storachaPieceInfoFromFileName accepts only Storacha migration inputs named
// <piece-cid-v2>.car. Invalid names or unparsable CIDs are skipped; valid names
// are converted to the v1 piece CID plus padded/raw sizes used by parked_pieces.
func storachaPieceInfoFromFileName(name string) (*mk20.PieceInfo, bool, error) {
	pcidStr, ok := strings.CutSuffix(name, ".car")
	if !ok || pcidStr == "" {
		return nil, false, nil
	}

	pcid2, err := cid.Parse(pcidStr)
	if err != nil {
		return nil, false, nil
	}

	pi, err := mk20.GetPieceInfo(pcid2)
	if err != nil {
		return nil, false, xerrors.Errorf("getting piece info: %w", err)
	}

	return pi, true, nil
}

// importStagedStorachaPiece finishes one target/storacha-staging/<cid>.car.
// It first creates or reuses parked_pieces/refs in a committed DB transaction.
// If an existing incomplete parked_piece still has a live task, it returns
// Imported=false and leaves the staged file untouched for a later retry.
// Otherwise it consumes the staged file: remove it if the final
// target/piece/s-t00-<id> already exists, or rename it to that final path. It
// then declares the piece location and marks parked_pieces.complete unless the
// row was already complete.
func importStagedStorachaPiece(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, targetPath, stagingPath string) (storachaImportResult, error) {
	info, err := os.Stat(stagingPath)
	if err != nil {
		return storachaImportResult{}, xerrors.Errorf("checking staging file: %w", err)
	}
	if info.IsDir() {
		return storachaImportResult{}, nil
	}

	pi, ok, err := storachaPieceInfoFromFileName(filepath.Base(stagingPath))
	if err != nil {
		return storachaImportResult{}, err
	}
	if !ok {
		return storachaImportResult{}, nil
	}

	var state storachaParkedPieceState
	// The transaction only decides ownership and DB references. It deliberately
	// does not move the file to s-t00-<id>; the id must be committed before the
	// filename depends on it.
	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		parkedPieceID, createdParkedPiece, err := parkpiece.UpsertSkipWithInserted(tx, pi.PieceCIDV1.String(), int64(pi.Size), int64(pi.RawSize), true, true)
		if err != nil {
			return false, xerrors.Errorf("upsert parked piece: %w", err)
		}

		state = storachaParkedPieceState{
			ID:            parkedPieceID,
			UseStagedFile: createdParkedPiece,
		}

		if !createdParkedPiece {
			state, err = claimExistingStorachaPieceTx(tx, parkedPieceID)
			if err != nil {
				return false, err
			}
		}

		if !state.UseStagedFile {
			return true, nil
		}

		if err := ensureStorachaRefsTx(tx, parkedPieceID, pi); err != nil {
			return false, err
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return storachaImportResult{}, xerrors.Errorf("upsert parked piece: %w", err)
	}
	if !comm {
		return storachaImportResult{}, xerrors.Errorf("failed to commit transaction")
	}
	if !state.UseStagedFile {
		// An existing incomplete parked_piece still has a live harmony task. Leave
		// the staged file in place; a later retry can use it if that task disappears.
		return storachaImportResult{}, nil
	}

	finalPath := storachaFinalPiecePath(targetPath, state.ID)
	finalInfo, err := os.Stat(finalPath)
	switch {
	case err == nil:
		// A final file means an earlier run got past rename. The staged file is
		// now only a duplicate input for the same parked piece.
		if finalInfo.IsDir() {
			return storachaImportResult{}, xerrors.Errorf("final piece path is a directory: %s", finalPath)
		}
		if err := os.Remove(stagingPath); err != nil {
			return storachaImportResult{}, xerrors.Errorf("removing staged duplicate: %w", err)
		}
	case os.IsNotExist(err):
		// Normal post-commit path: the DB id is durable, so it is now safe for the
		// final filename to depend on parked_pieces.id.
		if err := os.Rename(stagingPath, finalPath); err != nil {
			return storachaImportResult{}, xerrors.Errorf("renaming staging file: %w", err)
		}
	default:
		return storachaImportResult{}, xerrors.Errorf("checking final piece file: %w", err)
	}

	if err := declareStorachaPiece(ctx, si, storageID, state.ID); err != nil {
		return storachaImportResult{}, err
	}

	if !state.AlreadyComplete {
		// complete is last. If we crash before this, final-file recovery will see
		// s-t00-<id>, declare it again idempotently, and mark the row complete.
		if err := markParkedPieceComplete(ctx, db, state.ID); err != nil {
			return storachaImportResult{}, err
		}
	}

	return storachaImportResult{Imported: true, PieceCIDV2: strings.TrimSuffix(filepath.Base(stagingPath), ".car")}, nil
}

// recoverFinalStorachaPieces scans incomplete storacha-migration rows where the
// DB/ref side already exists. For each row it checks whether the file has
// already reached its final target/piece/s-t00-<id> path. If yes, it finishes
// declaration and complete=true. If no final file exists, it leaves the row
// alone; there is nothing safe to do without either the staged file or the final
// file.
func recoverFinalStorachaPieces(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, targetPath string, out *importPiecesOutput, batchSize int) error {
	if out.Count >= batchSize {
		return nil
	}

	var rows []storachaFinalRecoveryRow
	err := db.Select(ctx, &rows, `
		SELECT pp.id, pp.piece_cid, pp.piece_padded_size, pp.piece_raw_size
		FROM parked_pieces pp
		WHERE pp.complete = FALSE
		  AND pp.skip = TRUE
		  AND pp.cleanup_task_id IS NULL
		  AND EXISTS (
		      SELECT 1
		      FROM parked_piece_refs ppr
		      WHERE ppr.piece_id = pp.id
		        AND ppr.data_url = $1
		        AND ppr.long_term = TRUE
		  )
		ORDER BY pp.id
		LIMIT $2`, storachaMigrationDataURL, batchSize-out.Count)
	if err != nil {
		return xerrors.Errorf("query final storacha pieces: %w", err)
	}

	for _, row := range rows {
		if out.Count >= batchSize {
			break
		}

		imported, err := recoverFinalStorachaPiece(ctx, db, si, storageID, targetPath, row)
		if err != nil {
			return err
		}
		if imported {
			pcidV2, err := storachaPieceCIDV2FromRow(row)
			if err != nil {
				return err
			}
			out.Pieces = append(out.Pieces, pcidV2)
			out.Count++
		}
	}

	return nil
}

func storachaPieceCIDV2FromRow(row storachaFinalRecoveryRow) (string, error) {
	pcidV1, err := cid.Parse(row.PieceCID)
	if err != nil {
		return "", xerrors.Errorf("parsing piece cid %s: %w", row.PieceCID, err)
	}

	pcidV2, err := commcid.PieceCidV2FromV1(pcidV1, uint64(row.RawSize))
	if err != nil {
		return "", xerrors.Errorf("getting piece cid v2: %w", err)
	}

	return pcidV2.String(), nil
}

// recoverFinalStorachaPiece handles the post-rename crash window for one DB row.
// It requires target/piece/s-t00-<id> to exist, rebuilds the piece info from the
// DB row, reuses the same claim/ref checks as staged import, then declares the
// final file and marks the parked piece complete. It returns false when the
// final file is missing or a live task still owns the parked_piece.
func recoverFinalStorachaPiece(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, targetPath string, row storachaFinalRecoveryRow) (bool, error) {
	finalPath := storachaFinalPiecePath(targetPath, row.ID)
	info, err := os.Stat(finalPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, xerrors.Errorf("checking final piece file: %w", err)
	}
	if info.IsDir() {
		return false, xerrors.Errorf("final piece path is a directory: %s", finalPath)
	}

	pcid, err := cid.Parse(row.PieceCID)
	if err != nil {
		return false, xerrors.Errorf("parsing piece cid %s: %w", row.PieceCID, err)
	}
	pi := &mk20.PieceInfo{
		PieceCIDV1: pcid,
		Size:       abi.PaddedPieceSize(row.PaddedSize),
		RawSize:    uint64(row.RawSize),
	}

	var state storachaParkedPieceState
	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		state, err = claimExistingStorachaPieceTx(tx, row.ID)
		if err != nil {
			return false, err
		}
		if !state.UseStagedFile {
			return true, nil
		}
		if err := ensureStorachaRefsTx(tx, row.ID, pi); err != nil {
			return false, err
		}
		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("claim final storacha piece: %w", err)
	}
	if !comm {
		return false, xerrors.Errorf("failed to commit transaction")
	}
	if !state.UseStagedFile {
		return false, nil
	}

	if err := declareStorachaPiece(ctx, si, storageID, row.ID); err != nil {
		return false, err
	}
	if !state.AlreadyComplete {
		if err := markParkedPieceComplete(ctx, db, row.ID); err != nil {
			return false, err
		}
	}

	return true, nil
}

// claimExistingStorachaPieceTx locks and classifies an existing parked_pieces
// row. Complete rows return AlreadyComplete=true and allow this migration to
// consume the staged file. Incomplete rows with a live harmony task return
// UseStagedFile=false. Incomplete rows without a live task are claimed by
// clearing task_id and setting skip=true, then this migration can finish them.
func claimExistingStorachaPieceTx(tx *harmonydb.Tx, parkedPieceID int64) (storachaParkedPieceState, error) {
	var complete bool
	var taskID sql.NullInt64
	var taskExists bool
	err := tx.QueryRow(`
		SELECT pp.complete, pp.task_id, ht.id IS NOT NULL AS task_exists
		FROM parked_pieces pp
		LEFT JOIN harmony_task ht ON ht.id = pp.task_id
		WHERE pp.id = $1
		FOR UPDATE OF pp`, parkedPieceID).Scan(&complete, &taskID, &taskExists)
	if err != nil {
		return storachaParkedPieceState{}, xerrors.Errorf("query parked piece: %w", err)
	}

	state := storachaParkedPieceState{
		ID:              parkedPieceID,
		AlreadyComplete: complete,
	}
	if complete {
		state.UseStagedFile = true
		return state, nil
	}
	if taskID.Valid && taskExists {
		return state, nil
	}

	n, err := tx.Exec(`
		UPDATE parked_pieces
		SET task_id = NULL, skip = TRUE
		WHERE id = $1
		  AND complete = FALSE
		  AND (task_id IS NULL OR task_id = $2)`, parkedPieceID, taskID.Int64)
	if err != nil {
		return storachaParkedPieceState{}, xerrors.Errorf("claim parked piece: %w", err)
	}
	if n != 1 {
		return storachaParkedPieceState{}, xerrors.Errorf("claim parked piece: expected 1, got %d", n)
	}

	state.UseStagedFile = true
	return state, nil
}

// ensureStorachaRefsTx enforces the intended one-to-one shape for this import.
// It locks storacha-migration parked_piece_refs for this parked piece, fails if
// more than one exists, inserts the missing ref if needed, then does the same
// for pdp_piecerefs through the unique piece_ref relation. Existing PDP refs
// must already point at the same service and piece CID.
func ensureStorachaRefsTx(tx *harmonydb.Tx, parkedPieceID int64, pi *mk20.PieceInfo) error {
	var refs []struct {
		RefID int64 `db:"ref_id"`
	}
	err := tx.Select(&refs, `
		SELECT ref_id
		FROM parked_piece_refs
		WHERE piece_id = $1
		  AND data_url = $2
		  AND long_term = TRUE
		ORDER BY ref_id
		FOR UPDATE`, parkedPieceID, storachaMigrationDataURL)
	if err != nil {
		return xerrors.Errorf("query storacha parked piece refs: %w", err)
	}
	if len(refs) > 1 {
		return xerrors.Errorf("storacha parked piece refs: expected at most 1 for parked piece %d, got %d", parkedPieceID, len(refs))
	}

	var pieceRef int64
	if len(refs) == 1 {
		pieceRef = refs[0].RefID
	} else {
		err = tx.QueryRow(`
			INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
			VALUES ($1, $2, TRUE)
			RETURNING ref_id`, parkedPieceID, storachaMigrationDataURL).Scan(&pieceRef)
		if err != nil {
			return xerrors.Errorf("insert storacha parked piece ref: %w", err)
		}
	}

	var pdpRefs []struct {
		ID       int64  `db:"id"`
		Service  string `db:"service"`
		PieceCID string `db:"piece_cid"`
	}
	err = tx.Select(&pdpRefs, `
		SELECT id, service, piece_cid
		FROM pdp_piecerefs
		WHERE piece_ref = $1
		FOR UPDATE`, pieceRef)
	if err != nil {
		return xerrors.Errorf("query pdp piece refs: %w", err)
	}
	if len(pdpRefs) > 1 {
		return xerrors.Errorf("pdp piece refs: expected at most 1 for parked piece ref %d, got %d", pieceRef, len(pdpRefs))
	}
	if len(pdpRefs) == 1 {
		if pdpRefs[0].Service != serviceName || pdpRefs[0].PieceCID != pi.PieceCIDV1.String() {
			return xerrors.Errorf("pdp piece ref %d does not match storacha migration piece %s", pdpRefs[0].ID, pi.PieceCIDV1.String())
		}
		return nil
	}

	needsSaveCache := padreader.PaddedSize(pi.RawSize).Padded() >= minSaveCacheSize
	n, err := tx.Exec(`
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		VALUES ($1, $2, $3, NOW(), $4)`, serviceName, pi.PieceCIDV1.String(), pieceRef, needsSaveCache)
	if err != nil {
		return xerrors.Errorf("insert pdp piece ref: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert pdp piece ref: expected 1, got %d", n)
	}

	return nil
}

// storachaFinalPiecePath mirrors Curio's piece-park sector naming: parked piece
// id N is stored as miner-zero piece sector s-t00-N under target/piece.
func storachaFinalPiecePath(targetPath string, parkedPieceID int64) string {
	pieceNumber := storiface.PieceNumber(parkedPieceID)
	return filepath.Join(targetPath, "piece", storiface.SectorName(pieceNumber.Ref().ID))
}

// declareStorachaPiece records the final piece file in sector_location. The
// storage index upsert is idempotent, so recovery can call this again after a
// crash between rename and complete=true.
func declareStorachaPiece(ctx context.Context, si paths.SectorIndex, storageID storiface.ID, parkedPieceID int64) error {
	pieceNumber := storiface.PieceNumber(parkedPieceID)
	err := si.StorageDeclareSector(ctx, storageID, pieceNumber.Ref().ID, storiface.FTPiece, true)
	if err != nil {
		return xerrors.Errorf("declaring storacha piece: %w", err)
	}
	return nil
}

// markParkedPieceComplete is intentionally the last state transition. Once this
// is true, ParkPieceTask will not try to download/write the piece, so the final
// file and sector_location declaration must already be in place.
func markParkedPieceComplete(ctx context.Context, db *harmonydb.DB, parkedPieceID int64) error {
	n, err := db.Exec(ctx, `UPDATE parked_pieces SET complete = TRUE WHERE id = $1`, parkedPieceID)
	if err != nil {
		return xerrors.Errorf("updating parked pieces: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("updating parked pieces: expected 1, got %d", n)
	}
	return nil
}
