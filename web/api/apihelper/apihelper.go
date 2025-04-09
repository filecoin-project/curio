package apihelper

import (
	"net/http"
	"runtime/debug"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("apihelper")

func OrHTTPFail(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), 500)
		log.Errorw("http fail", "err", err, "stack", string(debug.Stack()))
		panic(err)
	}
}
