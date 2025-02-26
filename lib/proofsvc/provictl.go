package proofsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/proofsvc/common"
)

var MaxRetryTime = 15 * time.Minute

var log = logging.Logger("proofsvc")

const marketUrl = "https://svc0.fsp.sh/v0/proofs"

// retryWithBackoff executes the given function with exponential backoff
// It will retry until the context is canceled or the function succeeds
// Uses generics to provide type safety for the result
func retryWithBackoff[T any](ctx context.Context, f func() (T, error)) (T, error) {
	var lastErr error
	backoff := 1 * time.Second
	maxBackoff := 60 * time.Second
	var zero T

	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return zero, xerrors.Errorf("context canceled: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return zero, xerrors.Errorf("context canceled: %w", ctx.Err())
		default:
		}

		result, err := f()
		if err == nil {
			return result, nil
		}

		lastErr = err
		log.Warnw("operation failed, retrying", "error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return zero, xerrors.Errorf("context canceled during backoff: %w (last error: %v)", ctx.Err(), lastErr)
		case <-time.After(backoff):
		}

		// Exponential backoff with a maximum
		backoff = min(backoff*2, maxBackoff)
	}
}

func CreateWorkAsk(address string, price abi.TokenAmount) (int64, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/ask/%s?price=%s", marketUrl, address, price), nil)
	if err != nil {
		return 0, xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, xerrors.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, xerrors.Errorf("failed to create work ask: %s", resp.Status)
	}

	var workAsk common.WorkAsk
	if err := json.NewDecoder(resp.Body).Decode(&workAsk); err != nil {
		return 0, xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	return workAsk.ID, nil
}

func PollWork(address string) (common.WorkResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), MaxRetryTime)
	defer cancel()

	return retryWithBackoff(ctx, func() (common.WorkResponse, error) {
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/provider/work/poll/%s", marketUrl, address), nil)
		if err != nil {
			return common.WorkResponse{}, xerrors.Errorf("failed to create request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return common.WorkResponse{}, xerrors.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return common.WorkResponse{}, xerrors.Errorf("failed to poll work: %s", resp.Status)
		}

		var work common.WorkResponse
		if err := json.NewDecoder(resp.Body).Decode(&work); err != nil {
			return common.WorkResponse{}, xerrors.Errorf("failed to unmarshal response body: %w", err)
		}

		return work, nil
	})
}

// 	r.HandleFunc("/provider/proof/{proof-cid}", ps.HandleGetVanillaProof).Methods("GET")

func GetProof(cid cid.Cid) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), MaxRetryTime)
	defer cancel()

	return retryWithBackoff(ctx, func() ([]byte, error) {
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/provider/proof/%s", marketUrl, cid.String()), nil)
		if err != nil {
			return nil, xerrors.Errorf("failed to create request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, xerrors.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, xerrors.Errorf("failed to get proof: %s", resp.Status)
		}

		proofData, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, xerrors.Errorf("failed to read proof data: %w", err)
		}

		return proofData, nil
	})
}

func RespondWork(address string, request common.WorkRequest, proof common.ProofResponse) (common.ProofReward, error) {
	ctx, cancel := context.WithTimeout(context.Background(), MaxRetryTime)
	defer cancel()

	return retryWithBackoff(ctx, func() (common.ProofReward, error) {
		req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/complete/%s/%d", marketUrl, address, request.ID), bytes.NewReader(proof.Proof))
		if err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to create request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return common.ProofReward{}, xerrors.Errorf("failed to respond to work: %s", resp.Status)
		}

		var reward common.ProofReward
		if err := json.NewDecoder(resp.Body).Decode(&reward); err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to decode proof reward: %w", err)
		}

		log.Infow("responded to work request",
			"requestID", request.ID,
			"status", reward.Status,
			"nonce", reward.Nonce,
			"amount", types.FIL(reward.Amount).String(),
			"cumulativeAmount", types.FIL(reward.CumulativeAmount).String())

		return reward, nil
	})
}
