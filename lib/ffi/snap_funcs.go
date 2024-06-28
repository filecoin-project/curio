package ffi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/detailyang/go-fallocate"
	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/asyncwrite"
	"github.com/filecoin-project/curio/lib/ffiselect"
	paths2 "github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/tarutil"

	"github.com/filecoin-project/lotus/storage/sealer/fr32"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func (sb *SealCalls) EncodeUpdate(
	ctx context.Context,
	sectorKeyCid cid.Cid,
	taskID harmonytask.TaskID,
	proofType abi.RegisteredUpdateProof,
	sector storiface.SectorRef,
	data io.Reader,
	pieces []abi.PieceInfo,
	keepUnsealed bool) (sealedCID cid.Cid, unsealedCID cid.Cid, err error) {
	noDecl := storiface.FTNone
	if keepUnsealed {
		noDecl = storiface.FTUnsealed
	}

	paths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, &taskID, sector, storiface.FTNone, storiface.FTUpdate|storiface.FTUpdateCache|storiface.FTUnsealed, storiface.PathSealing)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector(noDecl)

	if paths.Update == "" || paths.UpdateCache == "" {
		return cid.Undef, cid.Undef, xerrors.Errorf("update paths not set")
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

	keyPath := filepath.Join(paths.UpdateCache, "cu-sector-key.dat")           // can this be a named pipe - no, mmap in proofs
	keyCachePath := filepath.Join(paths.UpdateCache, "cu-sector-key-fincache") // some temp copy (finalized cache directory)
	stagedDataPath := paths.Unsealed

	var cleanupStagedFiles func() error
	{
		// hack until we do snap encode ourselves and just call into proofs for CommR

		keyFile, err := os.Create(keyPath)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating key file: %w", err)
		}

		err = os.Mkdir(keyCachePath, 0755)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating key cache dir: %w", err)
		}

		stagedFile, err := os.Create(stagedDataPath)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating temp file: %w", err)
		}

		keyPath = keyFile.Name()
		stagedDataPath = stagedFile.Name()

		var cleanupDone bool
		cleanupStagedFiles = func() error {
			if cleanupDone {
				return nil
			}
			cleanupDone = true

			if keyFile != nil {
				if err := keyFile.Close(); err != nil {
					return xerrors.Errorf("closing key file: %w", err)
				}
			}
			if stagedFile != nil {
				if err := stagedFile.Close(); err != nil {
					return xerrors.Errorf("closing staged file: %w", err)
				}
			}

			if err := os.Remove(keyPath); err != nil {
				return xerrors.Errorf("removing key file: %w", err)
			}
			if err := os.RemoveAll(keyCachePath); err != nil {
				return xerrors.Errorf("removing key cache: %w", err)
			}
			if !keepUnsealed {
				if err := os.Remove(stagedDataPath); err != nil {
					return xerrors.Errorf("removing staged file: %w", err)
				}
			}

			return nil
		}

		defer func() {
			clerr := cleanupStagedFiles()
			if clerr != nil {
				log.Errorf("cleanup error: %+v", clerr)
			}
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

	vps, err := ffi.SectorUpdate.GenerateUpdateVanillaProofs(proofType, sectorKeyCid, out.Sealed, out.Unsealed, paths.Update, paths.UpdateCache, keyPath, keyCachePath)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("generate vanilla update proofs: %w", err)
	}

	ok, err := ffi.SectorUpdate.VerifyVanillaProofs(proofType, sectorKeyCid, out.Sealed, out.Unsealed, vps)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("verify vanilla update proofs: %w", err)
	}
	if !ok {
		return cid.Undef, cid.Undef, xerrors.Errorf("vanilla update proofs invalid")
	}

	// persist in UpdateCache/snap-vproof.json
	jb, err := json.Marshal(vps)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("marshal vanilla proofs: %w", err)
	}

	vpPath := filepath.Join(paths.UpdateCache, paths2.SnapVproofFile)
	if err := os.WriteFile(vpPath, jb, 0644); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("write vanilla proofs: %w", err)
	}

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting sector size: %w", err)
	}

	// cleanup
	if err := cleanupStagedFiles(); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("cleanup staged files: %w", err)
	}

	if err := ffi.ClearCache(uint64(ssize), paths.UpdateCache); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("clear cache: %w", err)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTUpdate|storiface.FTUpdateCache); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ensure one copy: %w", err)
	}

	return out.Sealed, out.Unsealed, nil
}

func (sb *SealCalls) ProveUpdate(ctx context.Context, proofType abi.RegisteredUpdateProof, sector storiface.SectorRef, key, sealed, unsealed cid.Cid) ([]byte, error) {
	jsonb, err := sb.sectors.storage.ReadSnapVanillaProof(ctx, sector)
	if err != nil {
		return nil, xerrors.Errorf("read snap vanilla proof: %w", err)
	}

	var vproofs [][]byte
	if err := json.Unmarshal(jsonb, &vproofs); err != nil {
		return nil, xerrors.Errorf("unmarshal snap vanilla proof: %w", err)
	}

	ctx = ffiselect.WithLogCtx(ctx, "sector", sector.ID, "key", key, "sealed", sealed, "unsealed", unsealed)
	return ffiselect.FFISelect.GenerateUpdateProofWithVanilla(ctx, proofType, key, sealed, unsealed, vproofs)
}

func (sb *SealCalls) MoveStorageSnap(ctx context.Context, sector storiface.SectorRef, taskID *harmonytask.TaskID) error {
	// only move the unsealed file if it still exists and needs moving
	moveUnsealed := storiface.FTUnsealed
	{
		found, unsealedPathType, err := sb.sectorStorageType(ctx, sector, storiface.FTUnsealed)
		if err != nil {
			return xerrors.Errorf("checking cache storage type: %w", err)
		}

		if !found || unsealedPathType == storiface.PathStorage {
			moveUnsealed = storiface.FTNone
		}
	}

	toMove := storiface.FTUpdateCache | storiface.FTUpdate | moveUnsealed

	var opts []storiface.AcquireOption
	if taskID != nil {
		resv, ok := sb.sectors.storageReservations.Load(*taskID)
		// if the reservation is missing MoveStorage will simply create one internally. This is fine as the reservation
		// will only be missing when the node is restarting, which means that the missing reservations will get recreated
		// anyways, and before we start claiming other tasks.
		if ok {
			defer resv.Release()

			if resv.Alloc != storiface.FTNone {
				return xerrors.Errorf("task %d has storage reservation with alloc", taskID)
			}
			if resv.Existing != toMove|storiface.FTUnsealed {
				return xerrors.Errorf("task %d has storage reservation with different existing", taskID)
			}

			opts = append(opts, storiface.AcquireInto(storiface.PathsWithIDs{Paths: resv.Paths, IDs: resv.PathIDs}))
		}
	}

	err := sb.sectors.storage.MoveStorage(ctx, sector, toMove, opts...)
	if err != nil {
		return xerrors.Errorf("moving storage: %w", err)
	}

	for _, fileType := range toMove.AllSet() {
		if err := sb.sectors.storage.RemoveCopies(ctx, sector.ID, fileType); err != nil {
			return xerrors.Errorf("rm copies (t:%s, s:%v): %w", fileType, sector, err)
		}
	}

	return nil
}
