package paths

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/partialfile"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/tarutil"
)

var log = logging.Logger("stores")

var _ PartialFileHandler = &DefaultPartialFileHandler{}

// DefaultPartialFileHandler is the default implementation of the PartialFileHandler interface.
// This is probably the only implementation we'll ever use because the purpose of the
// interface to is to mock out partial file related functionality during testing.
type DefaultPartialFileHandler struct{}

func (d *DefaultPartialFileHandler) OpenPartialFile(maxPieceSize abi.PaddedPieceSize, path string) (*partialfile.PartialFile, error) {
	return partialfile.OpenPartialFile(maxPieceSize, path)
}
func (d *DefaultPartialFileHandler) HasAllocated(pf *partialfile.PartialFile, offset storiface.UnpaddedByteIndex, size abi.UnpaddedPieceSize) (bool, error) {
	return pf.HasAllocated(offset, size)
}

func (d *DefaultPartialFileHandler) Reader(pf *partialfile.PartialFile, offset storiface.PaddedByteIndex, size abi.PaddedPieceSize) (io.Reader, error) {
	return pf.Reader(offset, size)
}

// Close closes the partial file
func (d *DefaultPartialFileHandler) Close(pf *partialfile.PartialFile) error {
	return pf.Close()
}

type FetchHandler struct {
	Local interface {
		Store
		StashStore
	}
	PfHandler PartialFileHandler
}

