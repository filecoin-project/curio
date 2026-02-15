package remoteseal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	"github.com/filecoin-project/curio/lib/tarutil"
	"github.com/filecoin-project/curio/market/sealmarket"
)

// RSealClientFetch downloads the sealed sector file and finalized cache tar
// from the remote provider after SDR+trees complete remotely. The sealed file
// is streamed directly to disk (32 GiB), and the cache tar is extracted into
// the local cache directory.
type RSealClientFetch struct {
	db     *harmonydb.DB
	client *RSealClient
	sc     *ffi.SealCalls
	sp     *RSealClientPoller
}

func NewRSealClientFetch(db *harmonydb.DB, client *RSealClient, sc *ffi.SealCalls, sp *RSealClientPoller) *RSealClientFetch {
	return &RSealClientFetch{
		db:     db,
		client: client,
		sc:     sc,
		sp:     sp,
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

	// Build SectorRef
	sref := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sector.SpID),
			Number: abi.SectorNumber(sector.SectorNumber),
		},
		ProofType: abi.RegisteredSealProof(sector.RegSealProof),
	}

	// Allocate local storage for sealed + cache
	sealedPaths, _, releaseSealed, err := f.sc.Sectors.AcquireSector(ctx, nil, sref, storiface.FTNone, storiface.FTSealed|storiface.FTCache, storiface.PathStorage)
	if err != nil {
		return false, xerrors.Errorf("acquiring sector storage: %w", err)
	}
	defer releaseSealed()

	if sealedPaths.Sealed == "" {
		return false, xerrors.Errorf("no sealed path allocated")
	}
	if sealedPaths.Cache == "" {
		return false, xerrors.Errorf("no cache path allocated")
	}

	// Download sealed file from provider
	log.Infow("downloading sealed file from provider",
		"sp_id", sector.SpID, "sector", sector.SectorNumber,
		"sealed_path", sealedPaths.Sealed)

	err = f.client.FetchSealedData(ctx, sector.ProviderURL, sector.ProviderToken,
		sector.SpID, sector.SectorNumber, sealedPaths.Sealed)
	if err != nil {
		return false, xerrors.Errorf("fetching sealed data: %w", err)
	}

	// Download cache tar from provider and extract
	log.Infow("downloading cache data from provider",
		"sp_id", sector.SpID, "sector", sector.SectorNumber,
		"cache_path", sealedPaths.Cache)

	err = f.client.FetchCacheData(ctx, sector.ProviderURL, sector.ProviderToken,
		sector.SpID, sector.SectorNumber, sealedPaths.Cache)
	if err != nil {
		return false, xerrors.Errorf("fetching cache data: %w", err)
	}

	// Write c1.url file in cache dir so that GeneratePoRepVanillaProof can fetch
	// C1 output from the remote provider when the PoRep task runs.
	c1Info := paths.RemoteSealC1Info{
		C1URL:        fmt.Sprintf("%s%scommit1", sector.ProviderURL, sealmarket.DelegatedSealPath),
		PartnerToken: sector.ProviderToken,
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
	}
	c1InfoJSON, err := json.Marshal(c1Info)
	if err != nil {
		return false, xerrors.Errorf("marshaling c1 url info: %w", err)
	}
	if err := os.WriteFile(filepath.Join(sealedPaths.Cache, paths.RemoteSealC1UrlFile), c1InfoJSON, 0644); err != nil {
		return false, xerrors.Errorf("writing c1.url file: %w", err)
	}

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// Mark fetch as done
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
	return harmonytask.TaskTypeDetails{
		Name: "RSealClientFetch",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20, // 64 MiB - streaming to disk
		},
		MaxFailures: 20,
		RetryWait:   taskhelp.RetryWaitLinear(5*time.Minute, 5*time.Minute),
	}
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

// FetchSealedData downloads the sealed sector file from the provider and writes it to disk.
// GET /remoteseal/delegated/v0/sealed-data/{sp_id}/{sector_number}?token=...
func (c *RSealClient) FetchSealedData(ctx context.Context, providerURL, token string, spID, sectorNumber int64, destPath string) error {
	url := fmt.Sprintf("%s%ssealed-data/%d/%d?token=%s",
		providerURL, sealmarket.DelegatedSealPath, spID, sectorNumber, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return xerrors.Errorf("creating request: %w", err)
	}

	// Use a client without the default 30s timeout for large file downloads
	dlClient := &http.Client{}
	resp, err := dlClient.Do(req)
	if err != nil {
		return xerrors.Errorf("performing request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("unexpected status %d from %s: %s", resp.StatusCode, url, string(body))
	}

	// Stream directly to disk
	f, err := os.Create(destPath)
	if err != nil {
		return xerrors.Errorf("creating sealed file %s: %w", destPath, err)
	}

	buf := make([]byte, 1<<20) // 1 MiB buffer
	_, err = io.CopyBuffer(f, resp.Body, buf)
	if err != nil {
		_ = f.Close()
		return xerrors.Errorf("writing sealed data to %s: %w", destPath, err)
	}

	if err := f.Close(); err != nil {
		return xerrors.Errorf("closing sealed file %s: %w", destPath, err)
	}

	return nil
}

// FetchCacheData downloads the finalized cache tar from the provider and extracts it.
// GET /remoteseal/delegated/v0/cache-data/{sp_id}/{sector_number}?token=...
func (c *RSealClient) FetchCacheData(ctx context.Context, providerURL, token string, spID, sectorNumber int64, cachePath string) error {
	url := fmt.Sprintf("%s%scache-data/%d/%d?token=%s",
		providerURL, sealmarket.DelegatedSealPath, spID, sectorNumber, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return xerrors.Errorf("creating request: %w", err)
	}

	// Use a client without the default 30s timeout for cache downloads
	dlClient := &http.Client{}
	resp, err := dlClient.Do(req)
	if err != nil {
		return xerrors.Errorf("performing request to %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("unexpected status %d from %s: %s", resp.StatusCode, url, string(body))
	}

	buf := make([]byte, 1<<20) // 1 MiB buffer
	_, err = tarutil.ExtractTar(tarutil.FinCacheFileConstraints, resp.Body, cachePath, buf)
	if err != nil {
		return xerrors.Errorf("extracting cache tar to %s: %w", cachePath, err)
	}

	return nil
}

var _ = harmonytask.Reg(&RSealClientFetch{})
var _ harmonytask.TaskInterface = &RSealClientFetch{}
