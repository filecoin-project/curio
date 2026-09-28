package hashspace

import (
	"encoding/hex"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// ServeHTTP serves /hashspace/{storage id}/{hash} for clustermates:
// GET reads the open-pieces file (Range supported), DELETE removes it.
// POST /hashspace/notify reloads the local map after another node changed it.
// Callers must authenticate the request before it reaches this handler.
func (c *Cluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == notifyPath {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if c.open != nil {
			if err := c.refresh(r.Context(), true); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == accountPath {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if c.open != nil {
			c.publishUsed(r.Context())
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, httpPrefix), "/")
	if len(parts) == 2 && parts[1] == "list" {
		c.serveList(w, r, parts[0])
		return
	}
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

func (c *Cluster) serveList(w http.ResponseWriter, r *http.Request, storageID string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	limit := LIST_PAGE
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	if limit > 4096 {
		limit = 4096
	}
	if !c.HasLocal(storageID) {
		http.NotFound(w, r)
		return
	}
	low, high, after := q.Get("low"), q.Get("high"), q.Get("after")
	for _, b := range []string{low, high, after} {
		if b == "" {
			continue
		}
		if _, err := hex.DecodeString(b); err != nil {
			http.Error(w, "bad hash bound", http.StatusBadRequest)
			return
		}
	}
	hashes, err := c.listPieceHashes(r.Context(), storageID, low, high, after, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	for _, h := range hashes {
		_, _ = w.Write([]byte(h + "\n"))
	}
}
