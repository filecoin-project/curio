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
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi/cunative"
	"github.com/filecoin-project/curio/lib/ffiselect"
	paths2 "github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofpaths"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/supraffi"
	"github.com/filecoin-project/curio/lib/tarutil"
	commutil "github.com/filecoin-project/go-commp-utils/nonffi"

	"github.com/filecoin-project/lotus/storage/sealer/commitment"
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
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting sector size: %w", err)
	}

	paths, pathIDs, releaseSector, err := sb.Sectors.AcquireSector(ctx, &taskID, sector, storiface.FTNone, storiface.FTUpdate|storiface.FTUpdateCache, storiface.PathSealing)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

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

	////////////////////
	// Prepare sector key
	////////////////////

	keyPath := filepath.Join(paths.UpdateCache, "cu-sector-key.dat")           // can this be a named pipe - no, mmap in proofs
	keyCachePath := filepath.Join(paths.UpdateCache, "cu-sector-key-fincache") // some temp copy (finalized cache directory)
	var keyFile *os.File

	var cleanupStagedFiles func() error
	{
		keyFile, err = os.Create(keyPath)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating key file: %w", err)
		}

		err = os.Mkdir(keyCachePath, 0755)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("creating key cache dir: %w", err)
		}

		keyPath = keyFile.Name()

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

			if err := os.Remove(keyPath); err != nil {
				return xerrors.Errorf("removing key file: %w", err)
			}
			if err := os.RemoveAll(keyCachePath); err != nil {
				return xerrors.Errorf("removing key cache: %w", err)
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

		r, err := sb.Sectors.storage.ReaderSeq(ctx, sector, storiface.FTSealed)
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

		keyFile, err = os.OpenFile(keyPath, os.O_RDONLY, 0644)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("opening key file: %w", err)
		}

		// fetch cache
		var buf bytes.Buffer // usually 73.2 MiB
		err = sb.Sectors.storage.ReadMinCacheInto(ctx, sector, storiface.FTCache, &buf)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("reading cache: %w", err)
		}

		_, err = tarutil.ExtractTar(tarutil.FinCacheFileConstraints, &buf, keyCachePath, make([]byte, 1<<20))
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("extracting cache: %w", err)
		}

		if err := proof.EnsureTauxForType(sector.ProofType, keyCachePath); err != nil {
			return cid.Cid{}, cid.Cid{}, xerrors.Errorf("ensuring t_aux exists: %w", err)
		}
	}

	commD, err := commutil.GenerateUnsealedCID(sector.ProofType, pieces)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("generate unsealed cid: %w", err)
	}

	treeDPath := filepath.Join(paths.UpdateCache, proofpaths.TreeDName)

	// STEP 0: TreeD
	treeCommD, err := proof.BuildTreeD(data, true, treeDPath, abi.PaddedPieceSize(ssize))
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("build tree d: %w", err)
	}

	if commD != treeCommD {
		return cid.Undef, cid.Undef, xerrors.Errorf("comm d mismatch: piece: %s != tree: %s", commD, treeCommD)
	}

	////////////////////
	// Allocate update file
	////////////////////

	var updateFile *os.File
	{
		keyStat, err := os.Stat(keyPath)
		if err != nil {
			return cid.Undef, cid.Undef, err
		}
		sealedSize := keyStat.Size()

		updateFile, err = os.OpenFile(paths.Update, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("ensuring updated replica file exists: %w", err)
		}
		if err := fallocate.Fallocate(updateFile, 0, sealedSize); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("allocating space for replica update file: %w", err)
		}
	}

	// STEP 1: SupraEncode

	treeDFile, err := os.Open(treeDPath)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("open tree d file: %w", err)
	}
	defer treeDFile.Close()

	err = cunative.EncodeSnap(sector.ProofType, commD, sectorKeyCid, keyFile, treeDFile, updateFile)

	// (close early)
	// here we don't care about the error, as treeDFile was read-only
	_ = treeDFile.Close()

	// (close early)
	if cerr := updateFile.Close(); cerr != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("close update file: %w", cerr)
	}

	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("encode snap: %w", err)
	}

	// STEP 2: SupraTreeR

	res := supraffi.TreeRFile(paths.Update, treeDPath, paths.UpdateCache, uint64(ssize))
	if res != 0 {
		return cid.Undef, cid.Undef, xerrors.Errorf("tree r file %s: %w", paths.Update, err)
	}

	// STEP 2.5: Read PAux-es, transplant CC CommC, write back, calculate CommR

	_, updateCommRLast, err := proof.ReadPAux(paths.UpdateCache)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("read update p aux: %w", err)
	}

	ccCommC, _, err := proof.ReadPAux(keyCachePath)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("read cc p aux: %w", err)
	}

	if err := proof.WritePAux(paths.UpdateCache, ccCommC, updateCommRLast); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("write p aux: %w", err)
	}

	commR, err := commitment.CommR(ccCommC, updateCommRLast)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("compute comm r: %w", err)
	}

	if err := proof.WritePAux(paths.UpdateCache, ccCommC, updateCommRLast); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("write comm r p aux: %w", err)
	}

	sealedCid, err := commcid.ReplicaCommitmentV1ToCID(commR[:])
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("compute sealed cid: %w", err)
	}

	// STEP 3: Generate update proofs

	vps, err := ffi.SectorUpdate.GenerateUpdateVanillaProofs(proofType, sectorKeyCid, sealedCid, commD, paths.Update, paths.UpdateCache, keyPath, keyCachePath)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("generate vanilla update proofs: %w", err)
	}

	ok, err := ffi.SectorUpdate.VerifyVanillaProofs(proofType, sectorKeyCid, sealedCid, commD, vps)
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

	// Create unsealed file from tree-d prefix (same bytes)
	{
		var uPaths, uPathIDs storiface.SectorPaths

		uPaths.Cache = paths.UpdateCache
		uPathIDs.Cache = pathIDs.UpdateCache

		if err := sb.GenerateUnsealedSector(ctx, sector, &uPaths, &uPathIDs, keepUnsealed); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("generate unsealed sector: %w", err)
		}

		paths.Unsealed = uPaths.Unsealed
		pathIDs.Unsealed = uPathIDs.Unsealed
	}

	// cleanup
	if err := cleanupStagedFiles(); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("cleanup staged files: %w", err)
	}

	if err := ffi.ClearCache(paths.UpdateCache); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("clear cache: %w", err)
	}

	ensureTypes := storiface.FTUpdate | storiface.FTUpdateCache
	if keepUnsealed {
		ensureTypes |= storiface.FTUnsealed
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, ensureTypes); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ensure one copy: %w", err)
	}

	return sealedCid, commD, nil
}

func (sb *SealCalls) ProveUpdate(ctx context.Context, proofType abi.RegisteredUpdateProof, sector storiface.SectorRef, key, sealed, unsealed cid.Cid) ([]byte, error) {
	jsonb, err := sb.Sectors.storage.ReadSnapVanillaProof(ctx, sector)
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
		resvs, ok := sb.Sectors.storageReservations.Load(*taskID)
		// if the reservation is missing MoveStorage will simply create one internally. This is fine as the reservation
		// will only be missing when the node is restarting, which means that the missing reservations will get recreated
		// anyways, and before we start claiming other tasks.
		if ok {
			if len(resvs) != 1 {
				return xerrors.Errorf("task %d has %d reservations, expected 1", taskID, len(resvs))
			}
			resv := resvs[0]

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

	err := sb.Sectors.storage.MoveStorage(ctx, sector, toMove, opts...)
	if err != nil {
		return xerrors.Errorf("moving storage: %w", err)
	}

	for _, fileType := range toMove.AllSet() {
		if err := sb.Sectors.storage.RemoveCopies(ctx, sector.ID, fileType); err != nil {
			return xerrors.Errorf("rm copies (t:%s, s:%v): %w", fileType, sector, err)
		}
	}

	return nil
}
