package proofshare

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

// ClientServiceAPI defines the interface for interacting with the client service
type ClientServiceAPI interface {
	// ChainHead returns the current chain head
	ChainHead(context.Context) (*types.TipSet, error)
	// StateGetRandomnessFromBeacon gets randomness from the beacon
	StateGetRandomnessFromBeacon(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
	// StateLookupID looks up the ID address of an address
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	// WalletSign signs a message
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)
}

// TaskRemotePoRep handles requesting PoRep proofs from remote providers
type TaskRemotePoRep struct {
	db      *harmonydb.DB
	api     ClientServiceAPI
	storage *paths.Remote
	router  *common.Service
}

// NewTaskRemotePoRep creates a new TaskRemotePoRep
func NewTaskRemotePoRep(db *harmonydb.DB, api api.FullNode, storage *paths.Remote) *TaskRemotePoRep {
	return &TaskRemotePoRep{
		db:      db,
		api:     api,
		storage: storage,
		router:  common.NewServiceCustomSend(api, nil),
	}
}

// Adder implements harmonytask.TaskInterface
func (t *TaskRemotePoRep) Adder(add harmonytask.AddTaskFunc) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {

		for range ticker.C {
			var more bool
			log.Infow("TaskRemotePoRep.Adder() ticker fired, looking for sectors to process")

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
					log.Errorw("TaskRemotePoRep.Adder() failed to query client settings", "error", err)
					return false, xerrors.Errorf("failed to query client settings: %w", err)
				}

				if len(enabledFor) == 0 {
					log.Infow("TaskRemotePoRep.Adder() no enabled client settings found for PoRep")
					return false, nil
				}

				// get the minimum pending seconds
				minPendingSeconds := enabledFor[0].MinimumPendingSeconds
				log.Infow("TaskRemotePoRep.Adder() found enabled client settings", "count", len(enabledFor), "minPendingSeconds", minPendingSeconds)

				// claim [sectors] pipeline entries
				var sectors []struct {
					SpID         int64  `db:"sp_id"`
					SectorNumber int64  `db:"sector_number"`
					TaskIDPorep  *int64 `db:"task_id_porep"`
				}

				cutoffTime := time.Now().Add(-time.Duration(minPendingSeconds) * time.Second)
				log.Infow("TaskRemotePoRep.Adder() querying for sectors with cutoff time", "cutoffTime", cutoffTime)

				err = tx.Select(&sectors, `SELECT sp_id, sector_number, task_id_porep FROM sectors_sdr_pipeline
												LEFT JOIN harmony_task ht on sectors_sdr_pipeline.task_id_porep = ht.id
												WHERE after_porep = FALSE AND task_id_porep IS NOT NULL AND ht.owner_id IS NULL AND ht.name = 'PoRep' AND ht.posted_time < $1 LIMIT 1`, cutoffTime)
				if err != nil {
					log.Errorw("TaskRemotePoRep.Adder() failed to query sectors", "error", err)
					return false, xerrors.Errorf("getting tasks: %w", err)
				}

				if len(sectors) == 0 {
					log.Infow("TaskRemotePoRep.Adder() no sectors found to process")
					return false, nil
				}

				log.Infow("TaskRemotePoRep.Adder() creating task", "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)

				// Create task
				_, err = tx.Exec(`
					UPDATE sectors_sdr_pipeline
					SET task_id_porep = $1
					WHERE sp_id = $2 AND sector_number = $3
				`, taskID, sectors[0].SpID, sectors[0].SectorNumber)
				if err != nil {
					log.Errorw("TaskRemotePoRep.Adder() failed to update sector", "error", err, "taskID", taskID, "spID", sectors[0].SpID, "sectorNumber", sectors[0].SectorNumber)
					return false, xerrors.Errorf("failed to update sector: %w", err)
				}

				if sectors[0].TaskIDPorep != nil {
					log.Infow("TaskRemotePoRep.Adder() deleting old task", "oldTaskID", *sectors[0].TaskIDPorep, "newTaskID", taskID)
					_, err := tx.Exec(`DELETE FROM harmony_task WHERE id = $1`, *sectors[0].TaskIDPorep)
					if err != nil {
						log.Errorw("TaskRemotePoRep.Adder() failed to delete old task", "error", err, "oldTaskID", *sectors[0].TaskIDPorep)
						return false, xerrors.Errorf("deleting old task: %w", err)
					}
				}

				more = true
				return true, nil
			})

			if more {
				more = false
				log.Infow("TaskRemotePoRep.Adder() more sectors to process, continuing")
				goto again
			}
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface
func (t *TaskRemotePoRep) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

// Do implements harmonytask.TaskInterface
func (t *TaskRemotePoRep) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
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

		// Get the current state of the client request
		const partitionCost = 10 // 126 for ni-porep
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

		if err != nil {
			return false, err
		}

		// If the state didn't change, wait before trying again
		if !stateChanged {
			// Randomize wait time between 0.5-2 seconds to avoid thundering herd
			waitTime := time.Duration(50+rand.Intn(150)) * time.Second / 100
			select {
			case <-time.After(waitTime):
				// Continue polling
			case <-ctx.Done():
				return false, ctx.Err()
			}
		} else {
			log.Infow("state changed", "inState", inState, "taskID", taskID, "sectorID", sectorInfo.SectorNumber, "spID", sectorInfo.SpID)
		}
	}
}

