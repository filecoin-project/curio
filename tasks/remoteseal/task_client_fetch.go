package remoteseal

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	ffi "github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"
)

// RSealClientFetch downloads the sealed sector file and finalized cache tar
// from the remote provider after SDR+trees complete remotely. The sealed file
// is streamed directly to disk (32 GiB), and the cache tar is extracted into
// the local cache directory.
type RSealClientFetch struct {
	db  *harmonydb.DB
	sc  *ffi.SealCalls
	sp  *RSealClientPoller
	max int
}

func NewRSealClientFetch(db *harmonydb.DB, client *RSealClient, sc *ffi.SealCalls, sp *RSealClientPoller, max int) *RSealClientFetch {
	return &RSealClientFetch{
		db:  db,
		sc:  sc,
		sp:  sp,
		max: max,
	}
}

func (f *RSealClientFetch) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Find the sector assigned to this fetch task
	var sectors []struct {
		SpID          int64  `db:"sp_id"`
		SectorNumber  int64  `db:"sector_number"`
		RegSealProof  int    `db:"reg_seal_proof"`
		ProviderURL   string `db:"provider_url"`
		ProviderToken string `db:"provider_token"`
	}

	err = f.db.Select(ctx, &sectors, `
		SELECT c.sp_id, c.sector_number, c.reg_seal_proof,
		       pr.provider_url, pr.provider_token
		FROM rseal_client_pipeline c
		JOIN rseal_client_providers pr ON c.provider_id = pr.id
		WHERE c.task_id_fetch = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("querying sector for fetch task: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for fetch task, got %d", len(sectors))
	}
	sector := sectors[0]

	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sector.SpID),
			Number: abi.SectorNumber(sector.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sector.RegSealProof),
	}

	log.Infow("downloading remote seal data",
		"sp_id", sector.SpID, "sector", sector.SectorNumber,
		"provider", sector.ProviderURL)

	err = f.sc.DownloadRemoteSealData(ctx, &taskID, sref, ffi.RemoteSealFetchParams{
		SealedURL: endpoint(sector.ProviderURL, fmt.Sprintf("sealed-data/%d/%d", sector.SpID, sector.SectorNumber)) + "?token=" + sector.ProviderToken,
		CacheURL:  endpoint(sector.ProviderURL, fmt.Sprintf("cache-data/%d/%d", sector.SpID, sector.SectorNumber)) + "?token=" + sector.ProviderToken,
		C1Info: paths.RemoteSealC1Info{
			C1URL:        endpoint(sector.ProviderURL, "commit1"),
			PartnerToken: sector.ProviderToken,
			SpID:         sector.SpID,
			SectorNumber: sector.SectorNumber,
		},
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
	})
	if err != nil {
		return false, xerrors.Errorf("downloading remote seal data: %w", err)
	}

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// Mark fetch as done — sealed data is now in local storage so the sector
	// can proceed through the normal pipeline (precommit, PoRep, etc).
	_, err = f.db.Exec(ctx, `
		UPDATE rseal_client_pipeline
		SET after_fetch = TRUE, task_id_fetch = NULL
		WHERE sp_id = $1 AND sector_number = $2`,
		sector.SpID, sector.SectorNumber)
	if err != nil {
		return false, xerrors.Errorf("updating rseal_client_pipeline: %w", err)
	}

	log.Infow("remote seal fetch completed",
		"sp_id", sector.SpID, "sector", sector.SectorNumber)

	return true, nil
}

func (f *RSealClientFetch) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (f *RSealClientFetch) TypeDetails() harmonytask.TaskTypeDetails {
	ssize := abi.SectorSize(32 << 30) // todo task details needs taskID to get correct sector size

	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(f.max),
		Name: "RSealClientFetch",
		Cost: resources.Resources{
			Cpu:     0,
			Gpu:     0,
			Ram:     64 << 20, // 64 MiB - streaming to disk
			Storage: f.sc.Storage(f.taskToSector, storiface.FTCache|storiface.FTSealed, storiface.FTNone, ssize, storiface.PathStorage, paths.MinFreeStoragePercentage),
		},
		MaxFailures: 20,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Minute, 5*time.Minute),
	}
}

func (f *RSealClientFetch) taskToSector(id harmonytask.TaskID) (ffi.SectorRef, error) {
	var refs []ffi.SectorRef

	err := f.db.Select(context.Background(), &refs, `SELECT sp_id, sector_number, reg_seal_proof FROM rseal_client_pipeline WHERE task_id_fetch = $1`, id)
	if err != nil {
		return ffi.SectorRef{}, xerrors.Errorf("getting sector ref: %w", err)
	}

	if len(refs) != 1 {
		return ffi.SectorRef{}, xerrors.Errorf("expected 1 sector ref, got %d", len(refs))
	}

	return refs[0], nil
}

func (f *RSealClientFetch) Adder(taskFunc harmonytask.AddTaskFunc) {
	f.sp.pollers[pollerClientFetch].Set(taskFunc)
}

func (f *RSealClientFetch) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := f.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (f *RSealClientFetch) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(),
		`SELECT sp_id, sector_number FROM rseal_client_pipeline WHERE task_id_fetch = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealClientFetch{})
var _ harmonytask.TaskInterface = &RSealClientFetch{}
