package retrieval

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	commcid "github.com/filecoin-project/go-fil-commcid"

	"github.com/filecoin-project/curio/lib/cachedreader"
	"github.com/filecoin-project/curio/lib/storiface"
)

type sniffReaderAt struct {
	data      []byte
	lastSize  int
	readCount int
}

func (s *sniffReaderAt) ReadAt(p []byte, off int64) (int, error) {
	s.readCount++
	s.lastSize = len(p)
	if off >= int64(len(s.data)) {
		return 0, io.EOF
	}
	n := copy(p, s.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestSniffContentTypeReadsExactWindow(t *testing.T) {
	payload := append([]byte("%PDF-1.4"), bytes.Repeat([]byte("x"), 4096)...)
	r := &sniffReaderAt{data: payload}

	ct, err := sniffContentType(r)
	require.NoError(t, err)
	require.Equal(t, "application/pdf", ct)
	require.Equal(t, 1, r.readCount)
	require.Equal(t, int(MIME_SNIFF_BYTES), r.lastSize)
}

func TestSniffContentTypeShortPiece(t *testing.T) {
	r := &sniffReaderAt{data: []byte("hello")}
	ct, err := sniffContentType(r)
	require.NoError(t, err)
	require.Equal(t, "text/plain; charset=utf-8", ct)
	require.Equal(t, int(MIME_SNIFF_BYTES), r.lastSize)
}

func TestServeContentHeadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/piece/test", nil)
	w := httptest.NewRecorder()

	http.ServeContent(w, req, "", lastModified, bytes.NewReader([]byte("0123456789")))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "bytes", res.Header.Get("Accept-Ranges"))
	require.Equal(t, "10", res.Header.Get("Content-Length"))
	require.Equal(t, lastModified.UTC().Format(http.TimeFormat), res.Header.Get("Last-Modified"))
	require.Empty(t, body)
}

func TestServeContentRangeRequestReturnsPartial(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/piece/test", nil)
	req.Header.Set("Range", "bytes=2-4")
	w := httptest.NewRecorder()

	http.ServeContent(w, req, "", lastModified, bytes.NewReader([]byte("0123456789")))

	res := w.Result()
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusPartialContent, res.StatusCode)
	require.Equal(t, "bytes 2-4/10", res.Header.Get("Content-Range"))
	require.Equal(t, "3", res.Header.Get("Content-Length"))
	require.Equal(t, []byte("234"), body)
}

func TestSetHeadersWritesQuotedETag(t *testing.T) {
	pieceCid, err := cid.Parse("bafkqaaa")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	setHeaders(w, pieceCid, "video/mp4")

	require.Equal(t, `"`+pieceCid.String()+`"`, w.Header().Get("ETag"))
}

type byteReader struct {
	*bytes.Reader
}

func (byteReader) Close() error { return nil }

type aclFiles struct {
	files map[cid.Cid][]byte
}

func (a *aclFiles) HasPiece(_ context.Context, pc cid.Cid) (bool, error) {
	_, ok := a.files[pc]
	return ok, nil
}

func (a *aclFiles) ReadPiece(_ context.Context, pc cid.Cid, _ int64) (storiface.Reader, error) {
	data, ok := a.files[pc]
	if !ok {
		return nil, nil
	}
	return byteReader{bytes.NewReader(data)}, nil
}

func TestHandleDownload(t *testing.T) {
	payload := bytes.Repeat([]byte("hello acl piece"), 10)
	v1, err := cid.Decode("baga6ea4seaqomqafu276g53zko4k23xzh4h4uecjwicbmvhsuqi7o4bhthhm4aq")
	require.NoError(t, err)
	v2, err := commcid.PieceCidV2FromV1(v1, uint64(len(payload)))
	require.NoError(t, err)

	cpr := &cachedreader.CachedPieceReader{}
	cpr.SetACLPieceReader(&aclFiles{files: map[cid.Cid][]byte{v2: payload}})
	mux := chi.NewMux()
	Router(mux, &Provider{cpr: cpr}, testDenyFilter())
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/download/" + v2.String())
	require.NoError(t, err)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)
	require.Equal(t, "FOC-Grant", res.Header.Get("WWW-Authenticate"))
	require.Empty(t, body)

	head, err := http.Head(srv.URL + "/download/" + v2.String())
	require.NoError(t, err)
	require.NoError(t, head.Body.Close())
	require.Equal(t, http.StatusUnauthorized, head.StatusCode)
	require.Equal(t, "FOC-Grant", head.Header.Get("WWW-Authenticate"))

	miss, err := http.Get(srv.URL + "/download/" + v1.String())
	require.NoError(t, err)
	require.NoError(t, miss.Body.Close())
	require.Equal(t, http.StatusNotFound, miss.StatusCode)
}
