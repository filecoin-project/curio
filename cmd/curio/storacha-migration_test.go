package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/market/mk20"
)

type storachaMigrationTestEnv struct {
	ctx        context.Context
	db         *harmonydb.DB
	si         paths.SectorIndex
	storageID  storiface.ID
	sourceDir  string
	targetDir  string
	stagingDir string
}

type storachaPieceFixture struct {
	cidV2    string
	fileName string
	info     *mk20.PieceInfo
	data     []byte
}

type storachaPieceFixtures struct {
	t    *testing.T
	seed uint64
}

type testParkedPiece struct {
	id       int64
	complete bool
	skip     bool
	longTerm bool
	taskID   sql.NullInt64
}

type testPDPRef struct {
	ID       int64  `db:"id"`
	Service  string `db:"service"`
	PieceCID string `db:"piece_cid"`
	PieceRef int64  `db:"piece_ref"`
}

func TestStorachaPieceInfoFromFileName(t *testing.T) {
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()

	pi, ok, err := storachaPieceInfoFromFileName(fx.fileName)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, fx.info.PieceCIDV1.String(), pi.PieceCIDV1.String())
	require.Equal(t, fx.info.Size, pi.Size)
	require.Equal(t, fx.info.RawSize, pi.RawSize)

	pi, ok, err = storachaPieceInfoFromFileName("not-a-car.txt")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, pi)

	pi, ok, err = storachaPieceInfoFromFileName("not-a-cid.car")
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, pi)

	var commP [32]byte
	pcidV1, err := commcid.DataCommitmentV1ToCID(commP[:])
	require.NoError(t, err)
	pi, ok, err = storachaPieceInfoFromFileName(pcidV1.String() + ".car")
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, pi)
}

func TestWriteImportPiecesResultIncludesError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	expected := importPiecesOutput{
		Count:  2,
		Pieces: []string{"piece-a", "piece-b"},
		Error:  "import failed",
	}

	require.NoError(t, writeImportPiecesResult(path, expected))

	mb, err := os.ReadFile(path)
	require.NoError(t, err)

	var actual importPiecesOutput
	require.NoError(t, json.Unmarshal(mb, &actual))
	require.Equal(t, expected, actual)
}

func TestStorachaMigrationFreshImport(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	writeSourcePiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)
	require.Equal(t, []string{fx.cidV2}, out.Pieces)
	require.NotEqual(t, []string{fx.info.PieceCIDV1.String()}, out.Pieces)

	pp := parkedPieceByCID(t, env, fx)
	require.True(t, pp.complete)
	require.True(t, pp.skip)
	require.True(t, pp.longTerm)
	require.False(t, pp.taskID.Valid)

	assertMissing(t, filepath.Join(env.sourceDir, fx.fileName))
	assertMissing(t, filepath.Join(env.stagingDir, fx.fileName))
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pp.id), fx.data)
	assertStorachaRefs(t, env, pp.id, 1)
	assertPDPRefs(t, env, pp.id, fx.info.PieceCIDV1.String(), 1)
	assertSectorLocation(t, env, pp.id, 1)
}

func TestStorachaMigrationExistingCompleteMovesStagedWhenFinalMissing(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, true, false, nil)
	writeStagedPiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.True(t, pp.complete)
	require.False(t, pp.skip)
	assertMissing(t, filepath.Join(env.stagingDir, fx.fileName))
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pieceID), fx.data)
	assertStorachaRefs(t, env, pieceID, 1)
	assertPDPRefs(t, env, pieceID, fx.info.PieceCIDV1.String(), 1)
	assertSectorLocation(t, env, pieceID, 1)
}

func TestStorachaMigrationExistingCompleteRemovesStagedDuplicateWhenFinalExists(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, true, false, nil)
	finalBytes := []byte("already-final")
	writeStagedPiece(t, env, fx)
	writeFinalPiece(t, env, pieceID, finalBytes)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)

	assertMissing(t, filepath.Join(env.stagingDir, fx.fileName))
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pieceID), finalBytes)
	assertStorachaRefs(t, env, pieceID, 1)
	assertPDPRefs(t, env, pieceID, fx.info.PieceCIDV1.String(), 1)
	assertSectorLocation(t, env, pieceID, 1)
}

