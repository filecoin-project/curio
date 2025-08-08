package mk20

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

// RetrievalV1 defines a structure for managing retrieval settings
type RetrievalV1 struct {
	// Indexing indicates if the deal is to be indexed in the provider's system to support CIDs based retrieval
	Indexing bool `json:"indexing"`

	// AnnouncePayload indicates whether the payload should be announced to IPNI.
	AnnouncePayload bool `json:"announce_payload"`

	// AnnouncePiece indicates whether the piece information should be announced to IPNI.
	AnnouncePiece bool `json:"announce_piece"`
}

func (r *RetrievalV1) Validate(db *harmonydb.DB, cfg *config.MK20Config) (DealCode, error) {
	code, err := IsProductEnabled(db, r.ProductName())
	if err != nil {
		return code, err
	}

	if !r.Indexing && r.AnnouncePayload {
		return ErrProductValidationFailed, xerrors.Errorf("deal cannot be announced to IPNI without indexing")
	}

	if r.AnnouncePiece && r.AnnouncePayload {
		return ErrProductValidationFailed, xerrors.Errorf("cannot announce both payload and piece to IPNI at the same time")
	}
	return Ok, nil
}

func (r *RetrievalV1) ProductName() ProductName {
	return ProductNameRetrievalV1
}

var _ product = &RetrievalV1{}
