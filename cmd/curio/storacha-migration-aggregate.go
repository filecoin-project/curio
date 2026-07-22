package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-data-segment/datasegment"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/commcidv2"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

const storachaAggregateDefaultPieceSize = abi.PaddedPieceSize(1 << 30)

// storachaAggregatePieceSize is a variable only so tests can exercise the
// recovery paths without writing 1 GiB aggregate files. Production uses the
// default 1 GiB padded aggregate size.
var storachaAggregatePieceSize = storachaAggregateDefaultPieceSize

// aggregate-pieces is intentionally a file-backed pipeline, not a task table.
//
// The input is treated as read-only and is identified by its SHA256. That hash
// gives every distinct input file a distinct work directory, so changing the
// input never silently resumes with stale bucket/group data.
//
// The durable stages are:
//  1. raw buckets: stream the input, hydrate parked_piece ids from the DB, and
//     append full records to padded-size bucket files with a cursor for resume.
//  2. deduped buckets: use the OS sort command per bucket, then drop adjacent
//     duplicate PieceCIDv2 records.
//  3. groups.jsonl: write one atomic grouping file. This is the replay plan; a
//     partial regroup would change aggregate membership and is not resumable.
//  4. complete/*.json: process each group into a real aggregate file and write
//     a per-group commit marker after the piece is imported.
//
// Completed JSONL stage files are validated by size, record count, and SHA256
// before they are trusted on retry. Aggregate body files use a different
// boundary: temp files are uncommitted scratch and can be overwritten, staged
// files are trusted only after staged/<PieceCIDv2> exists, and final files are
// trusted from their DB row plus final path/size. After the staged marker exists
// we do not run CommP again; a rename or failed fsync can lose a directory entry
// but cannot change already-written bytes into another valid file.
type aggregatePiecesOutput struct {
	Error      string       `json:"error,omitempty"`
	Aggregates []aggregates `json:"aggregates"`
}

// aggregates is the public result contract for this command. It reports only
// aggregates that reached a completion marker, so callers can safely consume a
// partial result even when the command exits with an error.
type aggregates struct {
	PieceCid  cid.Cid   `json:"piece_cid"`
	SubPieces []cid.Cid `json:"sub_pieces"`
	Count     int       `json:"count"`
}

// pieceDetails is the transient hydrated form of one input line. The JSONL
// bucket records below are the durable form; this type exists only while a DB
// hydration batch is in memory.
type pieceDetails struct {
	PieceCidV2 cid.Cid
	PieceCID   cid.Cid             `db:"piece_cid"`
	PaddedSize abi.PaddedPieceSize `db:"piece_padded_size"`
	RawSize    uint64
	ID         int64 `db:"id"`
}

const (
	storachaAggregateManifestVersion = 1
	storachaAggregateHydrateBatch    = 5000
	storachaAggregateGroupIDWidth    = 20

	storachaAggregateWorkDir        = "storacha-aggregate-work"
	storachaAggregateManifestFile   = "manifest.json"
	storachaAggregateBucketCursor   = "bucket-cursor.json"
	storachaAggregateRawBucketDir   = "buckets.raw"
	storachaAggregateDedupeDir      = "buckets"
	storachaAggregateGroupsFile     = "groups.jsonl"
	storachaAggregateCompleteDir    = "complete"
	storachaAggregateTempDir        = "aggregate-tmp"
	storachaAggregateStagingDirName = "storacha-aggregate-staging"
	storachaAggregateStagedDirName  = "staged"
)

// storachaBucketRecord is the durable "full hydrated record" for one input
// piece. PieceCIDV2 is deliberately the first JSON field: OS sort compares
// whole JSONL lines, so putting the dedupe key first makes equal PieceCIDv2
// records adjacent without a custom sorter or an in-memory seen set.
type storachaBucketRecord struct {
	PieceCIDV2 string `json:"piece_cid_v2"`
	PieceID    int64  `json:"piece_id"`
	PieceCIDV1 string `json:"piece_cid_v1"`
	RawSize    uint64 `json:"raw_size"`
	PaddedSize uint64 `json:"padded_size"`
}

// storachaAggregateGroup is the single atomic grouping artifact. Once this
// file exists, aggregate creation is just replaying deterministic work. That is
// important because a partial regrouping would change which subpieces belong to
// which aggregate and make resume semantics ambiguous.
type storachaAggregateGroup struct {
	GroupID    int64                  `json:"group_id"`
	PieceCIDV1 string                 `json:"piece_cid_v1"`
	PaddedSize uint64                 `json:"padded_size"`
	Pieces     []storachaBucketRecord `json:"pieces"`
}

// storachaAggregateCompletion is a per-group commit marker. The aggregate file
// and DB rows may be recoverable after a crash, but this marker is the cheap
// local proof that this exact group was already imported and can be reported to
// the caller.
type storachaAggregateCompletion struct {
	GroupID    int64    `json:"group_id"`
	PieceCID   string   `json:"piece_cid"`
	SubPieces  []string `json:"sub_pieces"`
	Count      int      `json:"count"`
	PieceCIDV1 string   `json:"piece_cid_v1"`
	RawSize    uint64   `json:"raw_size"`
	PaddedSize uint64   `json:"padded_size"`
}

// storachaAggregateWorkManifest is the durable state machine for a single
// input hash. A stage is marked complete only after its files are fully written,
// synced, renamed where needed, and checksummed.
type storachaAggregateWorkManifest struct {
	Version     int    `json:"version"`
	InputSHA256 string `json:"input_sha256"`
	InputSize   int64  `json:"input_size"`

	RawBuckets     storachaAggregateStageManifest `json:"raw_buckets"`
	DedupedBuckets storachaAggregateStageManifest `json:"deduped_buckets"`
	Groups         storachaAggregateGroupManifest `json:"groups"`
}

// storachaAggregateStageManifest describes append-only JSONL stage artifacts.
// It lets a retry trust a completed stage without rescanning all earlier input.
type storachaAggregateStageManifest struct {
	Complete bool                         `json:"complete"`
	Records  int64                        `json:"records"`
	Files    []storachaAggregateFileState `json:"files"`
}

// storachaAggregateGroupManifest records the single grouping artifact. The
// piece count and group count are checked again before processing.
type storachaAggregateGroupManifest struct {
	Complete bool                       `json:"complete"`
	Pieces   int64                      `json:"pieces"`
	Groups   int64                      `json:"groups"`
	File     storachaAggregateFileState `json:"file"`
}

