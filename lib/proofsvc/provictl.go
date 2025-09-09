// SPDX-License-Identifier: CCL-1.0

package proofsvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
)

var MaxRetryTime = 30 * time.Minute

var log = logging.Logger("proofsvc")

// --- Metrics ---

var (
	provictlBuckets  = []float64{0.05, 0.2, 0.5, 1, 5, 15, 45} // seconds
	provictlDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "curio_psvc_provictl_duration_seconds",
		Help:    "Duration of proofsvc provider control operations",
		Buckets: provictlBuckets,
	}, []string{"call"})
)

func init() {
	_ = prometheus.Register(provictlDuration)
}

func recordProvictlDuration(call string, start time.Time) {
	provictlDuration.WithLabelValues(call).Observe(time.Since(start).Seconds())
}

const marketUrl = "https://mainnet.snass.fsp.sh/v0/proofs"

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

func CreateWorkAsk(ctx context.Context, resolver *AddressResolver, signer address.Address, price abi.TokenAmount) (int64, error) {
	start := time.Now()
	defer recordProvictlDuration("CreateWorkAsk", start)
	priceStr := price.String()

	// Create signature for the work ask
	signature, err := Sign(ctx, resolver, signer, "work-ask", []byte(priceStr), time.Now())
	if err != nil {
		return 0, xerrors.Errorf("failed to sign work ask: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/ask/%s?price=%s&signature=%s",
		marketUrl, signer.String(), priceStr, signature), nil)
	if err != nil {
		return 0, xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, xerrors.Errorf("failed to create work ask: %s - %s", resp.Status, string(bodyBytes))
	}

	var workAsk common.WorkAsk
	if err := json.NewDecoder(resp.Body).Decode(&workAsk); err != nil {
		return 0, xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	return workAsk.ID, nil
}

func PollWork(address string) (common.WorkResponse, error) {
	start := time.Now()
	defer recordProvictlDuration("PollWork", start)
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
		defer func() {
			_ = resp.Body.Close()
		}()

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

func WithdrawAsk(ctx context.Context, resolver *AddressResolver, signer address.Address, askID int64) error {
	start := time.Now()
	defer recordProvictlDuration("WithdrawAsk", start)
	askIDStr := fmt.Sprintf("%d", askID)

	// Create signature for the work withdraw
	signature, err := Sign(ctx, resolver, signer, "work-withdraw", []byte(askIDStr), time.Now())
	if err != nil {
		return xerrors.Errorf("failed to sign work withdraw: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/withdraw/%d?provider-id=%s&signature=%s",
		marketUrl, askID, signer.String(), signature), nil)
	if err != nil {
		return xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return xerrors.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return xerrors.Errorf("failed to withdraw ask: %s - %s", resp.Status, string(bodyBytes))
	}

	return nil
}

func GetProof(cid cid.Cid) ([]byte, error) {
	start := time.Now()
	defer recordProvictlDuration("GetProof", start)
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
		defer func() {
			_ = resp.Body.Close()
		}()

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

func RespondWork(ctx context.Context, resolver *AddressResolver, address address.Address, rcid string, proof []byte) (common.ProofReward, bool, error) {
	start := time.Now()
	defer recordProvictlDuration("RespondWork", start)
	ctxWithTimeout, cancel := context.WithTimeout(ctx, MaxRetryTime)
	defer cancel()

	var gone bool

	r, err := retryWithBackoff(ctxWithTimeout, func() (common.ProofReward, error) {
		// Create signature for the work complete
		signature, err := Sign(ctx, resolver, address, "work-complete", []byte(rcid), time.Now())
		if err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to sign work complete: %w", err)
		}

		req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/complete/%s/%s?signature=%s",
			marketUrl, address.String(), rcid, signature), bytes.NewReader(proof))
		if err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to create request: %w", err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to send request: %w", err)
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Infow("failed to respond to work", "requestID", rcid, "status", resp.StatusCode, "body", string(body))
			if resp.StatusCode == http.StatusGone {
				gone = true
				return common.ProofReward{}, nil
			}
			return common.ProofReward{}, xerrors.Errorf("failed to respond to work: %d", resp.StatusCode)
		}

		var reward common.ProofReward
		if err := json.NewDecoder(resp.Body).Decode(&reward); err != nil {
			return common.ProofReward{}, xerrors.Errorf("failed to decode proof reward: %w", err)
		}

		log.Infow("responded to work request",
			"request", rcid,
			"status", reward.Status,
			"nonce", reward.Nonce,
			"amount", types.FIL(reward.Amount).String(),
			"cumulativeAmount", types.FIL(reward.CumulativeAmount).String())

		return reward, nil
	})

	if err != nil {
		return common.ProofReward{}, gone, err
	}

	return r, gone, nil
}
