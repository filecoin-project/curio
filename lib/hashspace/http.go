package hashspace

import (
	"encoding/hex"
	"net/http"
	"os"
	"strings"
)

// ServeHTTP serves /hashspace/{storage id}/{hash} for clustermates:
// GET reads the open-pieces file (Range supported), DELETE removes it.
// Callers must authenticate the request before it reaches this handler.
func (c *Cluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, httpPrefix), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	storageID, hexHash := parts[0], parts[1]
	if b, err := hex.DecodeString(hexHash); err != nil || len(b) != HASH_BYTES {
		http.Error(w, "bad piece hash", http.StatusBadRequest)
		return
	}
	root, err := c.rootOf(storageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		f, err := c.open.openHashOn(root, hexHash)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = f.Close() }()
		info, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "", info.ModTime(), f)
	case http.MethodDelete:
		if err := c.open.deleteHashOn(root, hexHash); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c.publishOne(r.Context(), storageID, root)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
