package pdp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/yugabyte/pgx/v5"

	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/filecoin-project/curio/pdp/contract/FWSS"
)

type dataSetTerminationStatusResponse struct {
	TerminationTxHash       string `json:"terminationTxHash"`
	TxStatus                string `json:"txStatus"`
	TxSuccess               *bool  `json:"txSuccess"`
	FWSSTerminated          bool   `json:"fwssTerminated"`
	ServiceTerminationEpoch *int64 `json:"serviceTerminationEpoch"`
}

type dataSetTerminationConflictResponse struct {
	Error                   string `json:"error"`
	ServiceTerminationEpoch int64  `json:"serviceTerminationEpoch"`
}

func (p *PDPService) handleTerminateDataSet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	serviceLabel, err := p.AuthService(r)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return
	}

	dataSetIDStr := chi.URLParam(r, "dataSetId")
	if dataSetIDStr == "" {
		http.Error(w, "Missing data set ID in URL", http.StatusBadRequest)
		return
	}

	dataSetID, err := strconv.ParseUint(dataSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}

	var payload struct {
		ExtraData *string `json:"extraData"`
	}
	err = json.NewDecoder(r.Body).Decode(&payload)
	if err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	extraDataBytes, err := decodeExtraData(payload.ExtraData)
	if err != nil {
		http.Error(w, "Invalid extraData format (must be hex encoded)", http.StatusBadRequest)
		return
	}
	if len(extraDataBytes) == 0 {
		http.Error(w, "extraData is required for client-requested termination", http.StatusBadRequest)
		return
	}

	dataSetService, err := p.getDataSetService(ctx, dataSetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Data set not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to retrieve data set", err)
		return
	}
	if dataSetService != serviceLabel {
		http.Error(w, "Data set not found", http.StatusNotFound)
		return
	}

	terminationEpoch, terminated, err := p.getFWSSServiceTerminationEpoch(ctx, dataSetID)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to check FWSS termination state", err)
		return
	}
	if terminated {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(dataSetTerminationConflictResponse{
			Error:                   "data_set_already_terminated",
			ServiceTerminationEpoch: terminationEpoch,
		})
		return
	}

	var terminationQueued bool
	err = p.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pdp_delete_data_set
			WHERE id = $1
		)
	`, dataSetID).Scan(&terminationQueued)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to check termination request state", err)
		return
	}
	if terminationQueued {
		http.Error(w, "Data set termination is already pending or complete", http.StatusConflict)
		return
	}

	fwssAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	supported, err := FWSS.SupportsClientTermination(contract.EthCallOpts(ctx), fwssAddr, p.ethClient)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to check FWSS version", err)
		return
	}
	if !supported {
		http.Error(w, "FWSS contract does not support client-requested termination", http.StatusServiceUnavailable)
		return
	}

	n, err := p.db.Exec(ctx, `
		INSERT INTO pdp_delete_data_set (
			id,
			client_requested_termination,
			termination_requested_at,
			termination_extra_data,
			after_terminate_service
		)
		VALUES ($1, TRUE, NOW(), $2, FALSE)
		ON CONFLICT (id) DO NOTHING
	`, dataSetID, extraDataBytes)
	if err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to queue termination request", err)
		return
	}
	if n != 1 {
		http.Error(w, "Data set termination is already pending or complete", http.StatusConflict)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (p *PDPService) handleGetDataSetTerminationStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	serviceLabel, err := p.AuthService(r)
	if err != nil {
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
		return
	}

	dataSetIDStr := chi.URLParam(r, "dataSetId")
	if dataSetIDStr == "" {
		http.Error(w, "Missing data set ID in URL", http.StatusBadRequest)
		return
	}
	dataSetID, err := strconv.ParseUint(dataSetIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid data set ID format", http.StatusBadRequest)
		return
	}

	var row struct {
		TerminateTxHash         sql.NullString `db:"terminate_tx_hash"`
		ServiceTerminationEpoch sql.NullInt64  `db:"service_termination_epoch"`
		TxStatus                sql.NullString `db:"tx_status"`
		TxSuccess               sql.NullBool   `db:"tx_success"`
	}
	err = p.db.QueryRow(ctx, `
		SELECT pdds.terminate_tx_hash, pdds.service_termination_epoch, mwe.tx_status, mwe.tx_success
		FROM pdp_delete_data_set pdds
		INNER JOIN pdp_data_sets pds ON pds.id = pdds.id
		LEFT JOIN message_waits_eth mwe ON mwe.signed_tx_hash = pdds.terminate_tx_hash
		WHERE pdds.id = $1
		  AND pds.service = $2
		  AND pdds.client_requested_termination = TRUE
	`, dataSetID, serviceLabel).Scan(&row.TerminateTxHash, &row.ServiceTerminationEpoch, &row.TxStatus, &row.TxSuccess)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Termination not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to query termination status", err)
		return
	}

	response := dataSetTerminationStatusResponse{
		FWSSTerminated: row.ServiceTerminationEpoch.Valid,
	}
	if row.TerminateTxHash.Valid {
		response.TerminationTxHash = row.TerminateTxHash.String
	}
	if row.TxStatus.Valid {
		response.TxStatus = row.TxStatus.String
	}
	if row.TxSuccess.Valid {
		success := row.TxSuccess.Bool
		response.TxSuccess = &success
	}
	if row.ServiceTerminationEpoch.Valid {
		epoch := row.ServiceTerminationEpoch.Int64
		response.ServiceTerminationEpoch = &epoch
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		httpServerError(w, http.StatusInternalServerError, "Failed to encode response", err)
		return
	}
}

func (p *PDPService) getDataSetService(ctx context.Context, dataSetID uint64) (string, error) {
	var service string
	err := p.db.QueryRow(ctx, `
		SELECT service
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetID).Scan(&service)
	return service, err
}

func (p *PDPService) getFWSSServiceTerminationEpoch(ctx context.Context, dataSetID uint64) (int64, bool, error) {
	serviceAddr := contract.ContractAddresses().AllowedPublicRecordKeepers.FWSService
	viewAddr, err := contract.ResolveViewAddress(ctx, serviceAddr, p.ethClient)
	if err != nil {
		return 0, false, err
	}
	fwssv, err := FWSS.NewFilecoinWarmStorageServiceStateView(viewAddr, p.ethClient)
	if err != nil {
		return 0, false, err
	}
	ds, err := fwssv.GetDataSet(contract.EthCallOpts(ctx), new(big.Int).SetUint64(dataSetID))
	if err != nil {
		return 0, false, err
	}
	if ds.PdpEndEpoch == nil || ds.PdpEndEpoch.Sign() == 0 {
		return 0, false, nil
	}
	return ds.PdpEndEpoch.Int64(), true, nil
}