func (handler *FetchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { // /remote/
	mux := mux.NewRouter()

	mux.HandleFunc("/remote/stat/{id}", handler.remoteStatFs).Methods("GET")
	mux.HandleFunc("/remote/vanilla/single", handler.generateSingleVanillaProof).Methods("POST")
	mux.HandleFunc("/remote/vanilla/porep", handler.generatePoRepVanillaProof).Methods("POST")
	mux.HandleFunc("/remote/vanilla/snap", handler.readSnapVanillaProof).Methods("POST")
	mux.HandleFunc("/remote/stash/{id}", handler.remoteGetStash).Methods("GET")
	mux.HandleFunc("/remote/{type}/{id}/{spt}/allocated/{offset}/{size}", handler.remoteGetAllocated).Methods("GET")
	mux.HandleFunc("/remote/{type}/{id}", handler.remoteGetSector).Methods("GET")
	mux.HandleFunc("/remote/{type}/{id}", handler.remoteDeleteSector).Methods("DELETE")

	mux.ServeHTTP(w, r)
}

func (handler *FetchHandler) remoteStatFs(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := storiface.ID(vars["id"])

	st, err := handler.Local.FsStat(r.Context(), id)
	switch err {
	case errPathNotFound:
		w.WriteHeader(404)
		return
	case nil:
		break
	default:
		w.WriteHeader(500)
		log.Errorf("error getting stat for ID %s: %s", id, err.Error())
		return
	}

	if err := json.NewEncoder(w).Encode(&st); err != nil {
		log.Warnf("error writing stat response: %+v", err)
	}
}

// remoteGetSector returns the sector file/tared directory byte stream for the sectorID and sector file type sent in the request.
// returns an error if it does NOT have the required sector file/dir.
func (handler *FetchHandler) remoteGetSector(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	id, err := storiface.ParseSectorID(vars["id"])
	if err != nil {
		log.Errorf("parsing sectorID: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	ft, err := FileTypeFromString(vars["type"])
	if err != nil {
		log.Errorf("parsing fileType: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	// The caller has a lock on this sector already, no need to get one here
	// passing 0 spt because we don't allocate anything
	si := storiface.SectorRef{
		ID:        id,
		ProofType: 0,
	}

	paths, _, err := handler.Local.AcquireSector(r.Context(), si, ft, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err != nil {
		log.Errorf("acquiring sector from local storage: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	// TODO: reserve local storage here

	path := storiface.PathByType(paths, ft)
	if path == "" {
		log.Error("acquired path was empty")
		w.WriteHeader(500)
		return
	}

	stat, err := os.Stat(path)
	if err != nil {
		log.Errorf("failed to stat path %s: %s", path, err.Error())
		w.WriteHeader(500)
		return
	}

	if stat.IsDir() {
		if _, has := r.Header["Range"]; has {
			log.Error("Range not supported on directories")
			w.WriteHeader(500)
			return
		}

		w.Header().Set("Content-Type", "application/x-tar")
		w.WriteHeader(200)

		constraints := tarutil.CacheFileConstraints
		if _, ok := r.URL.Query()["mincache"]; ok {
			constraints = tarutil.FinCacheFileConstraints
		}

		err := tarutil.TarDirectory(constraints, path, w, make([]byte, CopyBuf))
		if err != nil {
			log.Errorf("failed to tar and send the directory %s: %s", path, err.Error())
			return
		}
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
		// will do a ranged read over the file at the given path if the caller has asked for a ranged read in the request headers.
		http.ServeFile(w, r, path)
	}

	log.Debugw("served sector file/dir", "sectorID", id, "fileType", ft, "path", path)
}

func (handler *FetchHandler) remoteDeleteSector(w http.ResponseWriter, r *http.Request) {
	log.Infof("SERVE DELETE %s", r.URL)
	vars := mux.Vars(r)

	id, err := storiface.ParseSectorID(vars["id"])
	if err != nil {
		log.Errorf("parsing sectorID: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	ft, err := FileTypeFromString(vars["type"])
	if err != nil {
		log.Errorf("parsing fileType: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	if err := handler.Local.Remove(r.Context(), id, ft, false, storiface.ParseIDList(r.FormValue("keep"))); err != nil {
		log.Errorf("removing sector: %s", err.Error())
		w.WriteHeader(500)
		return
	}
}

// remoteGetAllocated returns `http.StatusOK` if the worker already has an Unsealed sector file
// containing the Unsealed piece sent in the request.
// returns `http.StatusRequestedRangeNotSatisfiable` otherwise.
func (handler *FetchHandler) remoteGetAllocated(w http.ResponseWriter, r *http.Request) {
	log.Infof("SERVE Alloc check %s", r.URL)
	vars := mux.Vars(r)

	id, err := storiface.ParseSectorID(vars["id"])
	if err != nil {
		log.Errorf("parsing sectorID: %+v", err)
		w.WriteHeader(500)
		return
	}

	ft, err := FileTypeFromString(vars["type"])
	if err != nil {
		log.Errorf("parsing fileType: %s", err.Error())
		w.WriteHeader(500)
		return
	}
	if ft != storiface.FTUnsealed {
		log.Errorf("/allocated only supports unsealed sector files")
		w.WriteHeader(500)
		return
	}

	spti, err := strconv.ParseInt(vars["spt"], 10, 64)
	if err != nil {
		log.Errorf("parsing registered seal proof type: %s", err.Error())
		w.WriteHeader(500)
		return
	}
	spt := abi.RegisteredSealProof(spti)
	ssize, err := spt.SectorSize()
	if err != nil {
		log.Errorf("spt.SectorSize(): %s", err.Error())
		w.WriteHeader(500)
		return
	}

	offi, err := strconv.ParseInt(vars["offset"], 10, 64)
	if err != nil {
		log.Errorf("parsing offset: %s", err.Error())
		w.WriteHeader(500)
		return
	}
	szi, err := strconv.ParseInt(vars["size"], 10, 64)
	if err != nil {
		log.Errorf("parsing size: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	// The caller has a lock on this sector already, no need to get one here

	// passing 0 spt because we don't allocate anything
	si := storiface.SectorRef{
		ID:        id,
		ProofType: 0,
	}

	// get the path of the local Unsealed file for the given sector.
	// return error if we do NOT have it.
	paths, _, err := handler.Local.AcquireSector(r.Context(), si, ft, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err != nil {
		log.Errorf("acquiring sector on local storage: %s", err.Error())
		w.WriteHeader(500)
		return
	}

	path := storiface.PathByType(paths, ft)
	if path == "" {
		log.Error("acquired path was empty")
		w.WriteHeader(500)
		return
	}

	// open the Unsealed file and check if it has the Unsealed sector for the piece at the given offset and size.
	pf, err := handler.PfHandler.OpenPartialFile(abi.PaddedPieceSize(ssize), path)
	if err != nil {
		log.Error("opening partial file: ", err)
		w.WriteHeader(500)
		return
	}
	defer func() {
		if err := pf.Close(); err != nil {
			log.Error("closing partial file: ", err)
		}
	}()

	has, err := handler.PfHandler.HasAllocated(pf, storiface.UnpaddedByteIndex(offi), abi.UnpaddedPieceSize(szi))
	if err != nil {
		log.Error("has allocated: ", err)
		w.WriteHeader(500)
		return
	}

	if has {
		log.Debugf("returning ok: worker has unsealed file with unsealed piece, sector:%+v, offset:%d, size:%d", id, offi, szi)
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Debugf("returning StatusRequestedRangeNotSatisfiable: worker does NOT have unsealed file with unsealed piece, sector:%+v, offset:%d, size:%d", id, offi, szi)
	w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
}

func (handler *FetchHandler) remoteGetStash(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]

	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid UUID", http.StatusBadRequest)
		return
	}

	readCloser, err := handler.Local.ServeAndRemove(r.Context(), id)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "stash not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return
	}
	defer readCloser.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	_, err = io.Copy(w, readCloser)
	if err != nil {
		log.Errorf("error copying stash data: %+v", err)
		// If the read was incomplete, ServeAndRemove won't remove the file
		return
	}
}

type SingleVanillaParams struct {
	Miner     abi.ActorID
	Sector    storiface.PostSectorChallenge
	ProofType abi.RegisteredPoStProof
}

func (handler *FetchHandler) generateSingleVanillaProof(w http.ResponseWriter, r *http.Request) {
	var params SingleVanillaParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	vanilla, err := handler.Local.GenerateSingleVanillaProof(r.Context(), params.Miner, params.Sector, params.ProofType)
	if err != nil {
		log.Errorw("failed to generate single vanilla proof:", "miner", params.Miner, "sector", params.Sector, "proofType", params.ProofType, "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(vanilla))
}

type PoRepVanillaParams struct {
	Sector   storiface.SectorRef
	Sealed   cid.Cid
	Unsealed cid.Cid
	Ticket   abi.SealRandomness
	Seed     abi.InteractiveSealRandomness
}

func (handler *FetchHandler) generatePoRepVanillaProof(w http.ResponseWriter, r *http.Request) {
	var params PoRepVanillaParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	vanilla, err := handler.Local.GeneratePoRepVanillaProof(r.Context(), params.Sector, params.Sealed, params.Unsealed, params.Ticket, params.Seed)
	if err != nil {
		log.Errorw(
			"failed to generate porep vanilla proof",
			"sector", params.Sector,
			"sealed", params.Sealed,
			"unsealed", params.Unsealed,
			"ticket", params.Ticket,
			"seed", params.Seed,
			"err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(vanilla))
}

type SnapVanillaParams struct {
	Sector storiface.SectorRef
}

func (handler *FetchHandler) readSnapVanillaProof(w http.ResponseWriter, r *http.Request) {
	var params SnapVanillaParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	vanilla, err := handler.Local.ReadSnapVanillaProof(r.Context(), params.Sector)
	if err != nil {
		log.Errorw("failed to read snap vanilla proof", "sector", params.Sector, "err", err)
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(vanilla))
}

func FileTypeFromString(t string) (storiface.SectorFileType, error) {
	return storiface.TypeFromString(t)
}
