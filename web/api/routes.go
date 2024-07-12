// Package api provides the HTTP API for the lotus curio web gui.
package api

import (
	"github.com/gorilla/mux"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/web/api/config"
	"github.com/filecoin-project/curio/web/api/sector"
	"github.com/filecoin-project/curio/web/api/webrpc"
)

func Routes(r *mux.Router, deps *deps.Deps, activeTasks []harmonytask.TaskInterface) {
	webrpc.Routes(r.PathPrefix("/webrpc").Subrouter(), deps, activeTasks)
	config.Routes(r.PathPrefix("/config").Subrouter(), deps)
	sector.Routes(r.PathPrefix("/sector").Subrouter(), deps)
}