// storachaAggregateFileState is deliberately simple metadata: it catches torn
// JSONL records, external edits, and stale artifacts without needing a DB table.
type storachaAggregateFileState struct {
	Name    string `json:"name"`
	Records int64  `json:"records"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

// storachaAggregateBucketCursorState is used only while raw bucketing is
// incomplete. The cursor is written after bucket files and required raw-bucket
// directory entries are synced, so on retry any bytes past these offsets are
// known-uncommitted tails and can be truncated.
type storachaAggregateBucketCursorState struct {
	InputSHA256   string           `json:"input_sha256"`
	InputOffset   int64            `json:"input_offset"`
	InputLine     int64            `json:"input_line"`
	Records       int64            `json:"records"`
	BucketOffsets map[string]int64 `json:"bucket_offsets"`
}

var aggregatePiecesCmd = &cli.Command{
	Name:  "aggregate-pieces",
	Usage: "Aggregates already existing pieces from storage into PODSI and park them",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "source",
			Usage: "path to read the pieces from",
		},
		&cli.StringFlag{
			Name:  "target",
			Usage: "path to storage directory in Curio's attached permanent storage",
		},
		&cli.StringFlag{
			Name:  "result",
			Usage: "path to write the JSON result",
		},
		&cli.StringFlag{
			Name:  "input",
			Usage: "path to read the CIDs from",
		},
	},
	Action: func(cctx *cli.Context) (err error) {
		ctx := cctx.Context
		var workDir string

		if cctx.String("result") == "" {
			return fmt.Errorf("result is required")
		}

		resultPath, err := homedir.Expand(cctx.String("result"))
		if err != nil {
			return xerrors.Errorf("expanding result path: %w", err)
		}
		resultPath, err = filepath.Abs(filepath.Clean(resultPath))
		if err != nil {
			return xerrors.Errorf("resolving result path: %w", err)
		}

		defer func() {
			// Always write the result file, including errors. This gives the
			// caller the list of aggregates that are already committed and a
			// machine-readable reason why the run stopped.
			writeErr := writeAggregatePiecesResultFromWork(resultPath, workDir, err)
			if writeErr != nil {
				err = errors.Join(err, writeErr)
			}
		}()

		if cctx.String("source") == "" || cctx.String("target") == "" || cctx.String("input") == "" {
			return fmt.Errorf("source, target and input are required")
		}

		inputPath, err := homedir.Expand(cctx.String("input"))
		if err != nil {
			return xerrors.Errorf("expanding input path: %w", err)
		}
		inputPath, err = filepath.Abs(filepath.Clean(inputPath))
		if err != nil {
			return xerrors.Errorf("expanding input path: %w", err)
		}

		// Dedupe is intentionally delegated to the system sort implementation so
		// the command can handle tens of millions of PieceCIDv2 records without
		// building a process-wide seen set.
		sortPath, err := exec.LookPath("sort")
		if err != nil {
			return xerrors.Errorf("aggregate-pieces requires OS sort in PATH: %w", err)
		}

		sourcePath, err := homedir.Expand(cctx.String("source"))
		if err != nil {
			return xerrors.Errorf("expanding source path: %w", err)
		}
		sourcePath, err = filepath.Abs(filepath.Clean(sourcePath))
		if err != nil {
			return xerrors.Errorf("resolving source path: %w", err)
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

		workDir, err = runAggregatePieces(ctx, db, si, storageID, sortPath, inputPath, sourcePath, targetPath)
		if err != nil {
			return err
		}

		return nil
	},
}

func runAggregatePieces(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, sortPath, inputPath, sourcePath, targetPath string) (string, error) {
	// The input hash is part of the workdir path. If the user edits/replaces the
	// input, the command starts a new deterministic pipeline instead of trying to
	// reuse grouping state that was computed for different contents.
	inputSHA256, inputSize, err := hashFileSHA256(inputPath)
	if err != nil {
		return "", xerrors.Errorf("hashing input file: %w", err)
	}

	workDir := filepath.Join(targetPath, storachaAggregateWorkDir, inputSHA256)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return workDir, xerrors.Errorf("creating aggregate work directory: %w", err)
	}

	manifest, err := loadStorachaAggregateManifest(workDir, inputSHA256, inputSize)
	if err != nil {
		return workDir, err
	}

	// A completed stage is trusted only after its checksum/size/line-count still
	// matches the manifest. If any completed artifact changed, the safest answer
	// is to stop and require manual cleanup of this hash-specific workdir.
	if err := validateCompletedStorachaAggregateStages(workDir, manifest); err != nil {
		return workDir, err
	}

	// The manifest is monotonic: raw buckets must complete before dedupe, and
	// dedupe must complete before groups. The validation above checked the
	// deepest completed stage we are resuming from. Each builder below only marks
	// its stage complete after durable output is written and recorded, so the
	// remaining control flow can stay as a simple forward pipeline.
	if !manifest.RawBuckets.Complete {
		if err := buildRawStorachaAggregateBuckets(ctx, db, inputPath, inputSHA256, workDir, manifest); err != nil {
			return workDir, err
		}
	}

	if !manifest.DedupedBuckets.Complete {
		if err := dedupeStorachaAggregateBuckets(ctx, sortPath, workDir, manifest); err != nil {
			return workDir, err
		}
	}

	if !manifest.Groups.Complete {
		if err := buildStorachaAggregateGroups(workDir, manifest); err != nil {
			return workDir, err
		}
	}

	if err := processStorachaAggregateGroups(ctx, db, si, storageID, sourcePath, targetPath, workDir, manifest); err != nil {
		return workDir, err
	}

	return workDir, nil
}

func hydrateStorachaSubPieces(ctx context.Context, db *harmonydb.DB, subPieces []pieceDetails) error {
	for i := 0; i < len(subPieces); i += storachaAggregateHydrateBatch {
		batchEnd := i + storachaAggregateHydrateBatch
		if batchEnd > len(subPieces) {
			batchEnd = len(subPieces)
		}

		batch := subPieces[i:batchEnd]
		pcids := make([]string, 0, len(batch))
		psizes := make([]int64, 0, len(batch))
		for _, piece := range batch {
			pcids = append(pcids, piece.PieceCID.String())
			psizes = append(psizes, int64(piece.PaddedSize))
		}

		var dbPieces []struct {
			Ord int64 `db:"ord"`
			ID  int64 `db:"id"`
		}
		// WITH ORDINALITY gives each input tuple a 1-based position inside this
		// batch. That ordinal is not a DB identity; it is only the "slot number"
		// of the element produced by unnest().
		//
		// We need it because the input is allowed to contain duplicates. A plain
		// join on piece CID/size would return the same parked_pieces row for each
		// duplicate, but without the ordinal we could not safely write the id back
		// to the matching slice element. The ordinal keeps hydration positional:
		// input slot N gets the first matching complete parked piece for slot N.
		err := db.Select(ctx, &dbPieces, `
			WITH input AS (
				SELECT u.piece_cid, u.piece_padded_size, u.ord
				FROM unnest($1::text[], $2::bigint[]) WITH ORDINALITY AS u(piece_cid, piece_padded_size, ord)
			)
			SELECT i.ord, pp.id
			FROM input i
			JOIN parked_pieces pp
			  ON pp.piece_cid = i.piece_cid
			 AND pp.piece_padded_size = i.piece_padded_size
			 AND pp.complete = TRUE
			 AND pp.long_term = TRUE
			 AND pp.cleanup_task_id IS NULL
			ORDER BY i.ord, pp.id`, pcids, psizes)
		if err != nil {
			return xerrors.Errorf("querying parked_pieces: %w", err)
		}

		for _, row := range dbPieces {
			if row.Ord < 1 || int(row.Ord) > len(batch) {
				return xerrors.Errorf("querying parked_pieces: invalid input ordinal %d", row.Ord)
			}
			idx := i + int(row.Ord) - 1
			if subPieces[idx].ID == 0 {
				subPieces[idx].ID = row.ID
			}
		}

		for j := i; j < batchEnd; j++ {
			if subPieces[j].ID == 0 {
				return xerrors.Errorf("piece %s is not a complete active parked piece", subPieces[j].PieceCidV2)
			}
		}
	}

	return nil
}

func hashFileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, xerrors.Errorf("opening %s: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	h := sha256.New()
	n, err := io.Copy(h, file)
	if err != nil {
		return "", 0, xerrors.Errorf("reading %s: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func loadStorachaAggregateManifest(workDir, inputSHA256 string, inputSize int64) (*storachaAggregateWorkManifest, error) {
	path := filepath.Join(workDir, storachaAggregateManifestFile)
	mb, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, xerrors.Errorf("reading aggregate manifest: %w", err)
		}

		manifest := &storachaAggregateWorkManifest{
			Version:     storachaAggregateManifestVersion,
			InputSHA256: inputSHA256,
			InputSize:   inputSize,
		}
		if err := writeStorachaAggregateManifest(workDir, manifest); err != nil {
			return nil, err
		}
		return manifest, nil
	}

	var manifest storachaAggregateWorkManifest
	if err := json.Unmarshal(mb, &manifest); err != nil {
		return nil, xerrors.Errorf("unmarshalling aggregate manifest: %w", err)
	}
	if manifest.Version != storachaAggregateManifestVersion {
		return nil, xerrors.Errorf("aggregate manifest version mismatch: expected %d, got %d; cleanup %s manually before retrying",
			storachaAggregateManifestVersion, manifest.Version, workDir)
	}
	// The workdir name already includes the SHA, but the manifest repeats both
	// the SHA and byte length. If either differs, something outside this command
	// moved or edited state and automatic recovery would be unsafe.
	if manifest.InputSHA256 != inputSHA256 || manifest.InputSize != inputSize {
		return nil, xerrors.Errorf("aggregate manifest input mismatch in %s; cleanup manually before retrying", workDir)
	}

	return &manifest, nil
}

func writeStorachaAggregateManifest(workDir string, manifest *storachaAggregateWorkManifest) error {
	if err := writeJSONFileAtomic(filepath.Join(workDir, storachaAggregateManifestFile), manifest); err != nil {
		return xerrors.Errorf("writing aggregate manifest: %w", err)
	}
	return nil
}

func validateCompletedStorachaAggregateStages(workDir string, manifest *storachaAggregateWorkManifest) error {
	if manifest.DedupedBuckets.Complete && !manifest.RawBuckets.Complete {
		return xerrors.Errorf("aggregate manifest has dedupe complete before raw buckets; cleanup %s manually before retrying", workDir)
	}
	if manifest.Groups.Complete && !manifest.DedupedBuckets.Complete {
		return xerrors.Errorf("aggregate manifest has groups complete before dedupe; cleanup %s manually before retrying", workDir)
	}

	// Validate only the deepest completed stage. Each stage is derived entirely
	// from the previous one, so once groups.jsonl is complete we only need to
	// trust that replay plan; earlier buckets are no longer read.
	switch {
	case manifest.Groups.Complete:
		return validateStorachaAggregateGroupFile(workDir, manifest)
	case manifest.DedupedBuckets.Complete:
		return validateAggregateFileStates(workDir, manifest.DedupedBuckets.Files)
	case manifest.RawBuckets.Complete:
		return validateAggregateFileStates(workDir, manifest.RawBuckets.Files)
	default:
		return nil
	}
}

func validateStorachaAggregateGroupFile(workDir string, manifest *storachaAggregateWorkManifest) error {
	if !manifest.Groups.Complete {
		return nil
	}

	if err := validateAggregateFileState(workDir, manifest.Groups.File); err != nil {
		return err
	}
	if manifest.Groups.File.Records != manifest.Groups.Groups {
		return xerrors.Errorf("aggregate group manifest mismatch: file has %d groups, manifest has %d; cleanup %s manually before retrying",
			manifest.Groups.File.Records, manifest.Groups.Groups, workDir)
	}

	groupsPath := filepath.Join(workDir, storachaAggregateGroupsFile)
	file, err := os.Open(groupsPath)
	if err != nil {
		return xerrors.Errorf("opening aggregate group file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	reader := bufio.NewReaderSize(file, 1024*1024)
	var groups int64
	var pieces int64
	for {
		line, ok, err := readJSONLLine(reader)
		if err != nil {
			return xerrors.Errorf("reading aggregate group file: %w", err)
		}
		if !ok {
			break
		}

		var group storachaAggregateGroup
		if err := json.Unmarshal(line, &group); err != nil {
			return xerrors.Errorf("decoding aggregate group %d: %w", groups, err)
		}
		if group.GroupID != groups {
			return xerrors.Errorf("aggregate group id mismatch: expected %d, got %d; cleanup %s manually before retrying", groups, group.GroupID, workDir)
		}
		if err := validateStorachaAggregateGroup(group); err != nil {
			return xerrors.Errorf("validating aggregate group %d: %w", group.GroupID, err)
		}

		groups++
		pieces += int64(len(group.Pieces))
	}

	if groups != manifest.Groups.Groups {
		return xerrors.Errorf("aggregate group count mismatch: file has %d, manifest has %d; cleanup %s manually before retrying",
			groups, manifest.Groups.Groups, workDir)
	}
	if pieces != manifest.Groups.Pieces {
		return xerrors.Errorf("aggregate group piece count mismatch: file has %d, manifest has %d; cleanup %s manually before retrying",
			pieces, manifest.Groups.Pieces, workDir)
	}

	return nil
}

func validateAggregateFileStates(workDir string, states []storachaAggregateFileState) error {
	for _, expected := range states {
		if err := validateAggregateFileState(workDir, expected); err != nil {
			return err
		}
	}
	return nil
}

func validateAggregateFileState(workDir string, expected storachaAggregateFileState) error {
	actual, err := computeAggregateFileState(workDir, expected.Name)
	if err != nil {
		return err
	}
	if actual.Records != expected.Records || actual.Bytes != expected.Bytes || actual.SHA256 != expected.SHA256 {
		return xerrors.Errorf("aggregate stage file %s changed; cleanup %s manually before retrying", expected.Name, workDir)
	}
	return nil
}

func computeAggregateFileState(workDir, relName string) (storachaAggregateFileState, error) {
	path := filepath.Join(workDir, filepath.FromSlash(relName))
	file, err := os.Open(path)
	if err != nil {
		return storachaAggregateFileState{}, xerrors.Errorf("opening aggregate stage file %s: %w", relName, err)
	}
	defer func() {
		_ = file.Close()
	}()

	info, err := file.Stat()
	if err != nil {
		return storachaAggregateFileState{}, xerrors.Errorf("checking aggregate stage file %s: %w", relName, err)
	}
	if info.IsDir() {
		return storachaAggregateFileState{}, xerrors.Errorf("aggregate stage file is a directory: %s", relName)
	}

	h := sha256.New()
	reader := bufio.NewReaderSize(file, 1024*1024)
	var records int64
	var bytesRead int64
	// Stage artifacts are JSONL and every committed record ends in '\n'. A file
	// ending in a partial line is considered torn and is never recorded as a
	// completed stage.
	for {
		line, ok, err := readJSONLLine(reader)
		if err != nil {
			return storachaAggregateFileState{}, xerrors.Errorf("reading aggregate stage file %s: %w", relName, err)
		}
		if !ok {
			break
		}
		if _, err := h.Write(line); err != nil {
			return storachaAggregateFileState{}, xerrors.Errorf("hashing aggregate stage file %s: %w", relName, err)
		}
		records++
		bytesRead += int64(len(line))
	}

	if bytesRead != info.Size() {
		return storachaAggregateFileState{}, xerrors.Errorf("aggregate stage file %s changed while reading: stat=%d read=%d", relName, info.Size(), bytesRead)
	}

	return storachaAggregateFileState{
		Name:    filepath.ToSlash(relName),
		Records: records,
		Bytes:   bytesRead,
		SHA256:  hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func readJSONLLine(reader *bufio.Reader) ([]byte, bool, error) {
	line, err := reader.ReadBytes('\n')
	if err == nil {
		return line, true, nil
	}
	if errors.Is(err, io.EOF) {
		if len(line) == 0 {
			return nil, false, nil
		}
		// EOF after bytes but before '\n' means a writer died mid-record.
		// Callers either truncate back to a cursor or reject the completed stage.
		return nil, false, xerrors.Errorf("torn JSONL record without trailing newline")
	}
	return nil, false, err
}

func buildRawStorachaAggregateBuckets(ctx context.Context, db *harmonydb.DB, inputPath, inputSHA256, workDir string, manifest *storachaAggregateWorkManifest) error {
	rawDir := filepath.Join(workDir, storachaAggregateRawBucketDir)
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		return xerrors.Errorf("creating raw bucket directory: %w", err)
	}

	// Raw bucketing is the only non-atomic stage because the input can be huge.
	// The cursor gives us a two-file protocol: bucket files are fsynced first,
	// newly referenced bucket directory entries are fsynced next, then the cursor
	// is atomically replaced with the new input/bucket offsets. On retry we seek
	// the input to the cursor and truncate buckets to the last committed offsets
	// before appending more records.
	cursor, err := loadStorachaAggregateBucketCursor(workDir, inputSHA256)
	if err != nil {
		return err
	}
	if err := truncateStorachaAggregateRawBuckets(rawDir, cursor); err != nil {
		return err
	}

	input, err := os.Open(inputPath)
	if err != nil {
		return xerrors.Errorf("opening aggregate input: %w", err)
	}
	defer func() {
		_ = input.Close()
	}()

	if _, err := input.Seek(cursor.InputOffset, io.SeekStart); err != nil {
		return xerrors.Errorf("seeking aggregate input to cursor offset %d: %w", cursor.InputOffset, err)
	}

	reader := bufio.NewReaderSize(input, 1024*1024)
	batch := make([]pieceDetails, 0, storachaAggregateHydrateBatch)
	offset := cursor.InputOffset
	lineNumber := cursor.InputLine
	records := cursor.Records

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		// Hydrate only a bounded batch into memory. The durable bucket record
		// contains the parked_piece id, both CID forms, raw size, and padded size
		// so later stages do not have to ask the DB again for subpiece metadata.
		if err := hydrateStorachaSubPieces(ctx, db, batch); err != nil {
			return err
		}
		if err := appendStorachaAggregateRawBucketBatch(rawDir, cursor, batch); err != nil {
			return err
		}

		// Cursor update is the commit point for this batch. If the process dies
		// before this write, the next run truncates any bucket bytes from the
		// failed attempt and re-reads these input lines.
		records += int64(len(batch))
		cursor.InputOffset = offset
		cursor.InputLine = lineNumber
		cursor.Records = records
		if err := writeStorachaAggregateBucketCursor(workDir, cursor); err != nil {
			return err
		}

		batch = batch[:0]
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return xerrors.Errorf("reading aggregate input line %d: %w", lineNumber+1, readErr)
		}

		offset += int64(len(line))
		lineNumber++

		piece, err := parseStorachaAggregateInputLine(line, lineNumber)
		if err != nil {
			return err
		}
		batch = append(batch, piece)

		if len(batch) == storachaAggregateHydrateBatch {
			if err := flushBatch(); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}

	if err := flushBatch(); err != nil {
		return err
	}
	if records == 0 {
		return fmt.Errorf("input has no pieces")
	}

	files, err := finishStorachaAggregateRawBuckets(workDir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return xerrors.Errorf("aggregate raw bucketing produced no files")
	}

	manifest.RawBuckets = storachaAggregateStageManifest{
		Complete: true,
		Records:  records,
		Files:    files,
	}
	if err := writeStorachaAggregateManifest(workDir, manifest); err != nil {
		return err
	}

	return nil
}

func loadStorachaAggregateBucketCursor(workDir, inputSHA256 string) (*storachaAggregateBucketCursorState, error) {
	path := filepath.Join(workDir, storachaAggregateBucketCursor)
	mb, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, xerrors.Errorf("reading aggregate bucket cursor: %w", err)
		}
		return &storachaAggregateBucketCursorState{
			InputSHA256:   inputSHA256,
			BucketOffsets: map[string]int64{},
		}, nil
	}

	var cursor storachaAggregateBucketCursorState
	if err := json.Unmarshal(mb, &cursor); err != nil {
		return nil, xerrors.Errorf("unmarshalling aggregate bucket cursor: %w", err)
	}
	if cursor.InputSHA256 != inputSHA256 {
		return nil, xerrors.Errorf("aggregate bucket cursor input mismatch in %s; cleanup manually before retrying", workDir)
	}
	if cursor.BucketOffsets == nil {
		cursor.BucketOffsets = map[string]int64{}
	}
	return &cursor, nil
}

func writeStorachaAggregateBucketCursor(workDir string, cursor *storachaAggregateBucketCursorState) error {
	if err := writeJSONFileAtomic(filepath.Join(workDir, storachaAggregateBucketCursor), cursor); err != nil {
		return xerrors.Errorf("writing aggregate bucket cursor: %w", err)
	}
	return nil
}

func truncateStorachaAggregateRawBuckets(rawDir string, cursor *storachaAggregateBucketCursorState) error {
	for _, size := range storachaAggregateBucketSizesAscending() {
		name := storachaAggregateBucketFileName(size)
		path := filepath.Join(rawDir, name)
		offset := cursor.BucketOffsets[name]

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				if offset != 0 {
					return xerrors.Errorf("aggregate raw bucket %s is missing but cursor expects %d bytes; cleanup manually before retrying", path, offset)
				}
				continue
			}
			return xerrors.Errorf("checking aggregate raw bucket %s: %w", path, err)
		}
		if info.IsDir() {
			return xerrors.Errorf("aggregate raw bucket is a directory: %s", path)
		}
		if offset > info.Size() {
			return xerrors.Errorf("aggregate raw bucket %s is shorter than cursor offset %d; cleanup manually before retrying", path, offset)
		}
		if info.Size() != offset {
			// The cursor is written only after bucket writes have been flushed and
			// synced. Any bytes beyond the cursor are an uncommitted tail from a
			// previous crash and must be rolled back before appending more JSONL.
			if err := os.Truncate(path, offset); err != nil {
				return xerrors.Errorf("truncating aggregate raw bucket %s to cursor offset %d: %w", path, offset, err)
			}
		}
	}

	return nil
}

func parseStorachaAggregateInputLine(line []byte, lineNumber int64) (pieceDetails, error) {
	text := strings.TrimSpace(string(line))
	if text == "" {
		return pieceDetails{}, xerrors.Errorf("aggregate input line %d is empty", lineNumber)
	}

	pcidV2, err := cid.Parse(text)
	if err != nil {
		return pieceDetails{}, xerrors.Errorf("parsing aggregate input line %d: %w", lineNumber, err)
	}
	if !commcidv2.IsPieceCidV2(pcidV2) {
		return pieceDetails{}, xerrors.Errorf("aggregate input line %d is not a PieceCIDv2: %s", lineNumber, pcidV2)
	}

	// PieceCIDv2 carries the raw payload size. We read raw from the CID, then
	// compute padded size from that raw value for parked_pieces lookup. We never
	// derive raw size from padded/unpadded size in the aggregate path.
	pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcidV2)
	if err != nil {
		return pieceDetails{}, xerrors.Errorf("decoding PieceCIDv2 on line %d: %w", lineNumber, err)
	}
	paddedSize := padreader.PaddedSize(rawSize).Padded()
	if err := paddedSize.Validate(); err != nil {
		return pieceDetails{}, xerrors.Errorf("invalid padded size for aggregate input line %d: %w", lineNumber, err)
	}
	if !storachaAggregateIsBucketSize(paddedSize) {
		return pieceDetails{}, xerrors.Errorf("aggregate input line %d padded size %d is outside the supported 128-byte to %d-byte bucket range", lineNumber, paddedSize, storachaAggregatePieceSize)
	}

	return pieceDetails{
		PieceCidV2: pcidV2,
		PieceCID:   pcidV1,
		RawSize:    rawSize,
		PaddedSize: paddedSize,
	}, nil
}

type storachaAggregateRawBucketWriter struct {
	file            *os.File
	writer          *bufio.Writer
	offset          int64
	needsRawDirSync bool
}

func appendStorachaAggregateRawBucketBatch(rawDir string, cursor *storachaAggregateBucketCursorState, batch []pieceDetails) error {
	// One batch may touch multiple padded-size buckets. We open only the buckets
	// needed for this batch, append JSONL, flush/fsync every touched file, and
	// sync the raw bucket directory when a bucket becomes newly referenced. Only
	// after those durability steps do we update cursor.BucketOffsets for the
	// caller to commit.
	writers := map[string]*storachaAggregateRawBucketWriter{}
	closeWriters := func() error {
		var err error
		for _, writer := range writers {
			if writer.file != nil {
				err = errors.Join(err, writer.file.Close())
			}
		}
		return err
	}
	defer func() {
		_ = closeWriters()
	}()

	openWriter := func(name string) (*storachaAggregateRawBucketWriter, error) {
		if writer, ok := writers[name]; ok {
			return writer, nil
		}

		path := filepath.Join(rawDir, name)
		created := false
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0644)
		if os.IsNotExist(err) {
			file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				created = true
			}
		}
		if err != nil {
			return nil, xerrors.Errorf("opening aggregate raw bucket %s: %w", path, err)
		}

		offset := cursor.BucketOffsets[name]
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, xerrors.Errorf("checking aggregate raw bucket %s: %w", path, err)
		}
		if info.Size() != offset {
			_ = file.Close()
			return nil, xerrors.Errorf("aggregate raw bucket %s has size %d, cursor expects %d; cleanup manually before retrying", path, info.Size(), offset)
		}

		writer := &storachaAggregateRawBucketWriter{
			file:            file,
			writer:          bufio.NewWriterSize(file, 1024*1024),
			offset:          offset,
			needsRawDirSync: created || offset == 0,
		}
		writers[name] = writer
		return writer, nil
	}

	for _, piece := range batch {
		record := storachaBucketRecord{
			PieceCIDV2: piece.PieceCidV2.String(),
			PieceID:    piece.ID,
			PieceCIDV1: piece.PieceCID.String(),
			RawSize:    piece.RawSize,
			PaddedSize: uint64(piece.PaddedSize),
		}

		name := storachaAggregateBucketFileName(piece.PaddedSize)
		writer, err := openWriter(name)
		if err != nil {
			return err
		}
		n, err := writeJSONLine(writer.writer, record)
		if err != nil {
			return xerrors.Errorf("writing aggregate raw bucket %s: %w", name, err)
		}
		writer.offset += int64(n)
	}

	newOffsets := map[string]int64{}
	needsRawDirSync := false
	for name, writer := range writers {
		if err := writer.writer.Flush(); err != nil {
			return xerrors.Errorf("flushing aggregate raw bucket %s: %w", name, err)
		}
		if err := writer.file.Sync(); err != nil {
			return xerrors.Errorf("syncing aggregate raw bucket %s: %w", name, err)
		}
		if err := writer.file.Close(); err != nil {
			return xerrors.Errorf("closing aggregate raw bucket %s: %w", name, err)
		}
		writer.file = nil
		newOffsets[name] = writer.offset
		needsRawDirSync = needsRawDirSync || writer.needsRawDirSync
	}

	if needsRawDirSync {
		if err := syncDir(rawDir); err != nil {
			return xerrors.Errorf("syncing aggregate raw bucket directory before cursor commit: %w", err)
		}
	}

	for name, offset := range newOffsets {
		cursor.BucketOffsets[name] = offset
	}

	return nil
}

func finishStorachaAggregateRawBuckets(workDir string) ([]storachaAggregateFileState, error) {
	rawDir := filepath.Join(workDir, storachaAggregateRawBucketDir)
	var files []storachaAggregateFileState
	var removedEmpty bool

	// Some size buckets may be opened but receive no records in small test runs
	// or interrupted runs. Delete empties before recording the stage so later
	// stages only scan files that contain data.
	for _, size := range storachaAggregateBucketSizesAscending() {
		rel := filepath.ToSlash(filepath.Join(storachaAggregateRawBucketDir, storachaAggregateBucketFileName(size)))
		path := filepath.Join(workDir, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, xerrors.Errorf("checking aggregate raw bucket %s: %w", path, err)
		}
		if info.IsDir() {
			return nil, xerrors.Errorf("aggregate raw bucket is a directory: %s", path)
		}
		if info.Size() == 0 {
			if err := os.Remove(path); err != nil {
				return nil, xerrors.Errorf("removing empty aggregate raw bucket %s: %w", path, err)
			}
			removedEmpty = true
			continue
		}

		state, err := computeAggregateFileState(workDir, rel)
		if err != nil {
			return nil, err
		}
		files = append(files, state)
	}

	if removedEmpty {
		if err := syncDir(rawDir); err != nil {
			return nil, err
		}
	}

	return files, nil
}

func dedupeStorachaAggregateBuckets(ctx context.Context, sortPath, workDir string, manifest *storachaAggregateWorkManifest) error {
	if !manifest.RawBuckets.Complete {
		return xerrors.Errorf("aggregate raw buckets are not complete")
	}

	dedupeDir := filepath.Join(workDir, storachaAggregateDedupeDir)
	if err := os.MkdirAll(dedupeDir, 0755); err != nil {
		return xerrors.Errorf("creating dedupe bucket directory: %w", err)
	}

	// Dedupe is per padded-size bucket. Because PieceCIDv2 is the first JSON
	// field, byte sorting the whole line groups duplicate input records together.
	// This keeps memory bounded even when the input contains tens of millions of
	// pieces.
	var files []storachaAggregateFileState
	var totalRecords int64
	for _, rawState := range manifest.RawBuckets.Files {
		if err := ctx.Err(); err != nil {
			return err
		}

		rawPath := filepath.Join(workDir, filepath.FromSlash(rawState.Name))
		sortedTmp := filepath.Join(dedupeDir, filepath.Base(rawState.Name)+".sorted.tmp")

		cmd := exec.CommandContext(ctx, sortPath, "-o", sortedTmp, rawPath)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		sortOut, err := cmd.CombinedOutput()
		if err != nil {
			return xerrors.Errorf("sorting aggregate bucket %s: %w: %s", rawState.Name, err, strings.TrimSpace(string(sortOut)))
		}

		finalRel := filepath.ToSlash(filepath.Join(storachaAggregateDedupeDir, filepath.Base(rawState.Name)))
		finalPath := filepath.Join(workDir, filepath.FromSlash(finalRel))
		records, err := dedupeSortedStorachaAggregateBucket(sortedTmp, finalPath)
		removeErr := os.Remove(sortedTmp)
		if removeErr != nil && !os.IsNotExist(removeErr) {
			err = errors.Join(err, xerrors.Errorf("removing sorted aggregate bucket temp %s: %w", sortedTmp, removeErr))
		}
		if err != nil {
			return err
		}
		if records == 0 {
			if removeErr := os.Remove(finalPath); removeErr != nil && !os.IsNotExist(removeErr) {
				return xerrors.Errorf("removing empty deduped aggregate bucket %s: %w", finalPath, removeErr)
			}
			continue
		}

		state, err := computeAggregateFileState(workDir, finalRel)
		if err != nil {
			return err
		}
		if state.Records != records {
			return xerrors.Errorf("deduped aggregate bucket %s record count changed while finalizing", finalRel)
		}
		files = append(files, state)
		totalRecords += records
	}

	if totalRecords == 0 {
		return xerrors.Errorf("aggregate dedupe produced no records")
	}

	manifest.DedupedBuckets = storachaAggregateStageManifest{
		Complete: true,
		Records:  totalRecords,
		Files:    files,
	}
	if err := writeStorachaAggregateManifest(workDir, manifest); err != nil {
		return err
	}

	return nil
}

func dedupeSortedStorachaAggregateBucket(sortedPath, finalPath string) (int64, error) {
	input, err := os.Open(sortedPath)
	if err != nil {
		return 0, xerrors.Errorf("opening sorted aggregate bucket %s: %w", sortedPath, err)
	}
	defer func() {
		_ = input.Close()
	}()

	tmpPath := finalPath + ".tmp"
	output, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return 0, xerrors.Errorf("creating deduped aggregate bucket %s: %w", tmpPath, err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	reader := bufio.NewReaderSize(input, 1024*1024)
	writer := bufio.NewWriterSize(output, 1024*1024)
	var previousPieceCIDV2 string
	var records int64
	// The sorted input makes duplicates adjacent. Keep the first hydrated record
	// and drop later records for the same PieceCIDv2; all retained records are
	// written back as canonical JSONL with a newline terminator.
	for {
		line, ok, err := readJSONLLine(reader)
		if err != nil {
			_ = output.Close()
			return 0, xerrors.Errorf("reading sorted aggregate bucket %s: %w", sortedPath, err)
		}
		if !ok {
			break
		}

		var record storachaBucketRecord
		if err := json.Unmarshal(line, &record); err != nil {
			_ = output.Close()
			return 0, xerrors.Errorf("decoding sorted aggregate bucket %s: %w", sortedPath, err)
		}
		if err := validateStorachaBucketRecord(record); err != nil {
			_ = output.Close()
			return 0, xerrors.Errorf("validating sorted aggregate bucket %s: %w", sortedPath, err)
		}
		if record.PieceCIDV2 == previousPieceCIDV2 {
			continue
		}

		if _, err := writeJSONLine(writer, record); err != nil {
			_ = output.Close()
			return 0, xerrors.Errorf("writing deduped aggregate bucket %s: %w", tmpPath, err)
		}
		previousPieceCIDV2 = record.PieceCIDV2
		records++
	}

	if err := writer.Flush(); err != nil {
		_ = output.Close()
		return 0, xerrors.Errorf("flushing deduped aggregate bucket %s: %w", tmpPath, err)
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		return 0, xerrors.Errorf("syncing deduped aggregate bucket %s: %w", tmpPath, err)
	}
	if err := output.Close(); err != nil {
		return 0, xerrors.Errorf("closing deduped aggregate bucket %s: %w", tmpPath, err)
	}

	if records == 0 {
		removeTmp = false
		if err := os.Remove(tmpPath); err != nil {
			return 0, xerrors.Errorf("removing empty deduped aggregate bucket %s: %w", tmpPath, err)
		}
		return 0, nil
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return 0, xerrors.Errorf("renaming deduped aggregate bucket %s: %w", finalPath, err)
	}
	removeTmp = false
	if err := syncDir(filepath.Dir(finalPath)); err != nil {
		return 0, err
	}

	return records, nil
}

type storachaAggregateBucketReader struct {
	// pending is the next unconsumed record from this size bucket. Keeping one
	// record buffered per bucket lets grouping stream all buckets without loading
	// bucket files into memory.
	size    abi.PaddedPieceSize
	path    string
	file    *os.File
	reader  *bufio.Reader
	pending *storachaBucketRecord
}

func buildStorachaAggregateGroups(workDir string, manifest *storachaAggregateWorkManifest) error {
	if !manifest.DedupedBuckets.Complete {
		return xerrors.Errorf("aggregate dedupe buckets are not complete")
	}

	// Grouping reads from at most one file for each power-of-two padded size.
	// For the 128-byte through 1-GiB range that is 24 readers, independent of
	// how many total pieces are in the migration input.
	readers, err := openStorachaAggregateBucketReaders(workDir, manifest.DedupedBuckets.Files)
	if err != nil {
		return err
	}
	defer func() {
		for _, reader := range readers {
			_ = reader.close()
		}
	}()

	groupsPath := filepath.Join(workDir, storachaAggregateGroupsFile)
	tmpPath := groupsPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return xerrors.Errorf("creating aggregate group temp file: %w", err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	writer := bufio.NewWriterSize(file, 1024*1024)
	maxEntries := datasegment.MaxIndexEntriesInDeal(storachaAggregatePieceSize)
	var groupID int64
	var groupedPieces int64
	for groupedPieces < manifest.DedupedBuckets.Records {
		placement := storachaAggregatePlacement{}
		groupPieces := make([]storachaBucketRecord, 0, int(maxEntries))

		// The greedy walk restarts from the largest bucket for every aggregate.
		// Within a size bucket we keep sorted input order. We consume as many
		// same-size pieces as fit, then move exactly one power smaller. With
		// power-of-two piece sizes this keeps placement dense and deterministic.
		for _, bucket := range readers {
			for bucket.pending != nil {
				nextOffsetNodes, fits, err := placement.tryAdd(abi.PaddedPieceSize(bucket.pending.PaddedSize), maxEntries)
				if err != nil {
					return xerrors.Errorf("checking aggregate fit for group %d: %w", groupID, err)
				}
				if !fits {
					if len(groupPieces) == 0 {
						return xerrors.Errorf("piece %s with padded size %d does not fit in a %d padded aggregate after reserving the datasegment index",
							bucket.pending.PieceCIDV2, bucket.pending.PaddedSize, storachaAggregatePieceSize)
					}
					break
				}

				groupPieces = append(groupPieces, *bucket.pending)
				placement.accept(nextOffsetNodes)
				if err := bucket.advance(); err != nil {
					return err
				}
			}
		}

		if len(groupPieces) == 0 {
			return xerrors.Errorf("next aggregate subpiece does not fit in a %d padded aggregate after reserving the datasegment index", storachaAggregatePieceSize)
		}

		// The fit check above is intentionally cheap. Before we persist the
		// group, datasegment builds the aggregate metadata and computes the exact
		// PieceCIDv1 that this replay plan must later produce.
		expectedCIDV1, err := expectedStorachaAggregatePieceCID(groupPieces)
		if err != nil {
			return xerrors.Errorf("validating aggregate group %d with datasegment: %w", groupID, err)
		}

		group := storachaAggregateGroup{
			GroupID:    groupID,
			PieceCIDV1: expectedCIDV1.String(),
			PaddedSize: uint64(storachaAggregatePieceSize),
			Pieces:     groupPieces,
		}
		if _, err := writeJSONLine(writer, group); err != nil {
			return xerrors.Errorf("writing aggregate group %d: %w", groupID, err)
		}

		groupID++
		groupedPieces += int64(len(groupPieces))
	}

	if groupedPieces != manifest.DedupedBuckets.Records {
		return xerrors.Errorf("aggregate grouping mismatch: grouped %d pieces, expected %d", groupedPieces, manifest.DedupedBuckets.Records)
	}

	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return xerrors.Errorf("flushing aggregate group file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return xerrors.Errorf("syncing aggregate group file: %w", err)
	}
	if err := file.Close(); err != nil {
		return xerrors.Errorf("closing aggregate group file: %w", err)
	}
	if err := os.Rename(tmpPath, groupsPath); err != nil {
		return xerrors.Errorf("renaming aggregate group file: %w", err)
	}
	removeTmp = false
	if err := syncDir(workDir); err != nil {
		return err
	}

	state, err := computeAggregateFileState(workDir, storachaAggregateGroupsFile)
	if err != nil {
		return err
	}
	manifest.Groups = storachaAggregateGroupManifest{
		Complete: true,
		Pieces:   groupedPieces,
		Groups:   groupID,
		File:     state,
	}
	if err := writeStorachaAggregateManifest(workDir, manifest); err != nil {
		return err
	}

	return nil
}

func openStorachaAggregateBucketReaders(workDir string, files []storachaAggregateFileState) ([]*storachaAggregateBucketReader, error) {
	filesByBucket := map[string]storachaAggregateFileState{}
	for _, state := range files {
		filesByBucket[filepath.Base(state.Name)] = state
	}

	var readers []*storachaAggregateBucketReader
	// Readers are opened largest first so the grouping pass starts every
	// aggregate with the largest available pieces, then walks one power smaller
	// when the current size no longer fits.
	for _, size := range storachaAggregateBucketSizesDescending() {
		name := storachaAggregateBucketFileName(size)
		state, ok := filesByBucket[name]
		if !ok {
			continue
		}

		path := filepath.Join(workDir, filepath.FromSlash(state.Name))
		file, err := os.Open(path)
		if err != nil {
			for _, reader := range readers {
				_ = reader.close()
			}
			return nil, xerrors.Errorf("opening deduped aggregate bucket %s: %w", state.Name, err)
		}

		reader := &storachaAggregateBucketReader{
			size:   size,
			path:   path,
			file:   file,
			reader: bufio.NewReaderSize(file, 1024*1024),
		}
		if err := reader.advance(); err != nil {
			_ = reader.close()
			for _, existing := range readers {
				_ = existing.close()
			}
			return nil, err
		}
		readers = append(readers, reader)
	}

	return readers, nil
}

func (r *storachaAggregateBucketReader) advance() error {
	line, ok, err := readJSONLLine(r.reader)
	if err != nil {
		return xerrors.Errorf("reading deduped aggregate bucket %s: %w", r.path, err)
	}
	if !ok {
		r.pending = nil
		return nil
	}

	var record storachaBucketRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return xerrors.Errorf("decoding deduped aggregate bucket %s: %w", r.path, err)
	}
	if err := validateStorachaBucketRecord(record); err != nil {
		return xerrors.Errorf("validating deduped aggregate bucket %s: %w", r.path, err)
	}
	if record.PaddedSize != uint64(r.size) {
		return xerrors.Errorf("deduped aggregate bucket %s contains padded size %d, expected %d", r.path, record.PaddedSize, r.size)
	}

	r.pending = &record
	return nil
}

func (r *storachaAggregateBucketReader) close() error {
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

type storachaAggregatePlacement struct {
	nextOffsetNodes uint64
	count           uint
}

func (p storachaAggregatePlacement) tryAdd(size abi.PaddedPieceSize, maxEntries uint) (uint64, bool, error) {
	if err := size.Validate(); err != nil {
		return 0, false, err
	}
	if p.count >= maxEntries {
		return 0, false, nil
	}

	// This is the same alignment step used by datasegment.ComputeDealPlacement:
	// choose the next location aligned for this piece size, then advance by one
	// piece. We use it only to decide whether a candidate can fit cheaply while
	// streaming bucket files. Before any group is committed, NewAggregate below
	// recomputes placement and is the authoritative validation.
	sizeInNodes := uint64(size) / 32
	index := (p.nextOffsetNodes + sizeInNodes - 1) / sizeInNodes
	nextOffsetNodes := (index + 1) * sizeInNodes
	packedPaddedSize := nextOffsetNodes * 32
	indexPaddedSize := uint64(maxEntries) * datasegment.EntrySize

	return nextOffsetNodes, packedPaddedSize+indexPaddedSize <= uint64(storachaAggregatePieceSize), nil
}

func (p *storachaAggregatePlacement) accept(nextOffsetNodes uint64) {
	p.nextOffsetNodes = nextOffsetNodes
	p.count++
}

func validateStorachaAggregateGroup(group storachaAggregateGroup) error {
	if group.PaddedSize != uint64(storachaAggregatePieceSize) {
		return xerrors.Errorf("aggregate group padded size is %d, expected %d", group.PaddedSize, storachaAggregatePieceSize)
	}
	if len(group.Pieces) == 0 {
		return xerrors.Errorf("aggregate group has no pieces")
	}
	for i, piece := range group.Pieces {
		if err := validateStorachaBucketRecord(piece); err != nil {
			return xerrors.Errorf("piece %d: %w", i, err)
		}
	}

	// Completed grouping state is trusted only if datasegment still agrees with
	// the exact ordered list of subpieces and the precomputed aggregate CID.
	expectedCIDV1, err := expectedStorachaAggregatePieceCID(group.Pieces)
	if err != nil {
		return err
	}
	if group.PieceCIDV1 != expectedCIDV1.String() {
		return xerrors.Errorf("aggregate group commP mismatch: group has %s, datasegment computes %s", group.PieceCIDV1, expectedCIDV1)
	}

	return nil
}

func expectedStorachaAggregatePieceCID(records []storachaBucketRecord) (cid.Cid, error) {
	// This computes the expected aggregate PieceCIDv1 from the subpiece CIDs and
	// padded sizes only. The PieceCIDv2 for the aggregate is not known until the
	// aggregate body is actually written, because v2 also carries raw bytes.
	pinfos, err := storachaBucketRecordsToPieceInfos(records)
	if err != nil {
		return cid.Undef, err
	}
	aggr, err := datasegment.NewAggregate(storachaAggregatePieceSize, pinfos)
	if err != nil {
		return cid.Undef, err
	}
	pcid, err := aggr.PieceCID()
	if err != nil {
		return cid.Undef, xerrors.Errorf("computing aggregate PieceCIDv1: %w", err)
	}
	return pcid, nil
}

func storachaBucketRecordsToPieceInfos(records []storachaBucketRecord) ([]abi.PieceInfo, error) {
	pinfos := make([]abi.PieceInfo, 0, len(records))
	for i, record := range records {
		pcidV1, err := cid.Parse(record.PieceCIDV1)
		if err != nil {
			return nil, xerrors.Errorf("parsing PieceCIDv1 for record %d: %w", i, err)
		}
		pinfos = append(pinfos, abi.PieceInfo{
			PieceCID: pcidV1,
			Size:     abi.PaddedPieceSize(record.PaddedSize),
		})
	}
	return pinfos, nil
}

func validateStorachaBucketRecord(record storachaBucketRecord) error {
	if record.PieceID <= 0 {
		return xerrors.Errorf("invalid parked piece id %d", record.PieceID)
	}

	// A bucket record is self-validating: PieceCIDv2 gives us PieceCIDv1 and raw
	// size, raw size gives us padded size, and the bucket filename/checking code
	// ensures the record lives in the expected power-of-two bucket.
	pcidV2, err := cid.Parse(record.PieceCIDV2)
	if err != nil {
		return xerrors.Errorf("parsing PieceCIDv2 %q: %w", record.PieceCIDV2, err)
	}
	if !commcidv2.IsPieceCidV2(pcidV2) {
		return xerrors.Errorf("not a PieceCIDv2: %s", pcidV2)
	}

	pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcidV2)
	if err != nil {
		return xerrors.Errorf("decoding PieceCIDv2 %s: %w", pcidV2, err)
	}
	if pcidV1.String() != record.PieceCIDV1 {
		return xerrors.Errorf("PieceCIDv1 mismatch for %s: record has %s, CID decodes to %s", pcidV2, record.PieceCIDV1, pcidV1)
	}
	if rawSize != record.RawSize {
		return xerrors.Errorf("raw size mismatch for %s: record has %d, CID decodes to %d", pcidV2, record.RawSize, rawSize)
	}
	paddedSize := padreader.PaddedSize(rawSize).Padded()
	if uint64(paddedSize) != record.PaddedSize {
		return xerrors.Errorf("padded size mismatch for %s: record has %d, raw size pads to %d", pcidV2, record.PaddedSize, paddedSize)
	}
	if err := paddedSize.Validate(); err != nil {
		return xerrors.Errorf("invalid padded size for %s: %w", pcidV2, err)
	}
	if !storachaAggregateIsBucketSize(paddedSize) {
		return xerrors.Errorf("padded size %d is outside supported aggregate bucket range", paddedSize)
	}

	return nil
}

func processStorachaAggregateGroups(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, sourcePath, targetPath, workDir string, manifest *storachaAggregateWorkManifest) error {
	for _, dir := range []string{
		filepath.Join(workDir, storachaAggregateCompleteDir),
		filepath.Join(workDir, storachaAggregateTempDir),
		storachaAggregateStagingDir(targetPath),
		storachaAggregateStagedDir(targetPath),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return xerrors.Errorf("creating aggregate directory %s: %w", dir, err)
		}
	}

	// Processing is a linear replay of groups.jsonl. Group ids must match the
	// file order so completion marker names are deterministic and partial result
	// reporting can stream the same plan.
	groupsPath := filepath.Join(workDir, storachaAggregateGroupsFile)
	file, err := os.Open(groupsPath)
	if err != nil {
		return xerrors.Errorf("opening aggregate group file: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	reader := bufio.NewReaderSize(file, 1024*1024)
	var groups int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, ok, err := readJSONLLine(reader)
		if err != nil {
			return xerrors.Errorf("reading aggregate group file: %w", err)
		}
		if !ok {
			break
		}

		var group storachaAggregateGroup
		if err := json.Unmarshal(line, &group); err != nil {
			return xerrors.Errorf("decoding aggregate group %d: %w", groups, err)
		}
		if group.GroupID != groups {
			return xerrors.Errorf("aggregate group id mismatch while processing: expected %d, got %d", groups, group.GroupID)
		}
		if err := processStorachaAggregateGroup(ctx, db, si, storageID, sourcePath, targetPath, workDir, group); err != nil {
			return err
		}
		groups++
	}

	if groups != manifest.Groups.Groups {
		return xerrors.Errorf("processed %d aggregate groups, manifest expects %d; cleanup %s manually before retrying", groups, manifest.Groups.Groups, workDir)
	}

	return nil
}

func processStorachaAggregateGroup(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, sourcePath, targetPath, workDir string, group storachaAggregateGroup) error {
	// Recovery order is from most committed to least committed:
	//  1. completion marker: nothing else to do;
	//  2. final Curio piece file + DB row: recreate metadata;
	//  3. committed staging marker + staging file: run the normal import path;
	//  4. no committed file: rebuild from read-only source pieces.
	if _, ok, err := readCompletedStorachaAggregateGroup(workDir, group); err != nil {
		return err
	} else if ok {
		return nil
	}

	expectedCIDV1, err := cid.Parse(group.PieceCIDV1)
	if err != nil {
		return xerrors.Errorf("parsing aggregate group %d PieceCIDv1: %w", group.GroupID, err)
	}
	expectedPaddedSize := abi.PaddedPieceSize(group.PaddedSize)

	if recovered, err := recoverStorachaAggregateFromFinal(ctx, db, si, storageID, targetPath, workDir, group, expectedCIDV1, expectedPaddedSize); err != nil {
		return err
	} else if recovered {
		return nil
	}

	if recovered, err := recoverStorachaAggregateFromStaging(ctx, db, si, storageID, targetPath, workDir, group, expectedCIDV1, expectedPaddedSize); err != nil {
		return err
	} else if recovered {
		return nil
	}

	tempPath, verified, err := createStorachaAggregateTempFile(sourcePath, workDir, group, expectedCIDV1, expectedPaddedSize)
	if err != nil {
		return err
	}
	stagingPath, err := stageStorachaAggregateFile(targetPath, group, tempPath, verified)
	if err != nil {
		return err
	}
	return importStagedStorachaAggregateFile(ctx, db, si, storageID, targetPath, workDir, group, stagingPath, verified)
}

func readCompletedStorachaAggregateGroup(workDir string, group storachaAggregateGroup) (storachaAggregateCompletion, bool, error) {
	path := storachaAggregateCompletionPath(workDir, group.GroupID)
	mb, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return storachaAggregateCompletion{}, false, nil
		}
		return storachaAggregateCompletion{}, false, xerrors.Errorf("reading aggregate completion marker %s: %w", path, err)
	}

	var complete storachaAggregateCompletion
	if err := json.Unmarshal(mb, &complete); err != nil {
		return storachaAggregateCompletion{}, false, xerrors.Errorf("decoding aggregate completion marker %s: %w; cleanup manually before retrying", path, err)
	}
	// A marker is trusted only if it describes this exact group. A corrupt or
	// stale marker would otherwise make us skip a group that still needs work.
	if err := validateStorachaAggregateCompletion(group, complete); err != nil {
		return storachaAggregateCompletion{}, false, xerrors.Errorf("invalid aggregate completion marker %s: %w; cleanup manually before retrying", path, err)
	}

	return complete, true, nil
}

func validateStorachaAggregateCompletion(group storachaAggregateGroup, complete storachaAggregateCompletion) error {
	if complete.GroupID != group.GroupID {
		return xerrors.Errorf("group id mismatch: marker has %d, group has %d", complete.GroupID, group.GroupID)
	}
	if complete.PieceCIDV1 != group.PieceCIDV1 {
		return xerrors.Errorf("PieceCIDv1 mismatch: marker has %s, group has %s", complete.PieceCIDV1, group.PieceCIDV1)
	}
	if complete.PaddedSize != group.PaddedSize {
		return xerrors.Errorf("padded size mismatch: marker has %d, group has %d", complete.PaddedSize, group.PaddedSize)
	}
	if complete.Count != len(group.Pieces) || len(complete.SubPieces) != len(group.Pieces) {
		return xerrors.Errorf("subpiece count mismatch: marker has %d/%d, group has %d", complete.Count, len(complete.SubPieces), len(group.Pieces))
	}
	pcidV2, err := cid.Parse(complete.PieceCID)
	if err != nil {
		return xerrors.Errorf("parsing aggregate PieceCIDv2 %q: %w", complete.PieceCID, err)
	}
	if !commcidv2.IsPieceCidV2(pcidV2) {
		return xerrors.Errorf("aggregate PieceCID is not v2: %s", pcidV2)
	}
	pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcidV2)
	if err != nil {
		return xerrors.Errorf("decoding aggregate PieceCIDv2 %s: %w", pcidV2, err)
	}
	if pcidV1.String() != complete.PieceCIDV1 {
		return xerrors.Errorf("aggregate PieceCIDv1 mismatch: marker has %s, v2 decodes to %s", complete.PieceCIDV1, pcidV1)
	}
	if rawSize != complete.RawSize {
		return xerrors.Errorf("aggregate raw size mismatch: marker has %d, v2 decodes to %d", complete.RawSize, rawSize)
	}
	if uint64(padreader.PaddedSize(rawSize).Padded()) != complete.PaddedSize {
		return xerrors.Errorf("aggregate padded size mismatch: marker has %d, raw size pads to %d", complete.PaddedSize, padreader.PaddedSize(rawSize).Padded())
	}
	for i, piece := range group.Pieces {
		if complete.SubPieces[i] != piece.PieceCIDV2 {
			return xerrors.Errorf("subpiece %d mismatch: marker has %s, group has %s", i, complete.SubPieces[i], piece.PieceCIDV2)
		}
	}
	return nil
}

func recoverStorachaAggregateFromFinal(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, targetPath, workDir string, group storachaAggregateGroup, expectedCIDV1 cid.Cid, expectedPaddedSize abi.PaddedPieceSize) (bool, error) {
	// A crash can happen after import-pieces moved the file into Curio storage
	// but before aggregate-pieces wrote its completion marker. At this point the
	// file has passed the staging marker boundary, so we trust the DB row and final
	// filename instead of re-reading the whole aggregate for CommP.
	var rows []storachaFinalRecoveryRow
	err := db.Select(ctx, &rows, `
		SELECT id, piece_cid, piece_padded_size, piece_raw_size
		FROM parked_pieces
		WHERE piece_cid = $1
		  AND piece_padded_size = $2
		  AND long_term = TRUE
		  AND cleanup_task_id IS NULL
		ORDER BY id`, expectedCIDV1.String(), int64(expectedPaddedSize))
	if err != nil {
		return false, xerrors.Errorf("querying aggregate parked pieces: %w", err)
	}

	for _, row := range rows {
		finalPath := storachaFinalPiecePath(targetPath, row.ID)
		info, err := os.Stat(finalPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, xerrors.Errorf("checking aggregate final file %s: %w", finalPath, err)
		}
		if info.IsDir() {
			return false, xerrors.Errorf("aggregate final path is a directory: %s", finalPath)
		}
		if info.Size() < 0 || uint64(info.Size()) != uint64(row.RawSize) {
			return false, xerrors.Errorf("aggregate final file %s size mismatch for parked piece %d: db=%d file=%d; cleanup manually before retrying", finalPath, row.ID, row.RawSize, info.Size())
		}

		verified, err := storachaVerifiedAggregatePieceFromRow(row)
		if err != nil {
			return false, err
		}

		imported, err := recoverFinalStorachaPiece(ctx, db, si, storageID, targetPath, row)
		if err != nil {
			return false, err
		}
		if !imported {
			return false, nil
		}

		if err := finishStorachaAggregateGroup(targetPath, workDir, group, verified); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

func recoverStorachaAggregateFromStaging(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, targetPath, workDir string, group storachaAggregateGroup, expectedCIDV1 cid.Cid, expectedPaddedSize abi.PaddedPieceSize) (bool, error) {
	// Staged markers are the commit point for staging. The marker filename is the
	// aggregate PieceCIDv2; if it decodes to this group, the matching .car has
	// already been written, fsynced, named from CommP, and committed by marker.
	staged := storachaAggregateStagedDir(targetPath)
	entries, err := os.ReadDir(staged)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, xerrors.Errorf("reading aggregate staged marker directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			return false, xerrors.Errorf("aggregate staged marker is a directory: %s; cleanup manually before retrying", filepath.Join(staged, entry.Name()))
		}

		pcidV2, err := cid.Parse(entry.Name())
		if err != nil || !commcidv2.IsPieceCidV2(pcidV2) {
			return false, xerrors.Errorf("aggregate staged marker is not a PieceCIDv2: %s; cleanup manually before retrying", filepath.Join(staged, entry.Name()))
		}
		pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcidV2)
		if err != nil {
			return false, xerrors.Errorf("decoding aggregate staged marker %s: %w; cleanup manually before retrying", filepath.Join(staged, entry.Name()), err)
		}
		if !pcidV1.Equals(expectedCIDV1) || padreader.PaddedSize(rawSize).Padded() != expectedPaddedSize {
			continue
		}

		stagingPath := storachaAggregateStagingPath(targetPath, pcidV2)
		info, err := os.Stat(stagingPath)
		if err != nil {
			if os.IsNotExist(err) {
				return false, xerrors.Errorf("aggregate staged marker %s exists but staging file %s is missing; cleanup manually before retrying", filepath.Join(staged, entry.Name()), stagingPath)
			}
			return false, xerrors.Errorf("checking aggregate staging file %s: %w", stagingPath, err)
		}
		if info.IsDir() {
			return false, xerrors.Errorf("aggregate staging path is a directory: %s", stagingPath)
		}
		if info.Size() < 0 || uint64(info.Size()) != rawSize {
			return false, xerrors.Errorf("aggregate staging file %s size mismatch: marker raw=%d file=%d; cleanup manually before retrying", stagingPath, rawSize, info.Size())
		}

		verified := storachaVerifiedAggregatePiece{
			PieceCIDV2: pcidV2,
			PieceCIDV1: pcidV1,
			RawSize:    rawSize,
			PaddedSize: expectedPaddedSize,
		}

		if err := importStagedStorachaAggregateFile(ctx, db, si, storageID, targetPath, workDir, group, stagingPath, verified); err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

type storachaVerifiedAggregatePiece struct {
	// RawSize is the exact number of bytes written/read for the aggregate file.
	// PaddedSize and PieceCIDv1 come from CommP. PieceCIDv2 is computed from the
	// same digest plus RawSize; there is no padded/unpadded-to-raw conversion.
	PieceCIDV2 cid.Cid
	PieceCIDV1 cid.Cid
	RawSize    uint64
	PaddedSize abi.PaddedPieceSize
}

func storachaVerifiedAggregatePieceFromRow(row storachaFinalRecoveryRow) (storachaVerifiedAggregatePiece, error) {
	pcidV1, err := cid.Parse(row.PieceCID)
	if err != nil {
		return storachaVerifiedAggregatePiece{}, xerrors.Errorf("parsing aggregate final PieceCIDv1 %s: %w", row.PieceCID, err)
	}

	pcidV2, err := commcid.PieceCidV2FromV1(pcidV1, uint64(row.RawSize))
	if err != nil {
		return storachaVerifiedAggregatePiece{}, xerrors.Errorf("computing aggregate final PieceCIDv2 from DB row: %w", err)
	}

	return storachaVerifiedAggregatePiece{
		PieceCIDV2: pcidV2,
		PieceCIDV1: pcidV1,
		RawSize:    uint64(row.RawSize),
		PaddedSize: abi.PaddedPieceSize(row.PaddedSize),
	}, nil
}

func createStorachaAggregateTempFile(sourcePath, workDir string, group storachaAggregateGroup, expectedCIDV1 cid.Cid, expectedPaddedSize abi.PaddedPieceSize) (string, storachaVerifiedAggregatePiece, error) {
	// Rebuild the datasegment aggregate from the deterministic group file, then
	// stream read-only source/piece/s-t00-<parked_piece_id> files into a local
	// temp aggregate. The temp file is fsynced and handed to staging only after
	// its CommP and padded size match the group plan.
	pinfos, err := storachaBucketRecordsToPieceInfos(group.Pieces)
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, err
	}

	aggr, err := datasegment.NewAggregate(expectedPaddedSize, pinfos)
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("creating aggregate group %d: %w", group.GroupID, err)
	}
	pcidV1, err := aggr.PieceCID()
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("computing aggregate group %d PieceCIDv1: %w", group.GroupID, err)
	}
	if !pcidV1.Equals(expectedCIDV1) {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("aggregate group %d PieceCIDv1 changed: datasegment computes %s, group has %s", group.GroupID, pcidV1, expectedCIDV1)
	}

	readers, closeReaders, err := openStorachaAggregateSubPieceReaders(sourcePath, group.Pieces)
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, err
	}
	defer func() {
		_ = closeReaders()
	}()

	outR, err := aggr.AggregateObjectReader(readers)
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("creating aggregate reader for group %d: %w", group.GroupID, err)
	}

	tmpPath := storachaAggregateTempFilePath(workDir, group.GroupID)
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0755); err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("creating aggregate temp directory: %w", err)
	}
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("creating aggregate temp file %s: %w", tmpPath, err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	cp := new(commp.Calc)
	defer cp.Reset()
	n, copyErr := io.Copy(io.MultiWriter(cp, file), outR)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("writing aggregate group %d: %w", group.GroupID, err)
	}

	digest, actualPaddedSize, err := cp.Digest()
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("computing aggregate group %d digest: %w", group.GroupID, err)
	}
	if abi.PaddedPieceSize(actualPaddedSize) != expectedPaddedSize {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("aggregate group %d padded size mismatch: expected %d, got %d", group.GroupID, expectedPaddedSize, actualPaddedSize)
	}

	actualCIDV1, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("computing aggregate group %d PieceCIDv1 from digest: %w", group.GroupID, err)
	}
	if !actualCIDV1.Equals(expectedCIDV1) {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("aggregate group %d commP mismatch: calculated %s, expected %s", group.GroupID, actualCIDV1, expectedCIDV1)
	}

	// Raw size is the number of bytes AggregateObjectReader actually emitted.
	// It is not inferred from padded size. This value is what PieceCIDv2 needs.
	actualCIDV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
	if err != nil {
		return "", storachaVerifiedAggregatePiece{}, xerrors.Errorf("computing aggregate group %d PieceCIDv2: %w", group.GroupID, err)
	}

	removeTmp = false

	return tmpPath, storachaVerifiedAggregatePiece{
		PieceCIDV2: actualCIDV2,
		PieceCIDV1: actualCIDV1,
		RawSize:    uint64(n),
		PaddedSize: abi.PaddedPieceSize(actualPaddedSize),
	}, nil
}

func stageStorachaAggregateFile(targetPath string, group storachaAggregateGroup, tempPath string, verified storachaVerifiedAggregatePiece) (string, error) {
	// This is the only place that turns a verified aggregate temp file into
	// committed staging state. The order is: rename to <PieceCIDv2>.car, fsync
	// directories, then write staged/<PieceCIDv2>. Staging recovery trusts only
	// files that have crossed this marker boundary.
	staging := storachaAggregateStagingDir(targetPath)
	if err := os.MkdirAll(staging, 0755); err != nil {
		return "", xerrors.Errorf("creating aggregate staging directory: %w", err)
	}
	if err := os.MkdirAll(storachaAggregateStagedDir(targetPath), 0755); err != nil {
		return "", xerrors.Errorf("creating aggregate staged marker directory: %w", err)
	}

	stagingPath := storachaAggregateStagingPath(targetPath, verified.PieceCIDV2)
	if filepath.Clean(tempPath) == filepath.Clean(stagingPath) {
		return "", xerrors.Errorf("aggregate group %d is already at staging path %s", group.GroupID, stagingPath)
	}

	if info, err := os.Stat(stagingPath); err == nil {
		if info.IsDir() {
			return "", xerrors.Errorf("aggregate staging path is a directory: %s", stagingPath)
		}
		if info.Size() < 0 || uint64(info.Size()) != verified.RawSize {
			return "", xerrors.Errorf("aggregate staging file %s size mismatch: expected %d, got %d; cleanup manually before retrying", stagingPath, verified.RawSize, info.Size())
		}
		marked, err := storachaAggregateStagedMarkerExists(targetPath, verified.PieceCIDV2)
		if err != nil {
			return "", err
		}
		if marked {
			return "", xerrors.Errorf("committed aggregate staging file already exists: %s; retry should recover it before staging another aggregate", stagingPath)
		}
		return "", xerrors.Errorf("uncommitted aggregate staging file already exists: %s; cleanup manually before retrying", stagingPath)
	} else if !os.IsNotExist(err) {
		return "", xerrors.Errorf("checking aggregate staging path %s: %w", stagingPath, err)
	}

	if err := os.Rename(tempPath, stagingPath); err != nil {
		return "", xerrors.Errorf("moving aggregate group %d to staging: %w", group.GroupID, err)
	}
	if err := syncDir(filepath.Dir(tempPath)); err != nil {
		return "", err
	}
	if err := syncDir(staging); err != nil {
		return "", err
	}
	if err := writeStorachaAggregateStagedMarker(targetPath, verified.PieceCIDV2); err != nil {
		return "", err
	}

	return stagingPath, nil
}

func importStagedStorachaAggregateFile(ctx context.Context, db *harmonydb.DB, si paths.SectorIndex, storageID storiface.ID, targetPath, workDir string, group storachaAggregateGroup, stagingPath string, verified storachaVerifiedAggregatePiece) error {
	// This function assumes staging has already been committed by
	// staged/<PieceCIDv2>. It does not create or refresh that marker, and it does
	// not check it again or recompute CommP. It only performs cheap consistency
	// checks, imports the staged file through the existing import-pieces path, then
	// advances the group to complete/<groupID>.json.
	expectedStagingPath := storachaAggregateStagingPath(targetPath, verified.PieceCIDV2)
	if filepath.Clean(stagingPath) != filepath.Clean(expectedStagingPath) {
		return xerrors.Errorf("aggregate staging path mismatch: got %s, expected %s", stagingPath, expectedStagingPath)
	}

	info, err := os.Stat(stagingPath)
	if err != nil {
		return xerrors.Errorf("checking aggregate staging file %s: %w", stagingPath, err)
	}
	if info.IsDir() {
		return xerrors.Errorf("aggregate staging path is a directory: %s", stagingPath)
	}
	if info.Size() < 0 || uint64(info.Size()) != verified.RawSize {
		return xerrors.Errorf("aggregate staging file %s size mismatch: expected %d, got %d; cleanup manually before retrying", stagingPath, verified.RawSize, info.Size())
	}

	result, err := importStagedStorachaPiece(ctx, db, si, storageID, targetPath, stagingPath)
	if err != nil {
		return err
	}
	if !result.Imported {
		return xerrors.Errorf("aggregate piece %s is not importable yet", verified.PieceCIDV2)
	}

	if err := finishStorachaAggregateGroup(targetPath, workDir, group, verified); err != nil {
		return err
	}
	return nil
}

func openStorachaAggregateSubPieceReaders(sourcePath string, subPieces []storachaBucketRecord) ([]io.Reader, func() error, error) {
	// datasegment consumes one reader per subpiece in group order. We cap each
	// reader at the raw size recorded from the PieceCIDv2 so stray bytes in a
	// source file cannot affect the aggregate stream.
	readers := make([]io.Reader, 0, len(subPieces))
	files := make([]*os.File, 0, len(subPieces))

	closeReaders := func() error {
		var err error
		for _, file := range files {
			err = errors.Join(err, file.Close())
		}
		return err
	}

	for _, piece := range subPieces {
		// The source is read-only and is addressed exactly like Curio piece park:
		// source/piece/s-t00-<parked_piece_id>. We do not fall back to CID-named
		// files here because aggregate-pieces is replaying already imported
		// parked pieces, not consuming import-pieces source files.
		piecePath := storachaFinalPiecePath(sourcePath, piece.PieceID)
		file, err := os.Open(piecePath)
		if err != nil {
			_ = closeReaders()
			return nil, nil, xerrors.Errorf("opening subpiece %s: %w", piecePath, err)
		}
		files = append(files, file)

		info, err := file.Stat()
		if err != nil {
			_ = closeReaders()
			return nil, nil, xerrors.Errorf("checking subpiece %s: %w", piecePath, err)
		}
		if info.IsDir() {
			_ = closeReaders()
			return nil, nil, xerrors.Errorf("subpiece path is a directory: %s", piecePath)
		}
		if info.Size() < 0 || uint64(info.Size()) != piece.RawSize {
			_ = closeReaders()
			return nil, nil, xerrors.Errorf("subpiece %s has size %d, expected %d", piecePath, info.Size(), piece.RawSize)
		}

		readers = append(readers, io.LimitReader(file, int64(piece.RawSize)))
	}

	return readers, closeReaders, nil
}

func writeStorachaAggregateCompletion(workDir string, group storachaAggregateGroup, verified storachaVerifiedAggregatePiece) error {
	// This marker is written last. After it exists, retries skip the group and
	// result generation reports the aggregate to the caller.
	subPieces := make([]string, 0, len(group.Pieces))
	for _, piece := range group.Pieces {
		subPieces = append(subPieces, piece.PieceCIDV2)
	}

	complete := storachaAggregateCompletion{
		GroupID:    group.GroupID,
		PieceCID:   verified.PieceCIDV2.String(),
		SubPieces:  subPieces,
		Count:      len(subPieces),
		PieceCIDV1: group.PieceCIDV1,
		RawSize:    verified.RawSize,
		PaddedSize: group.PaddedSize,
	}
	if err := writeJSONFileAtomic(storachaAggregateCompletionPath(workDir, group.GroupID), complete); err != nil {
		return xerrors.Errorf("writing aggregate completion marker for group %d: %w", group.GroupID, err)
	}
	return nil
}

func finishStorachaAggregateGroup(targetPath, workDir string, group storachaAggregateGroup, verified storachaVerifiedAggregatePiece) error {
	if err := removeStorachaAggregateStagedMarker(targetPath, verified.PieceCIDV2); err != nil {
		return err
	}
	return writeStorachaAggregateCompletion(workDir, group, verified)
}

func writeStorachaAggregateStagedMarker(targetPath string, pcidV2 cid.Cid) error {
	markerPath := storachaAggregateStagedMarkerPath(targetPath, pcidV2)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0755); err != nil {
		return xerrors.Errorf("creating aggregate staged marker directory: %w", err)
	}

	tmpPath := markerPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return xerrors.Errorf("creating aggregate staged marker temp %s: %w", tmpPath, err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return xerrors.Errorf("writing aggregate staged marker temp %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, markerPath); err != nil {
		return xerrors.Errorf("renaming aggregate staged marker %s: %w", markerPath, err)
	}
	removeTmp = false
	if err := syncDir(filepath.Dir(markerPath)); err != nil {
		return err
	}
	return nil
}

func storachaAggregateStagedMarkerExists(targetPath string, pcidV2 cid.Cid) (bool, error) {
	markerPath := storachaAggregateStagedMarkerPath(targetPath, pcidV2)
	info, err := os.Stat(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, xerrors.Errorf("checking aggregate staged marker %s: %w", markerPath, err)
	}
	if info.IsDir() {
		return false, xerrors.Errorf("aggregate staged marker is a directory: %s; cleanup manually before retrying", markerPath)
	}
	return true, nil
}

func removeStorachaAggregateStagedMarker(targetPath string, pcidV2 cid.Cid) error {
	markerPath := storachaAggregateStagedMarkerPath(targetPath, pcidV2)
	if err := os.Remove(markerPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return xerrors.Errorf("removing aggregate staged marker %s: %w", markerPath, err)
	}
	if err := syncDir(filepath.Dir(markerPath)); err != nil {
		return err
	}
	return nil
}

func storachaAggregateStagingDir(targetPath string) string {
	return filepath.Join(targetPath, storachaAggregateStagingDirName)
}

func storachaAggregateStagedDir(targetPath string) string {
	return filepath.Join(storachaAggregateStagingDir(targetPath), storachaAggregateStagedDirName)
}

func storachaAggregateStagingPath(targetPath string, pcidV2 cid.Cid) string {
	return filepath.Join(storachaAggregateStagingDir(targetPath), pcidV2.String()+".car")
}

func storachaAggregateStagedMarkerPath(targetPath string, pcidV2 cid.Cid) string {
	return filepath.Join(storachaAggregateStagedDir(targetPath), pcidV2.String())
}

func storachaAggregateCompletionPath(workDir string, groupID int64) string {
	return filepath.Join(workDir, storachaAggregateCompleteDir, fmt.Sprintf("%0*d.json", storachaAggregateGroupIDWidth, groupID))
}

func storachaAggregateTempFilePath(workDir string, groupID int64) string {
	return filepath.Join(workDir, storachaAggregateTempDir, fmt.Sprintf("%0*d.car.tmp", storachaAggregateGroupIDWidth, groupID))
}

func storachaAggregateBucketFileName(size abi.PaddedPieceSize) string {
	return fmt.Sprintf("%d.jsonl", uint64(size))
}

func storachaAggregateBucketSizesAscending() []abi.PaddedPieceSize {
	// Filecoin piece sizes are powers of two. For a 1-GiB aggregate target this
	// creates bucket names from 128.jsonl through 1073741824.jsonl.
	sizes := make([]abi.PaddedPieceSize, 0, 24)
	for size := abi.PaddedPieceSize(128); size <= storachaAggregatePieceSize; size <<= 1 {
		sizes = append(sizes, size)
	}
	return sizes
}

func storachaAggregateBucketSizesDescending() []abi.PaddedPieceSize {
	// Grouping consumes largest buckets first, but raw finalization/checksums are
	// easier to reason about in ascending filename order.
	ascending := storachaAggregateBucketSizesAscending()
	descending := make([]abi.PaddedPieceSize, 0, len(ascending))
	for i := len(ascending) - 1; i >= 0; i-- {
		descending = append(descending, ascending[i])
	}
	return descending
}

func storachaAggregateIsBucketSize(size abi.PaddedPieceSize) bool {
	if size < 128 || size > storachaAggregatePieceSize {
		return false
	}
	if err := size.Validate(); err != nil {
		return false
	}
	return true
}

func writeJSONLine(writer *bufio.Writer, value any) (int, error) {
	line, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	line = append(line, '\n')
	return writer.Write(line)
}

func writeAggregatePiecesResultFromWork(path, workDir string, runErr error) error {
	// The result is reconstructed from completion markers, not from in-memory
	// state. That makes it safe to report completed aggregates after retries,
	// crashes, or a final failure in a later group.
	if workDir == "" {
		out := aggregatePiecesOutput{Aggregates: []aggregates{}}
		if runErr != nil {
			out.Error = runErr.Error()
		}
		return writeAggregatePiecesResult(path, out)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return xerrors.Errorf("creating aggregate result directory: %w", err)
	}

	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return xerrors.Errorf("creating aggregate result temp file: %w", err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	writer := bufio.NewWriterSize(file, 1024*1024)
	if _, err := writer.WriteString("{\n"); err != nil {
		_ = file.Close()
		return xerrors.Errorf("writing aggregate result: %w", err)
	}
	if runErr != nil {
		errJSON, err := json.Marshal(runErr.Error())
		if err != nil {
			_ = file.Close()
			return xerrors.Errorf("marshalling aggregate result error: %w", err)
		}
		if _, err := writer.WriteString("  \"error\": "); err != nil {
			_ = file.Close()
			return xerrors.Errorf("writing aggregate result: %w", err)
		}
		if _, err := writer.Write(errJSON); err != nil {
			_ = file.Close()
			return xerrors.Errorf("writing aggregate result: %w", err)
		}
		if _, err := writer.WriteString(",\n"); err != nil {
			_ = file.Close()
			return xerrors.Errorf("writing aggregate result: %w", err)
		}
	}
	if _, err := writer.WriteString("  \"aggregates\": ["); err != nil {
		_ = file.Close()
		return xerrors.Errorf("writing aggregate result: %w", err)
	}

	first := true
	groupsPath := filepath.Join(workDir, storachaAggregateGroupsFile)
	groupsFile, err := os.Open(groupsPath)
	if err == nil {
		// Stream groups.jsonl instead of loading it. There may be many aggregate
		// groups, but only completed groups produce result entries.
		reader := bufio.NewReaderSize(groupsFile, 1024*1024)
		for {
			line, ok, readErr := readJSONLLine(reader)
			if readErr != nil {
				if runErr != nil {
					break
				}
				_ = groupsFile.Close()
				_ = file.Close()
				return xerrors.Errorf("reading aggregate groups for result: %w", readErr)
			}
			if !ok {
				break
			}

			var group storachaAggregateGroup
			if err := json.Unmarshal(line, &group); err != nil {
				if runErr != nil {
					break
				}
				_ = groupsFile.Close()
				_ = file.Close()
				return xerrors.Errorf("decoding aggregate group for result: %w", err)
			}

			complete, ok, err := readCompletedStorachaAggregateGroup(workDir, group)
			if err != nil {
				if runErr != nil {
					continue
				}
				_ = groupsFile.Close()
				_ = file.Close()
				return err
			}
			if !ok {
				continue
			}

			result := struct {
				PieceCID  string   `json:"piece_cid"`
				SubPieces []string `json:"sub_pieces"`
				Count     int      `json:"count"`
			}{
				PieceCID:  complete.PieceCID,
				SubPieces: complete.SubPieces,
				Count:     complete.Count,
			}
			resultJSON, err := json.Marshal(result)
			if err != nil {
				_ = groupsFile.Close()
				_ = file.Close()
				return xerrors.Errorf("marshalling aggregate result: %w", err)
			}

			if first {
				if _, err := writer.WriteString("\n    "); err != nil {
					_ = groupsFile.Close()
					_ = file.Close()
					return xerrors.Errorf("writing aggregate result: %w", err)
				}
				first = false
			} else {
				if _, err := writer.WriteString(",\n    "); err != nil {
					_ = groupsFile.Close()
					_ = file.Close()
					return xerrors.Errorf("writing aggregate result: %w", err)
				}
			}
			if _, err := writer.Write(resultJSON); err != nil {
				_ = groupsFile.Close()
				_ = file.Close()
				return xerrors.Errorf("writing aggregate result: %w", err)
			}
		}
		if err := groupsFile.Close(); err != nil {
			_ = file.Close()
			return xerrors.Errorf("closing aggregate groups for result: %w", err)
		}
	} else if !os.IsNotExist(err) {
		_ = file.Close()
		return xerrors.Errorf("opening aggregate groups for result: %w", err)
	}

	if first {
		if _, err := writer.WriteString("]\n"); err != nil {
			_ = file.Close()
			return xerrors.Errorf("writing aggregate result: %w", err)
		}
	} else {
		if _, err := writer.WriteString("\n  ]\n"); err != nil {
			_ = file.Close()
			return xerrors.Errorf("writing aggregate result: %w", err)
		}
	}
	if _, err := writer.WriteString("}\n"); err != nil {
		_ = file.Close()
		return xerrors.Errorf("writing aggregate result: %w", err)
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return xerrors.Errorf("flushing aggregate result: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return xerrors.Errorf("syncing aggregate result: %w", err)
	}
	if err := file.Close(); err != nil {
		return xerrors.Errorf("closing aggregate result: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return xerrors.Errorf("renaming aggregate result: %w", err)
	}
	removeTmp = false
	return syncDir(filepath.Dir(path))
}

func writeAggregatePiecesResult(path string, out aggregatePiecesOutput) error {
	if out.Aggregates == nil {
		out.Aggregates = []aggregates{}
	}
	if err := writeJSONFileAtomic(path, out); err != nil {
		return xerrors.Errorf("writing aggregate result: %w", err)
	}
	return nil
}

func writeJSONFileAtomic(path string, value any) error {
	// All small state files use the same tmp-file protocol: write, fsync, close,
	// rename, then fsync the directory so the rename itself survives a crash.
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return xerrors.Errorf("creating directory for %s: %w", path, err)
	}

	tmpPath := path + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return xerrors.Errorf("creating temp JSON file %s: %w", tmpPath, err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(value)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(encodeErr, syncErr, closeErr); err != nil {
		return xerrors.Errorf("writing temp JSON file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return xerrors.Errorf("renaming temp JSON file %s to %s: %w", tmpPath, path, err)
	}
	removeTmp = false
	return syncDir(filepath.Dir(path))
}

func syncDir(path string) error {
	// Directory fsync is what makes a preceding rename durable on filesystems
	// that otherwise may persist file contents without persisting the directory
	// entry update.
	dir, err := os.Open(path)
	if err != nil {
		return xerrors.Errorf("opening directory %s for sync: %w", path, err)
	}
	defer func() {
		_ = dir.Close()
	}()
	if err := dir.Sync(); err != nil {
		return xerrors.Errorf("syncing directory %s: %w", path, err)
	}
	return nil
}
