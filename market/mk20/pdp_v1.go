package mk20

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// PDPV1 represents configuration for product-specific PDP version 1 deals.
type PDPV1 struct {
	// CreateProofSet indicated that this deal is meant to create a new ProofSet for the client by storage provider.
	CreateProofSet bool `json:"create_proof_set"`

	// DeleteProofSet indicated that this deal is meant to delete an existing ProofSet created by SP for the client.
	// ProofSetID must be defined.
	DeleteProofSet bool `json:"delete_proof_set"`

	// AddRoot indicated that this deal is meant to add root to a given ProofSet. ProofSetID must be defined.
	AddRoot bool `json:"add_root"`

	// DeleteRoot indicates whether the root of the data should be deleted. ProofSetID must be defined.
	DeleteRoot bool `json:"delete_root"`

	// ProofSetID is PDP verified contract proofset ID. It must be defined for all deals except when CreateProofSet is true.
	ProofSetID *uint64 `json:"proof_set_id"`

	// RecordKeeper specifies the record keeper contract address for the new PDP proofset.
	RecordKeeper string `json:"record_keeper"`

	// RootIDs is a list of root ids in a proof set.
	RootIDs []uint64 `json:"root_ids"`

	// ExtraData can be used to send additional information to service contract when Verifier action like AddRoot, DeleteRoot etc. are performed.
	ExtraData []byte `json:"extra_data"`
}

func (p *PDPV1) Validate(db *harmonydb.DB, cfg *config.MK20Config) (DealCode, error) {
	code, err := IsProductEnabled(db, p.ProductName())
	if err != nil {
		return code, err
	}

	if ok := p.CreateProofSet || p.DeleteProofSet || p.AddRoot || p.DeleteRoot; !ok {
		return ErrBadProposal, xerrors.Errorf("deal must have one of the following flags set: create_proof_set, delete_proof_set, add_root, delete_root")
	}

	if p.CreateProofSet {
		if p.ProofSetID != nil {
			return ErrBadProposal, xerrors.Errorf("create_proof_set cannot be set with proof_set_id")
		}
		if p.RecordKeeper == "" {
			return ErrBadProposal, xerrors.Errorf("record_keeper must be defined for create_proof_set")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for create_proof_set")
		}
		if !common.IsHexAddress(p.RecordKeeper) {
			return ErrBadProposal, xerrors.Errorf("record_keeper must be a valid address")
		}
	}

	if p.DeleteProofSet {
		if p.ProofSetID == nil {
			return ErrBadProposal, xerrors.Errorf("delete_proof_set must have proof_set_id defined")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for delete_proof_set")
		}
	}

	if p.AddRoot {
		if p.ProofSetID == nil {
			return ErrBadProposal, xerrors.Errorf("add_root must have proof_set_id defined")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for add_root")
		}
	}

	if p.DeleteRoot {
		if p.ProofSetID == nil {
			return ErrBadProposal, xerrors.Errorf("delete_root must have proof_set_id defined")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for delete_root")
		}
		if len(p.RootIDs) == 0 {
			return ErrBadProposal, xerrors.Errorf("root_ids must be defined for delete_proof_set")
		}
	}

	// Only 1 action is allowed per deal
	if btoi(p.CreateProofSet)+btoi(p.DeleteProofSet)+btoi(p.AddRoot)+btoi(p.DeleteRoot) > 1 {
		return ErrBadProposal, xerrors.Errorf("only one action is allowed per deal")
	}

	ctx := context.Background()

	if p.DeleteProofSet {
		pid := *p.ProofSetID
		var exists bool
		err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_proof_set WHERE id = $1 AND removed = FALSE)`, pid).Scan(&exists)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("checking if proofset exists: %w", err)
		}
		if !exists {
			return ErrBadProposal, xerrors.Errorf("proofset does not exist")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for delete_proof_set")
		}
	}

	if p.AddRoot {
		pid := *p.ProofSetID
		var exists bool
		err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_proof_set WHERE id = $1 AND removed = FALSE)`, pid).Scan(&exists)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("checking if proofset exists: %w", err)
		}
		if !exists {
			return ErrBadProposal, xerrors.Errorf("proofset does not exist")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for add_root")
		}
	}

	if p.DeleteRoot {
		pid := *p.ProofSetID
		var exists bool
		err := db.QueryRow(ctx, `SELECT COUNT(*) = cardinality($2::BIGINT[]) AS all_exist_and_active
										FROM pdp_proofset_root r
										JOIN pdp_proof_set s ON r.proofset = s.id
										WHERE r.proofset = $1
										  AND r.root = ANY($2)
										  AND r.removed = FALSE
										  AND s.removed = FALSE;
										)`, pid, p.RootIDs).Scan(&exists)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("checking if proofset and roots exists: %w", err)
		}
		if !exists {
			return ErrBadProposal, xerrors.Errorf("proofset or one of the roots does not exist")
		}
		if len(p.ExtraData) == 0 {
			return ErrBadProposal, xerrors.Errorf("extra_data must be defined for delete_root")
		}
	}

	return Ok, nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (p *PDPV1) ProductName() ProductName {
	return ProductNamePDPV1
}

var _ product = &PDPV1{}
