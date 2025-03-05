package proofshare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/curio/lib/storiface"
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
	wallet  address.Address
	storage *paths.Remote
	router  *common.Service
}

// NewTaskRemotePoRep creates a new TaskRemotePoRep
func NewTaskRemotePoRep(db *harmonydb.DB, api api.FullNode, wallet address.Address, storage *paths.Remote) *TaskRemotePoRep {
	return &TaskRemotePoRep{
		db:      db,
		api:     api,
		wallet:  wallet,
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

		again:
			add(func(taskID harmonytask.TaskID, tx *harmonydb.Tx) (shouldCommit bool, seriousError error) {
				// Check if client settings are enabled for PoRep
				var enabledFor []struct {
					SpID                  int64 `db:"sp_id"`
					MinimumPendingSeconds int64 `db:"minimum_pending_seconds"`
				}
				err := tx.Select(&enabledFor, `
					SELECT sp_id, minimum_pending_seconds
					FROM proofshare_client_settings
					WHERE enabled = TRUE AND do_porep = TRUE
				`)
				if err != nil {
					return false, xerrors.Errorf("failed to query client settings: %w", err)
				}

				if len(enabledFor) == 0 {
					return false, nil
				}

				// get the minimum pending seconds
				minPendingSeconds := enabledFor[0].MinimumPendingSeconds

				// claim [sectors] pipeline entries
				var sectors []struct {
					SpID         int64  `db:"sp_id"`
					SectorNumber int64  `db:"sector_number"`
					TaskIDPorep  *int64 `db:"task_id_porep"`
				}

				err = tx.Select(&sectors, `SELECT sp_id, sector_number, task_id_porep FROM sectors_sdr_pipeline
												LEFT JOIN harmony_task ht on sectors_sdr_pipeline.task_id_porep = ht.id
												WHERE after_sdr = FALSE AND (task_id_porep IS NULL OR (ht.owner_id IS NULL AND ht.name = 'PoRep')) AND ht.posted_time < $1 LIMIT 1`, time.Now().Add(-time.Duration(minPendingSeconds)*time.Second))
				if err != nil {
					return false, xerrors.Errorf("getting tasks: %w", err)
				}

				if len(sectors) == 0 {
					return false, nil
				}

				// Create task
				_, err = tx.Exec(`
					UPDATE sectors_sdr_pipeline
					SET task_id_porep = $1
					WHERE sp_id = $2 AND sector_number = $3
				`, taskID, sectors[0].SpID, sectors[0].SectorNumber)
				if err != nil {
					return false, xerrors.Errorf("failed to update sector: %w", err)
				}

				if sectors[0].TaskIDPorep != nil {
					_, err := tx.Exec(`DELETE FROM harmony_task WHERE id = $1`, *sectors[0].TaskIDPorep)
					if err != nil {
						return false, xerrors.Errorf("deleting old task: %w", err)
					}
				}

				more = true
				return true, nil
			})

			if more {
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
	var request struct {
		SpID         int64  `db:"sp_id"`
		SectorNumber int64  `db:"sector_number"`
		RegSealProof int    `db:"reg_seal_proof"`
		TicketEpoch  int64  `db:"ticket_epoch"`
		TicketValue  []byte `db:"ticket_value"`
		SeedEpoch    int64  `db:"seed_epoch"`
		SealedCID    string `db:"tree_r_cid"`
		UnsealedCID  string `db:"tree_d_cid"`
	}

	err = t.db.QueryRow(ctx, `
		SELECT sp_id, sector_number, reg_seal_proof, ticket_epoch, ticket_value, seed_epoch, tree_r_cid, tree_d_cid
		FROM sectors_sdr_pipeline
		WHERE task_id_porep = $1
	`, taskID).Scan(
		&request.SpID, &request.SectorNumber, &request.RegSealProof,
		&request.TicketEpoch, &request.TicketValue, &request.SeedEpoch,
		&request.SealedCID, &request.UnsealedCID,
	)
	if err != nil {
		return false, xerrors.Errorf("failed to get sector info: %w", err)
	}

	// Check if we already have a client request for this task
	var clientRequest struct {
		RequestCID      *string `db:"request_cid"`
		RequestUploaded bool    `db:"request_uploaded"`
		PaymentWallet   *int64  `db:"payment_wallet"`
		PaymentNonce    *int64  `db:"payment_nonce"`
		RequestSent     *bool   `db:"request_sent"`
		ResponseData    []byte  `db:"response_data"`
		Done            bool    `db:"done"`
	}

	err = t.db.QueryRow(ctx, `
		SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done
		FROM proofshare_client_requests
		WHERE task_id = $1
	`, taskID).Scan(
		&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
		&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, xerrors.Errorf("failed to get client request: %w", err)
	}

	// If we don't have a client request yet, create one
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = t.db.Exec(ctx, `
			INSERT INTO proofshare_client_requests (task_id, sp_id, sector_num, created_at)
			VALUES ($1, $2, $3, NOW())
		`, taskID, request.SpID, request.SectorNumber)
		if err != nil {
			return false, xerrors.Errorf("failed to create client request: %w", err)
		}

		// Reload the client request
		err = t.db.QueryRow(ctx, `
			SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done
			FROM proofshare_client_requests
			WHERE task_id = $1
		`, taskID).Scan(
			&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
			&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to reload client request: %w", err)
		}
	}

	// If the request is already done, update the sector and return
	if clientRequest.Done && clientRequest.ResponseData != nil {
		// Get chain head for randomness
		ts, err := t.api.ChainHead(ctx)
		if err != nil {
			return false, xerrors.Errorf("failed to get chain head: %w", err)
		}

		// Create miner address
		maddr, err := address.NewIDAddress(uint64(request.SpID))
		if err != nil {
			return false, xerrors.Errorf("failed to create miner address: %w", err)
		}

		// Get randomness
		buf := new(bytes.Buffer)
		if err := maddr.MarshalCBOR(buf); err != nil {
			return false, xerrors.Errorf("failed to marshal miner address: %w", err)
		}

		rand, err := t.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_InteractiveSealChallengeSeed, abi.ChainEpoch(request.SeedEpoch), buf.Bytes(), ts.Key())
		if err != nil {
			return false, xerrors.Errorf("failed to get randomness for computing seal proof: %w", err)
		}

		// Update sector with proof
		_, err = t.db.Exec(ctx, `
			UPDATE sectors_sdr_pipeline
			SET after_porep = TRUE, 
				seed_value = $3, 
				porep_proof = $4, 
				task_id_porep = NULL
			WHERE sp_id = $1 AND sector_number = $2
		`, request.SpID, request.SectorNumber, rand, clientRequest.ResponseData)
		if err != nil {
			return false, xerrors.Errorf("failed to update sector: %w", err)
		}

		log.Infow("remote porep completed successfully",
			"spID", request.SpID,
			"sectorNumber", request.SectorNumber,
			"proofSize", len(clientRequest.ResponseData))
		return true, nil
	}

	// Parse CIDs
	sealed, err := cid.Parse(request.SealedCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse sealed cid: %w", err)
	}

	unsealed, err := cid.Parse(request.UnsealedCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse unsealed cid: %w", err)
	}

	// Get chain head
	ts, err := t.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}

	// Create miner address
	maddr, err := address.NewIDAddress(uint64(request.SpID))
	if err != nil {
		return false, xerrors.Errorf("failed to create miner address: %w", err)
	}

	// Get randomness
	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		return false, xerrors.Errorf("failed to marshal miner address: %w", err)
	}

	rand, err := t.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_InteractiveSealChallengeSeed, abi.ChainEpoch(request.SeedEpoch), buf.Bytes(), ts.Key())
	if err != nil {
		return false, xerrors.Errorf("failed to get randomness for computing seal proof: %w", err)
	}

	// Get client ID from wallet address
	clientIDAddr, err := t.api.StateLookupID(ctx, t.wallet, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to lookup client ID: %w", err)
	}

	clientID, err := address.IDFromAddress(clientIDAddr)
	if err != nil {
		return false, xerrors.Errorf("failed to get client ID from address: %w", err)
	}

	ticket := request.TicketValue
	seed := rand

	// Step 1: Upload ProofData if not already uploaded
	if clientRequest.RequestCID == nil || !clientRequest.RequestUploaded {
		// Create PoRep request
		spt := abi.RegisteredSealProof(request.RegSealProof)

		p, err := t.storage.GeneratePoRepVanillaProof(ctx, storiface.SectorRef{
			ID: abi.SectorID{
				Miner:  abi.ActorID(request.SpID),
				Number: abi.SectorNumber(request.SectorNumber),
			},
			ProofType: spt,
		}, unsealed, sealed, ticket, abi.InteractiveSealRandomness(seed))
		if err != nil {
			return false, xerrors.Errorf("failed to generate porep vanilla proof: %w", err)
		}

		proofDec, err := proof.DecodeCommit1OutRaw(bytes.NewReader(p))
		if err != nil {
			return false, xerrors.Errorf("failed to decode proof: %w", err)
		}

		// Create ProofData
		proofData := common.ProofData{
			SectorID: &abi.SectorID{
				Miner:  abi.ActorID(request.SpID),
				Number: abi.SectorNumber(request.SectorNumber),
			},
			PoRep: &proofDec,
		}

		// Validate the ProofData
		if err := proofData.Validate(); err != nil {
			return false, xerrors.Errorf("invalid proof data: %w", err)
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

		// Reload the client request
		err = t.db.QueryRow(ctx, `
			SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done
			FROM proofshare_client_requests
			WHERE task_id = $1
		`, taskID).Scan(
			&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
			&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to reload client request: %w", err)
		}
	}

	// Step 2: Create payment if not already created
	if clientRequest.PaymentWallet == nil || clientRequest.PaymentNonce == nil {
		// Check if there's an unconsumed payment
		var unconsumedPayment struct {
			Wallet int64 `db:"wallet"`
			Nonce  int64 `db:"nonce"`
		}

		err = t.db.QueryRow(ctx, `
			SELECT wallet, nonce
			FROM proofshare_client_payments
			WHERE wallet = $1 AND consumed = FALSE
			LIMIT 1
		`, clientID).Scan(&unconsumedPayment.Wallet, &unconsumedPayment.Nonce)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, xerrors.Errorf("failed to check for unconsumed payments: %w", err)
		}

		// If there's an unconsumed payment, we need to wait for it to be consumed
		if err == nil {
			log.Infow("waiting for previous payment to be consumed",
				"wallet", unconsumedPayment.Wallet,
				"nonce", unconsumedPayment.Nonce)
			return false, nil
		}

		// Get current price for the proof
		price, err := proofsvc.GetCurrentPrice()
		if err != nil {
			return false, xerrors.Errorf("failed to get current price: %w", err)
		}

		// Get the next nonce for this wallet
		var nextNonce int64
		err = t.db.QueryRow(ctx, `
			SELECT COALESCE(MAX(nonce) + 1, 0)
			FROM proofshare_client_payments
			WHERE wallet = $1
		`, clientID).Scan(&nextNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to get next nonce: %w", err)
		}

		// Create voucher
		voucher, err := t.router.CreateClientVoucher(ctx, clientID, cumulativeAmount, clientRequest.PaymentNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to create voucher: %w", err)
		}

		sig, err := t.api.WalletSign(ctx, clientID, voucher)
		if err != nil {
			return false, xerrors.Errorf("failed to sign voucher: %w", err)
		}

		// Insert the payment
		_, err = t.db.Exec(ctx, `
			INSERT INTO proofshare_client_payments (wallet, nonce, cumulative_amount, signature, consumed)
			VALUES ($1, $2, $3, $4, FALSE)
		`, clientID, nextNonce, price.String(), paymentSignature)
		if err != nil {
			return false, xerrors.Errorf("failed to insert payment: %w", err)
		}

		// Update the client request with the payment info
		_, err = t.db.Exec(ctx, `
			UPDATE proofshare_client_requests
			SET payment_wallet = $2, payment_nonce = $3
			WHERE task_id = $1
		`, taskID, clientID, nextNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to update client request with payment info: %w", err)
		}

		// Reload the client request
		err = t.db.QueryRow(ctx, `
			SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done
			FROM proofshare_client_requests
			WHERE task_id = $1
		`, taskID).Scan(
			&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
			&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to reload client request: %w", err)
		}
	}

	// Step 3: Send the request if not already sent
	if clientRequest.RequestSent == nil || !*clientRequest.RequestSent {

		// Get the payment details
		var payment struct {
			CumulativeAmount string `db:"cumulative_amount"`
			Signature        []byte `db:"signature"`
		}

		err = t.db.QueryRow(ctx, `
			SELECT cumulative_amount, signature
			FROM proofshare_client_payments
			WHERE wallet = $1 AND nonce = $2
		`, clientRequest.PaymentWallet, clientRequest.PaymentNonce).Scan(
			&payment.CumulativeAmount, &payment.Signature,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to get payment details: %w", err)
		}


		// Parse the request CID
		requestCid, err := cid.Parse(*clientRequest.RequestCID)
		if err != nil {
			return false, xerrors.Errorf("failed to parse request CID: %w", err)
		}

		// Create the ProofRequest
		proofRequest := common.ProofRequest{
			Data: requestCid,

			PriceEpoch: int64(ts.Height()),

			PaymentClientID:         *clientRequest.PaymentWallet,
			PaymentNonce:            *clientRequest.PaymentNonce,
			PaymentCumulativeAmount: cumulativeAmount,
			PaymentSignature:        payment.Signature,
		}

		// Submit the request
		err = proofsvc.RequestProof(proofRequest)
		if err != nil {
			return false, xerrors.Errorf("failed to submit proof request: %w", err)
		}

		// Mark the request as sent
		requestSent := true
		_, err = t.db.Exec(ctx, `
			UPDATE proofshare_client_requests
			SET request_sent = $2
			WHERE task_id = $1
		`, taskID, requestSent)
		if err != nil {
			return false, xerrors.Errorf("failed to mark request as sent: %w", err)
		}

		// Mark the payment as consumed
		_, err = t.db.Exec(ctx, `
			UPDATE proofshare_client_payments
			SET consumed = TRUE
			WHERE wallet = $1 AND nonce = $2
		`, clientRequest.PaymentWallet, clientRequest.PaymentNonce)
		if err != nil {
			return false, xerrors.Errorf("failed to mark payment as consumed: %w", err)
		}

		// Reload the client request
		err = t.db.QueryRow(ctx, `
			SELECT request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done
			FROM proofshare_client_requests
			WHERE task_id = $1
		`, taskID).Scan(
			&clientRequest.RequestCID, &clientRequest.RequestUploaded, &clientRequest.PaymentWallet,
			&clientRequest.PaymentNonce, &clientRequest.RequestSent, &clientRequest.ResponseData, &clientRequest.Done,
		)
		if err != nil {
			return false, xerrors.Errorf("failed to reload client request: %w", err)
		}
	}

	// Step 4: Poll for the proof
	// Try to get the proof status from the service using the request CID
	requestCid, err := cid.Parse(*clientRequest.RequestCID)
	if err != nil {
		return false, xerrors.Errorf("failed to parse request CID: %w", err)
	}

	// Get proof status by CID
	proofResp, err := proofsvc.GetProofStatus(requestCid)
	if err == nil && proofResp.Proof != nil {
		// We got a valid proof response, update the database
		_, err = t.db.Exec(ctx, `
			UPDATE proofshare_client_requests
			SET done = TRUE, response_data = $2, done_at = NOW()
			WHERE task_id = $1
		`, taskID, proofResp.Proof)
		if err != nil {
			return false, xerrors.Errorf("failed to update client request with proof: %w", err)
		}

		// Update sector with proof
		_, err = t.db.Exec(ctx, `
			UPDATE sectors_sdr_pipeline
			SET after_porep = TRUE, 
				seed_value = $3, 
				porep_proof = $4, 
				task_id_porep = NULL
			WHERE sp_id = $1 AND sector_number = $2
		`, request.SpID, request.SectorNumber, seed, proofResp.Proof)
		if err != nil {
			return false, xerrors.Errorf("failed to update sector: %w", err)
		}

		log.Infow("remote porep completed successfully",
			"spID", request.SpID,
			"sectorNumber", request.SectorNumber,
			"proofSize", len(proofResp.Proof))
		return true, nil
	}

	// Wait before polling again
	select {
	case <-time.After(30 * time.Second):
		// Continue polling
	case <-ctx.Done():
		return false, ctx.Err()
	}

	return false, nil
}

// TypeDetails implements harmonytask.TaskInterface
func (t *TaskRemotePoRep) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Name: "RemotePoRep",
		Cost: resources.Resources{
			Cpu: 1,
			Gpu: 0,
			Ram: 32 << 20, // 32MB - minimal resources since computation is remote
		},
		MaxFailures: 5,
		RetryWait: func(retries int) time.Duration {
			return time.Second * 10 * time.Duration(retries)
		},
	}
}

// Register with the harmonytask engine
var _ = harmonytask.Reg(&TaskRemotePoRep{})
