package retrieval

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	"go.opencensus.io/stats"

	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/keysmith"
	"github.com/filecoin-project/curio/lib/pieceprovider"
	"github.com/filecoin-project/curio/market/retrieval/remoteblockstore"
)

// For data served by the endpoints in the HTTP server that never changes
// (eg pieces identified by a piece CID) send a cache header with a constant,
// non-zero last modified time.
var lastModified = time.UnixMilli(1)

// MIME_SNIFF_BYTES is the net/http / WHATWG sniff window. Aliased to
// pieceprovider.ExactReadAtMax so peeks are not amplified on ReadAt.
const MIME_SNIFF_BYTES = pieceprovider.ExactReadAtMax

func (rp *Provider) handleByPieceCid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()
	stats.Record(ctx, remoteblockstore.HttpPieceByCidRequestCount.M(1))

	pieceCidStr := chi.URLParam(r, "cid")
	if pieceCidStr == "" {
		log.Errorf("path '%s' is missing piece CID", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid400ResponseCount.M(1))
		return
	}
	log.Debugw("piece request received for CID", "cid", pieceCidStr)
	pieceCid, err := cid.Parse(pieceCidStr)
	if err != nil {
		log.Errorf("parsing piece CID '%s': %s", pieceCidStr, err.Error())
		w.WriteHeader(http.StatusBadRequest)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid400ResponseCount.M(1))
		return
	}

	// Get a reader over the piece
	reader, rawSize, err := rp.cpr.GetSharedPieceReader(ctx, pieceCid, true)
	if err != nil {
		log.Errorf("server error getting content for piece CID %s: %s", pieceCid, err)
		if errors.Is(err, cachedreader.ErrNoDeal) {
			w.WriteHeader(http.StatusNotFound)
			stats.Record(ctx, remoteblockstore.HttpPieceByCid404ResponseCount.M(1))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid500ResponseCount.M(1))
		return
	}
	defer func() {
		_ = reader.Close()
	}()

	// Exact MIME_SNIFF_BYTES ReadAt (no Seek). HEAD still sniffs for Content-Type;
	// ServeContent below writes no body for HEAD.
	contentType, err := sniffContentType(reader)
	if err != nil {
		log.Errorf("error sniffing content type for piece CID %s: %s", pieceCid, err)
		w.WriteHeader(http.StatusInternalServerError)
		stats.Record(ctx, remoteblockstore.HttpPieceByCid500ResponseCount.M(1))
		return
	}

	setHeaders(w, pieceCid, contentType)

	// Let's try to serve the original data without padding
	sr := io.NewSectionReader(reader, 0, int64(rawSize))

	// Note that the last modified time is a constant value because the data
	// in a piece identified by a cid will never change.
	http.ServeContent(w, r, "", lastModified, sr)

	stats.Record(ctx, remoteblockstore.HttpPieceByCid200ResponseCount.M(1))
	stats.Record(ctx, remoteblockstore.HttpPieceByCidRequestDuration.M(float64(time.Since(startTime).Milliseconds())))

}

// handleDownload serves a piece from acl-pieces. A missing piece is 404.
// The caller has already applied the denylist and the piece request limiter.
func (rp *Provider) handleDownload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prefixLen := len(downloadPrefix)
	if len(r.URL.Path) <= prefixLen {
		log.Errorf("path '%s' is missing piece CID", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	pieceCidStr := r.URL.Path[prefixLen:]
	pieceCid, err := cid.Parse(pieceCidStr)
	if err != nil {
		log.Errorf("parsing piece CID '%s': %s", pieceCidStr, err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if rp.cpr == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	reader, rawSize, err := rp.cpr.ReadACLPiece(ctx, pieceCid)
	if err != nil {
		log.Errorf("server error getting acl piece %s: %s", pieceCid, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if reader == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	defer func() {
		_ = reader.Close()
	}()

	if err := authorizeACLRead(r, reader, pieceCid, rp.blockChain); err != nil {
		if errors.Is(err, errBlockChainUnset) {
			log.Warnw("acl download refused: blockchain is not configured", "piece", pieceCid)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if errors.Is(err, keysmith.ErrNoGrant) {
			log.Debugw("acl download refused", "piece", pieceCid, "error", err)
			w.Header().Set("WWW-Authenticate", "FOC-Grant")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if errors.Is(err, keysmith.ErrInvalidGrant) {
			log.Debugw("acl download refused", "piece", pieceCid, "error", err)
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if errors.Is(err, keysmith.ErrInvalidEnvelope) {
			log.Debugw("acl download refused", "piece", pieceCid, "error", err)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		log.Errorw("acl download could not read the piece", "piece", pieceCid, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	contentType, err := sniffContentType(reader)
	if err != nil {
		log.Errorf("error sniffing content type for piece CID %s: %s", pieceCid, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	setHeaders(w, pieceCid, contentType)
	http.ServeContent(w, r, "", lastModified, io.NewSectionReader(reader, 0, int64(rawSize)))
}

var errBlockChainUnset = errors.New("blockchain is not configured")

// authorizeACLRead checks the longest FOC grant on r against piece.
// An ACL piece has a FEE envelope; the grant must reach that piece's labels.
// The caller has an open reader; the envelope is read from the front and the offset is left unchanged.
func authorizeACLRead(r *http.Request, reader io.ReaderAt, piece cid.Cid, blockChain *big.Int) error {
	raw, err := keysmith.Credential(r)
	if err != nil {
		return err
	}
	if blockChain == nil {
		return errBlockChainUnset
	}
	labels, err := keysmith.ReadEnvelopeLabelsAt(reader)
	if err != nil {
		return err
	}
	_, err = keysmith.VerifyPresentation(raw, piece.Bytes(), labels.Piece(), blockChain, time.Now())
	return err
}

func sniffContentType(r io.ReaderAt) (string, error) {
	buf := make([]byte, MIME_SNIFF_BYTES)
	n, err := r.ReadAt(buf, 0)
	if n == 0 && err != nil && err != io.EOF {
		return "", err
	}
	return http.DetectContentType(buf[:n]), nil
}

func setHeaders(w http.ResponseWriter, pieceCid cid.Cid, contentType string) {
	w.Header().Set("Vary", "Accept-Encoding")
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
	w.Header().Set("ETag", fmt.Sprintf(`"%s"`, pieceCid.String()))
}