func TestStorachaMigrationClaimsIncompleteParkedPieceWithNoTask(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, false, nil)
	writeStagedPiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.True(t, pp.complete)
	require.True(t, pp.skip)
	require.False(t, pp.taskID.Valid)
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pieceID), fx.data)
	assertStorachaRefs(t, env, pieceID, 1)
	assertPDPRefs(t, env, pieceID, fx.info.PieceCIDV1.String(), 1)
	assertSectorLocation(t, env, pieceID, 1)
}

func TestStorachaMigrationClaimsIncompleteParkedPieceWithStaleTask(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	staleTaskID := int64(999999)
	pieceID := insertParkedPiece(t, env, fx, false, false, &staleTaskID)
	writeStagedPiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.True(t, pp.complete)
	require.True(t, pp.skip)
	require.False(t, pp.taskID.Valid)
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pieceID), fx.data)
	assertSectorLocation(t, env, pieceID, 1)
}

func TestStorachaMigrationLeavesLiveTaskPieceAlone(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	taskID := insertHarmonyTask(t, env, "live-park-piece")
	pieceID := insertParkedPiece(t, env, fx, false, false, &taskID)
	writeStagedPiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 0, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	require.False(t, pp.skip)
	require.True(t, pp.taskID.Valid)
	require.Equal(t, taskID, pp.taskID.Int64)
	assertFileBytes(t, filepath.Join(env.stagingDir, fx.fileName), fx.data)
	assertStorachaRefs(t, env, pieceID, 0)
	assertSectorLocation(t, env, pieceID, 0)
}

func TestStorachaMigrationReusesExistingRefs(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, false, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	pdpID := insertPDPRef(t, env, refID, serviceName, fx.info.PieceCIDV1.String())
	writeStagedPiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)

	refs := storachaRefIDs(t, env, pieceID)
	require.Equal(t, []int64{refID}, refs)
	pdpRefs := pdpRefsForPiece(t, env, pieceID)
	require.Len(t, pdpRefs, 1)
	require.Equal(t, pdpID, pdpRefs[0].ID)
}

func TestStorachaMigrationInsertsMissingPDPRefForExistingParkedRef(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, false, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	writeStagedPiece(t, env, fx)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)
	require.Equal(t, []int64{refID}, storachaRefIDs(t, env, pieceID))
	assertPDPRefs(t, env, pieceID, fx.info.PieceCIDV1.String(), 1)
}

func TestStorachaMigrationDuplicateStorachaRefsErrors(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, false, nil)
	insertStorachaParkedRef(t, env, pieceID)
	insertStorachaParkedRef(t, env, pieceID)
	writeStagedPiece(t, env, fx)

	_, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.ErrorContains(t, err, "expected at most 1")

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	assertFileBytes(t, filepath.Join(env.stagingDir, fx.fileName), fx.data)
	assertSectorLocation(t, env, pieceID, 0)
}

func TestStorachaMigrationMismatchedPDPRefErrors(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, false, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	insertPDPRef(t, env, refID, serviceName, "wrong-piece-cid")
	writeStagedPiece(t, env, fx)

	_, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.ErrorContains(t, err, "does not match storacha migration piece")

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	assertFileBytes(t, filepath.Join(env.stagingDir, fx.fileName), fx.data)
	assertSectorLocation(t, env, pieceID, 0)
}

func TestStorachaMigrationRecoversFinalFile(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, true, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	insertPDPRef(t, env, refID, serviceName, fx.info.PieceCIDV1.String())
	writeFinalPiece(t, env, pieceID, fx.data)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.True(t, pp.complete)
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pieceID), fx.data)
	assertStorachaRefs(t, env, pieceID, 1)
	assertPDPRefs(t, env, pieceID, fx.info.PieceCIDV1.String(), 1)
	assertSectorLocation(t, env, pieceID, 1)
}

func TestStorachaMigrationFinalRecoveryMissingFileNoops(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, true, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	insertPDPRef(t, env, refID, serviceName, fx.info.PieceCIDV1.String())

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 0, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	assertSectorLocation(t, env, pieceID, 0)
}

func TestStorachaMigrationFinalRecoveryDirectoryErrors(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, true, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	insertPDPRef(t, env, refID, serviceName, fx.info.PieceCIDV1.String())
	require.NoError(t, os.MkdirAll(storachaFinalPiecePath(env.targetDir, pieceID), 0755))

	_, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.ErrorContains(t, err, "final piece path is a directory")

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	assertSectorLocation(t, env, pieceID, 0)
}

