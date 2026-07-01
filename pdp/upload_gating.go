package pdp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func optionalPayerAddress(extraData []byte) *string {
	if len(extraData) == 0 {
		return nil
	}
	payer, err := PayerFromCreateExtraData(extraData)
	if err != nil {
		log.Debugw("could not decode payer from create extraData", "error", err)
		return nil
	}
	hex := strings.ToLower(payer.Hex())
	return &hex
}

func (p *PDPService) validateUploadInit(w http.ResponseWriter, r *http.Request, serviceLabel string, dataSetID *uint64) bool {
	requireAuth := p.uploadRequireAuth
	hasDataSet := dataSetID != nil && *dataSetID > 0

	if requireAuth {
		if serviceLabel == "public" {
			httpServerError(w, http.StatusUnauthorized, "anonymous piece uploads are disabled; provide a registered service JWT", nil)
			return false
		}
		if !hasDataSet {
			httpServerError(w, http.StatusBadRequest, "dataSetId is required", nil)
			return false
		}
	}

	if !hasDataSet {
		return true
	}

	ctx := r.Context()
	if err := verifyDataSetForService(ctx, p.db, serviceLabel, *dataSetID); err != nil {
		switch {
		case errors.Is(err, ErrDataSetNotFound):
			if p.recordIPOffense(w, r, OffenseBadDataSetAdd) {
				return false
			}
			httpServerError(w, http.StatusNotFound, "Data set not found", err)
		case errors.Is(err, ErrDataSetTerminated):
			if p.recordIPOffense(w, r, OffenseBadDataSetAdd) {
				return false
			}
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			httpServerError(w, http.StatusInternalServerError, "Failed to retrieve data set: "+err.Error(), err)
		}
		return false
	}

	return true
}

func (p *PDPService) verifyUploadAuthHeader(w http.ResponseWriter, r *http.Request) bool {
	if !p.uploadRequireAuth {
		return true
	}

	raw := r.Header.Get(PieceUploadAuthHeader)
	auth, err := ParsePieceUploadAuthHeader(raw)
	if err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return false
	}

	if err := p.uploadAuthVerifier.Verify(r.Context(), auth); err != nil {
		httpServerError(w, http.StatusUnauthorized, "Unauthorized: "+err.Error(), err)
		return false
	}

	return true
}

func parseOptionalDataSetIDBody(r *http.Request) (*uint64, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}

	var req struct {
		DataSetID *uint64 `json:"dataSetId"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return req.DataSetID, nil
}
