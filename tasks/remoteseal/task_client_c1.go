package remoteseal

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/market/sealmarket"
)

// RSealClientC1Exchange exchanges the C1 seed for C1 output with the remote provider.
// This runs after precommit lands on chain and the seed epoch is available.
// The provider computes SealCommit1 using the seed and returns the vanilla proofs,
// which are then used by the local PoRep (C2) task.
type RSealClientC1Exchange struct {
	db     *harmonydb.DB
	client *RSealClient
	sp     *RSealClientPoller
}

func NewRSealClientC1Exchange(db *harmonydb.DB, client *RSealClient, sp *RSealClientPoller) *RSealClientC1Exchange {
	return &RSealClientC1Exchange{
		db:     db,
		client: client,
		sp:     sp,
	}
}

func (c *RSealClientC1Exchange) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Find the sector assigned to this C1 exchange task
	var sectors []struct {
		SpID          int64  `db:"sp_id"`
		SectorNumber  int64  `db:"sector_number"`
		RegSealProof  int    `db:"reg_seal_proof"`
		ProviderURL   string `db:"provider_url"`
		ProviderToken string `db:"provider_token"`
		SeedEpoch     int64  `db:"seed_epoch"`
		SeedValue     []byte `db:"seed_value"`
	}

	err = c.db.Select(ctx, &sectors, `
		SELECT c.sp_id, c.sector_number, c.reg_seal_proof,
		       pr.provider_url, pr.provider_token,
		       s.seed_epoch, s.seed_value
		FROM rseal_client_pipeline c
		JOIN rseal_client_providers pr ON c.provider_id = pr.id
		JOIN sectors_sdr_pipeline s ON c.sp_id = s.sp_id AND c.sector_number = s.sector_number
		WHERE c.task_id_c1_exchange = $1`, taskID)
	if err != nil {
		return false, xerrors.Errorf("querying sector for c1 exchange: %w", err)
	}

	if len(sectors) != 1 {
		return false, xerrors.Errorf("expected 1 sector for c1 exchange, got %d", len(sectors))
	}
	sector := sectors[0]

	// Send C1 request to provider
	c1Resp, err := c.client.SendCommit1(ctx, sector.ProviderURL, sector.ProviderToken, &sealmarket.Commit1Request{
		SpID:         sector.SpID,
		SectorNumber: sector.SectorNumber,
		SeedEpoch:    sector.SeedEpoch,
		SeedValue:    sector.SeedValue,
	})
	if err != nil {
		return false, xerrors.Errorf("sending commit1 to provider: %w", err)
	}

	if len(c1Resp.C1Output) == 0 {
		return false, xerrors.Errorf("provider returned empty C1 output")
	}

	// Sanity-check C1 output size bounds.
	// Full pre-validation via validatePoRep() is not feasible here because
	// SealCommit1 returns a binary format (vanilla proofs) that differs from
	// the Commit1OutRaw bincode format expected by the proof validator.
	// The actual cryptographic validation happens later in PoRepSnarkWithVanilla
	// which calls VerifySeal().
	const minC1Size = 1 << 10  // 1 KiB - vanilla proofs are at least this large
	const maxC1Size = 10 << 20 // 10 MiB - well above expected ~192 KiB
	if len(c1Resp.C1Output) < minC1Size || len(c1Resp.C1Output) > maxC1Size {
		return false, xerrors.Errorf("C1 output size %d out of expected range [%d, %d]", len(c1Resp.C1Output), minC1Size, maxC1Size)
	}

	if !stillOwned() {
		return false, xerrors.Errorf("task no longer owned")
	}

	// Store C1 output and mark exchange as done.
	// The C1 output (vanilla proofs) is stored in rseal_client_pipeline.c1_output.
	// The PoRep task reads this and skips its own SealCommit1 call for remote-sealed
	// sectors, proceeding directly to SealCommit2.
	_, err = c.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		n, err := tx.Exec(`
			UPDATE rseal_client_pipeline
			SET after_c1_exchange = TRUE, task_id_c1_exchange = NULL, c1_output = $3
			WHERE sp_id = $1 AND sector_number = $2`,
			sector.SpID, sector.SectorNumber, c1Resp.C1Output)
		if err != nil {
			return false, xerrors.Errorf("updating rseal_client_pipeline: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to update 1 rseal_client_pipeline row, updated %d", n)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return false, xerrors.Errorf("c1 exchange transaction: %w", err)
	}

	log.Infow("c1 exchange completed",
		"sp_id", sector.SpID, "sector", sector.SectorNumber,
		"c1_output_size", len(c1Resp.C1Output))

	return true, nil
}

func (c *RSealClientC1Exchange) CanAccept(ids []harmonytask.TaskID, _ *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

func (c *RSealClientC1Exchange) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "RSealClientC1",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 64 << 20, // 64 MiB - C1 output can be significant
		},
		MaxFailures: 20,
		RetryWait:   taskhelp.RetryWaitLinear(30*time.Second, 30*time.Second),
	}
}

func (c *RSealClientC1Exchange) Adder(taskFunc harmonytask.AddTaskFunc) {
	c.sp.pollers[pollerClientC1Exchange].Set(taskFunc)
}

func (c *RSealClientC1Exchange) GetSpid(db *harmonydb.DB, taskID int64) string {
	sid, err := c.GetSectorID(db, taskID)
	if err != nil {
		log.Errorf("getting sector id: %s", err)
		return ""
	}
	return sid.Miner.String()
}

func (c *RSealClientC1Exchange) GetSectorID(db *harmonydb.DB, taskID int64) (*abi.SectorID, error) {
	var spId, sectorNumber uint64
	err := db.QueryRow(context.Background(),
		`SELECT sp_id, sector_number FROM rseal_client_pipeline WHERE task_id_c1_exchange = $1`, taskID).Scan(&spId, &sectorNumber)
	if err != nil {
		return nil, err
	}
	return &abi.SectorID{
		Miner:  abi.ActorID(spId),
		Number: abi.SectorNumber(sectorNumber),
	}, nil
}

var _ = harmonytask.Reg(&RSealClientC1Exchange{})
var _ harmonytask.TaskInterface = &RSealClientC1Exchange{}