func TestStorachaMigrationFinalRecoveryLeavesLiveTaskPieceAlone(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	taskID := insertHarmonyTask(t, env, "live-final")
	pieceID := insertParkedPiece(t, env, fx, false, true, &taskID)
	refID := insertStorachaParkedRef(t, env, pieceID)
	insertPDPRef(t, env, refID, serviceName, fx.info.PieceCIDV1.String())
	writeFinalPiece(t, env, pieceID, fx.data)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.NoError(t, err)
	require.Equal(t, 0, out.Count)

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	require.True(t, pp.taskID.Valid)
	require.Equal(t, taskID, pp.taskID.Int64)
	assertSectorLocation(t, env, pieceID, 0)
}

func TestStorachaMigrationBatchProcessesStagingBeforeSource(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	staged := pieces.next()
	source := pieces.next()
	writeStagedPiece(t, env, staged)
	writeSourcePiece(t, env, source)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 1)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)
	require.Equal(t, []string{staged.cidV2}, out.Pieces)

	stagedPP := parkedPieceByCID(t, env, staged)
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, stagedPP.id), staged.data)
	assertFileBytes(t, filepath.Join(env.sourceDir, source.fileName), source.data)
	assertMissing(t, filepath.Join(env.stagingDir, staged.fileName))
}

func TestStorachaMigrationBatchProcessesFinalRecoveryBeforeSource(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	recovered := pieces.next()
	source := pieces.next()
	pieceID := insertParkedPiece(t, env, recovered, false, true, nil)
	refID := insertStorachaParkedRef(t, env, pieceID)
	insertPDPRef(t, env, refID, serviceName, recovered.info.PieceCIDV1.String())
	writeFinalPiece(t, env, pieceID, recovered.data)
	writeSourcePiece(t, env, source)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 1)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)
	require.Equal(t, []string{recovered.cidV2}, out.Pieces)

	pp := parkedPieceByID(t, env, pieceID)
	require.True(t, pp.complete)
	assertFileBytes(t, filepath.Join(env.sourceDir, source.fileName), source.data)
}

func TestStorachaMigrationInvalidStagedFileDoesNotConsumeBatch(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	source := pieces.next()
	invalidStagedPath := filepath.Join(env.stagingDir, "not-a-cid.car")
	writeFile(t, invalidStagedPath, []byte("invalid staged input"))
	writeSourcePiece(t, env, source)

	out, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 1)
	require.NoError(t, err)
	require.Equal(t, 1, out.Count)
	require.Equal(t, []string{source.cidV2}, out.Pieces)

	pp := parkedPieceByCID(t, env, source)
	assertFileBytes(t, storachaFinalPiecePath(env.targetDir, pp.id), source.data)
	assertFileBytes(t, invalidStagedPath, []byte("invalid staged input"))
}

func TestStorachaMigrationMissingSourcePathErrors(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	missingSource := filepath.Join(filepath.Dir(env.sourceDir), "missing-source")

	_, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, missingSource, env.targetDir, 20)
	require.ErrorContains(t, err, "reading directory")
}

func TestStorachaMigrationSourceImportErrorsWhenStagingPathExists(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	source := pieces.next()
	writeSourcePiece(t, env, source)
	require.NoError(t, os.MkdirAll(filepath.Join(env.stagingDir, source.fileName), 0755))

	_, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.ErrorContains(t, err, "staging file already exists")
	assertFileBytes(t, filepath.Join(env.sourceDir, source.fileName), source.data)
}

func TestStorachaMigrationStagedImportFinalDirectoryErrors(t *testing.T) {
	env := setupStorachaMigrationTest(t)
	pieces := newStorachaPieceFixtures(t)
	fx := pieces.next()
	pieceID := insertParkedPiece(t, env, fx, false, false, nil)
	writeStagedPiece(t, env, fx)
	require.NoError(t, os.MkdirAll(storachaFinalPiecePath(env.targetDir, pieceID), 0755))

	_, err := runImportPieces(env.ctx, env.db, env.si, env.storageID, env.sourceDir, env.targetDir, 20)
	require.ErrorContains(t, err, "final piece path is a directory")

	pp := parkedPieceByID(t, env, pieceID)
	require.False(t, pp.complete)
	assertFileBytes(t, filepath.Join(env.stagingDir, fx.fileName), fx.data)
	assertSectorLocation(t, env, pieceID, 0)
}

