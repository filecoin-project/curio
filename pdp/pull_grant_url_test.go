package pdp

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithFOCGrantUserinfoRoundTrip(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString([]byte("presentation"))
	raw, err := WithFOCGrantUserinfo("https://sp.example/download/cid", token)
	require.NoError(t, err)

	clean, user, err := SplitPullGrantURL(raw)
	require.NoError(t, err)
	require.Equal(t, "https://sp.example/download/cid", clean)
	require.NotNil(t, user)
	require.Equal(t, "FOC Grant", user.Username())
	pass, ok := user.Password()
	require.True(t, ok)
	require.Equal(t, token, pass)

	kept, err := WithFOCGrantUserinfo(raw, "other-token")
	require.NoError(t, err)
	require.Equal(t, raw, kept)

	require.NotContains(t, RedactPullSourceURL(raw), token)
	require.Contains(t, RedactPullSourceURL(raw), "FOC%20Grant@")
}

func TestPullGrantUserinfoIsBasicAuth(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString([]byte("presentation"))
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	raw, err := WithFOCGrantUserinfo(srv.URL+"/download/cid", token)
	require.NoError(t, err)
	clean, user, err := SplitPullGrantURL(raw)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, clean, nil)
	require.NoError(t, err)
	req.URL.User = user
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	require.Equal(t, "FOC Grant", gotUser)
	require.Equal(t, token, gotPass)
}

func TestValidatePullSourceURLGrantUserinfo(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString([]byte("presentation"))
	raw, err := WithFOCGrantUserinfo("https://sp.example/download/cid", token)
	require.NoError(t, err)
	require.NoError(t, ValidatePullSourceURL(raw))
	require.ErrorContains(t, ValidatePullSourceURL("https://alice:secret@sp.example/download/cid"), "not an FOC grant")
}
