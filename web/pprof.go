package web

import (
	"net/http"
	"net/http/pprof"

	"github.com/gorilla/mux"
)

func registerPprof(mx *mux.Router) {
	mx.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mx.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mx.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mx.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mx.PathPrefix("/debug/pprof/").HandlerFunc(pprof.Index)
	mx.Handle("/debug/pprof", http.RedirectHandler("/debug/pprof/", http.StatusFound))
}
