package proofsvc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/proofsvc/common"
)

var log = logging.Logger("proofsvc")

const marketUrl = "https://svc0.fsp.sh/v0/proofs"

func CreateWorkAsk(address string) (int64, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/ask/%s", marketUrl, address), nil)
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
}

func RespondWork(address string, request common.WorkRequest, proof common.ProofResponse) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/complete/%s/%d", marketUrl, address, request.ID), bytes.NewReader(proof.Proof))
	if err != nil {
		return xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return xerrors.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Errorw("reporting work complete", "error", err)
		return xerrors.Errorf("failed to respond to work: %s", resp.Status)
	}

	log.Infof("responded to work request %s", request.ID)

	return nil
}
