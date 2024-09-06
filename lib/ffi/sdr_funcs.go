package ffi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/KarpelesLab/reflink"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/puzpuzpuz/xsync/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffiselect"
	"github.com/filecoin-project/curio/lib/proof"
	storiface "github.com/filecoin-project/curio/lib/storiface"

	// TODO everywhere here that we call this we should call our proxy instead.
	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	proof2 "github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proofpaths"
)

const C1CheckNumber = 3

var log = logging.Logger("cu/ffi")

/*
type ExternPrecommit2 func(ctx context.Context, sector storiface.SectorRef, cache, sealed string, pc1out storiface.PreCommit1Out) (sealedCID cid.Cid, unsealedCID cid.Cid, err error)

	type ExternalSealer struct {
		PreCommit2 ExternPrecommit2
	}
*/
type SealCalls struct {
	sectors *storageProvider

	/*// externCalls cointain overrides for calling alternative sealing logic
	externCalls ExternalSealer*/
}

func NewSealCalls(st *paths.Remote, ls *paths.Local, si paths.SectorIndex) *SealCalls {
	return &SealCalls{
		sectors: &storageProvider{
			storage:             st,
			localStore:          ls,
			sindex:              si,
			storageReservations: xsync.NewIntegerMapOf[harmonytask.TaskID, *StorageReservation](),
		},
	}
}

type storageProvider struct {
	storage             *paths.Remote
	localStore          *paths.Local
	sindex              paths.SectorIndex
	storageReservations *xsync.MapOf[harmonytask.TaskID, *StorageReservation]
}

func (l *storageProvider) AcquireSector(ctx context.Context, taskID *harmonytask.TaskID, sector storiface.SectorRef, existing, allocate storiface.SectorFileType, sealing storiface.PathType) (fspaths, ids storiface.SectorPaths, release func(dontDeclare ...storiface.SectorFileType), err error) {
	var sectorPaths, storageIDs storiface.SectorPaths
	var releaseStorage func()

	var ok bool
	var resv *StorageReservation
	if taskID != nil {
		resv, ok = l.storageReservations.Load(*taskID)
	}
	if ok && resv != nil {
		if resv.Alloc != allocate || resv.Existing != existing {
			// this should never happen, only when task definition is wrong
			return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, xerrors.Errorf("storage reservation type mismatch")
		}

		log.Debugw("using existing storage reservation", "task", taskID, "sector", sector, "existing", existing, "allocate", allocate)

		sectorPaths = resv.Paths
		storageIDs = resv.PathIDs
		releaseStorage = resv.Release

		if len(existing.AllSet()) > 0 {
			// there are some "existing" files in the reservation. Some of them may need fetching, so call l.storage.AcquireSector
			// (which unlike in the reservation code will be called on the paths.Remote instance) to ensure that the files are
			// present locally. Note that we do not care about 'allocate' reqeuests, those files don't exist, and are just
			// proposed paths with a reservation of space.

			_, checkPathIDs, err := l.storage.AcquireSector(ctx, sector, existing, storiface.FTNone, sealing, storiface.AcquireMove, storiface.AcquireInto(storiface.PathsWithIDs{Paths: sectorPaths, IDs: storageIDs}))
			if err != nil {
				return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, xerrors.Errorf("acquire reserved existing files: %w", err)
			}

			// assert that checkPathIDs is the same as storageIDs
			if storageIDs.Subset(existing) != checkPathIDs.Subset(existing) {
				return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, xerrors.Errorf("acquire reserved existing files: pathIDs mismatch %#v != %#v", storageIDs, checkPathIDs)
			}
		}
	} else {
		// No related reservation, acquire storage as usual

		var err error
		sectorPaths, storageIDs, err = l.storage.AcquireSector(ctx, sector, existing, allocate, sealing, storiface.AcquireMove)
		if err != nil {
			return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, err
		}

		releaseStorage, err = l.localStore.Reserve(ctx, sector, allocate, storageIDs, storiface.FSOverheadSeal, paths.MinFreeStoragePercentage)
		if err != nil {
			return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, xerrors.Errorf("reserving storage space: %w", err)
		}
	}

	log.Debugf("acquired sector %d (e:%d; a:%d): %v", sector, existing, allocate, sectorPaths)

	return sectorPaths, storageIDs, func(dontDeclare ...storiface.SectorFileType) {
		releaseStorage()

	nextType:
		for _, fileType := range storiface.PathTypes {
			if fileType&allocate == 0 {
				continue
			}
			for _, dont := range dontDeclare {
				if fileType&dont != 0 {
					continue nextType
				}
			}

			sid := storiface.PathByType(storageIDs, fileType)
			if err := l.sindex.StorageDeclareSector(ctx, storiface.ID(sid), sector.ID, fileType, true); err != nil {
				log.Errorf("declare sector error: %+v", err)
			}
		}
	}, nil
}

