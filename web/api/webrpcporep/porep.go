//go:build !maxboom

package webrpcporep

import "github.com/filecoin-project/curio/web/api/webrpc"

// PoRep registers Curio-only sealing / PoRep / WindowPoSt WebRPC methods.
type PoRep struct {
	*webrpc.Handler
}

func New(h *webrpc.Handler) *PoRep {
	return &PoRep{Handler: h}
}
