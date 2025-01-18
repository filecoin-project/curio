package common

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
)

type WorkRequest struct {
	ID int64 `json:"id" db:"id"`

	Data *string `json:"data" db:"request_data"`
	Done *bool   `json:"done" db:"done"`

	WorkAskID int64 `json:"work_ask_id" db:"work_ask_id"`
}

type ProofResponse struct {
	ID    string `json:"id"`
	Proof []byte `json:"proof"`
	Error string `json:"error"`
}

type WorkResponse struct {
	Requests   []WorkRequest `json:"requests"`
	ActiveAsks []int64       `json:"active_asks"`
}

type WorkAsk struct {
	ID int64 `json:"id"`
}

type ProofRequest struct {
	SectorID *abi.SectorID

	// proof request enum
	PoRep *proof.Commit1OutRaw
}

func (p *ProofRequest) Validate() error {
	if p.PoRep != nil {
		if p.SectorID == nil {
			return xerrors.Errorf("sector id is required for PoRep")
		}

		// todo validate vanilla

		return nil
	}
	return xerrors.Errorf("invalid proof request: no proof request")
}
