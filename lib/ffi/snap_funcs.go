package ffi

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"os"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/tarutil"
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

	//sb.sectors.storage.ReaderSeq()

	keyPath := ""        // can this be a named pipe - no, mmap in proofs
	keyCachePath := ""   // some temp copy
	stagedDataPath := "" // can this be a named pipe - no, mmap in proofs

	{
		// hack until we do snap encode ourselves and just call into proofs for CommR

		// todo use storage subsystem for temp files
		keyFile, err := ioutil.TempFile("", "key-")
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		keyCachePath, err = os.MkdirTemp("", "keycache-")
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		stagedFile, err := ioutil.TempFile("", "staged-")
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		keyPath = keyFile.Name()
		stagedDataPath = stagedFile.Name()

		defer func() {
			if keyFile != nil {
				_ = keyFile.Close()
			}
			_ = stagedFile.Close()

			_ = os.Remove(keyFile.Name())
			_ = os.Remove(keyCachePath)
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

		// copy data into stagedFile and close both
		_, err = stagedFile.ReadFrom(data)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("copying staged data: %w", err)
		}

		_ = stagedFile.Close()
		if err := stagedFile.Close(); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("closing staged data file: %w", err)
		}
		stagedFile = nil

		// fetch cache
		buf := bytes.NewBuffer(make([]byte, 80<<20)) // usually 73.2 MiB
		err = sb.sectors.storage.ReadMinCacheInto(ctx, sector, storiface.FTCache, buf)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("reading cache: %w", err)
		}

		_, err = tarutil.ExtractTar(tarutil.FinCacheFileConstraints, buf, keyCachePath, make([]byte, 1<<20))
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("extracting cache: %w", err)
		}
	}

	sealed, unsealed, err := ffi.SectorUpdate.EncodeInto(proofType, paths.Update, paths.UpdateCache, keyPath, keyCachePath, stagedDataPath, pieces)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ffi update encode: %w", err)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTUpdate|storiface.FTUpdateCache); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ensure one copy: %w", err)
	}

	return sealed, unsealed, nil
}
