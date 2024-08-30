package ipniculib

import (
	"github.com/ipld/go-ipld-prime/datamodel"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
)

func NodeToLink(node datamodel.Node, lp datamodel.LinkPrototype) (datamodel.Link, error) {
	linkSystem := cidlink.DefaultLinkSystem()
	return linkSystem.ComputeLink(lp, node)
}