func (sb *SealCalls) GenerateSDR(ctx context.Context, taskID harmonytask.TaskID, into storiface.SectorFileType, sector storiface.SectorRef, ticket abi.SealRandomness, commDcid cid.Cid) error {
	paths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, &taskID, sector, storiface.FTNone, into, storiface.PathSealing)
	if err != nil {
		return xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	// prepare SDR params
	commd, err := commcid.CIDToDataCommitmentV1(commDcid)
	if err != nil {
		return xerrors.Errorf("computing commD (%s): %w", commDcid, err)
	}

	replicaID, err := sector.ProofType.ReplicaId(sector.ID.Miner, sector.ID.Number, ticket, commd)
	if err != nil {
		return xerrors.Errorf("computing replica id: %w", err)
	}

	intoPath := storiface.PathByType(paths, into)
	intoTemp := intoPath + storiface.TempSuffix

	// make sure the cache dir is empty
	if err := os.RemoveAll(intoPath); err != nil {
		return xerrors.Errorf("removing into: %w", err)
	}
	if err := os.RemoveAll(intoTemp); err != nil {
		return xerrors.Errorf("removing intoTemp: %w", err)
	}
	if err := os.MkdirAll(intoTemp, 0755); err != nil {
		return xerrors.Errorf("mkdir intoTemp dir: %w", err)
	}

	// generate new sector key
	err = ffi.GenerateSDR(
		sector.ProofType,
		intoTemp,
		replicaID,
	)
	if err != nil {
		return xerrors.Errorf("generating SDR %d (%s): %w", sector.ID.Number, intoTemp, err)
	}

	onlyLastLayer := into == storiface.FTKey
	if onlyLastLayer {
		// move the last layer to the final location
		numLayers, err := proofpaths.SDRLayers(sector.ProofType)
		if err != nil {
			return xerrors.Errorf("getting number of layers: %w", err)
		}
		lastLayer := proofpaths.LayerFileName(numLayers)

		if err := os.Rename(filepath.Join(intoTemp, lastLayer), filepath.Join(intoPath)); err != nil {
			return xerrors.Errorf("renaming last layer: %w", err)
		}
	} else {
		if err := os.Rename(intoTemp, intoPath); err != nil {
			return xerrors.Errorf("renaming into: %w", err)
		}
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, into); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	return nil
}

// ensureOneCopy makes sure that there is only one version of sector data.
// Usually called after a successful operation was done successfully on sector data.
func (sb *SealCalls) ensureOneCopy(ctx context.Context, sid abi.SectorID, pathIDs storiface.SectorPaths, fts storiface.SectorFileType) error {
	if !pathIDs.HasAllSet(fts) {
		return xerrors.Errorf("ensure one copy: not all paths are set")
	}

	for _, fileType := range fts.AllSet() {
		pid := storiface.PathByType(pathIDs, fileType)
		keepIn := []storiface.ID{storiface.ID(pid)}

		log.Debugw("ensureOneCopy", "sector", sid, "type", fileType, "keep", keepIn)

		if err := sb.sectors.storage.Remove(ctx, sid, fileType, true, keepIn); err != nil {
			return err
		}
	}

	return nil
}