func setupStorachaMigrationTest(t *testing.T) storachaMigrationTestEnv {
	t.Helper()

	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	storageDir := filepath.Join(root, "storage")
	targetDir := storageDir
	stagingDir := filepath.Join(targetDir, "storacha-staging")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, storiface.FTPiece.String()), 0755))

	storageID := storiface.ID(uuid.New().String())
	meta := &storiface.LocalStorageMeta{
		ID:       storageID,
		Weight:   10,
		CanSeal:  true,
		CanStore: true,
	}
	mb, err := json.MarshalIndent(meta, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storageDir, paths.MetaFile), mb, 0644))

	index := paths.NewDBIndex(nil, db)

	return storachaMigrationTestEnv{
		ctx:        ctx,
		db:         db,
		si:         index,
		storageID:  storageID,
		sourceDir:  sourceDir,
		targetDir:  targetDir,
		stagingDir: stagingDir,
	}
}

func newStorachaPieceFixtures(t *testing.T) *storachaPieceFixtures {
	t.Helper()
	return &storachaPieceFixtures{t: t}
}

func (f *storachaPieceFixtures) next() storachaPieceFixture {
	f.t.Helper()
	f.seed++
	return newStorachaPieceFixture(f.t, f.seed)
}

func newStorachaPieceFixture(t *testing.T, seed uint64) storachaPieceFixture {
	t.Helper()

	rawSize := uint64(127)
	var commP [32]byte
	binary.BigEndian.PutUint64(commP[:8], seed)
	binary.BigEndian.PutUint64(commP[8:16], rawSize)
	pcidV2, err := commcid.DataCommitmentToPieceCidv2(commP[:], rawSize)
	require.NoError(t, err)

	info, err := mk20.GetPieceInfo(pcidV2)
	require.NoError(t, err)

	return storachaPieceFixture{
		cidV2:    pcidV2.String(),
		fileName: pcidV2.String() + ".car",
		info:     info,
		data:     bytes.Repeat([]byte{byte(seed % 251)}, int(rawSize)),
	}
}

func writeSourcePiece(t *testing.T, env storachaMigrationTestEnv, fx storachaPieceFixture) {
	t.Helper()
	writeFile(t, filepath.Join(env.sourceDir, fx.fileName), fx.data)
}

func writeStagedPiece(t *testing.T, env storachaMigrationTestEnv, fx storachaPieceFixture) {
	t.Helper()
	writeFile(t, filepath.Join(env.stagingDir, fx.fileName), fx.data)
}

func writeFinalPiece(t *testing.T, env storachaMigrationTestEnv, pieceID int64, data []byte) {
	t.Helper()
	writeFile(t, storachaFinalPiecePath(env.targetDir, pieceID), data)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, data, 0644))
}

func assertFileBytes(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "expected %s to be missing, got %v", path, err)
}

func insertParkedPiece(t *testing.T, env storachaMigrationTestEnv, fx storachaPieceFixture, complete, skip bool, taskID *int64) int64 {
	t.Helper()

	var pieceID int64
	err := env.db.QueryRow(env.ctx, `
		INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term, skip, task_id)
		VALUES ($1, $2, $3, $4, TRUE, $5, $6)
		RETURNING id
	`, fx.info.PieceCIDV1.String(), int64(fx.info.Size), int64(fx.info.RawSize), complete, skip, taskID).Scan(&pieceID)
	require.NoError(t, err)
	return pieceID
}

func insertStorachaParkedRef(t *testing.T, env storachaMigrationTestEnv, pieceID int64) int64 {
	t.Helper()

	var refID int64
	err := env.db.QueryRow(env.ctx, `
		INSERT INTO parked_piece_refs (piece_id, data_url, long_term)
		VALUES ($1, $2, TRUE)
		RETURNING ref_id
	`, pieceID, storachaMigrationDataURL).Scan(&refID)
	require.NoError(t, err)
	return refID
}

