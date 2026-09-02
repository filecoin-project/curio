package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/market/mk20"
)

type storachaAggregateTestPiece struct {
	storachaPieceFixture
	parkedID int64
}

func TestStorachaAggregateDedupeAndCompletedMarkerResume(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2, pieces[0].cidV2)

	workDir, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.NoError(t, err)

	manifest := readStorachaAggregateManifestForTest(t, workDir)
	require.Equal(t, int64(3), manifest.RawBuckets.Records)
	require.Equal(t, int64(2), manifest.DedupedBuckets.Records)
	require.Equal(t, int64(2), manifest.Groups.Pieces)

	group := readStorachaAggregateGroupForTest(t, workDir, 0)
	require.Len(t, group.Pieces, 2)
	complete := assertStorachaAggregateImported(t, env, workDir, group)

	// The completion marker is the resume boundary after a group is fully
	// imported. Remove the source subpieces to prove a rerun trusts the marker
	// instead of trying to read source bytes again.
	for _, piece := range pieces {
		require.NoError(t, os.Remove(storachaFinalPiecePath(env.sourceDir, piece.parkedID)))
	}

	_, err = runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.NoError(t, err)
	require.Equal(t, complete, readStorachaAggregateCompletionForTest(t, workDir, group))
}

func TestStorachaAggregateRawBucketCursorTruncatesTornTail(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	inputSHA256, _, err := hashFileSHA256(inputPath)
	require.NoError(t, err)

	workDir := filepath.Join(env.targetDir, storachaAggregateWorkDir, inputSHA256)
	rawDir := filepath.Join(workDir, storachaAggregateRawBucketDir)
	require.NoError(t, os.MkdirAll(rawDir, 0755))

	firstRecord := storachaAggregateBucketRecordForTest(pieces[0])
	firstRecordLine, err := json.Marshal(firstRecord)
	require.NoError(t, err)
	firstRecordLine = append(firstRecordLine, '\n')

	rawBucketName := storachaAggregateBucketFileName(abi.PaddedPieceSize(pieces[0].info.Size))
	rawBucketPath := filepath.Join(rawDir, rawBucketName)
	writeFile(t, rawBucketPath, append(firstRecordLine, []byte(`{"piece_cid_v2":"torn"`)...))
	require.NoError(t, writeJSONFileAtomic(filepath.Join(workDir, storachaAggregateBucketCursor), storachaAggregateBucketCursorState{
		InputSHA256: inputSHA256,
		InputOffset: int64(len(pieces[0].cidV2) + 1),
		InputLine:   1,
		Records:     1,
		BucketOffsets: map[string]int64{
			rawBucketName: int64(len(firstRecordLine)),
		},
	}))

	actualWorkDir, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.NoError(t, err)
	require.Equal(t, workDir, actualWorkDir)

	rawBytes, err := os.ReadFile(rawBucketPath)
	require.NoError(t, err)
	require.NotContains(t, string(rawBytes), "torn")

	manifest := readStorachaAggregateManifestForTest(t, workDir)
	require.Equal(t, int64(2), manifest.RawBuckets.Records)
	require.Equal(t, int64(2), manifest.DedupedBuckets.Records)
	assertStorachaAggregateImported(t, env, workDir, readStorachaAggregateGroupForTest(t, workDir, 0))
}

func TestStorachaAggregateIgnoresExistingTempFile(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	workDir, group := prepareStorachaAggregateGroupsForTest(t, env, inputPath)

	tempPath := storachaAggregateTempFilePath(workDir, group.GroupID)
	writeFile(t, tempPath, []byte("leftover temp data"))

	_, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.NoError(t, err)

	assertMissing(t, tempPath)
	assertStorachaAggregateImported(t, env, workDir, group)
}

func TestStorachaAggregateRecoversFromStagingFile(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	workDir, group := prepareStorachaAggregateGroupsForTest(t, env, inputPath)

	tempPath, verified := createStorachaAggregateTempFileForTest(t, env, workDir, group)
	stagingDir := storachaAggregateStagingDir(env.targetDir)
	require.NoError(t, os.MkdirAll(stagingDir, 0755))
	stagingPath := storachaAggregateStagingPath(env.targetDir, verified.PieceCIDV2)
	require.NoError(t, os.Rename(tempPath, stagingPath))
	require.NoError(t, syncDir(stagingDir))
	require.NoError(t, writeStorachaAggregateStagedMarker(env.targetDir, verified.PieceCIDV2))

	_, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.NoError(t, err)

	assertMissing(t, stagingPath)
	assertMissing(t, storachaAggregateStagedMarkerPath(env.targetDir, verified.PieceCIDV2))
	complete := assertStorachaAggregateImported(t, env, workDir, group)
	require.Equal(t, verified.PieceCIDV2.String(), complete.PieceCID)
}

