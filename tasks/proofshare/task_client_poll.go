package proofshare

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/proofsvc"

	"github.com/filecoin-project/lotus/chain/types"
)

var PollInterval = 20 * time.Second

type ClientServiceAPI interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateGetRandomnessFromBeacon(context.Context, crypto.DomainSeparationTag, abi.ChainEpoch, []byte, types.TipSetKey) (abi.Randomness, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)
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
	SpID         int64 `db:"sp_id"`
	SectorNumber int64 `db:"sector_num"`

	RequestCID           *string `db:"request_cid"`
	RequestUploaded      bool    `db:"request_uploaded"`
	RequestPartitionCost int64   `db:"request_partition_cost"`
	RequestType          string  `db:"request_type"`

	PaymentWallet *int64 `db:"payment_wallet"`
	PaymentNonce  *int64 `db:"payment_nonce"`

	RequestSent  bool   `db:"request_sent"`
	ResponseData []byte `db:"response_data"`

	Done bool `db:"done"`
}

type TaskClientPoll struct {
	db  *harmonydb.DB
	api ClientServiceAPI
}

// Adder implements harmonytask.TaskInterface.
func (t *TaskClientPoll) Adder(atf harmonytask.AddTaskFunc) {
	go func() {
		var more = true
		for {
			if !more {
				time.Sleep(PollInterval)
			}
			more = false

			atf(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// where request_sent is true and task_id_poll is null and done is false

				var spID, sectorNum int64
				var requestType string
				err := tx.QueryRow(`
					SELECT sp_id, sector_num, request_type
					FROM proofshare_client_requests
					WHERE request_sent = TRUE AND task_id_poll IS NULL AND done = FALSE
					LIMIT 1
				`).Scan(&spID, &sectorNum, &requestType)
				if err != nil {
					return false, err
				}

				// update proofshare_client_requests
				n, err := tx.Exec(`
					UPDATE proofshare_client_requests
					SET task_id_poll = $4
					WHERE sp_id = $1 AND sector_num = $2 AND request_type = $3 AND request_sent = TRUE AND task_id_poll IS NULL AND done = FALSE
				`, spID, sectorNum, requestType, taskID)
				if err != nil {
					return false, err
				}

				more = n > 0

				return n > 0, nil
			})
		}
	}()
}

// CanAccept implements harmonytask.TaskInterface.
func (t *TaskClientPoll) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	return &ids[0], nil
}

// Do implements harmonytask.TaskInterface.
func (t *TaskClientPoll) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	var clientRequest ClientRequest
	err = t.db.QueryRow(ctx, `
		SELECT sp_id, sector_num, request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done, request_partition_cost, request_type
		FROM proofshare_client_requests
		WHERE task_id_poll = $1
	`, taskID).Scan(
		&clientRequest.SpID, &clientRequest.SectorNumber,
		&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
		&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
		&clientRequest.RequestPartitionCost, &clientRequest.RequestType,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, xerrors.Errorf("failed to get client request: %w", err)
	}
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		log.Infow("client request not found", "taskID", taskID)
		return false, nil
	}

	var proof []byte
	for {
		var stateChanged bool
		stateChanged, proof, err = pollForProof(ctx, t.db, taskID, &clientRequest)
		if err != nil {
			return false, xerrors.Errorf("failed to poll for proof: %w", err)
		}

		if stateChanged {
			break
		}

		// Randomize wait time between 0.5-2 seconds to avoid thundering herd
		waitTime := time.Duration(50+rand.Intn(150)) * time.Second / 100
		time.Sleep(waitTime)

		// check if the task is still owned
		if !stillOwned() {
			return false, nil
		}
	}

	switch clientRequest.RequestType {
	case "porep":
		err = t.finalizeSectorPoRep(ctx, &clientRequest, proof)
	case "snap":
		err = t.finalizeSectorSnap(ctx, &clientRequest, proof)
	default:
		return false, xerrors.Errorf("unknown request type: %s", clientRequest.RequestType)
	}
	if err != nil {
		return false, xerrors.Errorf("failed to finalize sector: %w", err)
	}

	_, err = t.db.Exec(ctx, `
		UPDATE proofshare_client_requests
		SET task_id_poll = NULL
		WHERE task_id_poll = $1
	`, taskID)
	if err != nil {
		return false, xerrors.Errorf("failed to update client request: %w", err)
	}

	return true, nil
}

// TypeDetails implements harmonytask.TaskInterface.
func (t *TaskClientPoll) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "PSClientPoll",
		Cost: resources.Resources{
			Cpu: 0,
			Ram: 4 << 20,
			Gpu: 0,
		},
		RetryWait: func(retries int) time.Duration {
			return 10 * time.Second
		},
	}
}

func NewTaskClientPoll(db *harmonydb.DB, api ClientServiceAPI) *TaskClientPoll {
	return &TaskClientPoll{db: db, api: api}
}

// pollForProof polls for the proof status
func pollForProof(ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, clientRequest *ClientRequest) (bool, []byte, error) {
	log.Infow("pollForProof", "taskID", taskID, "requestCID", clientRequest.RequestCID)
	// Parse the request CID
	requestCid, err := cid.Parse(*clientRequest.RequestCID)
	if err != nil {
		return false, nil, xerrors.Errorf("failed to parse request CID: %w", err)
	}

	// Get proof status by CID
	proofResp, err := proofsvc.GetProofStatus(requestCid)
	if err != nil || proofResp.Proof == nil {
		log.Infow("proof not ready", "taskID", taskID, "spID", clientRequest.SpID, "sectorNumber", clientRequest.SectorNumber)
		// Not ready yet, continue polling
		return false, nil, nil
	}

	// We got a valid proof response, update the database
	_, err = db.Exec(ctx, `
		UPDATE proofshare_client_requests
		SET done = TRUE, response_data = $2, done_at = NOW()
		WHERE task_id_poll = $1
	`, taskID, proofResp.Proof)
	if err != nil {
		return false, nil, xerrors.Errorf("failed to update client request with proof: %w", err)
	}

	log.Infow("proof retrieved", "taskID", taskID, "spID", clientRequest.SpID, "sectorNumber", clientRequest.SectorNumber, "proofSize", len(proofResp.Proof))
	return true, proofResp.Proof, nil
}

var _ = harmonytask.Reg(&TaskClientPoll{})