func (sb *SealCalls) TreeRC(ctx context.Context, task *harmonytask.TaskID, sector storiface.SectorRef, unsealed cid.Cid, randomness abi.SealRandomness, pieces []abi.PieceInfo) (scid cid.Cid, ucid cid.Cid, err error) {
	p1o, err := sb.makePhase1Out(unsealed, sector.ProofType)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("make phase1 output: %w", err)
	}

	fspaths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, task, sector, storiface.FTCache, storiface.FTSealed, storiface.PathSealing)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	defer func() {
		if err != nil {
			clerr := removeDRCTrees(fspaths.Cache, false)
			if clerr != nil {
				log.Errorw("removing tree files after TreeDRC error", "error", clerr, "exec-error", err, "sector", sector, "cache", fspaths.Cache)
			}
		}
	}()

	// create sector-sized file at paths.Sealed; PC2 transforms it into a sealed sector in-place
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting sector size: %w", err)
	}

	{
		// copy TreeD prefix to sealed sector, SealPreCommitPhase2 will mutate it in place into the sealed sector

		// first try reflink + truncate, that should be way faster
		err := reflink.Always(filepath.Join(fspaths.Cache, proofpaths.TreeDName), fspaths.Sealed)
		if err == nil {
			err = os.Truncate(fspaths.Sealed, int64(ssize))
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("truncating reflinked sealed file: %w", err)
			}
		} else {
			log.Errorw("reflink treed -> sealed failed, falling back to slow copy, use single scratch btrfs or xfs filesystem", "error", err, "sector", sector, "cache", fspaths.Cache, "sealed", fspaths.Sealed)

			// fallback to slow copy, copy ssize bytes from treed to sealed
			dst, err := os.OpenFile(fspaths.Sealed, os.O_WRONLY|os.O_CREATE, 0644)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("opening sealed sector file: %w", err)
			}
			src, err := os.Open(filepath.Join(fspaths.Cache, proofpaths.TreeDName))
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("opening treed sector file: %w", err)
			}

			_, err = io.CopyN(dst, src, int64(ssize))
			derr := dst.Close()
			_ = src.Close()
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("copying treed -> sealed: %w", err)
			}
			if derr != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("closing sealed file: %w", derr)
			}
		}
	}

	ctx = ffiselect.WithLogCtx(ctx, "sector", sector.ID, "cache", fspaths.Cache, "sealed", fspaths.Sealed)
	out, err := ffiselect.FFISelect.SealPreCommitPhase2(ctx, p1o, fspaths.Cache, fspaths.Sealed)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("computing seal proof: %w", err)
	}

	if out.Unsealed != unsealed {
		return cid.Undef, cid.Undef, xerrors.Errorf("unsealed cid changed after sealing")
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTCache|storiface.FTSealed); err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("ensure one copy: %w", err)
	}

	if found, ok := abi.Synthetic[sector.ProofType]; !ok && !found {
		// Generate 3 random commits
		for i := 0; i < C1CheckNumber; i++ {
			var sd [32]byte
			_, _ = rand.Read(sd[:])
			_, err = ffi.SealCommitPhase1(sector.ProofType, out.Sealed, unsealed, fspaths.Cache, fspaths.Sealed, sector.ID.Number, sector.ID.Miner, randomness, sd[:], pieces)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("checking PreCommit for sector num:%d tkt:%v seed:%v sealedCID:%v, unsealedCID:%v failed: %w", sector.ID.Number, randomness, sd[:], out.Sealed, unsealed, err)
			}
		}
	}

	return out.Sealed, out.Unsealed, nil
}

func removeDRCTrees(cache string, isDTree bool) error {
	files, err := os.ReadDir(cache)
	if err != nil {
		return xerrors.Errorf("listing cache: %w", err)
	}

	var testFunc func(string) bool

	if isDTree {
		testFunc = proofpaths.IsTreeDFile
	} else {
		testFunc = proofpaths.IsTreeRCFile
	}

	for _, file := range files {
		if testFunc(file.Name()) {
			err := os.Remove(filepath.Join(cache, file.Name()))
			if err != nil {
				return xerrors.Errorf("removing tree file: %w", err)
			}
		}
	}
	return nil
}

func (sb *SealCalls) GenerateSynthPoRep() {
	panic("todo")
}

