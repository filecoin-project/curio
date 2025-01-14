package proofsvc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2"
	logging "github.com/ipfs/go-log/v2"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/proofsvc/common"
)

var log = logging.Logger("proofsvc")

var PollInterval = 5 * time.Second

const marketUrl = "https://svc0.fsp.sh/v0/proofs"

type ProviderCtl struct {
	id       uuid.UUID
	workFunc WorkFunc

	workRunning map[string]bool
	recentDone  *lru.Cache[string, common.ProofResponse]

	lock sync.Mutex
}

func (pc *ProviderCtl) Wait() {
	select {}
}

type WorkFunc func(request common.WorkRequest) (common.ProofResponse, error)

func NewProviderCtl(workFunc WorkFunc) (*ProviderCtl, error) {
	pc := &ProviderCtl{
		workFunc: workFunc,

		recentDone:  must.One(lru.New[string, common.ProofResponse](100)),
		workRunning: make(map[string]bool),
	}

	return pc, pc.register()
}

func (pc *ProviderCtl) register() error {
	slots := 1

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/provider/register?slots=%d", marketUrl, slots), nil)
	if err != nil {
		return xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return xerrors.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return xerrors.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Errorw("failed to register", "error", resp.Status, "body", string(body))
		return xerrors.Errorf("failed to register: %s [%s]", resp.Status, string(body))
	}

	var id struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(body, &id); err != nil {
		return xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	pc.id = id.ID

	log.Infof("registered provider with id %s", pc.id)

	go pc.run()

	return nil
}

func (pc *ProviderCtl) run() {
	for {
		if err := pc.pollWork(); err != nil {
			log.Errorf("failed to poll work: %w", err)
		}

		time.Sleep(PollInterval)
	}
}

func (pc *ProviderCtl) pollWork() error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/provider/work/poll/%s", marketUrl, pc.id), nil)
	if err != nil {
		return xerrors.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return xerrors.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return xerrors.Errorf("failed to poll work: %s", resp.Status)
	}

	var work common.WorkResponse
	if err := json.NewDecoder(resp.Body).Decode(&work); err != nil {
		return xerrors.Errorf("failed to unmarshal response body: %w", err)
	}

	if !work.Work {
		return nil
	}

	for _, request := range work.Requests {
		if request.Done {
			log.Infof("request %s is done", request.ID)
			continue
		}

		pc.lock.Lock()
		if pc.recentDone.Contains(request.ID) {
			pc.lock.Unlock()
			continue
		}

		if pc.workRunning[request.ID] {
			pc.lock.Unlock()
			continue
		}
		pc.workRunning[request.ID] = true
		pc.lock.Unlock()

		log.Infof("got work request %s", request.ID)

		go func(request common.WorkRequest) {
			proof, err := pc.workFunc(request)
			if err != nil {
				log.Errorf("failed to work on request %s: %w", request.ID, err)
				pc.lock.Lock()
				pc.recentDone.Add(request.ID, common.ProofResponse{
					Proof: []byte(""),
					ID:    request.ID,
					Error: err.Error(),
				})
				pc.workRunning[request.ID] = false
				pc.lock.Unlock()

				if err := pc.respondWork(request, common.ProofResponse{
					Proof: []byte(""),
					ID:    request.ID,
					Error: err.Error(),
				}); err != nil {
					log.Errorf("failed to respond to work: %w", err)
				}
				return
			}

			pc.lock.Lock()
			pc.recentDone.Add(request.ID, proof)
			pc.workRunning[request.ID] = false
			pc.lock.Unlock()

			if err := pc.respondWork(request, proof); err != nil {
				log.Errorf("failed to respond to work: %w", err)
			}
		}(request)
	}

	return nil
}

func (pc *ProviderCtl) respondWork(request common.WorkRequest, proof common.ProofResponse) error {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/provider/work/complete/%s/%s", marketUrl, pc.id, request.ID), bytes.NewReader(proof.Proof))
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
