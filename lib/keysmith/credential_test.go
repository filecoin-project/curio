package keysmith

import (
	"encoding/base64"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialUsesLongestPresentSource(t *testing.T) {
	short := base64.RawURLEncoding.EncodeToString([]byte("short-grant"))
	long := base64.RawURLEncoding.EncodeToString([]byte("this-grant-is-longer"))

	req := httptest.NewRequest("GET", "/download/piece", nil)
	req.Header.Set("Authorization", "FOC-Grant "+short)
	req.Header.Set("Cookie", "FOC Grant="+long)
	got, err := Credential(req)
	require.NoError(t, err)
	require.Equal(t, []byte("this-grant-is-longer"), got)

	req = httptest.NewRequest("GET", "/download/piece", nil)
	req.SetBasicAuth("FOC%20Grant", long)
	got, err = Credential(req)
	require.NoError(t, err)
	require.Equal(t, []byte("this-grant-is-longer"), got)

	req = httptest.NewRequest("GET", "/download/piece", nil)
	req.SetBasicAuth("FOC Grant", short)
	req.Header.Set("Cookie", "FOC%20Grant="+long)
	got, err = Credential(req)
	require.NoError(t, err)
	require.Equal(t, []byte("this-grant-is-longer"), got)

	req = httptest.NewRequest("GET", "/download/piece", nil)
	_, err = Credential(req)
	require.ErrorIs(t, err, ErrNoGrant)
}