func (sb *SealCalls) PoRepSnark(ctx context.Context, sn storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	vproof, err := sb.sectors.storage.GeneratePoRepVanillaProof(ctx, sn, sealed, unsealed, ticket, seed)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate vanilla proof: %w", err)
	}

	ctx = ffiselect.WithLogCtx(ctx, "sector", sn.ID, "sealed", sealed, "unsealed", unsealed, "ticket", ticket, "seed", seed)
	proof, err := ffiselect.FFISelect.SealCommitPhase2(ctx, vproof, sn.ID.Number, sn.ID.Miner)
	if err != nil {
		return nil, xerrors.Errorf("computing seal proof failed: %w", err)
	}

	ok, err := ffi.VerifySeal(proof2.SealVerifyInfo{
		SealProof:             sn.ProofType,
		SectorID:              sn.ID,
		DealIDs:               nil,
		Randomness:            ticket,
		InteractiveRandomness: seed,
		Proof:                 proof,
		SealedCID:             sealed,
		UnsealedCID:           unsealed,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to verify proof: %w", err)
	}
	if !ok {
		return nil, xerrors.Errorf("porep failed to validate")
	}

	return proof, nil
}

func (sb *SealCalls) makePhase1Out(unsCid cid.Cid, spt abi.RegisteredSealProof) ([]byte, error) {
	commd, err := commcid.CIDToDataCommitmentV1(unsCid)
	if err != nil {
		return nil, xerrors.Errorf("make uns cid: %w", err)
	}

	type Config struct {
		ID            string `json:"id"`
		Path          string `json:"path"`
		RowsToDiscard int    `json:"rows_to_discard"`
		Size          int    `json:"size"`
	}

	type Labels struct {
		H      *string  `json:"_h"` // proofs want this..
		Labels []Config `json:"labels"`
	}

	var phase1Output struct {
		CommD           [32]byte           `json:"comm_d"`
		Config          Config             `json:"config"` // TreeD
		Labels          map[string]*Labels `json:"labels"`
		RegisteredProof string             `json:"registered_proof"`
	}

	copy(phase1Output.CommD[:], commd)

	phase1Output.Config.ID = "tree-d"
	phase1Output.Config.Path = "/placeholder"
	phase1Output.Labels = map[string]*Labels{}

	switch spt {
	case abi.RegisteredSealProof_StackedDrg2KiBV1_1, abi.RegisteredSealProof_StackedDrg2KiBV1_1_Feat_SyntheticPoRep:
		phase1Output.Config.RowsToDiscard = 0
		phase1Output.Config.Size = 127
		phase1Output.Labels["StackedDrg2KiBV1"] = &Labels{}
		phase1Output.RegisteredProof = "StackedDrg2KiBV1_1"

		for i := 0; i < 2; i++ {
			phase1Output.Labels["StackedDrg2KiBV1"].Labels = append(phase1Output.Labels["StackedDrg2KiBV1"].Labels, Config{
				ID:            fmt.Sprintf("layer-%d", i+1),
				Path:          "/placeholder",
				RowsToDiscard: 0,
				Size:          64,
			})
		}

	case abi.RegisteredSealProof_StackedDrg8MiBV1_1, abi.RegisteredSealProof_StackedDrg8MiBV1_1_Feat_SyntheticPoRep:
		phase1Output.Config.RowsToDiscard = 0
		phase1Output.Config.Size = 524287
		phase1Output.Labels["StackedDrg8MiBV1"] = &Labels{}
		phase1Output.RegisteredProof = "StackedDrg8MiBV1_1"

		for i := 0; i < 2; i++ {
			phase1Output.Labels["StackedDrg8MiBV1"].Labels = append(phase1Output.Labels["StackedDrg8MiBV1"].Labels, Config{
				ID:            fmt.Sprintf("layer-%d", i+1),
				Path:          "/placeholder",
				RowsToDiscard: 0,
				Size:          262144,
			})
		}

	case abi.RegisteredSealProof_StackedDrg512MiBV1_1, abi.RegisteredSealProof_StackedDrg512MiBV1_1_Feat_SyntheticPoRep:
		phase1Output.Config.RowsToDiscard = 0
		phase1Output.Config.Size = 33554431
		phase1Output.Labels["StackedDrg512MiBV1"] = &Labels{}
		phase1Output.RegisteredProof = "StackedDrg512MiBV1_1"

		for i := 0; i < 2; i++ {
			phase1Output.Labels["StackedDrg512MiBV1"].Labels = append(phase1Output.Labels["StackedDrg512MiBV1"].Labels, Config{
				ID:            fmt.Sprintf("layer-%d", i+1),
				Path:          "placeholder",
				RowsToDiscard: 0,
				Size:          16777216,
			})
		}

	case abi.RegisteredSealProof_StackedDrg32GiBV1_1, abi.RegisteredSealProof_StackedDrg32GiBV1_1_Feat_SyntheticPoRep:
		phase1Output.Config.RowsToDiscard = 0
		phase1Output.Config.Size = 2147483647
		phase1Output.Labels["StackedDrg32GiBV1"] = &Labels{}
		phase1Output.RegisteredProof = "StackedDrg32GiBV1_1"

		for i := 0; i < 11; i++ {
			phase1Output.Labels["StackedDrg32GiBV1"].Labels = append(phase1Output.Labels["StackedDrg32GiBV1"].Labels, Config{
				ID:            fmt.Sprintf("layer-%d", i+1),
				Path:          "/placeholder",
				RowsToDiscard: 0,
				Size:          1073741824,
			})
		}

	case abi.RegisteredSealProof_StackedDrg64GiBV1_1, abi.RegisteredSealProof_StackedDrg64GiBV1_1_Feat_SyntheticPoRep:
		phase1Output.Config.RowsToDiscard = 0
		phase1Output.Config.Size = 4294967295
		phase1Output.Labels["StackedDrg64GiBV1"] = &Labels{}
		phase1Output.RegisteredProof = "StackedDrg64GiBV1_1"

		for i := 0; i < 11; i++ {
			phase1Output.Labels["StackedDrg64GiBV1"].Labels = append(phase1Output.Labels["StackedDrg64GiBV1"].Labels, Config{
				ID:            fmt.Sprintf("layer-%d", i+1),
				Path:          "/placeholder",
				RowsToDiscard: 0,
				Size:          2147483648,
			})
		}

	default:
		panic("proof type not handled")
	}

	return json.Marshal(phase1Output)
}

