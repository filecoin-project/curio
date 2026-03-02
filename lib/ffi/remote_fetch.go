package ffi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/tarutil"
)

// RemoteSealFetchParams describes everything needed to download sealed sector
// data and cache from a remote seal provider.
type RemoteSealFetchParams struct {
	// Full URL to download the sealed sector file (GET, supports Range).
	SealedURL string

	// Full URL to download the cache tar (GET, returns tar stream).
	CacheURL string

	// C1 metadata to write into the cache directory for PoRep.
	C1Info paths.RemoteSealC1Info

	SpID         int64
	SectorNumber int64
}

// DownloadRemoteSealData acquires storage for a sector's sealed file and cache,
// downloads them from the remote seal provider, writes the c1.url metadata file,
// and ensures only one copy of the data exists in the storage system.
func (sb *SealCalls) DownloadRemoteSealData(ctx context.Context, task *harmonytask.TaskID, sector storiface.SectorRef, params RemoteSealFetchParams) error {
	fspaths, pathIDs, release, err := sb.Sectors.AcquireSector(ctx, task, sector, storiface.FTNone, storiface.FTSealed|storiface.FTCache, storiface.PathStorage)
	if err != nil {
		return xerrors.Errorf("acquiring sector storage: %w", err)
	}
	defer release()

	if fspaths.Sealed == "" {
		return xerrors.Errorf("no sealed path allocated")
	}
	if fspaths.Cache == "" {
		return xerrors.Errorf("no cache path allocated")
	}

	// Download sealed sector file (32 GiB)
	log.Infow("downloading sealed file from provider",
		"sp_id", params.SpID, "sector", params.SectorNumber,
		"sealed_path", fspaths.Sealed)

	if err := fetchFile(ctx, fspaths.Sealed, params.SealedURL, params.SpID, params.SectorNumber); err != nil {
		return xerrors.Errorf("fetching sealed data: %w", err)
	}

	// Download and extract cache tar (p_aux, t_aux, tree-r-last-*)
	log.Infow("downloading cache data from provider",
		"sp_id", params.SpID, "sector", params.SectorNumber,
		"cache_path", fspaths.Cache)

	if err := fetchCacheTar(ctx, fspaths.Cache, params.CacheURL); err != nil {
		return xerrors.Errorf("fetching cache data: %w", err)
	}

	// Write c1.url file so PoRep can fetch C1 output from the remote provider
	c1InfoJSON, err := json.Marshal(params.C1Info)
	if err != nil {
		return xerrors.Errorf("marshaling c1 info: %w", err)
	}
	if err := os.WriteFile(filepath.Join(fspaths.Cache, paths.RemoteSealC1UrlFile), c1InfoJSON, 0644); err != nil {
		return xerrors.Errorf("writing c1.url file: %w", err)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTSealed|storiface.FTCache); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	return nil
}

// fetchFile downloads a file to destPath. If aria2c is installed it is used
// exclusively — we never fall back to Go HTTP after an aria2c failure because
// aria2c may leave a pre-allocated sparse file that would fool the Go HTTP
// resume logic into thinking the download is complete (the file stats as full
// size but contains null-byte holes for unfetched chunks). If aria2c is not
// installed at all, Go HTTP is used as the sole downloader.
func fetchFile(ctx context.Context, destPath, dlURL string, spID, sectorNumber int64) error {
	if _, err := exec.LookPath("aria2c"); err == nil {
		return fetchWithAria2c(ctx, destPath, dlURL)
	}

	log.Warnw("aria2c not found in PATH, using Go HTTP downloader",
		"sp_id", spID, "sector", sectorNumber)
	return fetchWithGoHTTP(ctx, destPath, dlURL)
}