func TestStorachaAggregateRecoversFromFinalFileAndFixesDB(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	workDir, group := prepareStorachaAggregateGroupsForTest(t, env, inputPath)

	tempPath, verified := createStorachaAggregateTempFileForTest(t, env, workDir, group)
	pieceID := insertAggregateParkedPiece(t, env, verified, false)
	assertStorachaRefs(t, env, pieceID, 0)
	assertStorachaRefsWithDataURL(t, env, pieceID, storachaMigrationAggregateDataURL, 0)
	assertSectorLocation(t, env, pieceID, 0)

	finalPath := storachaFinalPiecePath(env.targetDir, pieceID)
	require.NoError(t, os.Rename(tempPath, finalPath))

	_, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.NoError(t, err)

	pp := parkedPieceByID(t, env, pieceID)
	require.True(t, pp.complete)
	assertStorachaRefs(t, env, pieceID, 0)
	assertStorachaRefsWithDataURL(t, env, pieceID, storachaMigrationAggregateDataURL, 1)
	assertPDPRefs(t, env, pieceID, group.PieceCIDV1, 0)
	assertSectorLocation(t, env, pieceID, 1)

	complete := assertStorachaAggregateImported(t, env, workDir, group)
	require.Equal(t, verified.PieceCIDV2.String(), complete.PieceCID)
}

func TestStorachaAggregateUncommittedStagingRequiresManualCleanup(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	workDir, group := prepareStorachaAggregateGroupsForTest(t, env, inputPath)

	tempPath, verified := createStorachaAggregateTempFileForTest(t, env, workDir, group)
	require.NoError(t, os.Remove(tempPath))

	stagingDir := storachaAggregateStagingDir(env.targetDir)
	require.NoError(t, os.MkdirAll(stagingDir, 0755))
	stagingPath := storachaAggregateStagingPath(env.targetDir, verified.PieceCIDV2)
	writeFile(t, stagingPath, bytes.Repeat([]byte{0x7f}, int(verified.RawSize)))

	_, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.ErrorContains(t, err, "uncommitted aggregate staging file already exists")
	require.ErrorContains(t, err, "cleanup manually")
	assertMissing(t, storachaAggregateCompletionPath(workDir, group.GroupID))
}

func TestStorachaAggregateStagedMarkerMissingFileRequiresManualCleanup(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	workDir, group := prepareStorachaAggregateGroupsForTest(t, env, inputPath)

	tempPath, verified := createStorachaAggregateTempFileForTest(t, env, workDir, group)
	require.NoError(t, os.Remove(tempPath))
	require.NoError(t, writeStorachaAggregateStagedMarker(env.targetDir, verified.PieceCIDV2))

	_, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.ErrorContains(t, err, "staged marker")
	require.ErrorContains(t, err, "is missing")
	require.ErrorContains(t, err, "cleanup manually")
	assertMissing(t, storachaAggregateCompletionPath(workDir, group.GroupID))
}

func TestStorachaAggregateFinalSizeMismatchRequiresManualCleanup(t *testing.T) {
	withSmallStorachaAggregateSize(t)

	env := setupStorachaMigrationTest(t)
	pieces := prepareStorachaAggregateSourcePieces(t, env, 2)
	inputPath := writeStorachaAggregateInput(t, pieces[0].cidV2, pieces[1].cidV2)
	workDir, group := prepareStorachaAggregateGroupsForTest(t, env, inputPath)

	tempPath, verified := createStorachaAggregateTempFileForTest(t, env, workDir, group)
	require.NoError(t, os.Remove(tempPath))

	pieceID := insertAggregateParkedPiece(t, env, verified, false)
	writeFile(t, storachaFinalPiecePath(env.targetDir, pieceID), bytes.Repeat([]byte{0x42}, int(verified.RawSize)-1))

	_, err := runAggregatePieces(env.ctx, env.db, env.si, env.storageID, storachaAggregateSortPath(t), inputPath, env.sourceDir, env.targetDir)
	require.ErrorContains(t, err, "size mismatch")
	require.ErrorContains(t, err, "cleanup manually")
	assertMissing(t, storachaAggregateCompletionPath(workDir, group.GroupID))
	require.False(t, parkedPieceByID(t, env, pieceID).complete)
}

func withSmallStorachaAggregateSize(t *testing.T) {
	t.Helper()

	previous := storachaAggregatePieceSize
	storachaAggregatePieceSize = abi.PaddedPieceSize(4 << 20)
	t.Cleanup(func() {
		storachaAggregatePieceSize = previous
	})
}

