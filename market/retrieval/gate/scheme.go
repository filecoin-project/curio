package gate

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"golang.org/x/xerrors"
)

// Credential presentation (see RETRIEVAL-AUTH-SPEC.md §6):
//   Authorization: CurioRetrieval <token>      (SDK / server / browser fetch / headless agent)
//   ?auth=<token>                              (header-less browser tags)
// where <token> = base64url(JSON) carrying a fresh proof and (for delegated access) the voucher.
// uint256 fields are decimal STRINGS; addresses/signatures are 0x-hex; resource is the CID string.
const (
	AuthHeaderScheme = "CurioRetrieval"
	AuthQueryParam   = "auth"
	SchemeEIP712     = "eip712"
)

type wireVoucher struct {
	Grantee  string `json:"grantee"`
	Scope    string `json:"scope"`
	IssuedAt string `json:"issuedAt"`
	Deadline string `json:"deadline"`
}

type wireProof struct {
	Scope    string `json:"scope"`
	Resource string `json:"resource"`
	Deadline string `json:"deadline"`
}

type wireCredential struct {
	Scheme     string       `json:"scheme"`
	Proof      wireProof    `json:"proof"`
	ProofSig   string       `json:"proofSig"`
	Voucher    *wireVoucher `json:"voucher,omitempty"`    // omitted for owner-direct access
	VoucherSig string       `json:"voucherSig,omitempty"` // "" for owner-direct access
}

// Credential is a parsed, not-yet-verified retrieval credential.
type Credential struct {
	Scheme     string
	Proof      RetrievalProof
	ProofSig   []byte
	Voucher    *RetrievalVoucher // nil ⇒ owner-direct (the proof signer must itself be the owner)
	VoucherSig []byte
}

func atoiU(s string) (uint64, error) { return strconv.ParseUint(s, 10, 64) }
func atoiI(s string) (int64, error)  { return strconv.ParseInt(s, 10, 64) }

// ParseCredential decodes a base64url(JSON) token (signatures not yet checked).
func ParseCredential(token string) (*Credential, error) {
	token = strings.TrimSpace(token)
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		if raw, err = base64.URLEncoding.DecodeString(token); err != nil {
			return nil, xerrors.Errorf("decode credential token: %w", err)
		}
	}
	var wc wireCredential
	if err := json.Unmarshal(raw, &wc); err != nil {
		return nil, xerrors.Errorf("unmarshal credential: %w", err)
	}

	pScope, err := atoiU(wc.Proof.Scope)
	if err != nil {
		return nil, xerrors.Errorf("proof scope: %w", err)
	}
	pDeadline, err := atoiI(wc.Proof.Deadline)
	if err != nil {
		return nil, xerrors.Errorf("proof deadline: %w", err)
	}
	proofSig, err := hexutil.Decode(wc.ProofSig)
	if err != nil {
		return nil, xerrors.Errorf("decode proof signature: %w", err)
	}
	cred := &Credential{
		Scheme:   wc.Scheme,
		Proof:    RetrievalProof{Scope: pScope, Resource: wc.Proof.Resource, Deadline: pDeadline},
		ProofSig: proofSig,
	}

	if wc.Voucher != nil {
		if !common.IsHexAddress(wc.Voucher.Grantee) {
			return nil, xerrors.New("invalid grantee address")
		}
		vScope, err := atoiU(wc.Voucher.Scope)
		if err != nil {
			return nil, xerrors.Errorf("voucher scope: %w", err)
		}
		vIssuedAt, err := atoiI(wc.Voucher.IssuedAt)
		if err != nil {
			return nil, xerrors.Errorf("voucher issuedAt: %w", err)
		}
		vDeadline, err := atoiI(wc.Voucher.Deadline)
		if err != nil {
			return nil, xerrors.Errorf("voucher deadline: %w", err)
		}
		vs, err := hexutil.Decode(wc.VoucherSig)
		if err != nil {
			return nil, xerrors.Errorf("decode voucher signature: %w", err)
		}
		cred.Voucher = &RetrievalVoucher{
			Grantee:  common.HexToAddress(wc.Voucher.Grantee),
			Scope:    vScope,
			IssuedAt: vIssuedAt,
			Deadline: vDeadline,
		}
		cred.VoucherSig = vs
	}
	return cred, nil
}

// EncodeCredential builds the base64url token a client presents (reference client / tests).
func EncodeCredential(scheme string, p RetrievalProof, proofSig []byte, v *RetrievalVoucher, voucherSig []byte) string {
	wc := wireCredential{
		Scheme: scheme,
		Proof: wireProof{
			Scope:    strconv.FormatUint(p.Scope, 10),
			Resource: p.Resource,
			Deadline: strconv.FormatInt(p.Deadline, 10),
		},
		ProofSig: hexutil.Encode(proofSig),
	}
	if v != nil {
		wc.Voucher = &wireVoucher{
			Grantee:  v.Grantee.Hex(),
			Scope:    strconv.FormatUint(v.Scope, 10),
			IssuedAt: strconv.FormatInt(v.IssuedAt, 10),
			Deadline: strconv.FormatInt(v.Deadline, 10),
		}
		wc.VoucherSig = hexutil.Encode(voucherSig)
	}
	b, _ := json.Marshal(wc)
	return base64.RawURLEncoding.EncodeToString(b)
}
