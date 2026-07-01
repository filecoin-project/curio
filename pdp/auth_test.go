package pdp

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHybridAuthMissingHeaderReturnsPublic(t *testing.T) {
	t.Parallel()

	auth := NewHybridAuth(nil)
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece", nil)

	service, err := auth.AuthService(req)
	require.NoError(t, err)
	require.Equal(t, "public", service)
}

func TestHybridAuthInvalidJWT(t *testing.T) {
	t.Parallel()

	auth := NewHybridAuth(nil)
	req := httptest.NewRequest(http.MethodPost, "/pdp/piece", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-jwt")

	_, err := auth.AuthService(req)
	require.Error(t, err)
}

func TestParsePieceUploadAuthHeader(t *testing.T) {
	t.Parallel()

	raw := "42.7.1700000000.0x1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234"
	auth, err := ParsePieceUploadAuthHeader(raw)
	require.Error(t, err)

	validSig := make([]byte, 65)
	for i := range validSig {
		validSig[i] = byte(i + 1)
	}
	raw = FormatPieceUploadAuthHeader(42, big.NewInt(7), big.NewInt(1700000000), validSig)
	auth, err = ParsePieceUploadAuthHeader(raw)
	require.NoError(t, err)
	require.Equal(t, uint64(42), auth.DataSetID)
	require.Equal(t, int64(7), auth.Nonce.Int64())
	require.Equal(t, uint64(1700000000), auth.Expiry)
	require.Len(t, auth.Signature, 65)
}
