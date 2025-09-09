// SPDX-License-Identifier: CCL-1.0

package proofsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
)

const clientUrl = "https://mainnet.snass.fsp.sh/v0/proofs"

type NFilAmount = int64

// const maxNFilAmount = 1_000_000_000 * 2_000_000_000
const attoPerNano = 1_000_000_000 // 1 nFIL = 10^9 attoFIL
const roCacheTTL = 300 * time.Millisecond

var (
	lastAvailabilityLock  sync.Mutex
	lastAvailabilityCheck = time.Now().Add(-time.Hour)
	lastAvailability      = false

	lastPriceLock  sync.Mutex
	lastPriceCheck = time.Now().Add(-time.Hour)
	lastPrice      = PriceResponse{}
)

// --- Metrics ---

var (
	clientctlBuckets  = []float64{0.05, 0.2, 0.5, 1, 5} // seconds
	clientctlDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "curio_psvc_clientctl_duration_seconds",
		Help:    "Duration of proofsvc clientctl operations",
		Buckets: clientctlBuckets,
	}, []string{"call"})
)

func init() {
	_ = prometheus.Register(clientctlDuration)
}

func recordClientctlDuration(call string, start time.Time) {
	clientctlDuration.WithLabelValues(call).Observe(time.Since(start).Seconds())
}

// NfilFromTokenAmount converts a token amount in attoFIL to nanoFIL (nFIL).
// It returns an error if the token amount is not divisible by 1 nFIL.
func NfilFromTokenAmount(tokenAmount abi.TokenAmount) (NFilAmount, error) {

	if types.BigMod(tokenAmount, types.NewInt(attoPerNano)).Sign() != 0 {
		return 0, xerrors.Errorf("token amount %d is not divisible by 1 nFIL", tokenAmount)
	}

	return types.BigDiv(tokenAmount, types.NewInt(attoPerNano)).Int64(), nil
}

// TokenAmountFromNfil converts a nanoFIL (nFIL) amount to attoFIL.
func TokenAmountFromNfil(nfil NFilAmount) abi.TokenAmount {
	return types.BigMul(types.NewInt(uint64(nfil)), types.NewInt(attoPerNano))
}