func (sb *SealCalls) LocalStorage(ctx context.Context) ([]storiface.StoragePath, error) {
	return sb.sectors.localStore.Local(ctx)
}

func changePathType(path string, newType storiface.SectorFileType) (string, error) {
	// /some/parent/[type]/filename -> /some/parent/[newType]/filename

	dir, file := filepath.Split(path)
	if dir == "" {
		return "", xerrors.Errorf("path has too few components: %s", path)
	}

	components := strings.Split(strings.TrimRight(dir, string(filepath.Separator)), string(filepath.Separator))
	if len(components) < 1 {
		return "", xerrors.Errorf("path has too few components: %s", path)
	}

	newTypeName := newType.String()

	components[len(components)-1] = newTypeName
	newPath := filepath.Join(append(components, file)...)

	if filepath.IsAbs(path) {
		newPath = filepath.Join(string(filepath.Separator), newPath)
	}

	return newPath, nil
}
func (sb *SealCalls) FinalizeSector(ctx context.Context, sector storiface.SectorRef, keepUnsealed bool) error {
	sectorPaths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, nil, sector, storiface.FTCache, storiface.FTNone, storiface.PathSealing)
	if err != nil {
		return xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return xerrors.Errorf("getting sector size: %w", err)
	}

	if keepUnsealed {
		// We are going to be moving the unsealed file, no need to allocate storage specifically for it
		sectorPaths.Unsealed, err = changePathType(sectorPaths.Cache, storiface.FTUnsealed)
		if err != nil {
			return xerrors.Errorf("changing path type: %w", err)
		}

		pathIDs.Unsealed = pathIDs.Cache // this is just an uuid string

		defer func() {
			// We don't pass FTUnsealed to Acquire, so releaseSector won't declare it. Do it here.
			if err := sb.sectors.sindex.StorageDeclareSector(ctx, storiface.ID(pathIDs.Unsealed), sector.ID, storiface.FTUnsealed, true); err != nil {
				log.Errorf("declare unsealed sector error: %+v", err)
			}
		}()

		// tree-d contains exactly unsealed data in the prefix, so
		// * we move it to a temp file
		// * we truncate the temp file to the sector size
		// * we move the temp file to the unsealed location

		// temp path in cache where we'll move tree-d before truncating
		// it is in the cache directory so that we can use os.Rename to move it
		// to unsealed (which may be on a different filesystem)
		tempUnsealed := filepath.Join(sectorPaths.Cache, storiface.SectorName(sector.ID))

		_, terr := os.Stat(tempUnsealed)
		tempUnsealedExists := terr == nil

		// First handle an edge case where we have already gone through this step,
		// but ClearCache or later steps failed. In that case we'll see tree-d missing and unsealed present

		if _, err := os.Stat(filepath.Join(sectorPaths.Cache, proofpaths.TreeDName)); err != nil {
			if os.IsNotExist(err) {
				// check that unsealed exists and is the right size
				st, err := os.Stat(sectorPaths.Unsealed)
				if err != nil {
					if os.IsNotExist(err) {
						if tempUnsealedExists {
							// unsealed file does not exist, but temp unsealed file does
							// so we can just resume where the previous attempt left off
							goto retryUnsealedMove
						}
						return xerrors.Errorf("neither unsealed file nor temp-unsealed file exists")
					}
					return xerrors.Errorf("stat unsealed file: %w", err)
				}
				if st.Size() != int64(ssize) {
					if tempUnsealedExists {
						// unsealed file exists but is the wrong size, and temp unsealed file exists
						// so we can just resume where the previous attempt left off with some cleanup

						if err := os.Remove(sectorPaths.Unsealed); err != nil {
							return xerrors.Errorf("removing unsealed file from last attempt: %w", err)
						}

						goto retryUnsealedMove
					}
					return xerrors.Errorf("unsealed file is not the right size: %d != %d and temp unsealed is missing", st.Size(), ssize)
				}

				// all good, just log that this edge case happened
				log.Warnw("unsealed file exists but tree-d is missing, skipping move", "sector", sector.ID, "unsealed", sectorPaths.Unsealed, "cache", sectorPaths.Cache)
				goto afterUnsealedMove
			}
			return xerrors.Errorf("stat tree-d file: %w", err)
		}

		// If the state in clean do the move

		// move tree-d to temp file
		if err := os.Rename(filepath.Join(sectorPaths.Cache, proofpaths.TreeDName), tempUnsealed); err != nil {
			return xerrors.Errorf("moving tree-d to temp file: %w", err)
		}

	retryUnsealedMove:

		// truncate sealed file to sector size
		if err := os.Truncate(tempUnsealed, int64(ssize)); err != nil {
			return xerrors.Errorf("truncating unsealed file to sector size: %w", err)
		}

		// move temp file to unsealed location
		if err := paths.Move(tempUnsealed, sectorPaths.Unsealed); err != nil {
			return xerrors.Errorf("move temp unsealed sector to final location (%s -> %s): %w", tempUnsealed, sectorPaths.Unsealed, err)
		}
	}

