package web

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/deps/config"
)

func TestGuiTLSConfigAlwaysPresent(t *testing.T) {
	cfg := config.DefaultCurioConfig()
	tc, err := guiTLSConfig(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, tc)
	require.NotNil(t, tc.GetCertificate)

	cert, err := tc.GetCertificate(&tls.ClientHelloInfo{ServerName: "localhost"})
	require.NoError(t, err)
	require.NotNil(t, cert)
	require.NotEmpty(t, cert.Certificate)
}

func TestGuiTLSConfigFallsBackWhenNoAutocert(t *testing.T) {
	cfg := config.DefaultCurioConfig()
	cfg.HTTP.Enable = false
	tc, err := guiTLSConfig(cfg, nil)
	require.NoError(t, err)

	cert, err := tc.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.test"})
	require.NoError(t, err)
	require.NotNil(t, cert)

	cert2, err := tc.GetCertificate(&tls.ClientHelloInfo{ServerName: ""})
	require.NoError(t, err)
	require.NotNil(t, cert2)
}

func TestSelfSignedGUICert(t *testing.T) {
	c1, err := selfSignedGUICert()
	require.NoError(t, err)
	c2, err := selfSignedGUICert()
	require.NoError(t, err)
	require.Equal(t, c1.Certificate[0], c2.Certificate[0])
}
