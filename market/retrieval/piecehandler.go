package retrieval

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/ipfs/go-cid"
	"go.opencensus.io/stats"

	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/market/retrieval/remoteblockstore"
)

// For data served by the endpoints in the HTTP server that never changes
// (eg pieces identified by a piece CID) send a cache header with a constant,
// non-zero last modified time.
var lastModified = time.UnixMilli(1)

func (rp *Provider) handleByPieceCid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	stats.Record(ctx, remoteblockstore.HttpPieceByCidRequestCount.M(1))

	// Remove the path up to the piece cid
	prefixLen := len(piecePrefix)
	if len(r.URL.Path) <= prefixLen {
		log.Errorf("path '%s' is missing piece CID", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid400ResponseCount.M(1))
		return
	}

	pieceCidStr := r.URL.Path[prefixLen:]
	pieceCid, err := cid.Parse(pieceCidStr)
	if err != nil {
		log.Errorf("parsing piece CID '%s': %s", pieceCidStr, err.Error())
		w.WriteHeader(http.StatusBadRequest)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid400ResponseCount.M(1))
		return
	}

	// Get a reader over the piece
	reader, size, err := rp.cpr.GetSharedPieceReader(ctx, pieceCid)
	if err != nil {
		log.Errorf("server error getting content for piece CID %s: %s", pieceCid, err)
		if errors.Is(err, cachedreader.NoDealErr) {
			w.WriteHeader(http.StatusNotFound)
			stats.Record(ctx, remoteblockstore.HttpPieceByCid404ResponseCount.M(1))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid500ResponseCount.M(1))
		return
	}

	buf := make([]byte, 512)
	n, _ := reader.Read(buf)
	contentType := http.DetectContentType(buf[:n])

	// rewind reader before sending
	_, err = reader.Seek(0, io.SeekStart)
	if err != nil {
		log.Errorf("error rewinding reader for piece CID %s: %s", pieceCid, err)
		w.WriteHeader(http.StatusInternalServerError)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid500ResponseCount.M(1))
		return
	}

	setHeaders(w, pieceCid, contentType, int64(size))
	serveContent(w, r, reader)

	stats.Record(ctx, remoteblockstore.HttpPieceByCid200ResponseCount.M(1))
	stats.Record(ctx, remoteblockstore.HttpPieceByCidRequestDuration.M(float64(time.Since(startTime).Milliseconds())))
}

func setHeaders(w http.ResponseWriter, pieceCid cid.Cid, contentType string, size int64) {
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	w.Header().Set("Cache-Control", "public, max-age=29030400, immutable")
	w.Header().Set("Content-Type", contentType)
	if contentType != "application/octet-stream" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			ext := exts[0]
			filename := pieceCid.String() + ext
			encoded := url.PathEscape(filename)

			w.Header().Set("Content-Disposition",
				fmt.Sprintf(`inline; filename="%s"; filename*=UTF-8''%s`, filename, encoded))
		}
	}
	w.Header().Set("Etag", pieceCid.String())

}

func serveContent(res http.ResponseWriter, req *http.Request, content io.ReadSeeker) {
	// Note that the last modified time is a constant value because the data
	// in a piece identified by a cid will never change.

	if req.Method == http.MethodHead {
		// For an HTTP HEAD request ServeContent doesn't send any data (just headers)
		http.ServeContent(res, req, "", time.Time{}, nil)
		return
	}

	// Send the content
	http.ServeContent(res, req, "", lastModified, content)
}
