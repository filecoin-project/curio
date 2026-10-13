package gate

import (
	"context"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"

	"golang.org/x/xerrors"
)

var log = logging.Logger("retrieval-gate")

// maxProofTTL bounds how far in the future a proof's expiry may be, so a captured proof is only
// replayable for a short window (stateless replay protection — no server-side nonce store).
const maxProofTTL = 5 * time.Minute

type ctxKey int

const gatedKey ctxKey = iota

// IsGated reports whether the current request passed retrieval-ACL gating, so the piece handler can
// emit a private (non-shared-cacheable) Cache-Control instead of the public/immutable default.
func IsGated(ctx context.Context) bool {
	v, _ := ctx.Value(gatedKey).(bool)
	return v
}

// Backend supplies the data-set facts the gate needs. The production implementation (*Resolver)
// reads them from the DB and the FWSS view on-chain; tests inject a fake. Keeping this an interface
// is what lets the middleware, handler, reader and HTTP server be integration-tested without a
// deployed FWSS contract.
type Backend interface {
	// PieceDatasets returns the on-chain data set ids that contain the piece (may be empty).
	PieceDatasets(ctx context.Context, pieceCid cid.Cid) ([]uint64, error)
	// ContentDatasets returns the data set ids containing the piece(s) that hold a payload/IPLD CID
	// (via the index) — used for the /ipfs/ gateway. May be empty.
	ContentDatasets(ctx context.Context, contentCid cid.Cid) ([]uint64, error)
	// DatasetPrivate reports whether the data set opted into gated retrieval.
	DatasetPrivate(ctx context.Context, dataSetId uint64) (bool, error)
	// DatasetPayer returns the data set's (scope's) on-chain owner/payer.
	DatasetPayer(ctx context.Context, scope uint64) (common.Address, error)
	// ChainID returns the EVM chain id for the EIP-712 domain.
	ChainID(ctx context.Context) (*big.Int, error)
	// VerifyingContract returns the EIP-712 domain's verifyingContract (the owning service contract).
	VerifyingContract(ctx context.Context) (common.Address, error)
}

// Middleware enforces opt-in retrieval permissioning on content-addressed piece requests.
//
// Flow: resolve the data sets that contain the requested piece → if any is public, serve as today
// (public-wins). If all are private, require a valid, unexpired, dataset-scoped credential whose
// named data set contains the piece and whose signer is that data set's on-chain payer.
type Middleware struct {
	enabled bool
	prefix  string // path prefix stripped to obtain the CID (e.g. "/piece/", "/ipfs/")
	backend Backend
	// resolveDatasets maps the request's CID to candidate data set ids. For /piece/ the CID is a
	// piece CID (PieceDatasets); for /ipfs/ it is a payload CID resolved through the index
	// (ContentDatasets).
	resolveDatasets func(context.Context, cid.Cid) ([]uint64, error)
}

// NewMiddleware builds gating for the /piece/ endpoint (the CID is a piece CID). When enabled is
// false it is a pass-through (retrieval stays fully public).
func NewMiddleware(enabled bool, prefix string, backend Backend) *Middleware {
	return &Middleware{enabled: enabled, prefix: prefix, backend: backend, resolveDatasets: backend.PieceDatasets}
}

// NewContentMiddleware builds gating for the /ipfs/ gateway (the CID is a payload/IPLD CID, resolved
// to its piece(s) through the index). When enabled is false it is a pass-through.
func NewContentMiddleware(enabled bool, prefix string, backend Backend) *Middleware {
	return &Middleware{enabled: enabled, prefix: prefix, backend: backend, resolveDatasets: backend.ContentDatasets}
}

