package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestPprofOnAdminMux(t *testing.T) {
	mx := mux.NewRouter()
	registerPprof(mx)

	srv := httptest.NewServer(mx)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/debug/pprof/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "goroutine")

	resp, err = http.Get(srv.URL + "/debug/pprof")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(srv.URL + "/debug/pprof/goroutine?debug=1")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(body), "goroutine") || strings.Contains(string(body), "runtime."))
}
