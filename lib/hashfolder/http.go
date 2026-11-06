package hashfolder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/lib/fs2"
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
		if len(c.roots) > 0 {
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
	kind, err := parseSpaceKind(r.URL.Query().Get("space"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	space := c.space(kind)
	if space == nil {
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
		f, err := space.openHashOn(root, hexHash)
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
		n, existed, err := putHash(space, root, hexHash, r.Body, r.ContentLength)
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
		if err := space.deleteHashOn(root, hexHash); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		c.publishOne(r.Context(), storageID, root)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// putHash streams r into the piece file. With a known length, an existing
// shorter file is a partial copy and is replaced. Otherwise an existing file
// is left in place and its size returned with existed set.
func putHash(space *Space, root, hexHash string, r io.Reader, size int64) (int64, bool, error) {
	if r == nil {
		r = bytes.NewReader(nil)
	}
	var w io.WriteCloser
	var err error
	if size >= 0 {
		var existing int64
		w, existing, err = space.ReplaceShorterHashOn(root, hexHash, size)
		if err == nil && w == nil {
			_, _ = io.Copy(io.Discard, r)
			return existing, true, nil
		}
	} else {
		w, err = space.WriteHashOn(root, hexHash)
	}
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			// The client is already streaming. Drain it so the 204 can be
			// delivered instead of resetting the connection.
			_, _ = io.Copy(io.Discard, r)
			n, err := hashSize(space, root, hexHash)
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
			n, err := hashSize(space, root, hexHash)
			return n, true, err
		}
		return 0, false, err
	}
	return n, false, nil
}

func hashSize(space *Space, root, hexHash string) (int64, error) {
	f, err := space.openHashOn(root, hexHash)
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

// serveList answers GET/HEAD .../{storage}/list with up to limit piece
// hashes between low and high after after, one per line.
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
}

// listPieceHashes returns up to limit piece hashes on storageID within
// [low, high] that sort after after, skipping names that are not piece hashes.
func (c *Cluster) listPieceHashes(ctx context.Context, storageID, kind, low, high, after string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []string
	for len(out) < limit {
		batch, err := c.listHashes(ctx, storageID, kind, low, high, after, limit)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, h := range batch {
			after = h
			if !isPieceHash(h) {
				continue
			}
			out = append(out, h)
			if len(out) == limit {
				return out, nil
			}
		}
		if len(batch) < limit {
			break
		}
	}
	return out, nil
}

// listHashes lists one page of file names in storageID's kind folder,
// locally or through that node's list endpoint.
func (c *Cluster) listHashes(ctx context.Context, storageID, kind, low, high, after string, limit int) ([]string, error) {
	kind, err := parseSpaceKind(kind)
	if err != nil {
		return nil, err
	}
	if !c.HasLocal(storageID) {
		return c.remoteList(ctx, storageID, kind, low, high, after, limit)
	}
	root, err := c.rootOf(storageID)
	if err != nil {
		return nil, err
	}
	hashes, err := fs2.ListHashesInterval(filepath.Join(root, kind), low, high, after, limit)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, xerrors.Errorf("listing %s: %w", storageID, err)
	}
	return hashes, nil
}

// remoteList fetches one list page from the node serving storageID.
func (c *Cluster) remoteList(ctx context.Context, storageID, kind, low, high, after string, limit int) ([]string, error) {
	q := "?limit=" + strconv.Itoa(limit)
	if low != "" {
		q += "&low=" + low
	}
	if high != "" {
		q += "&high=" + high
	}
	if after != "" {
		q += "&after=" + after
	}
	var body []byte
	err := c.remoteHash(ctx, http.MethodGet, storageID, kind, "list"+q, nil, nil, func(r *http.Response) (bool, error) {
		defer func() { _ = r.Body.Close() }()
		if r.StatusCode != http.StatusOK {
			return false, xerrors.Errorf("GET list %s: %s", storageID, r.Status)
		}
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		return false, err
	})
	if err != nil {
		return nil, err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			out = append(out, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// isPieceHash reports whether h is a full-length hex hash.
func isPieceHash(h string) bool {
	if len(h) != HASH_BYTES*2 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}
