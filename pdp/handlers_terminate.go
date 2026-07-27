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
	FWSSTerminated          *bool  `json:"fwssTerminated"`
	ServiceTerminationEpoch *int64 `json:"serviceTerminationEpoch"`
}

type dataSetTerminationConflictResponse struct {
	Code                    terminateCode `json:"code"`
	Message                 string        `json:"message"`
	ServiceTerminationEpoch *int64        `json:"serviceTerminationEpoch"`
}

type terminateCode int

const (
	TerminateCode0 terminateCode = iota
	TerminateCode1 terminateCode = iota
)

func (t terminateCode) Message() string {
	switch t {
	case TerminateCode0:
		return "already terminated"
	case TerminateCode1:
		return "termination queued"
	default:
		return "unknown termination code"
	}
}

func (t terminateCode) EncodeResponse(w io.Writer, epoch *int64) {
	_ = json.NewEncoder(w).Encode(dataSetTerminationConflictResponse{
		Code:                    t,
		Message:                 t.Message(),
		ServiceTerminationEpoch: epoch,
	})
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
		TerminateCode0.EncodeResponse(w, &terminationEpoch)
		return
	}

	var terminatedEpoch sql.NullInt64
	err = p.db.QueryRow(ctx, `SELECT service_termination_epoch FROM pdp_delete_data_set WHERE id = $1`, dataSetID).Scan(&terminatedEpoch)
	if err == nil {
		if terminatedEpoch.Valid {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			TerminateCode0.EncodeResponse(w, &terminatedEpoch.Int64)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		TerminateCode1.EncodeResponse(w, nil)
		return
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		httpServerError(w, http.StatusInternalServerError, "Failed to check termination request state", err)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		TerminateCode1.EncodeResponse(w, nil)
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
	}
	err = p.db.QueryRow(ctx, `
		SELECT pdds.terminate_tx_hash, pdds.service_termination_epoch
		FROM pdp_delete_data_set pdds
		INNER JOIN pdp_data_sets pds ON pds.id = pdds.id
		WHERE pdds.id = $1
		  AND pds.service = $2
		  AND pdds.client_requested_termination = TRUE
	`, dataSetID, serviceLabel).Scan(&row.TerminateTxHash, &row.ServiceTerminationEpoch)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "Termination not found", http.StatusNotFound)
			return
		}
		httpServerError(w, http.StatusInternalServerError, "Failed to query termination status", err)
		return
	}

	// If terminated outside - no tx hash, termination epoch value, terminated
	// If no message sent - no tx hash, null termination epoch
	// If message send - tx hash, null termination epoch
	// if message executed success - tx hash, termination epoch value, terminated
	// if message executed failed - 404

	response := dataSetTerminationStatusResponse{}

	if !row.TerminateTxHash.Valid {
		if row.ServiceTerminationEpoch.Valid {
			response.FWSSTerminated = new(true)
			response.ServiceTerminationEpoch = new(row.ServiceTerminationEpoch.Int64)
		}
	} else {
		response.TerminationTxHash = row.TerminateTxHash.String
		if row.ServiceTerminationEpoch.Valid {
			response.FWSSTerminated = new(true)
			response.ServiceTerminationEpoch = new(row.ServiceTerminationEpoch.Int64)
		}
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
