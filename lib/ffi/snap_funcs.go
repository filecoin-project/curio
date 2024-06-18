package ffi

import (
	"bytes"
	"context"
	"github.com/detailyang/go-fallocate"
	"github.com/filecoin-project/curio/lib/asyncwrite"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"io"
	"os"

	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/tarutil"

	"github.com/filecoin-project/lotus/storage/sealer/fr32"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func (sb *SealCalls) EncodeUpdate(
	ctx context.Context,
	taskID harmonytask.TaskID,
	proofType abi.RegisteredUpdateProof,
	sector storiface.SectorRef,
	data io.Reader,
	pieces []abi.PieceInfo) (sealedCID cid.Cid, unsealedCID cid.Cid, err error) {

	paths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, &taskID, sector, storiface.FTNone, storiface.FTUpdate|storiface.FTUpdateCache, storiface.PathSealing)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	if paths.Update == "" || paths.UpdateCache == "" {
		return cid.Undef, cid.Undef, xerrors.Errorf("update paths not set")
	}

	keyPath := ""        // can this be a named pipe - no, mmap in proofs
	keyCachePath := ""   // some temp copy
	stagedDataPath := "" // can this be a named pipe - no, mmap in proofs

	{
		// hack until we do snap encode ourselves and just call into proofs for CommR

		// todo use storage subsystem for temp files
		keyFile, err := os.CreateTemp("", "cutmp-key-")
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		keyCachePath, err = os.MkdirTemp("", "cutmp-keycache-")
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		stagedFile, err := os.CreateTemp("", "cutmp-staged-")
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		keyPath = keyFile.Name()
		stagedDataPath = stagedFile.Name()

		defer func() {
			if keyFile != nil {
				_ = keyFile.Close()
			}
			if stagedFile != nil {
				_ = stagedFile.Close()
			}

			_ = os.Remove(keyPath)
			_ = os.Remove(keyCachePath)

			_ = os.Remove(stagedDataPath)
		}()

		log.Debugw("get key data", "keyPath", keyPath, "keyCachePath", keyCachePath, "sectorID", sector.ID, "taskID", taskID)

		r, err := sb.sectors.storage.ReaderSeq(ctx, sector, storiface.FTSealed)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("getting sealed sector reader: %w", err)
		}

		// copy r into keyFile and close both
		_, err = keyFile.ReadFrom(r)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("copying sealed data: %w", err)
		}

		_ = r.Close()
		if err := keyFile.Close(); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("closing sealed data file: %w", err)
		}
		keyFile = nil

		// wrap stagedFile into a async bg writer
		stagedOut := asyncwrite.New(stagedFile, 8)

		// copy data into stagedFile and close both
		upw := fr32.NewPadWriter(stagedOut)

		// also wrap upw into async bg writer, this makes all io on separate goroutines
		bgUpw := asyncwrite.New(upw, 2)

		copyBuf := pool.Get(32 << 20)
		_, err = io.CopyBuffer(bgUpw, data, copyBuf)
		pool.Put(copyBuf)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("copying unsealed data: %w", err)
		}
		if err := bgUpw.Close(); err != nil {
			return cid.Cid{}, cid.Cid{}, xerrors.Errorf("closing padWriter: %w", err)
		}

		if err := stagedOut.Close(); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("closing staged data file: %w", err)
		}
		stagedFile = nil
		stagedOut = nil

		// fetch cache
		var buf bytes.Buffer // usually 73.2 MiB
		err = sb.sectors.storage.ReadMinCacheInto(ctx, sector, storiface.FTCache, &buf)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("reading cache: %w", err)
		}

		_, err = tarutil.ExtractTar(tarutil.FinCacheFileConstraints, &buf, keyCachePath, make([]byte, 1<<20))
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("extracting cache: %w", err)
		}
	}

	// remove old Update/UpdateCache files if they exist
	if err := os.Remove(paths.Update); err != nil && !os.IsNotExist(err) {
		return cid.Cid{}, cid.Cid{}, xerrors.Errorf("removing old update file: %w", err)
	}
	if err := os.RemoveAll(paths.UpdateCache); err != nil {
		return cid.Cid{}, cid.Cid{}, xerrors.Errorf("removing old update cache: %w", err)
	}

	// ensure update cache dir exists
	if err := os.MkdirAll(paths.UpdateCache, 0755); err != nil {
		return cid.Cid{}, cid.Cid{}, xerrors.Errorf("mkdir update cache: %w", err)
	}

	// allocate update file
	{
		s, err := os.Stat(keyPath)
		if err != nil {
			return cid.Undef, cid.Undef, err
		}
		sealedSize := s.Size()

		u, err := os.OpenFile(paths.Update, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("ensuring updated replica file exists: %w", err)
		}
		if err := fallocate.Fallocate(u, 0, sealedSize); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("allocating space for replica update file: %w", err)
		}
		if err := u.Close(); err != nil {
			return cid.Undef, cid.Undef, err
		}
	}

	ctx = ffiselect.WithLogCtx(ctx, "sector", sector.ID, "task", taskID, "key", keyPath, "cache", keyCachePath, "staged", stagedDataPath, "update", paths.Update, "updateCache", paths.UpdateCache)
	out, err := ffiselect.FFISelect.EncodeInto(ctx, proofType, paths.Update, paths.UpdateCache, keyPath, keyCachePath, stagedDataPath, pieces)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ffi update encode: %w", err)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTUpdate|storiface.FTUpdateCache); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ensure one copy: %w", err)
	}

	return out.Sealed, out.Unsealed, nil
}
