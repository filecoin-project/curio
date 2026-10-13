package gate

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	datasets map[string][]uint64
	content  map[string][]uint64
	private  map[uint64]bool
	payer    map[uint64]common.Address
	chainID  *big.Int
	vc       common.Address
}

func (f *fakeBackend) PieceDatasets(_ context.Context, c cid.Cid) ([]uint64, error)   { return f.datasets[c.String()], nil }
func (f *fakeBackend) ContentDatasets(_ context.Context, c cid.Cid) ([]uint64, error) { return f.content[c.String()], nil }
func (f *fakeBackend) DatasetPrivate(_ context.Context, id uint64) (bool, error)      { return f.private[id], nil }
func (f *fakeBackend) DatasetPayer(_ context.Context, id uint64) (common.Address, error) {
	return f.payer[id], nil
}
func (f *fakeBackend) ChainID(context.Context) (*big.Int, error)              { return f.chainID, nil }
func (f *fakeBackend) VerifyingContract(context.Context) (common.Address, error) { return f.vc, nil }

func pieceStub() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Gated", fmt.Sprintf("%v", IsGated(r.Context())))
		_, _ = w.Write([]byte("PIECEBYTES"))
	})
}

func mustCID(t *testing.T, s string) cid.Cid {
	t.Helper()
	c, err := cid.Parse(s)
	require.NoError(t, err)
	return c
}

func hashCID(t *testing.T, seed string) cid.Cid {
	t.Helper()
	h, err := multihash.Sum([]byte(seed), multihash.SHA2_256, -1)
	require.NoError(t, err)
	return cid.NewCidV1(cid.Raw, h)
}

