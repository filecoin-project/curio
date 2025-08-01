package proofshare

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/lib/storiface"
)

type UpdateSectorInfo struct {
	SpID            int64                     `db:"sp_id"`
	SectorNumber    int64                     `db:"sector_number"`
	UpdateProofType abi.RegisteredUpdateProof `db:"upgrade_proof"`
	RegSealProof    abi.RegisteredSealProof   `db:"reg_seal_proof"`

	OldR proof.Commitment
	NewR proof.Commitment
	NewD proof.Commitment
}

func (s *UpdateSectorInfo) SectorID() abi.SectorID {
	return abi.SectorID{
		Miner:  abi.ActorID(s.SpID),
		Number: abi.SectorNumber(s.SectorNumber),
	}
}

func (t *TaskClientUpload) adderSnap(add harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {

		for range ticker.C {
			var more bool
			log.Infow("TaskClientUpload.adderSnap() ticker fired, looking for sectors to process")

		again:
			add(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Check if client settings are enabled for PoRep
				var enabledFor []struct {
					SpID                  int64  `db:"sp_id"`
					MinimumPendingSeconds int64  `db:"minimum_pending_seconds"`
					Wallet                string `db:"wallet"`
				}
				err := tx.Select(&enabledFor, `
					SELECT sp_id, minimum_pending_seconds, wallet
					FROM proofshare_client_settings
					WHERE enabled = TRUE AND do_snap = TRUE
				`)
				if err != nil {
					log.Errorw("TaskClientUpload.adderSnap() failed to query client settings", "error", err)
					return false, xerrors.Errorf("failed to query client settings: %w", err)
				}

				if len(enabledFor) == 0 {
					log.Infow("TaskClientUpload.adderSnap() no enabled client settings found for SnapPRU")
					return false, nil
				}

				// get the minimum pending seconds
				minPendingSeconds := enabledFor[0].MinimumPendingSeconds
				log.Infow("TaskClientUpload.adderSnap() found enabled client settings", "count", len(enabledFor), "minPendingSeconds", minPendingSeconds)

				// claim [sectors] pipeline entries
				var sectors []struct {
					SpID         int64  `db:"sp_id"`
					SectorNumber int64  `db:"sector_number"`
					TaskIDProve  *int64 `db:"task_id_prove"`
				}

				cutoffTime := time.Now().Add(-time.Duration(minPendingSeconds) * time.Second)
				log.Infow("TaskClientUpload.adderSnap() querying for sectors with cutoff time", "cutoffTime", cutoffTime)

				err = tx.Select(&sectors, `SELECT sp_id, sector_number, task_id_prove FROM sectors_snap_pipeline
												LEFT JOIN harmony_task ht on sectors_snap_pipeline.task_id_prove = ht.id
												WHERE after_prove = FALSE AND task_id_prove IS NOT NULL AND ht.owner_id IS NULL AND ht.name = 'UpdateProve' AND ht.posted_time < $1 LIMIT 1`, cutoffTime)
				if err != nil {
					log.Errorw("TaskClientUpload.adderSnap() failed to query sectors", "error", err)
					return false, xerrors.Errorf("getting tasks: %w", err)
				}

				if len(sectors) == 0 {
					log.Infow("TaskClientUpload.adderSnap() no sectors found to process")
					return false, nil
				}

				log.Infow("TaskClientUpload.adderSnap() creating task", "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)

				// Create task
				_, err = tx.Exec(`
					UPDATE sectors_snap_pipeline
					SET task_id_prove = $1
					WHERE sp_id = $2 AND sector_number = $3
				`, taskID, sectors[0].SpID, sectors[0].SectorNumber)
				if err != nil {
					log.Errorw("TaskClientUpload.adderSnap() failed to update sector", "error", err, "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)
					return false, xerrors.Errorf("failed to update sector: %w", err)
				}

				if sectors[0].TaskIDProve != nil {
					log.Infow("TaskClientUpload.adderSnap() deleting old task", "oldTaskID", *sectors[0].TaskIDProve, "newTaskID", taskID)
					_, err := tx.Exec(`DELETE FROM harmony_task WHERE id = $1`, *sectors[0].TaskIDProve)
					if err != nil {
						log.Errorw("TaskClientUpload.adderSnap() failed to delete old task", "error", err, "oldTaskID", *sectors[0].TaskIDProve)
						return false, xerrors.Errorf("deleting old task: %w", err)
					}
				}

				// Insert proofshare_client_requests
				_, err = tx.Exec(`
					INSERT INTO proofshare_client_requests (sp_id, sector_num, request_type, task_id_upload, request_partition_cost, created_at)
					VALUES ($1, $2, 'snap', $3, 16, current_timestamp)
				`, sectors[0].SpID, sectors[0].SectorNumber, taskID)
				if err != nil {
					log.Errorw("TaskClientUpload.adderSnap() failed to insert proofshare_client_requests", "error", err, "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)
					return false, xerrors.Errorf("failed to insert proofshare_client_requests: %w", err)
				}

				more = true
				return true, nil
			})

			if more {
				more = false
				log.Infow("TaskClientUpload.adderSnap() more sectors to process, continuing")
				goto again
			}
		}
	}()
}

func getSectorInfoSnap(ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID) (*UpdateSectorInfo, error) {
	var info UpdateSectorInfo
	var cidOldRs, cidNewRs, cidNewDs string

	err := db.QueryRow(ctx, `
		SELECT sp.sp_id, sp.sector_number, sm.reg_seal_proof, sp.upgrade_proof, sp.update_unsealed_cid, sp.update_sealed_cid, sm.orig_sealed_cid
		FROM sectors_snap_pipeline sp
		LEFT JOIN sectors_meta sm ON sp.sp_id = sm.sp_id AND sp.sector_number = sm.sector_num
		WHERE sp.task_id_prove = $1
	`, taskID).Scan(
		&info.SpID, &info.SectorNumber, &info.RegSealProof, &info.UpdateProofType,
		&cidNewDs, &cidNewRs, &cidOldRs,
	)
	if err != nil {
		return nil, xerrors.Errorf("failed to get sector info: %w", err)
	}

	// Parse CIDs
	cidOldR, err1 := cid.Parse(cidOldRs)
	cidNewR, err2 := cid.Parse(cidNewRs)
	cidNewD, err3 := cid.Parse(cidNewDs)
	if err1 != nil {
		return nil, xerrors.Errorf("failed to parse old r cid (%s): %w", cidOldRs, err1)
	}
	if err2 != nil {
		return nil, xerrors.Errorf("failed to parse new r cid (%s): %w", cidNewRs, err2)
	}
	if err3 != nil {
		return nil, xerrors.Errorf("failed to parse new d cid (%s): %w", cidNewDs, err3)
	}

	cmOldR, err := commcid.CIDToReplicaCommitmentV1(cidOldR)
	if err != nil {
		return nil, xerrors.Errorf("failed to convert old r cid to commitment (%s): %w", cidOldRs, err)
	}
	cmNewR, err := commcid.CIDToReplicaCommitmentV1(cidNewR)
	if err != nil {
		return nil, xerrors.Errorf("failed to convert new r cid to commitment (%s): %w", cidNewRs, err)
	}
	cmNewD, err := commcid.CIDToDataCommitmentV1(cidNewD)
	if err != nil {
		return nil, xerrors.Errorf("failed to convert new d cid to commitment (%s): %w", cidNewDs, err)
	}

	copy(info.OldR[:], cmOldR)
	copy(info.NewR[:], cmNewR)
	copy(info.NewD[:], cmNewD)

	log.Infow("update sector info", "taskID", taskID, "spID", info.SpID, "sectorNumber", info.SectorNumber)
	return &info, nil
}

func (t *TaskClientUpload) getProofDataSnap(ctx context.Context, taskID harmonytask.TaskID) ([]byte, abi.SectorID, error) {
	sectorInfo, err := getSectorInfoSnap(ctx, t.db, taskID)
	if err != nil {
		return nil, abi.SectorID{}, err
	}

	// Create PoRep request
	spt := abi.RegisteredSealProof(sectorInfo.RegSealProof)
	p, err := t.storage.ReadSnapVanillaProof(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorInfo.SpID),
			Number: abi.SectorNumber(sectorInfo.SectorNumber),
		},
		ProofType: spt,
	})
	if err != nil {
		return nil, abi.SectorID{}, xerrors.Errorf("failed to generate snap vanilla proof: %w", err)
	}

	var vproofs [][]byte
	if err := json.Unmarshal(p, &vproofs); err != nil {
		return nil, abi.SectorID{}, xerrors.Errorf("failed to unmarshal proof: %w", err)
	}

	// Create ProofData
	proofData := common.ProofData{
		SectorID: &abi.SectorID{
			Miner:  abi.ActorID(sectorInfo.SpID),
			Number: abi.SectorNumber(sectorInfo.SectorNumber),
		},
		Snap: &proof.Snap{
			ProofType: sectorInfo.UpdateProofType,
			OldR:      sectorInfo.OldR,
			NewR:      sectorInfo.NewR,
			NewD:      sectorInfo.NewD,
			Proofs:    vproofs,
		},
	}

	// Serialize the ProofData
	proofDataBytes, err := json.Marshal(proofData)
	if err != nil {
		return nil, abi.SectorID{}, xerrors.Errorf("failed to marshal proof data: %w", err)
	}

	return proofDataBytes, sectorInfo.SectorID(), nil
}
