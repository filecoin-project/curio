package hashspace

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const (
	headerPutSize    = "X-Curio-Size"
	headerPutExisted = "X-Curio-Existed"
)

// ServeHTTP serves /hashspace/{storage id}/{hash} for clustermates.
// GET reads the piece file (Range supported), PUT writes it, DELETE removes it.
// Query space selects open-pieces or acl-pieces; it defaults to open-pieces.
// POST /hashspace/notify reloads the local map after another node changed it.
// Callers must authenticate the request before it reaches this handler.
func (c *Cluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == notifyPath {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := c.syncMap(r.Context(), true); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
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
		/* Cluster.serveList */ func(w http.ResponseWriter, r *http.Request, storageID string) {
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
			kind, err := parseSpaceKind(q.Get("space"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
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
			hashes, err := c.listPieceHashes(r.Context(), storageID, kind, low, high, after, limit)
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
		}(w, r, parts[0])
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
	kind, err := parseSpaceKind(r.URL.Query().Get("space"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sp := c.space(kind)
	if sp == nil {
		http.NotFound(w, r)
		return
	}
	root, err := c.rootOf(storageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		f, err := sp.openHashOn(root, hexHash)
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
	case http.MethodPut:
		n, existed, err := /* putHash */ func(sp *Space, root, hexHash string, r io.Reader) (int64, bool, error) {
			if r == nil {
				r = bytes.NewReader(nil)
			}
			w, err := sp.WriteHashOn(root, hexHash)
			if err != nil {
				if errors.Is(err, os.ErrExist) {
					// The client is already streaming. Drain it so the 204 can be
					// delivered instead of resetting the connection.
					_, _ = io.Copy(io.Discard, r)
					n, err := hashSize(sp, root, hexHash)
					return n, true, err
				}
				return 0, false, err
			}
			n, err := io.CopyBuffer(w, r, make([]byte, 8<<20))
			if err != nil {
				if a, ok := w.(interface{ Abort() error }); ok {
					_ = a.Abort()
				}
				return 0, false, err
			}
			if err := w.Close(); err != nil {
				if errors.Is(err, os.ErrExist) {
					n, err := hashSize(sp, root, hexHash)
					return n, true, err
				}
				return 0, false, err
			}
			return n, false, nil
		}(sp, root, hexHash, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c.publishOne(r.Context(), storageID, root)
		if err := c.MaybeRebalance(r.Context(), storageID); err != nil {
			log.Warnw("hash space rebalance", "storage", storageID, "error", err)
		}
		w.Header().Set(headerPutSize, strconv.FormatInt(n, 10))
		if existed {
			w.Header().Set(headerPutExisted, "1")
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := sp.deleteHashOn(root, hexHash); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c.publishOne(r.Context(), storageID, root)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// putHash streams r into the piece file. An existing file is left in place
// and its size returned with existed set.

func hashSize(sp *Space, root, hexHash string) (int64, error) {
	f, err := sp.openHashOn(root, hexHash)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