afterUnsealedMove:
	if abi.Synthetic[sector.ProofType] {
		if err = ffi.ClearSyntheticProofs(uint64(ssize), sectorPaths.Cache); err != nil {
			return xerrors.Errorf("Unable to delete Synth cache: %w", err)
		}
	}

	if err := ffi.ClearCache(uint64(ssize), sectorPaths.Cache); err != nil {
		return xerrors.Errorf("clearing cache: %w", err)
	}

	maybeUns := storiface.FTUnsealed
	if !keepUnsealed {
		maybeUns = storiface.FTNone
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTCache|maybeUns); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	return nil
}

func (sb *SealCalls) MoveStorage(ctx context.Context, sector storiface.SectorRef, taskID *harmonytask.TaskID) error {
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

	toMove := storiface.FTCache | storiface.FTSealed | moveUnsealed

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

func (sb *SealCalls) sectorStorageType(ctx context.Context, sector storiface.SectorRef, ft storiface.SectorFileType) (sectorFound bool, ptype storiface.PathType, err error) {
	stores, err := sb.sectors.sindex.StorageFindSector(ctx, sector.ID, ft, 0, false)
	if err != nil {
		return false, "", xerrors.Errorf("finding sector: %w", err)
	}
	if len(stores) == 0 {
		return false, "", nil
	}

	for _, store := range stores {
		if store.CanSeal {
			return true, storiface.PathSealing, nil
		}
	}

	return true, storiface.PathStorage, nil
}

// PreFetch fetches the sector file to local storage before SDR and TreeRC Tasks
func (sb *SealCalls) PreFetch(ctx context.Context, sector storiface.SectorRef, task *harmonytask.TaskID) (fsPath, pathID storiface.SectorPaths, releaseSector func(...storiface.SectorFileType), err error) {
	fsPath, pathID, releaseSector, err = sb.sectors.AcquireSector(ctx, task, sector, storiface.FTCache, storiface.FTNone, storiface.PathSealing)
	if err != nil {
		return storiface.SectorPaths{}, storiface.SectorPaths{}, nil, xerrors.Errorf("acquiring sector paths: %w", err)
	}
	// Don't release the storage locks. They will be released in TreeD func()
	return
}

func (sb *SealCalls) TreeD(ctx context.Context, sector storiface.SectorRef, unsealed cid.Cid, size abi.PaddedPieceSize, data io.Reader, unpaddedData bool, fspaths, pathIDs storiface.SectorPaths) error {
	var err error
	defer func() {
		if err != nil {
			clerr := removeDRCTrees(fspaths.Cache, true)
			if clerr != nil {
				log.Errorw("removing tree files after TreeDRC error", "error", clerr, "exec-error", err, "sector", sector, "cache", fspaths.Cache)
			}
		}
	}()

	treeDUnsealed, err := proof.BuildTreeD(data, unpaddedData, filepath.Join(fspaths.Cache, proofpaths.TreeDName), size)
	if err != nil {
		return xerrors.Errorf("building tree-d: %w", err)
	}

	if treeDUnsealed != unsealed {
		return xerrors.Errorf("tree-d cid %s mismatch with supplied unsealed cid %s", treeDUnsealed, unsealed)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTCache); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	return nil
}

func (sb *SealCalls) SyntheticProofs(ctx context.Context, task *harmonytask.TaskID, sector storiface.SectorRef, sealed cid.Cid, unsealed cid.Cid, randomness abi.SealRandomness, pieces []abi.PieceInfo) error {
	fspaths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, task, sector, storiface.FTCache|storiface.FTSealed, storiface.FTNone, storiface.PathSealing)
	if err != nil {
		return xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return err
	}

	err = ffi.GenerateSynthProofs(sector.ProofType, sealed, unsealed, fspaths.Cache, fspaths.Sealed, sector.ID.Number, sector.ID.Miner, randomness, pieces)
	if err != nil {
		return xerrors.Errorf("generating synthetic proof: %w", err)
	}

	// Generate 3 random commits
	for i := 0; i < C1CheckNumber; i++ {
		var sd [32]byte
		_, _ = rand.Read(sd[:])
		_, err = ffi.SealCommitPhase1(sector.ProofType, sealed, unsealed, fspaths.Cache, fspaths.Sealed, sector.ID.Number, sector.ID.Miner, randomness, sd[:], pieces)
		if err != nil {
			return xerrors.Errorf("checking PreCommit for synthetic proofs for num:%d tkt:%v seed:%v sealedCID:%v, unsealedCID:%v failed: %w", sector.ID.Number, randomness, sd[:], sealed, unsealed, err)
		}
	}

	if err = ffi.ClearCache(uint64(ssize), fspaths.Cache); err != nil {
		return xerrors.Errorf("failed to clear cache for synthetic proof of sector %d of miner %d", sector.ID.Miner, sector.ID.Number)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTCache|storiface.FTSealed); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	return nil
}
