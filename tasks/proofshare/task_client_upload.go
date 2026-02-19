package proofshare

import (
	"context"
	"errors"

	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
)

type TaskClientUpload struct {
	db      *harmonydb.DB
	api     ClientServiceAPI
	storage *paths.Remote
	router  *common.Service
	max     int
}

func NewTaskClientUpload(db *harmonydb.DB, api ClientServiceAPI, storage *paths.Remote, router *common.Service, max int) *TaskClientUpload {
	return &TaskClientUpload{db: db, api: api, storage: storage, router: router, max: max}
}

// Adder implements harmonytask.TaskInterface.
func (t *TaskClientUpload) Adder(atf harmonytask.AddTaskFunc) {
	t.adderPorep(atf)
	t.adderSnap(atf)
}

// CanAccept implements harmonytask.TaskInterface.
func (t *TaskClientUpload) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) ([]harmonytask.TaskID, error) {
	return ids, nil
}

// Do implements harmonytask.TaskInterface.
func (t *TaskClientUpload) Do(ctx context.Context, taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {

	var clientRequest ClientRequest
	err = t.db.QueryRow(ctx, `
		SELECT sp_id, sector_num, request_cid, request_uploaded, payment_wallet, payment_nonce, request_sent, response_data, done, request_partition_cost, request_type
		FROM proofshare_client_requests
		WHERE task_id_upload = $1
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
		log.Infow("client upload not found", "taskID", taskID)
		return true, nil
	}

	if clientRequest.RequestSent && len(clientRequest.ResponseData) > 0 {
		// special case: pipeline was done but re-entered due to the retry button on the PrRep page being clicked
		// We just mark upload as done, and the task should proceeed to Done-poll
		_, err = t.db.Exec(ctx, `
			UPDATE proofshare_client_requests
			SET request_uploaded = TRUE, task_id_upload = NULL
			WHERE task_id_upload = $1
		`, taskID)
		if err != nil {
			return false, xerrors.Errorf("failed to mark request as uploaded: %w", err)
		}

		// we're done
		return true, nil
	}

	var proofData []byte
	var sectorID abi.SectorID
	switch clientRequest.RequestType {
	case "porep":
		proofData, sectorID, err = t.getProofDataPoRep(ctx, clientRequest.SpID, clientRequest.SectorNumber)
	case "snap":
		proofData, sectorID, err = t.getProofDataSnap(ctx, clientRequest.SpID, clientRequest.SectorNumber)
	default:
		return false, xerrors.Errorf("unknown request type: %s", clientRequest.RequestType)
	}
	if err != nil {
		return false, err
	}

	walletInfo, err := getWalletInfo(ctx, t.db, t.api, sectorID, clientRequest.RequestType)
	if err != nil {
		return false, err
	}

	proofDataCid, err := proofsvc.UploadProofData(ctx, proofData)
	if err != nil {
		return false, xerrors.Errorf("failed to upload proof data: %w", err)
	}

	_, err = t.db.Exec(ctx, `
		UPDATE proofshare_client_requests
		SET request_cid = $2, request_uploaded = TRUE
		WHERE task_id_upload = $1
	`, taskID, proofDataCid.String())
	if err != nil {
		return false, xerrors.Errorf("failed to update client request with proof data CID: %w", err)
	}

	// ensure wallet is in proofshare_client_sender
	_, err = t.db.Exec(ctx, `
		INSERT INTO proofshare_client_sender (wallet_id)
		VALUES ($1) ON CONFLICT (wallet_id) DO NOTHING
	`, walletInfo.WalletID)
	if err != nil {
		return false, err
	}

	return true, nil
}

// TypeDetails implements harmonytask.TaskInterface.
func (t *TaskClientUpload) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(t.max),
		Name: "PSPutVanilla",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 128 << 20,
			Gpu: 0,
		},
	}
}

type walletInfo struct {
	Wallet           address.Address
	WalletID         uint64
	MaxPerProofPrice big.Int
}

func getWalletInfo(ctx context.Context, db *harmonydb.DB, api ClientServiceAPI, sectorInfo abi.SectorID, requestType string) (*walletInfo, error) {
	// Get the wallet address from client settings for this SP ID
	var walletStr string
	var pprice string

	var needPorep = requestType == "porep"
	var needSnap = requestType == "snap"

	err := db.QueryRow(ctx, `
			SELECT wallet, pprice FROM proofshare_client_settings 
			WHERE sp_id = $1 AND enabled = TRUE AND (do_porep >= $2 OR do_snap >= $3)
		`, sectorInfo.Miner, needPorep, needSnap).Scan(&walletStr, &pprice)

	// If no specific settings for this SP ID, try the default (sp_id = 0)
	if errors.Is(err, pgx.ErrNoRows) {
		err = db.QueryRow(ctx, `
				SELECT wallet, pprice FROM proofshare_client_settings 
				WHERE sp_id = 0 AND enabled = TRUE AND (do_porep >= $1 OR do_snap >= $2)
			`, needPorep, needSnap).Scan(&walletStr, &pprice)
	}

	if err != nil {
		return nil, xerrors.Errorf("failed to get wallet from client settings: %w", err)
	}

	if walletStr == "" {
		return nil, xerrors.Errorf("no wallet configured for SP ID %d", sectorInfo.Miner)
	}

	// Parse the wallet address
	wallet, err := address.NewFromString(walletStr)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse wallet address: %w", err)
	}

	maxPerProofPrice, err := types.BigFromString(pprice)
	if err != nil {
		return nil, xerrors.Errorf("failed to parse max per proof price: %w", err)
	}

	// Get client ID from wallet address
	clientIDAddr, err := api.StateLookupID(ctx, wallet, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to lookup client ID: %w", err)
	}

	clientID, err := address.IDFromAddress(clientIDAddr)
	if err != nil {
		return nil, xerrors.Errorf("failed to get client ID from address: %w", err)
	}

	return &walletInfo{
		Wallet:           wallet,
		WalletID:         clientID,
		MaxPerProofPrice: maxPerProofPrice,
	}, nil
}

var _ = harmonytask.Reg(&TaskClientUpload{})