func TestRetrievalGateHTTP(t *testing.T) {
	const chainID = 314159
	const privateSet, publicSet = uint64(10), uint64(20)

	payerKey, _ := crypto.GenerateKey()
	payerPriv, payerAddr := crypto.FromECDSA(payerKey), crypto.PubkeyToAddress(payerKey.PublicKey)
	delegKey, _ := crypto.GenerateKey()
	delegPriv, delegAddr := crypto.FromECDSA(delegKey), crypto.PubkeyToAddress(delegKey.PublicKey)
	otherKey, _ := crypto.GenerateKey()
	otherPriv := crypto.FromECDSA(otherKey)

	privCID := mustCID(t, "bafkzcibcmqcnolmx4nc6b24hrjpbwmvosz7p53bzudwomdquxxmwr2yjhtu52pi")
	pubCID := mustCID(t, "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
	unknownCID := mustCID(t, "bafybeihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku")
	privContentCID := hashCID(t, "priv-content")
	pubContentCID := hashCID(t, "pub-content")

	vc := common.HexToAddress("0x00000000000000000000000000000000000000AA")
	backend := &fakeBackend{
		datasets: map[string][]uint64{privCID.String(): {privateSet}, pubCID.String(): {privateSet, publicSet}},
		content:  map[string][]uint64{privContentCID.String(): {privateSet}, pubContentCID.String(): {privateSet, publicSet}},
		private:  map[uint64]bool{privateSet: true, publicSet: false},
		payer:    map[uint64]common.Address{privateSet: payerAddr, publicSet: payerAddr},
		chainID:  big.NewInt(chainID),
		vc:       vc,
	}

	now := time.Now().Unix()
	freshVoucher := func(grantee common.Address) *RetrievalVoucher {
		return &RetrievalVoucher{Grantee: grantee, Scope: privateSet, IssuedAt: now - 10, Deadline: now + 3600}
	}
	// mkCred: proof over (scope, resource, proofDeadline) signed by proofPriv; optional voucher by voucherPriv.
	mkCred := func(resource string, proofDeadline int64, proofPriv []byte, voucher *RetrievalVoucher, voucherPriv []byte) string {
		p := RetrievalProof{Scope: privateSet, Resource: resource, Deadline: proofDeadline}
		psig, err := SignProof(p, big.NewInt(chainID), vc, proofPriv)
		require.NoError(t, err)
		var vsig []byte
		if voucher != nil {
			vsig, err = SignVoucher(*voucher, big.NewInt(chainID), vc, voucherPriv)
			require.NoError(t, err)
		}
		return EncodeCredential(SchemeEIP712, p, psig, voucher, vsig)
	}
	hdr := func(tok string) map[string]string { return map[string]string{"Authorization": AuthHeaderScheme + " " + tok} }

	newServer := func(enabled bool) *httptest.Server {
		return httptest.NewServer(NewMiddleware(enabled, "/piece/", backend).Handler(pieceStub()))
	}
	do := func(t *testing.T, srv *httptest.Server, path string, h map[string]string) (int, string, http.Header) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		for k, v := range h {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	privPath := "/piece/" + privCID.String()

	t.Run("disabled gate passes through", func(t *testing.T) {
		s := newServer(false)
		defer s.Close()
		code, _, _ := do(t, s, privPath, nil)
		require.Equal(t, http.StatusOK, code)
	})

	srv := newServer(true)
	defer srv.Close()

	t.Run("unknown piece serves publicly", func(t *testing.T) {
		code, _, _ := do(t, srv, "/piece/"+unknownCID.String(), nil)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("public-wins serves without a credential", func(t *testing.T) {
		code, _, h := do(t, srv, "/piece/"+pubCID.String(), nil)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "false", h.Get("X-Gated"))
	})
	t.Run("no credential -> 401", func(t *testing.T) {
		code, _, _ := do(t, srv, privPath, nil)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	// --- proof of possession ---
	t.Run("payer-direct proof -> 200", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+120, payerPriv, nil, nil)
		code, body, h := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "PIECEBYTES", body)
		require.Equal(t, "true", h.Get("X-Gated"))
	})
	t.Run("delegated: payer grant + grantee proof -> 200", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+120, delegPriv, freshVoucher(delegAddr), payerPriv)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("payer-direct via ?auth= -> 200", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+120, payerPriv, nil, nil)
		code, _, _ := do(t, srv, privPath+"?"+AuthQueryParam+"="+tok, nil)
		require.Equal(t, http.StatusOK, code)
	})

	// --- the whole point: a token/grant WITHOUT a valid fresh proof is useless ---
	t.Run("grantee grant but proof by a THIEF (not the grantee) -> 403", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+120, otherPriv, freshVoucher(delegAddr), payerPriv)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusForbidden, code)
	})
	t.Run("delegate proof but NO grant -> 403", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+120, delegPriv, nil, nil)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusForbidden, code)
	})
	t.Run("proof bound to a DIFFERENT resource -> 403 (stolen for another piece)", func(t *testing.T) {
		tok := mkCred("bafkzcibwrongresource", now+120, payerPriv, nil, nil)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusForbidden, code)
	})
	t.Run("expired proof -> 403", func(t *testing.T) {
		tok := mkCred(privCID.String(), now-60, payerPriv, nil, nil)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusForbidden, code)
	})
	t.Run("proof expiry too far in the future -> 403", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+int64(maxProofTTL.Seconds())+120, payerPriv, nil, nil)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusForbidden, code)
	})
	t.Run("grant signed by a NON-payer -> 403", func(t *testing.T) {
		tok := mkCred(privCID.String(), now+120, delegPriv, freshVoucher(delegAddr), otherPriv)
		code, _, _ := do(t, srv, privPath, hdr(tok))
		require.Equal(t, http.StatusForbidden, code)
	})

	// --- /ipfs/ gateway (resource = the payload CID in the URL) ---
	csrv := httptest.NewServer(NewContentMiddleware(true, "/ipfs/", backend).Handler(pieceStub()))
	defer csrv.Close()
	t.Run("ipfs public-wins -> 200", func(t *testing.T) {
		code, _, _ := do(t, csrv, "/ipfs/"+pubContentCID.String(), nil)
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("ipfs private no cred -> 401", func(t *testing.T) {
		code, _, _ := do(t, csrv, "/ipfs/"+privContentCID.String(), nil)
		require.Equal(t, http.StatusUnauthorized, code)
	})
	t.Run("ipfs private payer proof -> 200", func(t *testing.T) {
		tok := mkCred(privContentCID.String(), now+120, payerPriv, nil, nil)
		code, _, _ := do(t, csrv, "/ipfs/"+privContentCID.String(), hdr(tok))
		require.Equal(t, http.StatusOK, code)
	})
	t.Run("ipfs subpath under private root gated; proof over root CID authorizes", func(t *testing.T) {
		code, _, _ := do(t, csrv, "/ipfs/"+privContentCID.String()+"/dir/file.png", nil)
		require.Equal(t, http.StatusUnauthorized, code)
		tok := mkCred(privContentCID.String(), now+120, payerPriv, nil, nil)
		code2, _, _ := do(t, csrv, "/ipfs/"+privContentCID.String()+"/dir/file.png", hdr(tok))
		require.Equal(t, http.StatusOK, code2)
	})
}
