package proofshare

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/lib/storiface"
)

func (t *TaskClientUpload) adderPorep(add harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {

		for range ticker.C {
			var more bool
			log.Infow("TaskClientUpload.adderPorep() ticker fired, looking for sectors to process")

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
					WHERE enabled = TRUE AND do_porep = TRUE
				`)
				if err != nil {
					log.Errorw("TaskClientUpload.adderPorep() failed to query client settings", "error", err)
					return false, xerrors.Errorf("failed to query client settings: %w", err)
				}

				if len(enabledFor) == 0 {
					log.Infow("TaskClientUpload.adderPorep() no enabled client settings found for PoRep")
					return false, nil
				}

				// get the minimum pending seconds
				minPendingSeconds := enabledFor[0].MinimumPendingSeconds
				log.Infow("TaskClientUpload.adderPorep() found enabled client settings", "count", len(enabledFor), "minPendingSeconds", minPendingSeconds)

				// claim [sectors] pipeline entries
				var sectors []struct {
					SpID         int64  `db:"sp_id"`
					SectorNumber int64  `db:"sector_number"`
					TaskIDPorep  *int64 `db:"task_id_porep"`
				}

				cutoffTime := time.Now().Add(-time.Duration(minPendingSeconds) * time.Second)
				log.Infow("TaskClientUpload.adderPorep() querying for sectors with cutoff time", "cutoffTime", cutoffTime)

				err = tx.Select(&sectors, `SELECT sp_id, sector_number, task_id_porep FROM sectors_sdr_pipeline
												LEFT JOIN harmony_task ht on sectors_sdr_pipeline.task_id_porep = ht.id
												WHERE after_porep = FALSE AND task_id_porep IS NOT NULL AND ht.owner_id IS NULL AND ht.name = 'PoRep' AND ht.posted_time < $1 LIMIT 1`, cutoffTime)
				if err != nil {
					log.Errorw("TaskClientUpload.adderPorep() failed to query sectors", "error", err)
					return false, xerrors.Errorf("getting tasks: %w", err)
				}

				if len(sectors) == 0 {
					log.Infow("TaskClientUpload.adderPorep() no sectors found to process")
					return false, nil
				}

				log.Infow("TaskClientUpload.adderPorep() creating task", "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)

				// Create task
				_, err = tx.Exec(`
					UPDATE sectors_sdr_pipeline
					SET task_id_porep = $1
					WHERE sp_id = $2 AND sector_number = $3
				`, taskID, sectors[0].SpID, sectors[0].SectorNumber)
				if err != nil {
					log.Errorw("TaskClientUpload.adderPorep() failed to update sector", "error", err, "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)
					return false, xerrors.Errorf("failed to update sector: %w", err)
				}

				if sectors[0].TaskIDPorep != nil {
					log.Infow("TaskClientUpload.adderPorep() deleting old task", "oldTaskID", *sectors[0].TaskIDPorep, "newTaskID", taskID)
					_, err := tx.Exec(`DELETE FROM harmony_task WHERE id = $1`, *sectors[0].TaskIDPorep)
					if err != nil {
						log.Errorw("TaskClientUpload.adderPorep() failed to delete old task", "error", err, "oldTaskID", *sectors[0].TaskIDPorep)
						return false, xerrors.Errorf("deleting old task: %w", err)
					}
				}

				// Insert proofshare_client_requests
				// NOTE: the on-conflict spec allows the task to be retried in the sector-pipeline, essentially making the proofshare task/pipeline idempotent
				_, err = tx.Exec(`
					INSERT INTO proofshare_client_requests (sp_id, sector_num, request_type, task_id_upload, request_partition_cost, created_at)
					VALUES ($1, $2, 'porep', $3, 10, current_timestamp)
					ON CONFLICT (sp_id, sector_num, request_type) DO UPDATE SET task_id_upload = $3, done = false
				`, sectors[0].SpID, sectors[0].SectorNumber, taskID)
				if err != nil {
					log.Errorw("TaskClientUpload.adderPorep() failed to insert proofshare_client_requests", "error", err, "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)
					return false, xerrors.Errorf("failed to insert proofshare_client_requests: %w", err)
				}

				more = true
				return true, nil
			})

			if more {
				more = false
				log.Infow("TaskClientUpload.adderPorep() more sectors to process, continuing")
				goto again
			}
		}
	}()
}

func getSectorInfoPoRep(ctx context.Context, db *harmonydb.DB, spID int64, sectorNumber int64) (*SectorInfo, error) {
	var info SectorInfo
	err := db.QueryRow(ctx, `
		SELECT sp_id, sector_number, reg_seal_proof, ticket_epoch, ticket_value, seed_epoch, tree_r_cid, tree_d_cid
		FROM sectors_sdr_pipeline
		WHERE sp_id = $1 AND sector_number = $2
	`, spID, sectorNumber).Scan(
		&info.SpID, &info.SectorNumber, &info.RegSealProof,
		&info.TicketEpoch, &info.TicketValue, &info.SeedEpoch,
		&info.SealedCID, &info.UnsealedCID,
	)
	if err != nil {
		return nil, xerrors.Errorf("failed to get sector info: %w", err)
	}

	// Parse CIDs
	var err1, err2 error
	info.Sealed, err1 = cid.Parse(info.SealedCID)
	info.Unsealed, err2 = cid.Parse(info.UnsealedCID)
	if err1 != nil {
		return nil, xerrors.Errorf("failed to parse sealed cid: %w", err1)
	}
	if err2 != nil {
		return nil, xerrors.Errorf("failed to parse unsealed cid: %w", err2)
	}

	log.Infow("sector info", "spID", info.SpID, "sectorNumber", info.SectorNumber)
	return &info, nil
}

func getRandomnessPoRep(ctx context.Context, api ClientServiceAPI, sectorInfo *SectorInfo) ([]byte, error) {
	// Get chain head
	ts, err := api.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain head: %w", err)
	}

	// Create miner address
	maddr, err := address.NewIDAddress(uint64(sectorInfo.SpID))
	if err != nil {
		return nil, xerrors.Errorf("failed to create miner address: %w", err)
	}

	// Get randomness
	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal miner address: %w", err)
	}

	rand, err := api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_InteractiveSealChallengeSeed, abi.ChainEpoch(sectorInfo.SeedEpoch), buf.Bytes(), ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get randomness for computing seal proof: %w", err)
	}

	log.Infow("got randomness", "spID", sectorInfo.SpID, "sectorNumber", sectorInfo.SectorNumber)
	return rand, nil
}

func (t *TaskClientUpload) getProofDataPoRep(ctx context.Context, spID int64, sectorNumber int64) ([]byte, abi.SectorID, error) {
	sectorInfo, err := getSectorInfoPoRep(ctx, t.db, spID, sectorNumber)
	if err != nil {
		return nil, abi.SectorID{}, err
	}

	randomness, err := getRandomnessPoRep(ctx, t.api, sectorInfo)
	if err != nil {
		return nil, abi.SectorID{}, err
	}

	spt := abi.RegisteredSealProof(sectorInfo.RegSealProof)
	p, err := t.storage.GeneratePoRepVanillaProof(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorInfo.SpID),
			Number: abi.SectorNumber(sectorInfo.SectorNumber),
		},
		ProofType: spt,
	}, sectorInfo.Sealed, sectorInfo.Unsealed, sectorInfo.TicketValue, abi.InteractiveSealRandomness(randomness))
	if err != nil {
		return nil, abi.SectorID{}, xerrors.Errorf("failed to generate porep vanilla proof: %w", err)
	}

	var proofDec proof.Commit1OutRaw
	// json unmarshal proof
	if err := json.Unmarshal(p, &proofDec); err != nil {
		return nil, abi.SectorID{}, xerrors.Errorf("failed to unmarshal proof: %w", err)
	}

	// Create ProofData
	proofData := common.ProofData{
		SectorID: &abi.SectorID{
			Miner:  abi.ActorID(sectorInfo.SpID),
			Number: abi.SectorNumber(sectorInfo.SectorNumber),
		},
		PoRep: &proofDec,
	}

	// Serialize the ProofData
	proofDataBytes, err := json.Marshal(proofData)
	if err != nil {
		return nil, abi.SectorID{}, xerrors.Errorf("failed to marshal proof data: %w", err)
	}

	return proofDataBytes, sectorInfo.SectorID(), nil
}
