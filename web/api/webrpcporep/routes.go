//go:build !skiff

package webrpcporep

import (
	"github.com/gorilla/mux"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api/webrpc"
)

type curioHandler struct {
	*webrpc.Handler
	*PoRep
}

// Routes registers shared WebRPC handlers plus Curio-only PoRep endpoints.
func Routes(r *mux.Router, d *deps.Deps, debug bool) {
	h := webrpc.NewHandler(d)
	webrpc.MountRPC(r, &curioHandler{
		Handler: h,
		PoRep:   New(h),
	}, debug)
}