// Handler is the chi/net-http middleware.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.enabled {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()

		pieceCid, ok := m.parseCID(r)
		if !ok {
			next.ServeHTTP(w, r) // malformed path — let the handler produce its own 400
			return
		}

		datasets, err := m.resolveDatasets(ctx, pieceCid)
		if err != nil {
			log.Warnw("resolve datasets for piece", "cid", pieceCid, "err", err)
			http.Error(w, "retrieval authorization unavailable", http.StatusServiceUnavailable)
			return
		}
		if len(datasets) == 0 {
			next.ServeHTTP(w, r) // not a known PDP dataset piece (e.g. a market deal) — unchanged
			return
		}

		// public-wins: any public data set containing the piece makes it publicly retrievable.
		privateSets := make([]uint64, 0, len(datasets))
		for _, id := range datasets {
			priv, err := m.backend.DatasetPrivate(ctx, id)
			if err != nil {
				log.Warnw("dataset private check", "dataSetId", id, "err", err)
				http.Error(w, "retrieval authorization unavailable", http.StatusServiceUnavailable)
				return
			}
			if !priv {
				next.ServeHTTP(w, r)
				return
			}
			privateSets = append(privateSets, id)
		}

		// All containing data sets are private → require a credential.
		token, ok := extractToken(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "missing retrieval credential")
			return
		}
		cred, err := ParseCredential(token)
		if err != nil {
			writeErr(w, http.StatusForbidden, "invalid retrieval credential")
			return
		}
		chainID, err := m.backend.ChainID(ctx)
		if err != nil {
			log.Warnw("resolve chain id", "err", err)
			http.Error(w, "retrieval authorization unavailable", http.StatusServiceUnavailable)
			return
		}
		vc, err := m.backend.VerifyingContract(ctx)
		if err != nil {
			log.Warnw("resolve verifying contract", "err", err)
			http.Error(w, "retrieval authorization unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := m.authorize(ctx, cred, chainID, vc, privateSets, pieceCid.String()); err != nil {
			log.Debugw("retrieval denied", "cid", pieceCid, "err", err)
			writeErr(w, http.StatusForbidden, "not authorized to retrieve this piece")
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, gatedKey, true)))
	})
}

// authorize enforces capability + proof-of-possession. Every request carries a FRESH proof signed
// by the requester's key, bound to the exact resource CID; the grant token alone is never enough.
//
//	payer-direct : proof signer == the data set's on-chain payer.
//	delegated    : proof signer == grant.grantee AND the grant is payer-signed for the same data set.
func (m *Middleware) authorize(ctx context.Context, cred *Credential, chainID *big.Int, vc common.Address, privateSets []uint64, resource string) error {
	if cred.Scheme != SchemeEIP712 {
		return xerrors.Errorf("unsupported scheme %q", cred.Scheme)
	}
	now := time.Now().Unix()
	p := cred.Proof

	// The proof must bind THIS request: the exact resource CID and a near-future deadline.
	if p.Resource != resource {
		return xerrors.New("proof is not bound to the requested resource")
	}
	if p.Deadline <= now {
		return xerrors.New("proof expired")
	}
	if p.Deadline-now > int64(maxProofTTL.Seconds()) {
		return xerrors.New("proof deadline too far in the future")
	}
	// The proof's scope must be one of the (private) scopes containing the piece.
	if !containsUint64(privateSets, p.Scope) {
		return xerrors.New("proof scope does not contain this piece")
	}

	requester, err := RecoverProofSigner(p, chainID, vc, cred.ProofSig)
	if err != nil {
		return xerrors.Errorf("recover proof signer: %w", err)
	}
	owner, err := m.backend.DatasetPayer(ctx, p.Scope)
	if err != nil {
		return xerrors.Errorf("resolve owner: %w", err)
	}

	// Owner proving their own access — no voucher needed.
	if requester == owner {
		return nil
	}

	// Otherwise the requester must be a grantee the owner delegated to via a voucher.
	v := cred.Voucher
	if v == nil {
		return xerrors.New("proof signer is not the owner and no voucher was presented")
	}
	if v.Scope != p.Scope {
		return xerrors.New("voucher is for a different scope than the proof")
	}
	if v.Deadline <= now {
		return xerrors.New("voucher expired")
	}
	if v.Grantee != requester {
		return xerrors.New("proof was not signed by the voucher's grantee")
	}
	issuer, err := RecoverVoucherSigner(*v, chainID, vc, cred.VoucherSig)
	if err != nil {
		return xerrors.Errorf("recover voucher issuer: %w", err)
	}
	if issuer != owner {
		return xerrors.New("voucher was not signed by the scope owner")
	}
	return nil
}

func (m *Middleware) parseCID(r *http.Request) (cid.Cid, bool) {
	if len(r.URL.Path) <= len(m.prefix) {
		return cid.Undef, false
	}
	rest := r.URL.Path[len(m.prefix):]
	// The IPFS gateway path is /ipfs/{cid}[/sub/path]; take only the first segment.
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	c, err := cid.Parse(rest)
	if err != nil {
		return cid.Undef, false
	}
	return c, true
}

// extractToken returns the credential token from the Authorization header
// ("CurioRetrieval <token>") or the ?auth= query parameter, in that order.
func extractToken(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		if rest, ok := strings.CutPrefix(h, AuthHeaderScheme+" "); ok {
			if t := strings.TrimSpace(rest); t != "" {
				return t, true
			}
		}
	}
	if t := strings.TrimSpace(r.URL.Query().Get(AuthQueryParam)); t != "" {
		return t, true
	}
	return "", false
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, msg, code)
}

func containsUint64(s []uint64, v uint64) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
