package mk20

import (
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// PDPV1 represents configuration for product-specific PDP version 1 deals.
type PDPV1 struct {
	ProofSetID uint64 `json:"proof_set_id"`

	// DeleteRoot indicates whether the root of the data should be deleted. This basically means end of deal lifetime.
	DeleteRoot bool `json:"delete_root"`
}

func (p *PDPV1) Validate(db *harmonydb.DB, cfg *config.MK20Config) (ErrorCode, error) {
	code, err := IsProductEnabled(db, p.ProductName())
	if err != nil {
		return code, err
	}
	return Ok, nil
}

func (p *PDPV1) ProductName() ProductName {
	return ProductNamePDPV1
}

var _ product = &PDPV1{}
