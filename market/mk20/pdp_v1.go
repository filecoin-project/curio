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
	// CreateDataSet indicated that this deal is meant to create a new DataSet for the client by storage provider.
	CreateDataSet bool `json:"create_data_set"`

	// DeleteDataSet indicated that this deal is meant to delete an existing DataSet created by SP for the client.
	// DataSetID must be defined.
	DeleteDataSet bool `json:"delete_data_set"`

	// AddPiece indicated that this deal is meant to add Piece to a given DataSet. DataSetID must be defined.
	AddPiece bool `json:"add_piece"`

	// DeletePiece indicates whether the Piece of the data should be deleted. DataSetID must be defined.
	DeletePiece bool `json:"delete_piece"`

	// DataSetID is PDP verified contract dataset ID. It must be defined for all deals except when CreateDataSet is true.
	DataSetID *uint64 `json:"data_set_id,omitempty"`

	// RecordKeeper specifies the record keeper contract address for the new PDP dataset.
	RecordKeeper string `json:"record_keeper"`

	// PieceIDs is a list of Piece ids in a proof set.
	PieceIDs []uint64 `json:"piece_ids,omitempty"`

	// ExtraData can be used to send additional information to service contract when Verifier action like AddRoot, DeleteRoot etc. are performed.
	ExtraData []byte `json:"extra_data,omitempty"`
}

func (p *PDPV1) Validate(db *harmonydb.DB, cfg *config.MK20Config) (DealCode, error) {
	code, err := IsProductEnabled(db, p.ProductName())
	if err != nil {
		return code, err
	}

	if ok := p.CreateDataSet || p.DeleteDataSet || p.AddPiece || p.DeletePiece; !ok {
		return ErrBadProposal, xerrors.Errorf("deal must have one of the following flags set: create_data_set, delete_data_set, add_piece, delete_piece")
	}

	var existingAddress bool

	err = db.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM eth_keys WHERE role = 'pdp')`).Scan(&existingAddress)
	if err != nil {
		return ErrServerInternalError, xerrors.Errorf("checking if pdp address exists: %w", err)
	}

	if !existingAddress {
		return ErrServiceMaintenance, xerrors.Errorf("pdp key not configured by storage provider")
	}

	if p.CreateDataSet {
		if p.DataSetID != nil {
			return ErrBadProposal, xerrors.Errorf("create_proof_set cannot be set with data_set_id")
		}
		if p.RecordKeeper == "" {
			return ErrBadProposal, xerrors.Errorf("record_keeper must be defined for create_proof_set")
		}
		if !common.IsHexAddress(p.RecordKeeper) {
			return ErrBadProposal, xerrors.Errorf("record_keeper must be a valid address")
		}
	}

	// Only 1 action is allowed per deal
	if btoi(p.CreateDataSet)+btoi(p.DeleteDataSet)+btoi(p.AddPiece)+btoi(p.DeletePiece) > 1 {
		return ErrBadProposal, xerrors.Errorf("only one action is allowed per deal")
	}

	ctx := context.Background()

	if p.DeleteDataSet {
		if p.DataSetID == nil {
			return ErrBadProposal, xerrors.Errorf("delete_proof_set must have data_set_id defined")
		}
		pid := *p.DataSetID
		var exists bool
		err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_data_set WHERE id = $1 AND removed = FALSE)`, pid).Scan(&exists)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("checking if dataset exists: %w", err)
		}
		if !exists {
			return ErrBadProposal, xerrors.Errorf("dataset does not exist for the client")
		}
	}

	if p.AddPiece {
		if p.DataSetID == nil {
			return ErrBadProposal, xerrors.Errorf("add_root must have data_set_id defined")
		}
		pid := *p.DataSetID
		var exists bool
		err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pdp_data_set WHERE id = $1 AND removed = FALSE)`, pid).Scan(&exists)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("checking if dataset exists: %w", err)
		}
		if !exists {
			return ErrBadProposal, xerrors.Errorf("dataset does not exist for the client")
		}
	}

	if p.DeletePiece {
		if p.DataSetID == nil {
			return ErrBadProposal, xerrors.Errorf("delete_root must have data_set_id defined")
		}
		pid := *p.DataSetID
		if len(p.PieceIDs) == 0 {
			return ErrBadProposal, xerrors.Errorf("root_ids must be defined for delete_proof_set")
		}
		var exists bool
		err := db.QueryRow(ctx, `SELECT COUNT(*) = cardinality($2::BIGINT[]) AS all_exist_and_active
										FROM pdp_dataset_piece r
										JOIN pdp_data_set s ON r.data_set_id = s.id
										WHERE r.data_set_id = $1
										  AND r.piece = ANY($2)
										  AND r.removed = FALSE
										  AND s.removed = FALSE;`, pid, p.PieceIDs).Scan(&exists)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("checking if dataset and pieces exists: %w", err)
		}
		if !exists {
			return ErrBadProposal, xerrors.Errorf("dataset or one of the pieces does not exist for the client")
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
