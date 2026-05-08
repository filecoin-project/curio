package helpers

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func WaitForHTTP(t *testing.T, baseURL string) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	require.Eventually(t, func() bool {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode == http.StatusOK
	}, 45*time.Second, 250*time.Millisecond)
}

func AssertPieceResponseHeaders(t *testing.T, headers http.Header, pieceCID string, expectedBodyLen int) {
	t.Helper()

	require.Equal(t, "Accept-Encoding", headers.Get("Vary"))
	require.Equal(t, "public, max-age=29030400, immutable", headers.Get("Cache-Control"))
	etag := headers.Get("Etag")
	require.NotEmpty(t, etag)
	require.True(t, etag == pieceCID || etag == fmt.Sprintf(`"%s"`, pieceCID), "unexpected ETag header %q", etag)
	require.Equal(t, strconv.Itoa(expectedBodyLen), headers.Get("Content-Length"))
	require.NotEmpty(t, headers.Get("Content-Type"))
	require.Equal(t, "bytes", headers.Get("Accept-Ranges"))
}

func AssertIPFSCarResponseHeaders(t *testing.T, headers http.Header) {
	require.Contains(t, headers.Get("Content-Type"), "application/vnd.ipld.car")
	require.Contains(t, strings.ToLower(headers.Get("Content-Disposition")), "attachment")
	require.NotEmpty(t, headers.Get("Etag"))
}

func HTTPGet(t *testing.T, baseURL, path string, headers map[string]string) (int, []byte) {
	t.Helper()

	status, body, _ := HTTPGetWithHeaders(t, baseURL, path, headers)
	return status, body
}

func HTTPGetWithHeaders(t *testing.T, baseURL, path string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()

	return HTTPRequestWithHeaders(t, http.MethodGet, baseURL, path, headers)
}

func HTTPRequestWithHeaders(t *testing.T, method, baseURL, path string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()

	req, err := http.NewRequest(method, baseURL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Accept-Encoding", "identity")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body, resp.Header.Clone()
}
