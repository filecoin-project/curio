package ffi

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/supraffi"
	"github.com/filecoin-project/curio/lib/tarutil"

	"github.com/filecoin-project/lotus/storage/sealer/commitment"
)

// CheckSealedCommR verifies a sealed sector's CommR by recomputing tree-r from the sealed file.
// It uses supraseal's GPU-accelerated tree-r computation.
// Returns the computed CommR CID.
func (sb *SealCalls) CheckSealedCommR(ctx context.Context, s storiface.SectorRef) (cid.Cid, error) {
	return sb.checkCommR(ctx, s, storiface.FTSealed, storiface.FTCache, false)
}

// CheckUpdateCommR verifies a snap-updated sector's CommR by recomputing tree-r from the update file.
// It uses supraseal's GPU-accelerated tree-r computation.
// Returns the computed CommR CID.
func (sb *SealCalls) CheckUpdateCommR(ctx context.Context, s storiface.SectorRef) (cid.Cid, error) {
	return sb.checkCommR(ctx, s, storiface.FTUpdate, storiface.FTUpdateCache, true)
}

func (sb *SealCalls) checkCommR(ctx context.Context, s storiface.SectorRef, sealedType, cacheType storiface.SectorFileType, isUpdate bool) (cid.Cid, error) {
	ssize, err := s.ProofType.SectorSize()
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting sector size: %w", err)
	}

	startTime := time.Now() //nolint:staticcheck

	// Get sealed file path (local or streamed to temp)
	sealedPath, sealedCleanup, err := sb.getSectorFilePath(ctx, s, sealedType, uint64(ssize))
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting sealed file path: %w", err)
	}
	defer sealedCleanup()

	// Get cache path for reading p_aux (CommC)
	cachePath, cacheCleanup, err := sb.getCachePath(ctx, s, cacheType)
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting cache path: %w", err)
	}
	defer cacheCleanup()

	// Read original CommC from cache p_aux (we need it to compute full CommR)
	origCommC, _, err := proof.ReadPAux(cachePath) //nolint:staticcheck
	if err != nil {
		return cid.Undef, xerrors.Errorf("reading original p_aux: %w", err)
	}

	// Create temp directory for tree output
	tmpDir, err := os.MkdirTemp("", "commr-check-*")
	if err != nil {
		return cid.Undef, xerrors.Errorf("creating temp directory: %w", err)
	}
	defer func(path string) {
		_ = os.RemoveAll(path)
	}(tmpDir)

	// Run TreeR on the sealed file
	// For sealed sectors, we pass empty string for data_filename (CC semantics - zeros)
	res := supraffi.TreeRFile(sealedPath /* data_filename: empty => CC semantics (zeros) */, "", tmpDir, uint64(ssize))
	if res != 0 {
		if res == -1 {
			return cid.Undef, xerrors.Errorf("supraseal not available (missing CPU/GPU features)")
		}
		return cid.Undef, xerrors.Errorf("tree_r_file failed with code %d", res)
	}

	// Read computed comm_r_last from generated tree files
	computedCommRLast := make([]byte, 32)
	if !supraffi.GetCommRLastFromTree(computedCommRLast, tmpDir, uint64(ssize)) {
		return cid.Undef, xerrors.Errorf("failed to read comm_r_last from tree files")
	}

	// Compute full CommR = H(CommC, CommRLast)
	var commRLastArr [32]byte
	copy(commRLastArr[:], computedCommRLast)

	computedCommR, err := commitment.CommR(origCommC, commRLastArr)
	if err != nil {
		return cid.Undef, xerrors.Errorf("computing CommR: %w", err)
	}

	// Convert to CID
	commRCID, err := commcid.ReplicaCommitmentV1ToCID(computedCommR[:])
	if err != nil {
		return cid.Undef, xerrors.Errorf("converting CommR to CID: %w", err)
	}

	log.Infow("computed CommR", "cid", commRCID, "sector", s.ID, "duration", time.Since(startTime), "isUpdate", isUpdate)

	return commRCID, nil
}

// getSectorFilePath returns a local path to the sector file. If the file is not available
// locally, it streams from remote storage to a temp file.
func (sb *SealCalls) getSectorFilePath(ctx context.Context, s storiface.SectorRef, ft storiface.SectorFileType, ssize uint64) (path string, cleanup func(), err error) {
	// First try to find the file locally
	paths, _, err := sb.Sectors.localStore.AcquireSector(ctx, s, ft, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err == nil {
		localPath := storiface.PathByType(paths, ft)
		if localPath != "" {
			// File is available locally
			return localPath, func() {}, nil
		}
	}

	// File not available locally, need to stream from remote
	reader, err := sb.Sectors.storage.ReaderSeq(ctx, s, ft)
	if err != nil {
		return "", nil, xerrors.Errorf("getting reader for %s: %w", ft, err)
	}

	// Create temp file to store the streamed data
	tmpFile, err := os.CreateTemp("", "sector-*")
	if err != nil {
		_ = reader.Close()
		return "", nil, xerrors.Errorf("creating temp file: %w", err)
	}

	tmpPath := tmpFile.Name()
	cleanup = func() {
		_ = os.Remove(tmpPath)
	}

	// Copy from reader to temp file
	log.Infow("streaming sector file from remote", "sector", s.ID, "type", ft, "dest", tmpPath)
	written, err := io.Copy(tmpFile, reader)
	_ = reader.Close()
	_ = tmpFile.Close()

	if err != nil {
		cleanup()
		return "", nil, xerrors.Errorf("copying sector file: %w", err)
	}

	expectedSize := int64(ssize)
	if written != expectedSize {
		cleanup()
		return "", nil, xerrors.Errorf("incomplete sector file: got %d bytes, expected %d", written, expectedSize)
	}

	return tmpPath, cleanup, nil
}

// getCachePath returns a local path to the cache directory containing p_aux.
// If not available locally, it fetches the minimal cache from remote and extracts to a temp dir.
func (sb *SealCalls) getCachePath(ctx context.Context, s storiface.SectorRef, ft storiface.SectorFileType) (path string, cleanup func(), err error) {
	// First try to find the cache locally
	paths, _, err := sb.Sectors.localStore.AcquireSector(ctx, s, ft, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err == nil {
		localPath := storiface.PathByType(paths, ft)
		if localPath != "" {
			// Cache is available locally
			return localPath, func() {}, nil
		}
	}

	// Cache not available locally, fetch minimal cache from remote
	// This is similar to how SnapEncode fetches cache for sector key preparation
	var buf bytes.Buffer
	err = sb.Sectors.storage.ReadMinCacheInto(ctx, s, ft, &buf)
	if err != nil {
		return "", nil, xerrors.Errorf("reading minimal cache from remote: %w", err)
	}

	// Create temp directory to extract cache
	tmpDir, err := os.MkdirTemp("", "cache-*")
	if err != nil {
		return "", nil, xerrors.Errorf("creating temp cache directory: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tmpDir)
	}

	// Extract the tar archive
	_, err = tarutil.ExtractTar(tarutil.FinCacheFileConstraints, &buf, tmpDir, make([]byte, 1<<20))
	if err != nil {
		cleanup()
		return "", nil, xerrors.Errorf("extracting cache tar: %w", err)
	}

	log.Infow("fetched minimal cache from remote", "sector", s.ID, "type", ft, "dest", tmpDir)

	return tmpDir, cleanup, nil
}