// getSectorInfo retrieves the sector information from the database
func (t *TaskRemotePoRep) getSectorInfo(ctx context.Context, taskID harmonytask.TaskID) (*SectorInfo, error) {
	var info SectorInfo
	err := t.db.QueryRow(ctx, `
		SELECT sp_id, sector_number, reg_seal_proof, ticket_epoch, ticket_value, seed_epoch, tree_r_cid, tree_d_cid
		FROM sectors_sdr_pipeline
		WHERE task_id_porep = $1
	`, taskID).Scan(
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

	log.Infow("sector info", "taskID", taskID, "spID", info.SpID, "sectorNumber", info.SectorNumber)
	return &info, nil
}

// uploadProofData generates and uploads the proof data
func (t *TaskRemotePoRep) uploadProofData(ctx context.Context, taskID harmonytask.TaskID, sectorInfo *SectorInfo, clientRequest *ClientRequest) (bool, error) {
	log.Infow("uploadProofData start", "taskID", taskID, "spID", sectorInfo.SpID, "sectorNumber", sectorInfo.SectorNumber)
	// Get randomness
	randomness, err := t.getRandomness(ctx, sectorInfo)
	if err != nil {
		return false, err
	}

	// Create PoRep request
	spt := abi.RegisteredSealProof(sectorInfo.RegSealProof)
	p, err := t.storage.GeneratePoRepVanillaProof(ctx, storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  abi.ActorID(sectorInfo.SpID),
			Number: abi.SectorNumber(sectorInfo.SectorNumber),
		},
		ProofType: spt,
	}, sectorInfo.Sealed, sectorInfo.Unsealed, sectorInfo.TicketValue, abi.InteractiveSealRandomness(randomness))
	if err != nil {
		return false, xerrors.Errorf("failed to generate porep vanilla proof: %w", err)
	}

	var proofDec proof.Commit1OutRaw
	// json unmarshal proof
	if err := json.Unmarshal(p, &proofDec); err != nil {
		return false, xerrors.Errorf("failed to unmarshal proof: %w", err)
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
func (t *TaskRemotePoRep) finalizeSector(ctx context.Context, sectorInfo *SectorInfo, proofData []byte) (bool, error) {
	// Get randomness
	randomness, err := t.getRandomness(ctx, sectorInfo)
	if err != nil {
		return false, err
	}

	// Update sector with proof
	_, err = t.db.Exec(ctx, `
		UPDATE sectors_sdr_pipeline
		SET after_porep = TRUE, 
			seed_value = $3, 
			porep_proof = $4, 
			task_id_porep = NULL
		WHERE sp_id = $1 AND sector_number = $2
	`, sectorInfo.SpID, sectorInfo.SectorNumber, randomness, proofData)
	if err != nil {
		return false, xerrors.Errorf("failed to update sector: %w", err)
	}

	log.Infow("remote porep completed successfully",
		"spID", sectorInfo.SpID,
		"sectorNumber", sectorInfo.SectorNumber,
		"proofSize", len(proofData))
	return true, nil
}

// getRandomness gets the randomness for the sector
func (t *TaskRemotePoRep) getRandomness(ctx context.Context, sectorInfo *SectorInfo) ([]byte, error) {
	// Get chain head
	ts, err := t.api.ChainHead(ctx)
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

	rand, err := t.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_InteractiveSealChallengeSeed, abi.ChainEpoch(sectorInfo.SeedEpoch), buf.Bytes(), ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get randomness for computing seal proof: %w", err)
	}

	log.Infow("got randomness", "spID", sectorInfo.SpID, "sectorNumber", sectorInfo.SectorNumber)
	return rand, nil
}

// SectorInfo holds the sector information
type SectorInfo struct {
	SpID         int64  `db:"sp_id"`
	SectorNumber int64  `db:"sector_number"`
	RegSealProof int    `db:"reg_seal_proof"`
	TicketEpoch  int64  `db:"ticket_epoch"`
	TicketValue  []byte `db:"ticket_value"`
	SeedEpoch    int64  `db:"seed_epoch"`
	SealedCID    string `db:"tree_r_cid"`
	UnsealedCID  string `db:"tree_d_cid"`
	Sealed       cid.Cid
	Unsealed     cid.Cid
}

func (s *SectorInfo) SectorID() abi.SectorID {
	return abi.SectorID{
		Miner:  abi.ActorID(s.SpID),
		Number: abi.SectorNumber(s.SectorNumber),
	}
}

// ClientRequest holds the client request information
type ClientRequest struct {
	RequestCID           *string `db:"request_cid"`
	RequestUploaded      bool    `db:"request_uploaded"`
	RequestPartitionCost int64   `db:"request_partition_cost"`

	PaymentWallet *int64 `db:"payment_wallet"`
	PaymentNonce  *int64 `db:"payment_nonce"`

	RequestSent  bool   `db:"request_sent"`
	ResponseData []byte `db:"response_data"`

	Done bool `db:"done"`
}

// TypeDetails implements harmonytask.TaskInterface
func (t *TaskRemotePoRep) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "RemotePoRep",
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
var _ = harmonytask.Reg(&TaskRemotePoRep{})