func prepareStorachaAggregateSourcePieces(t *testing.T, env storachaMigrationTestEnv, count int) []storachaAggregateTestPiece {
	t.Helper()

	pieces := make([]storachaAggregateTestPiece, 0, count)
	for i := 0; i < count; i++ {
		fx := newValidStorachaAggregatePieceFixture(t, byte(i+1))
		pieceID := insertParkedPiece(t, env, fx, true, true, nil)
		writeFile(t, storachaFinalPiecePath(env.sourceDir, pieceID), fx.data)
		pieces = append(pieces, storachaAggregateTestPiece{
			storachaPieceFixture: fx,
			parkedID:             pieceID,
		})
	}
	return pieces
}

func newValidStorachaAggregatePieceFixture(t *testing.T, seed byte) storachaPieceFixture {
	t.Helper()

	data := bytes.Repeat([]byte{seed}, 127)
	cp := new(commp.Calc)
	defer cp.Reset()
	n, err := io.Copy(cp, bytes.NewReader(data))
	require.NoError(t, err)

	digest, paddedSize, err := cp.Digest()
	require.NoError(t, err)

	pcidV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
	require.NoError(t, err)

	info, err := mk20.GetPieceInfo(pcidV2)
	require.NoError(t, err)
	require.Equal(t, uint64(n), info.RawSize)
	require.Equal(t, abi.PaddedPieceSize(paddedSize), info.Size)

	return storachaPieceFixture{
		cidV2:    pcidV2.String(),
		fileName: pcidV2.String() + ".car",
		info:     info,
		data:     data,
	}
}

func writeStorachaAggregateInput(t *testing.T, pieceCIDs ...string) string {
	t.Helper()

	inputPath := filepath.Join(t.TempDir(), "aggregate-input.txt")
	writeFile(t, inputPath, []byte(strings.Join(pieceCIDs, "\n")+"\n"))
	return inputPath
}

func storachaAggregateSortPath(t *testing.T) string {
	t.Helper()

	sortPath, err := exec.LookPath("sort")
	require.NoError(t, err)
	return sortPath
}

func prepareStorachaAggregateGroupsForTest(t *testing.T, env storachaMigrationTestEnv, inputPath string) (string, storachaAggregateGroup) {
	t.Helper()

	inputSHA256, inputSize, err := hashFileSHA256(inputPath)
	require.NoError(t, err)

	workDir := filepath.Join(env.targetDir, storachaAggregateWorkDir, inputSHA256)
	require.NoError(t, os.MkdirAll(workDir, 0755))

	manifest, err := loadStorachaAggregateManifest(workDir, inputSHA256, inputSize)
	require.NoError(t, err)
	require.NoError(t, buildRawStorachaAggregateBuckets(env.ctx, env.db, inputPath, inputSHA256, workDir, manifest))
	require.NoError(t, dedupeStorachaAggregateBuckets(env.ctx, storachaAggregateSortPath(t), workDir, manifest))
	require.NoError(t, buildStorachaAggregateGroups(workDir, manifest))

	return workDir, readStorachaAggregateGroupForTest(t, workDir, 0)
}

func createStorachaAggregateTempFileForTest(t *testing.T, env storachaMigrationTestEnv, workDir string, group storachaAggregateGroup) (string, storachaVerifiedAggregatePiece) {
	t.Helper()

	expectedCIDV1, err := cid.Parse(group.PieceCIDV1)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, storachaAggregateTempDir), 0755))

	tempPath, verified, err := createStorachaAggregateTempFile(env.sourceDir, workDir, group, expectedCIDV1, abi.PaddedPieceSize(group.PaddedSize))
	require.NoError(t, err)
	require.Equal(t, group.PieceCIDV1, verified.PieceCIDV1.String())
	require.Equal(t, abi.PaddedPieceSize(group.PaddedSize), verified.PaddedSize)
	return tempPath, verified
}

func readStorachaAggregateManifestForTest(t *testing.T, workDir string) storachaAggregateWorkManifest {
	t.Helper()

	mb, err := os.ReadFile(filepath.Join(workDir, storachaAggregateManifestFile))
	require.NoError(t, err)

	var manifest storachaAggregateWorkManifest
	require.NoError(t, json.Unmarshal(mb, &manifest))
	return manifest
}

func readStorachaAggregateGroupForTest(t *testing.T, workDir string, groupID int64) storachaAggregateGroup {
	t.Helper()

	file, err := os.Open(filepath.Join(workDir, storachaAggregateGroupsFile))
	require.NoError(t, err)
	defer func() {
		_ = file.Close()
	}()

	reader := bufio.NewReader(file)
	for {
		line, ok, err := readJSONLLine(reader)
		require.NoError(t, err)
		require.True(t, ok, "group %d not found", groupID)

		var group storachaAggregateGroup
		require.NoError(t, json.Unmarshal(line, &group))
		if group.GroupID == groupID {
			return group
		}
	}
}

