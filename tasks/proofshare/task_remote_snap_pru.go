package proofshare

import (
	"context"
	"encoding/json"
	"time"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/lib/storiface"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

// TaskRemoteSnapPRU handles requesting Snap PRU proofs from remote providers
type TaskRemoteSnapPRU struct {
	db      *harmonydb.DB
	api     ClientServiceAPI
	storage *paths.Remote
	router  *common.Service
}

// NewTaskRemoteSnapPRU creates a new TaskRemoteSnapPRU
func NewTaskRemoteSnapPRU(db *harmonydb.DB, api api.FullNode, storage *paths.Remote) *TaskRemoteSnapPRU {
	return &TaskRemoteSnapPRU{
		db:      db,
		api:     api,
		storage: storage,
		router:  common.NewServiceCustomSend(api, nil),
	}
}
/* 
CREATE TABLE sectors_snap_pipeline (
    sp_id BIGINT NOT NULL,
    sector_number BIGINT NOT NULL,

    start_time TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    upgrade_proof INT NOT NULL,

    -- preload
    -- todo sector preload logic
    data_assigned BOOLEAN NOT NULL DEFAULT TRUE,

    -- encode
    update_unsealed_cid TEXT,
    update_sealed_cid TEXT,

    task_id_encode BIGINT,
    after_encode BOOLEAN NOT NULL DEFAULT FALSE,

    -- prove
    proof BYTEA,

    task_id_prove BIGINT,
    after_prove BOOLEAN NOT NULL DEFAULT FALSE,

    -- update_ready_at TIMESTAMP, // Added in 20241210-sdr-batching

    -- submit
    prove_msg_cid TEXT,

    task_id_submit BIGINT,
    after_submit BOOLEAN NOT NULL DEFAULT FALSE,

    after_prove_msg_success BOOLEAN NOT NULL DEFAULT FALSE,
    prove_msg_tsk BYTEA,

    -- move storage
    task_id_move_storage BIGINT,
    after_move_storage BOOLEAN NOT NULL DEFAULT FALSE,

    -- fail
    -- added in 20240809-snap-failures.sql
    -- Failure handling
    -- failed bool not null default false,
    -- failed_at timestamp with timezone,
    -- failed_reason varchar(20) not null default '',
    -- failed_reason_msg text not null default '',

    -- added in 20240826-sector-partition.sql
    -- when not null delays scheduling of the submit task
    -- submit_after TIMESTAMP WITH TIME ZONE,

    FOREIGN KEY (sp_id, sector_number) REFERENCES sectors_meta (sp_id, sector_num),
    PRIMARY KEY (sp_id, sector_number)
);

 */
func (t *TaskRemoteSnapPRU) Adder(add harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {

		for range ticker.C {
			var more bool
			log.Infow("TaskRemoteSnapPRU.Adder() ticker fired, looking for sectors to process")

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
					log.Errorw("TaskRemoteSnapPRU.Adder() failed to query client settings", "error", err)
					return false, xerrors.Errorf("failed to query client settings: %w", err)
				}

				if len(enabledFor) == 0 {
					log.Infow("TaskRemoteSnapPRU.Adder() no enabled client settings found for SnapPRU")
					return false, nil
				}

				// get the minimum pending seconds
				minPendingSeconds := enabledFor[0].MinimumPendingSeconds
				log.Infow("TaskRemoteSnapPRU.Adder() found enabled client settings", "count", len(enabledFor), "minPendingSeconds", minPendingSeconds)

				// claim [sectors] pipeline entries
				var sectors []struct {
					SpID         int64  `db:"sp_id"`
					SectorNumber int64  `db:"sector_number"`
					TaskIDProve  *int64 `db:"task_id_prove"`
				}

				cutoffTime := time.Now().Add(-time.Duration(minPendingSeconds) * time.Second)
				log.Infow("TaskRemoteSnapPRU.Adder() querying for sectors with cutoff time", "cutoffTime", cutoffTime)

				err = tx.Select(&sectors, `SELECT sp_id, sector_number, task_id_prove FROM sectors_snap_pipeline
												LEFT JOIN harmony_task ht on sectors_snap_pipeline.task_id_prove = ht.id
												WHERE after_prove = FALSE AND task_id_prove IS NOT NULL AND ht.owner_id IS NULL AND ht.name = 'UpdateProve' AND ht.posted_time < $1 LIMIT 1`, cutoffTime)
				if err != nil {
					log.Errorw("TaskRemoteSnapPRU.Adder() failed to query sectors", "error", err)
					return false, xerrors.Errorf("getting tasks: %w", err)
				}

				if len(sectors) == 0 {
					log.Infow("TaskRemoteSnapPRU.Adder() no sectors found to process")
					return false, nil
				}

				log.Infow("TaskRemoteSnapPRU.Adder() creating task", "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)

				// Create task
				_, err = tx.Exec(`
					UPDATE sectors_snap_pipeline
					SET task_id_prove = $1
					WHERE sp_id = $2 AND sector_number = $3
				`, taskID, sectors[0].SpID, sectors[0].SectorNumber)
				if err != nil {
					log.Errorw("TaskRemoteSnapPRU.Adder() failed to update sector", "error", err, "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)
					return false, xerrors.Errorf("failed to update sector: %w", err)
				}

				if sectors[0].TaskIDProve != nil {
					log.Infow("TaskRemoteSnapPRU.Adder() deleting old task", "oldTaskID", *sectors[0].TaskIDProve, "newTaskID", taskID)
					_, err := tx.Exec(`DELETE FROM harmony_task WHERE id = $1`, *sectors[0].TaskIDProve)
					if err != nil {
						log.Errorw("TaskRemoteSnapPRU.Adder() failed to delete old task", "error", err, "oldTaskID", *sectors[0].TaskIDProve)
						return false, xerrors.Errorf("deleting old task: %w", err)
					}
				}

				more = true
				return true, nil
			})

			if more {
				more = false
				log.Infow("TaskRemoteSnapPRU.Adder() more sectors to process, continuing")
				goto again
			}
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface
func (t *TaskRemoteSnapPRU) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

// Do implements harmonytask.TaskInterface
func (t *TaskRemoteSnapPRU) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Get sector info
	sectorInfo, err := t.getSectorInfo(ctx, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to get sector info: %w", err)
	}

	for {
		if !stillOwned() {
			return false, xerrors.Errorf("task no longer owned")
		}

		log.Infow("PSR SNAP PRU CYCLE BEGIN VVVVVVVVVVVVVVVVVVVVVVVVVVV", "taskID", taskID)

		// Get the current state of the client request
		const partitionCost = 16
		clientRequest, err := getClientRequest(ctx, t.db, taskID, sectorInfo.SectorID(), partitionCost)
		if err != nil {
			return false, err
		}

		// If the request is already done, update the sector and return
		if clientRequest.Done && clientRequest.ResponseData != nil {
			log.Infow("finalizing sector proof", "taskID", taskID, "sectorID", sectorInfo.SectorNumber, "spID", sectorInfo.SpID)
			return t.finalizeSector(ctx, sectorInfo, clientRequest.ResponseData)
		}

		// Process the request based on its current state
		var stateChanged bool
		var inState string

		if clientRequest.RequestCID == nil || !clientRequest.RequestUploaded {
			// Step 1: Upload proof data
			inState = "uploading proof data"
			stateChanged, err = t.uploadProofData(ctx, taskID, sectorInfo, clientRequest)
		} else if clientRequest.PaymentWallet == nil || clientRequest.PaymentNonce == nil {
			// Step 2: Create payment
			inState = "creating payment"

			stateChanged, err = createPayment(ctx, t.api, t.db, t.router, taskID, sectorInfo.SectorID(), clientRequest.RequestPartitionCost)
		} else if !clientRequest.RequestSent {
			// Step 3: Send request
			inState = "sending request"
			stateChanged, err = sendRequest(ctx, t.api, t.db, taskID, clientRequest)
			if err != nil {
				// request failed, see if we can unlock the payment (if not consumed in the market)
				rerr := undoPayment(ctx, t.db, taskID, clientRequest)
				if rerr != nil {
					log.Errorw("failed to undo payment", "error", rerr, "taskID", taskID, "sectorID", sectorInfo.SectorNumber, "spID", sectorInfo.SpID)
					err = multierror.Append(err, rerr)
				}
			}
		} else {
			// Step 4: Poll for proof
			inState = "polling for proof"
			stateChanged, err = pollForProof(ctx, t.db, taskID, clientRequest, sectorInfo.SectorID())
		}

		log.Infow("PSR SNAP PRU CYCLE END ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^", "taskID", taskID)

		if err != nil {
			return false, err
		}

		// If the state didn't change, wait before trying again
		if !stateChanged {
			select {
			case <-time.After(10 * time.Second):
				// Continue polling
			case <-ctx.Done():
				return false, ctx.Err()
			}
		} else {
			log.Infow("state changed", "state", inState, "taskID", taskID, "sectorID", sectorInfo.SectorNumber, "spID", sectorInfo.SpID)
		}
	}
}

type UpdateSectorInfo struct {
	SpID         int64  `db:"sp_id"`
	SectorNumber int64  `db:"sector_number"`
	UpdateProofType abi.RegisteredUpdateProof `db:"upgrade_proof"`
	RegSealProof abi.RegisteredSealProof `db:"reg_seal_proof"`

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
// getSectorInfo retrieves the sector information from the database
func (t *TaskRemoteSnapPRU) getSectorInfo(ctx context.Context, taskID harmonytask.TaskID) (*UpdateSectorInfo, error) {
	var info UpdateSectorInfo
	var cidOldRs, cidNewRs, cidNewDs string

	err := t.db.QueryRow(ctx, `
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

// uploadProofData generates and uploads the proof data
func (t *TaskRemoteSnapPRU) uploadProofData(ctx context.Context, taskID harmonytask.TaskID, sectorInfo *UpdateSectorInfo, clientRequest *ClientRequest) (bool, error) {
	log.Infow("uploadProofData start", "taskID", taskID, "spID", sectorInfo.SpID, "sectorNumber", sectorInfo.SectorNumber)

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
		return false, xerrors.Errorf("failed to generate snap vanilla proof: %w", err)
	}

	var vproofs [][]byte
	if err := json.Unmarshal(p, &vproofs); err != nil {
		return false, xerrors.Errorf("failed to unmarshal proof: %w", err)
	}

	// Create ProofData
	proofData := common.ProofData{
		SectorID: &abi.SectorID{
			Miner:  abi.ActorID(sectorInfo.SpID),
			Number: abi.SectorNumber(sectorInfo.SectorNumber),
		},
		Snap: &proof.Snap{
			ProofType: sectorInfo.UpdateProofType,
			OldR: sectorInfo.OldR,
			NewR: sectorInfo.NewR,
			NewD: sectorInfo.NewD,
			Proofs: vproofs,
		},
	}

	// Serialize the ProofData
	proofDataBytes, err := json.Marshal(proofData)
	if err != nil {
		return false, xerrors.Errorf("failed to marshal proof data: %w", err)
	}

	// Upload the ProofData
	proofDataCid, err := proofsvc.UploadProofData(ctx, proofDataBytes)
	if err != nil {
		return false, xerrors.Errorf("failed to upload proof data: %w", err)
	}

	// Update the client request with the ProofData CID
	_, err = t.db.Exec(ctx, `
		UPDATE proofshare_client_requests
		SET request_cid = $2, request_uploaded = TRUE
		WHERE task_id = $1
	`, taskID, proofDataCid.String())
	if err != nil {
		return false, xerrors.Errorf("failed to update client request with proof data CID: %w", err)
	}

	log.Infow("uploadProofData complete", "taskID", taskID, "cid", proofDataCid.String())
	return true, nil
}

// finalizeSector updates the sector with the proof and marks the task as done
func (t *TaskRemoteSnapPRU) finalizeSector(ctx context.Context, sectorInfo *UpdateSectorInfo, proofData []byte) (bool, error) {
	// Update sector with proof
	_, err := t.db.Exec(ctx, `
		UPDATE sectors_snap_pipeline
		SET after_prove = TRUE, 
			proof = $3, 
			task_id_prove = NULL
		WHERE sp_id = $1 AND sector_number = $2
	`, sectorInfo.SpID, sectorInfo.SectorNumber, proofData)
	if err != nil {
		return false, xerrors.Errorf("failed to update sector: %w", err)
	}

	log.Infow("remote snap prove completed successfully",
		"spID", sectorInfo.SpID,
		"sectorNumber", sectorInfo.SectorNumber,
		"proofSize", len(proofData))
	return true, nil
}


// TypeDetails implements harmonytask.TaskInterface
func (t *TaskRemoteSnapPRU) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "RemoteUpdateProv",
		Cost: resources.Resources{
			Cpu: 0,
			Gpu: 0,
			Ram: 32 << 20, // 32MB - minimal resources since computation is remote
		},
		MaxFailures: 0,
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10 * time.Duration(retries/5)
		},
	}
}

// Register with the harmonytask engine
var _ = harmonytask.Reg(&TaskRemoteSnapPRU{})
