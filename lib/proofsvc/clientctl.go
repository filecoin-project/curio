package proofsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
)

const clientUrl = "https://svc0.fsp.sh/v0/proofs"

type NFilAmount = int64

const maxNFilAmount = 1_000_000_000 * 2_000_000_000
const attoPerNano = 1_000_000_000 // 1 nFIL = 10^9 attoFIL

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

// GetCurrentPrice retrieves the current price for proof generation from the service
func GetCurrentPrice() (abi.TokenAmount, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/client/current-price", clientUrl), nil)
	if err != nil {
		return abi.NewTokenAmount(0), xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return abi.NewTokenAmount(0), xerrors.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return abi.NewTokenAmount(0), xerrors.Errorf("failed to get current price: %s", resp.Status)
	}

	var priceResp struct {
		Price int64 `json:"price_nfil"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&priceResp); err != nil {
		return abi.NewTokenAmount(0), xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	log.Infow("current price", "price", priceResp.Price)

	// TODO TMRW:
	// * Task display with correct restart in the UI
	// * Return unconsumed-by-service payments to not block the nonce

	return TokenAmountFromNfil(priceResp.Price), nil
}

// UploadProofData uploads proof data to the service and returns the CID
func UploadProofData(ctx context.Context, proofData []byte) (cid.Cid, error) {
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return cid.Undef, xerrors.Errorf("failed to upload proof data: %s - %s", resp.Status, string(bodyBytes))
	}

	return proofDataCid, nil
}

// RequestProof submits a proof request to the service
func RequestProof(request common.ProofRequest) error {
	reqBody, err := json.Marshal(request)
	if err != nil {
		return xerrors.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/client/request", clientUrl), bytes.NewReader(reqBody))
	if err != nil {
		return xerrors.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return xerrors.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("failed to submit proof request: %s - %s", resp.Status, string(bodyBytes))
	}

	var requestResp struct {
		ID cid.Cid `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&requestResp); err != nil {
		return xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	if requestResp.ID != request.Data {
		return xerrors.Errorf("proof request ID mismatch: %s != %s", requestResp.ID, request.Data)
	}

	return nil
}

// GetProofStatus checks the status of a proof request by ID
func GetProofStatus(requestCid cid.Cid) (common.ProofResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), MaxRetryTime)
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
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return common.ProofResponse{}, xerrors.Errorf("failed to get proof status: %s - %s", resp.Status, string(bodyBytes))
		}

		var proofResp common.ProofResponse
		if err := json.NewDecoder(resp.Body).Decode(&proofResp); err != nil {
			return common.ProofResponse{}, xerrors.Errorf("failed to unmarshal response body: %w", err)
		}

		// If the proof is not ready yet, return an error to trigger retry
		if proofResp.Proof == nil && proofResp.Error == "" {
			return common.ProofResponse{}, xerrors.Errorf("proof not ready yet")
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
	// Wait for the proof
	proofResp, err := GetProofStatus(request.Data)
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
	url := fmt.Sprintf("%s/client/payment/status/%d", clientUrl, walletID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, xerrors.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

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