func CheckAvailability() (bool, error) {
	start := time.Now()
	defer recordClientctlDuration("CheckAvailability", start)

	lastAvailabilityLock.Lock()
	defer lastAvailabilityLock.Unlock()

	if time.Since(lastAvailabilityCheck) < roCacheTTL {
		log.Infow("client cached avail", "last", lastAvailability, "since", time.Since(lastAvailabilityCheck))
		return lastAvailability, nil
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/client/available", clientUrl), nil)
	if err != nil {
		return false, xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return false, xerrors.Errorf("failed to check availability: %s", resp.Status)
	}

	var available struct {
		Available bool `json:"available"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&available); err != nil {
		return false, xerrors.Errorf("failed to decode response body: %w", err)
	}

	lastAvailability = available.Available
	lastAvailabilityCheck = time.Now()

	return available.Available, nil
}

type PriceResponse struct {
	Price               int64 `json:"price_nfil"`
	PriceNfilBase       int64 `json:"price_nfil_base"`
	PriceNfilServiceFee int64 `json:"price_nfil_service_fee"`
	FeeNum              int64 `json:"fee_num"`
	FeeDenom            int64 `json:"fee_denom"`
	Epoch               int64 `json:"epoch"`
}

// GetCurrentPrice retrieves the current price for proof generation from the service
func GetCurrentPrice() (PriceResponse, error) {
	start := time.Now()
	defer recordClientctlDuration("GetCurrentPrice", start)

	lastPriceLock.Lock()
	defer lastPriceLock.Unlock()

	if time.Since(lastPriceCheck) < roCacheTTL {
		log.Infow("client cached price", "last", lastPrice, "since", time.Since(lastPriceCheck))
		return lastPrice, nil
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/client/current-price", clientUrl), nil)
	if err != nil {
		return PriceResponse{}, xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return PriceResponse{}, xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return PriceResponse{}, xerrors.Errorf("failed to get current price: %s", resp.Status)
	}

	var priceResp PriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&priceResp); err != nil {
		return PriceResponse{}, xerrors.Errorf("failed to unmarshal response body: %w", err)
	}
	if priceResp.Price <= 0 {
		log.Errorw("bad price response", "resp", priceResp)
		return PriceResponse{}, xerrors.Errorf("invalid price received: %d", priceResp.Price)
	}

	log.Infow("current price", "price", priceResp.Price)

	lastPrice = priceResp
	lastPriceCheck = time.Now()

	return priceResp, nil
}

// UploadProofData uploads proof data to the service and returns the CID
func UploadProofData(ctx context.Context, proofData []byte) (cid.Cid, error) {
	start := time.Now()
	defer recordClientctlDuration("UploadProofData", start)
	// Calculate the CID of the proof data
	proofDataCid, err := cid.NewPrefixV1(cid.Raw, mh.SHA2_256).Sum(proofData)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to calculate proof data CID: %w", err)
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "PUT",
		fmt.Sprintf("%s/client/proof-data/%s", clientUrl, proofDataCid.String()),
		bytes.NewReader(proofData))
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cid.Undef, xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return cid.Undef, xerrors.Errorf("failed to upload proof data: %s - %s", resp.Status, string(bodyBytes))
	}

	return proofDataCid, nil
}

// RequestProof submits a proof request to the service
func RequestProof(request common.ProofRequest) (bool, error) {
	start := time.Now()
	defer recordClientctlDuration("RequestProof", start)
	reqBody, err := json.Marshal(request)
	if err != nil {
		return false, xerrors.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/client/request", clientUrl), bytes.NewReader(reqBody))
	if err != nil {
		return false, xerrors.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)

		backoff := resp.StatusCode == http.StatusServiceUnavailable

		return backoff, xerrors.Errorf("failed to submit proof request: %s - %s", resp.Status, string(bodyBytes))
	}

	var requestResp struct {
		ID cid.Cid `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&requestResp); err != nil {
		return false, xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	if requestResp.ID != request.Data {
		return false, xerrors.Errorf("proof request ID mismatch: %s != %s", requestResp.ID, request.Data)
	}

	return true, nil
}

// GetProofStatus checks the status of a proof request by ID
func GetProofStatus(ctx context.Context, requestCid cid.Cid) (common.ProofResponse, error) {
	start := time.Now()
	defer recordClientctlDuration("GetProofStatus", start)
	ctx, cancel := context.WithTimeout(ctx, MaxRetryTime)
	defer cancel()

	return retryWithBackoff(ctx, func() (common.ProofResponse, error) {
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/client/status/%s", clientUrl, requestCid.String()), nil)
		if err != nil {
			return common.ProofResponse{}, xerrors.Errorf("failed to create request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return common.ProofResponse{}, xerrors.Errorf("failed to send request: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return common.ProofResponse{}, xerrors.Errorf("failed to get proof status: %s - %s", resp.Status, string(bodyBytes))
		}

		var proofResp common.ProofResponse
		if err := json.NewDecoder(resp.Body).Decode(&proofResp); err != nil {
			return common.ProofResponse{}, xerrors.Errorf("failed to unmarshal response body: %w", err)
		}

		// not ready yet, return empty response to the poller above
		if proofResp.Proof == nil && proofResp.Error == "" {
			return common.ProofResponse{}, nil
		}

		// If there's an error in the proof generation, return it
		if proofResp.Error != "" {
			return common.ProofResponse{}, xerrors.Errorf("proof generation failed: %s", proofResp.Error)
		}

		return proofResp, nil
	})
}

// WaitForProof submits a proof request and waits for the result
func WaitForProof(request common.ProofRequest) ([]byte, error) {
	start := time.Now()
	defer recordClientctlDuration("WaitForProof", start)
	// Wait for the proof
	proofResp, err := GetProofStatus(context.Background(), request.Data)
	if err != nil {
		return nil, xerrors.Errorf("failed to get proof: %w", err)
	}

	return proofResp.Proof, nil
}

// ClientPaymentStatus represents the payment status for a client wallet
// as returned by the backend /client/payment/status/{wallet-id} endpoint.
type ClientPaymentStatus struct {
	Found                bool   `json:"found"`
	Nonce                int64  `json:"nonce"`
	CumulativeAmountNFil int64  `json:"cumulative_amount_nfil"`
	AmountNFil           int64  `json:"amount_nfil"`
	Signature            []byte `json:"signature"`
	CreatedAt            string `json:"created_at"`
}

// GetClientPaymentStatus retrieves the latest payment status for a given wallet ID.
func GetClientPaymentStatus(walletID abi.ActorID) (*ClientPaymentStatus, error) {
	start := time.Now()
	defer recordClientctlDuration("GetClientPaymentStatus", start)
	url := fmt.Sprintf("%s/client/payment/status/%d", clientUrl, walletID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, xerrors.Errorf("no payments found for wallet %d", walletID)
	}
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, xerrors.Errorf("failed to get payment status: %s - %s", resp.Status, string(bodyBytes))
	}

	var status ClientPaymentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, xerrors.Errorf("failed to decode payment status: %w", err)
	}

	return &status, nil
}