func insertPDPRef(t *testing.T, env storachaMigrationTestEnv, refID int64, service, pieceCID string) int64 {
	t.Helper()

	var pdpID int64
	err := env.db.QueryRow(env.ctx, `
		INSERT INTO pdp_piecerefs (service, piece_cid, piece_ref, created_at, needs_save_cache)
		VALUES ($1, $2, $3, NOW(), FALSE)
		RETURNING id
	`, service, pieceCID, refID).Scan(&pdpID)
	require.NoError(t, err)
	return pdpID
}

func insertHarmonyTask(t *testing.T, env storachaMigrationTestEnv, name string) int64 {
	t.Helper()

	var taskID int64
	err := env.db.QueryRow(env.ctx, `
		INSERT INTO harmony_task (name, added_by, posted_time)
		VALUES ($1, 1, CURRENT_TIMESTAMP)
		RETURNING id
	`, name).Scan(&taskID)
	require.NoError(t, err)
	return taskID
}

func parkedPieceByCID(t *testing.T, env storachaMigrationTestEnv, fx storachaPieceFixture) testParkedPiece {
	t.Helper()

	var row testParkedPiece
	err := env.db.QueryRow(env.ctx, `
		SELECT id, complete, skip, long_term, task_id
		FROM parked_pieces
		WHERE piece_cid = $1
		  AND piece_padded_size = $2
		  AND long_term = TRUE
		  AND cleanup_task_id IS NULL
	`, fx.info.PieceCIDV1.String(), int64(fx.info.Size)).Scan(&row.id, &row.complete, &row.skip, &row.longTerm, &row.taskID)
	require.NoError(t, err)
	return row
}

func parkedPieceByID(t *testing.T, env storachaMigrationTestEnv, pieceID int64) testParkedPiece {
	t.Helper()

	var row testParkedPiece
	err := env.db.QueryRow(env.ctx, `
		SELECT id, complete, skip, long_term, task_id
		FROM parked_pieces
		WHERE id = $1
	`, pieceID).Scan(&row.id, &row.complete, &row.skip, &row.longTerm, &row.taskID)
	require.NoError(t, err)
	return row
}

func storachaRefIDs(t *testing.T, env storachaMigrationTestEnv, pieceID int64) []int64 {
	t.Helper()

	var rows []struct {
		RefID int64 `db:"ref_id"`
	}
	err := env.db.Select(env.ctx, &rows, `
		SELECT ref_id
		FROM parked_piece_refs
		WHERE piece_id = $1
		  AND data_url = $2
		  AND long_term = TRUE
		ORDER BY ref_id
	`, pieceID, storachaMigrationDataURL)
	require.NoError(t, err)

	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.RefID)
	}
	return out
}

func pdpRefsForPiece(t *testing.T, env storachaMigrationTestEnv, pieceID int64) []testPDPRef {
	t.Helper()

	var rows []testPDPRef
	err := env.db.Select(env.ctx, &rows, `
		SELECT p.id, p.service, p.piece_cid, p.piece_ref
		FROM pdp_piecerefs p
		JOIN parked_piece_refs r ON r.ref_id = p.piece_ref
		WHERE r.piece_id = $1
		ORDER BY p.id
	`, pieceID)
	require.NoError(t, err)
	return rows
}

func assertStorachaRefs(t *testing.T, env storachaMigrationTestEnv, pieceID int64, expected int) {
	t.Helper()
	require.Len(t, storachaRefIDs(t, env, pieceID), expected)
}

func assertPDPRefs(t *testing.T, env storachaMigrationTestEnv, pieceID int64, pieceCID string, expected int) {
	t.Helper()
	refs := pdpRefsForPiece(t, env, pieceID)
	require.Len(t, refs, expected)
	for _, ref := range refs {
		require.Equal(t, serviceName, ref.Service)
		require.Equal(t, pieceCID, ref.PieceCID)
	}
}

func assertSectorLocation(t *testing.T, env storachaMigrationTestEnv, pieceID int64, expected int) {
	t.Helper()

	var count int
	err := env.db.QueryRow(env.ctx, `
		SELECT COUNT(*)
		FROM sector_location
		WHERE miner_id = 0
		  AND sector_num = $1
		  AND sector_filetype = $2
		  AND storage_id = $3
	`, pieceID, int(storiface.FTPiece), string(env.storageID)).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, expected, count)
}
