package gate

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestCredentialRoundTrip(t *testing.T) {
	ownerKey, _ := crypto.GenerateKey()
	granteeKey, _ := crypto.GenerateKey()
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	grantee := crypto.PubkeyToAddress(granteeKey.PublicKey)
	chainID := big.NewInt(314159)
	vc := common.HexToAddress("0x00000000000000000000000000000000000000AA")

	p := RetrievalProof{Scope: 9, Resource: "bafkzcibxyz", Deadline: 1234}
	psig, err := SignProof(p, chainID, vc, crypto.FromECDSA(granteeKey))
	require.NoError(t, err)
	v := RetrievalVoucher{Grantee: grantee, Scope: 9, IssuedAt: 100, Deadline: 200}
	vsig, err := SignVoucher(v, chainID, vc, crypto.FromECDSA(ownerKey))
	require.NoError(t, err)

	// delegated (voucher present)
	tok := EncodeCredential(SchemeEIP712, p, psig, &v, vsig)
	cred, err := ParseCredential(tok)
	require.NoError(t, err)
	require.Equal(t, SchemeEIP712, cred.Scheme)
	require.Equal(t, p, cred.Proof)
	require.NotNil(t, cred.Voucher)
	require.Equal(t, v, *cred.Voucher)
	rp, _ := RecoverProofSigner(cred.Proof, chainID, vc, cred.ProofSig)
	require.Equal(t, grantee, rp)
	ri, _ := RecoverVoucherSigner(*cred.Voucher, chainID, vc, cred.VoucherSig)
	require.Equal(t, owner, ri)

	// owner-direct (no voucher)
	cred2, err := ParseCredential(EncodeCredential(SchemeEIP712, p, psig, nil, nil))
	require.NoError(t, err)
	require.Nil(t, cred2.Voucher)
}

func TestParseCredentialRejectsGarbage(t *testing.T) {
	_, err := ParseCredential("not-base64!!")
	require.Error(t, err)
}

func TestExtractToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/piece/bafy", nil)
	r.Header.Set("Authorization", AuthHeaderScheme+" abc123")
	tok, ok := extractToken(r)
	require.True(t, ok)
	require.Equal(t, "abc123", tok)

	r2 := httptest.NewRequest(http.MethodGet, "/piece/bafy?"+AuthQueryParam+"=xyz789", nil)
	tok2, ok := extractToken(r2)
	require.True(t, ok)
	require.Equal(t, "xyz789", tok2)

	r3 := httptest.NewRequest(http.MethodGet, "/piece/bafy?"+AuthQueryParam+"=fromquery", nil)
	r3.Header.Set("Authorization", AuthHeaderScheme+" fromheader")
	tok3, _ := extractToken(r3)
	require.Equal(t, "fromheader", tok3)

	r5 := httptest.NewRequest(http.MethodGet, "/piece/bafy", nil)
	r5.Header.Set("Authorization", "Bearer sometoken")
	_, ok5 := extractToken(r5)
	require.False(t, ok5)
}
