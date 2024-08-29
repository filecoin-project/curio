package ipniculib

import (
	"crypto/sha256"

	"github.com/ipld/go-ipld-prime/codec/dagjson"
	"github.com/ipld/go-ipld-prime/datamodel"
	"golang.org/x/xerrors"
)

func NodeToLink(node datamodel.Node, lp datamodel.LinkPrototype) (datamodel.Link, error) {
	hasher := sha256.New()
	err := dagjson.Encode(node, hasher)
	if err != nil {
		return nil, xerrors.Errorf("failed to encode: %w", err)
	}
	return lp.BuildLink(hasher.Sum(nil)), nil
}