func readStorachaAggregateCompletionForTest(t *testing.T, workDir string, group storachaAggregateGroup) storachaAggregateCompletion {
	t.Helper()

	complete, ok, err := readCompletedStorachaAggregateGroup(workDir, group)
	require.NoError(t, err)
	require.True(t, ok)
	return complete
}

func assertStorachaAggregateImported(t *testing.T, env storachaMigrationTestEnv, workDir string, group storachaAggregateGroup) storachaAggregateCompletion {
	t.Helper()

	complete := readStorachaAggregateCompletionForTest(t, workDir, group)
	pcidV2, err := cid.Parse(complete.PieceCID)
	require.NoError(t, err)
	pcidV1, rawSize, err := commcid.PieceCidV1FromV2(pcidV2)
	require.NoError(t, err)
	require.Equal(t, group.PieceCIDV1, pcidV1.String())
	require.Equal(t, complete.RawSize, rawSize)

	var rows []storachaFinalRecoveryRow
	err = env.db.Select(env.ctx, &rows, `
		SELECT id, piece_cid, piece_padded_size, piece_raw_size
		FROM parked_pieces
		WHERE piece_cid = $1
		  AND piece_padded_size = $2
		  AND piece_raw_size = $3
		  AND long_term = TRUE
		  AND cleanup_task_id IS NULL
		ORDER BY id`, group.PieceCIDV1, int64(group.PaddedSize), int64(complete.RawSize))
	require.NoError(t, err)
	require.Len(t, rows, 1)

	pp := parkedPieceByID(t, env, rows[0].ID)
	require.True(t, pp.complete)
	assertStorachaRefs(t, env, rows[0].ID, 0)
	assertStorachaRefsWithDataURL(t, env, rows[0].ID, storachaMigrationAggregateDataURL, 1)
	assertPDPRefs(t, env, rows[0].ID, group.PieceCIDV1, 0)
	assertSectorLocation(t, env, rows[0].ID, 1)

	_, err = verifyStorachaAggregateFileForTest(storachaFinalPiecePath(env.targetDir, rows[0].ID), pcidV1, abi.PaddedPieceSize(group.PaddedSize))
	require.NoError(t, err)
	return complete
}

func verifyStorachaAggregateFileForTest(path string, expectedCIDV1 cid.Cid, expectedPaddedSize abi.PaddedPieceSize) (storachaVerifiedAggregatePiece, error) {
	file, err := os.Open(path)
	if err != nil {
		return storachaVerifiedAggregatePiece{}, err
	}
	defer func() {
		_ = file.Close()
	}()

	info, err := file.Stat()
	if err != nil {
		return storachaVerifiedAggregatePiece{}, err
	}
	if info.IsDir() {
		return storachaVerifiedAggregatePiece{}, os.ErrInvalid
	}

	cp := new(commp.Calc)
	defer cp.Reset()
	n, err := io.Copy(cp, file)
	if err != nil {
		return storachaVerifiedAggregatePiece{}, err
	}

	digest, actualPaddedSize, err := cp.Digest()
	if err != nil {
		return storachaVerifiedAggregatePiece{}, err
	}
	if abi.PaddedPieceSize(actualPaddedSize) != expectedPaddedSize {
		return storachaVerifiedAggregatePiece{}, os.ErrInvalid
	}

	actualCIDV1, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		return storachaVerifiedAggregatePiece{}, err
	}
	if !actualCIDV1.Equals(expectedCIDV1) {
		return storachaVerifiedAggregatePiece{}, os.ErrInvalid
	}

	actualCIDV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
	if err != nil {
		return storachaVerifiedAggregatePiece{}, err
	}

	return storachaVerifiedAggregatePiece{
		PieceCIDV2: actualCIDV2,
		PieceCIDV1: actualCIDV1,
		RawSize:    uint64(n),
		PaddedSize: abi.PaddedPieceSize(actualPaddedSize),
	}, nil
}

func insertAggregateParkedPiece(t *testing.T, env storachaMigrationTestEnv, verified storachaVerifiedAggregatePiece, complete bool) int64 {
	t.Helper()

	var pieceID int64
	err := env.db.QueryRow(env.ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term, skip)
		VALUES ($1, $2, $3, $4, TRUE, TRUE)
		RETURNING id
	`, verified.PieceCIDV1.String(), int64(verified.PaddedSize), int64(verified.RawSize), complete).Scan(&pieceID)
	require.NoError(t, err)
	return pieceID
}

func storachaAggregateBucketRecordForTest(piece storachaAggregateTestPiece) storachaBucketRecord {
	return storachaBucketRecord{
		PieceCIDV2: piece.cidV2,
		PieceID:    piece.parkedID,
		PieceCIDV1: piece.info.PieceCIDV1.String(),
		RawSize:    piece.info.RawSize,
		PaddedSize: uint64(piece.info.Size),
	}
}
