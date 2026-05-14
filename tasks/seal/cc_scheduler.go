package seal

import (
	"context"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/builtin"
	miner12 "github.com/filecoin-project/go-state-types/builtin/v12/miner"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/passcall"

	lotusapi "github.com/filecoin-project/lotus/api"
	apitypes "github.com/filecoin-project/lotus/api/types"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

// CCSchedulerAPI extends SDRAPI with methods needed for CC sector scheduling:
// allocating sector numbers and determining proof types from chain state.
type CCSchedulerAPI interface {
	SDRAPI
	StateMinerAllocated(context.Context, address.Address, types.TipSetKey) (*bitfield.BitField, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (lotusapi.MinerInfo, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (apitypes.NetworkVersion, error)
}

type ccSchedule struct {
	SpID         int64 `db:"sp_id"`
	ToSeal       int64 `db:"to_seal"`
	DurationDays int64 `db:"duration_days"`
}

// scheduleCC is the IAmBored callback for the SDR task. It creates new CC sectors
// from the sectors_cc_scheduler table for SPs that do NOT have an enabled remote
// seal provider. This ensures CC sectors are only created where SDR can actually run
// locally (IAmBored is only invoked when the machine has SDR capacity).
//
// SPs with enabled remote providers are handled by RSealDelegate instead.
func (s *SDRTask) scheduleCC(taskFunc harmonytask.AddTaskFunc) error {
	if s.ccAPI == nil {
		return nil // CC scheduling not configured
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find enabled CC schedules for SPs that do NOT have any enabled remote providers.
	// If an SP has an enabled remote provider, RSealDelegate handles CC scheduling for it.
	var schedules []ccSchedule
	err := s.db.Select(ctx, &schedules, `
		SELECT cs.sp_id, cs.to_seal, cs.duration_days
		FROM sectors_cc_scheduler cs
		WHERE cs.enabled = TRUE
		  AND cs.to_seal > 0
		  AND NOT EXISTS (
		    SELECT 1 FROM rseal_client_providers rcp
		    WHERE rcp.sp_id = cs.sp_id AND rcp.enabled = TRUE
		  )
		ORDER BY cs.sp_id`)
	if err != nil {
		return xerrors.Errorf("querying cc_scheduler: %w", err)
	}

	if len(schedules) == 0 {
		return nil
	}

	nv, err := s.ccAPI.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting network version: %w", err)
	}

	// Create one sector per IAmBored invocation (conservative — only when capacity confirmed).
	// Pick the first SP with to_seal > 0.
	schedule := schedules[0]

	maddr, err := address.NewIDAddress(uint64(schedule.SpID))
	if err != nil {
		return xerrors.Errorf("creating miner address: %w", err)
	}

	mi, err := s.ccAPI.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner info for %s: %w", maddr, err)
	}

	spt, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, mi.WindowPoStProofType, false)
	if err != nil {
		return xerrors.Errorf("getting seal proof type: %w", err)
	}

	userDuration := schedule.DurationDays * builtin.EpochsInDay
	if miner12.MaxSectorExpirationExtension < userDuration {
		return xerrors.Errorf("duration exceeds max allowed: %d > %d", userDuration, miner12.MaxSectorExpirationExtension)
	}
	if miner12.MinSectorExpiration > userDuration {
		return xerrors.Errorf("duration is too short: %d < %d", userDuration, miner12.MinSectorExpiration)
	}

	taskFunc(func(id harmonytask.TaskID, tx *harmonydb.Tx) (bool, error) {
		// Allocate one sector number inside the transaction
		sectorNumbers, err := AllocateSectorNumbers(ctx, s.ccAPI, tx, maddr, 1)
		if err != nil {
			return false, xerrors.Errorf("allocating sector number: %w", err)
		}
		if len(sectorNumbers) != 1 {
			return false, xerrors.Errorf("expected 1 sector number, got %d", len(sectorNumbers))
		}

		sectorNum := sectorNumbers[0]

		// Insert into sectors_sdr_pipeline
		_, err = tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof, user_sector_duration_epochs)
			VALUES ($1, $2, $3, $4)`,
			schedule.SpID, sectorNum, spt, userDuration)
		if err != nil {
			return false, xerrors.Errorf("inserting sector %d for SP %d: %w", sectorNum, schedule.SpID, err)
		}

		// Assign SDR task to the new sector
		n, err := tx.Exec(`UPDATE sectors_sdr_pipeline SET task_id_sdr = $1
			WHERE sp_id = $2 AND sector_number = $3 AND task_id_sdr IS NULL`,
			id, schedule.SpID, sectorNum)
		if err != nil {
			return false, xerrors.Errorf("setting task_id_sdr: %w", err)
		}
		if n != 1 {
			return false, xerrors.Errorf("expected to claim 1 sector, got %d", n)
		}

		// Decrement to_seal
		_, err = tx.Exec(`UPDATE sectors_cc_scheduler SET to_seal = to_seal - 1 WHERE sp_id = $1 AND to_seal > 0`, schedule.SpID)
		if err != nil {
			return false, xerrors.Errorf("decrementing to_seal: %w", err)
		}

		log.Infow("CC scheduler: created local CC sector",
			"sp_id", schedule.SpID,
			"sector", sectorNum,
			"proof", spt,
			"duration_days", schedule.DurationDays)

		return true, nil
	})

	return nil
}

// NewScheduleCCFunc returns a rate-limited scheduleCC suitable for IAmBored.
func (s *SDRTask) newScheduleCCFunc() func(harmonytask.AddTaskFunc) error {
	return passcall.Every(15*time.Second, s.scheduleCC)
}
