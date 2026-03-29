package retrieval

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestServeContentHeadRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/piece/test", nil)
	w := httptest.NewRecorder()

	http.ServeContent(w, req, "", lastModified, bytes.NewReader([]byte("0123456789")))

	res := w.Result()
	defer res.Body.Close()

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

	http.ServeContent(w, req,"", lastModified, bytes.NewReader([]byte("0123456789")))

	res := w.Result()
	defer res.Body.Close()

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