// fetchCacheTar downloads a cache tar stream and extracts it to cachePath.
// Extracts to cachePath + ".tmp" first, then renames to cachePath on success.
// Cleans up the temp directory on any error.
func fetchCacheTar(ctx context.Context, cachePath, dlURL string) (err error) {
	tmpPath := cachePath + ".tmp"

	// Clean up temp directory on error
	defer func() {
		if err != nil {
			if rmErr := os.RemoveAll(tmpPath); rmErr != nil {
				log.Warnw("failed to clean up temp cache dir after error", "path", tmpPath, "error", rmErr)
			}
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return xerrors.Errorf("creating request: %w", err)
	}

	dlClient := &http.Client{}
	resp, err := dlClient.Do(req)
	if err != nil {
		return xerrors.Errorf("performing request to %s: %w", dlURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("unexpected status %d from %s: %s", resp.StatusCode, dlURL, string(body))
	}

	buf := make([]byte, 1<<20) // 1 MiB buffer
	_, err = tarutil.ExtractTar(tarutil.FinCacheFileConstraints, resp.Body, tmpPath, buf)
	if err != nil {
		return xerrors.Errorf("extracting cache tar to %s: %w", tmpPath, err)
	}

	// Remove the cachePath if it exists (should be empty, created by AcquireSector)
	// and rename .tmp to final destination
	if err := os.RemoveAll(cachePath); err != nil {
		return xerrors.Errorf("removing existing cache path %s: %w", cachePath, err)
	}

	if err := os.Rename(tmpPath, cachePath); err != nil {
		return xerrors.Errorf("renaming temp cache dir to final destination: %w", err)
	}

	return nil
}

// fetchWithAria2c invokes aria2c as a subprocess for multi-connection resumable downloads.
// Downloads to destPath + ".tmp" first, then renames to destPath on success.
// Cleans up temp files and aria2 control files on any error.
func fetchWithAria2c(ctx context.Context, destPath, dlURL string) (err error) {
	aria2cPath, err := exec.LookPath("aria2c")
	if err != nil {
		return xerrors.New("aria2c not found in PATH")
	}

	tmpPath := destPath + ".tmp"
	aria2ControlPath := tmpPath + ".aria2"

	// Clean up temp files on error
	defer func() {
		if err != nil {
			if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
				log.Warnw("failed to clean up temp file after error", "path", tmpPath, "error", rmErr)
			}
			if rmErr := os.Remove(aria2ControlPath); rmErr != nil && !os.IsNotExist(rmErr) {
				log.Warnw("failed to clean up aria2 control file after error", "path", aria2ControlPath, "error", rmErr)
			}
		}
	}()

	cmd := exec.CommandContext(ctx, aria2cPath,
		"--file-allocation=none", // prevent sparse pre-allocation that creates full-size files with null holes
		"--lowest-speed-limit=4K",
		"--timeout=120",
		"--connect-timeout=60",
		"--max-tries=0", // infinite retries within the context deadline
		"--retry-wait=30",
		"--continue=true",
		"-x16",
		"-s16",
		"--auto-file-renaming=false", // don't create .1, .2 copies
		"--allow-overwrite=true",
		"--dir", filepath.Dir(tmpPath),
		"-o", filepath.Base(tmpPath),
		dlURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return xerrors.Errorf("aria2c failed: %w", err)
	}

	// Rename .tmp to final destination only after successful download
	if err := os.Rename(tmpPath, destPath); err != nil {
		return xerrors.Errorf("renaming temp file to final destination: %w", err)
	}

	return nil
}

// fetchWithGoHTTP downloads a file using a plain Go HTTP client with Range header
// support for resuming partial downloads. Downloads to destPath + ".tmp" first,
// then renames to destPath on success. Cleans up the temp file on any error.
func fetchWithGoHTTP(ctx context.Context, destPath, dlURL string) (err error) {
	tmpPath := destPath + ".tmp"

	// Clean up temp file on error
	defer func() {
		if err != nil {
			if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
				log.Warnw("failed to clean up temp file after error", "path", tmpPath, "error", rmErr)
			}
		}
	}()

	f, err := os.OpenFile(tmpPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return xerrors.Errorf("opening temp file %s: %w", tmpPath, err)
	}
	defer func() { _ = f.Close() }()

	fStat, err := f.Stat()
	if err != nil {
		return xerrors.Errorf("stat temp file %s: %w", tmpPath, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return xerrors.Errorf("creating request: %w", err)
	}

	if fStat.Size() > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%s-", strconv.FormatInt(fStat.Size(), 10)))
	}

	dlClient := &http.Client{}
	resp, err := dlClient.Do(req)
	if err != nil {
		return xerrors.Errorf("performing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		if fStat.Size() > 0 {
			if err := f.Truncate(0); err != nil {
				return xerrors.Errorf("truncating temp file for full rewrite: %w", err)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return xerrors.Errorf("seeking to start: %w", err)
			}
		}
	case http.StatusPartialContent:
		// Server is sending the remaining bytes from our Range offset.
	case http.StatusRequestedRangeNotSatisfiable:
		// File is already complete.
		// Close the temp file before renaming
		if err := f.Close(); err != nil {
			return xerrors.Errorf("closing temp file: %w", err)
		}
		// Rename .tmp to final destination
		if err := os.Rename(tmpPath, destPath); err != nil {
			return xerrors.Errorf("renaming temp file to final destination: %w", err)
		}
		return nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	buf := make([]byte, 1<<20) // 1 MiB buffer
	_, err = io.CopyBuffer(f, resp.Body, buf)
	if err != nil {
		return xerrors.Errorf("writing data to %s: %w", tmpPath, err)
	}

	// Close the file before renaming
	if err := f.Close(); err != nil {
		return xerrors.Errorf("closing temp file: %w", err)
	}

	// Rename .tmp to final destination only after successful download
	if err := os.Rename(tmpPath, destPath); err != nil {
		return xerrors.Errorf("renaming temp file to final destination: %w", err)
	}

	return nil
}
