package gate

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"golang.org/x/xerrors"
)

// The converged Curio retrieval-authorization scheme (see RETRIEVAL-AUTH-SPEC.md). Two EIP-712
// objects under domain CurioRetrieval/1/chainId/verifyingContract, where verifyingContract is the
// owning service's contract (the FWSS service address for PDP data sets):
//
//   RetrievalVoucher — the CAPABILITY. The scope's owner (payer) signs it once, offline, to delegate
//                      access for a whole scope (a data set id here) to a grantee key.
//   RetrievalProof   — PROOF OF POSSESSION. The requester signs it fresh per request, binding the
//                      exact resource CID and a short deadline. Stateless replay protection.

const (
	eip712DomainName    = "CurioRetrieval"
	eip712DomainVersion = "1"
	primaryTypeVoucher  = "RetrievalVoucher"
	primaryTypeProof    = "RetrievalProof"
)

type RetrievalVoucher struct {
	Grantee  common.Address
	Scope    uint64 // data set id (PDP) / deal id (PoRep)
	IssuedAt int64
	Deadline int64
}

// RetrievalProof binds a single request: the resource CID exactly as it appears in the URL and a
// short deadline.
type RetrievalProof struct {
	Scope    uint64
	Resource string
	Deadline int64
}

var domainFields = []apitypes.Type{
	{Name: "name", Type: "string"},
	{Name: "version", Type: "string"},
	{Name: "chainId", Type: "uint256"},
	{Name: "verifyingContract", Type: "address"},
}

func domain(chainID *big.Int, verifyingContract common.Address) apitypes.TypedDataDomain {
	return apitypes.TypedDataDomain{
		Name:              eip712DomainName,
		Version:           eip712DomainVersion,
		ChainId:           (*math.HexOrDecimal256)(chainID),
		VerifyingContract: verifyingContract.Hex(),
	}
}

func eip712Digest(primaryType string, fields []apitypes.Type, msg apitypes.TypedDataMessage, chainID *big.Int, vc common.Address) ([]byte, error) {
	td := apitypes.TypedData{
		Types:       apitypes.Types{"EIP712Domain": domainFields, primaryType: fields},
		PrimaryType: primaryType,
		Domain:      domain(chainID, vc),
		Message:     msg,
	}
	ds, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return nil, xerrors.Errorf("hash EIP712Domain: %w", err)
	}
	sh, err := td.HashStruct(primaryType, msg)
	if err != nil {
		return nil, xerrors.Errorf("hash %s: %w", primaryType, err)
	}
	raw := append([]byte{0x19, 0x01}, ds...)
	return crypto.Keccak256(append(raw, sh...)), nil
}

func recover65(digest, sig []byte) (common.Address, error) {
	if len(sig) != 65 {
		return common.Address{}, xerrors.Errorf("signature must be 65 bytes, got %d", len(sig))
	}
	rsv := make([]byte, 65)
	copy(rsv, sig)
	if rsv[64] >= 27 {
		rsv[64] -= 27
	}
	pub, err := crypto.SigToPub(digest, rsv)
	if err != nil {
		return common.Address{}, xerrors.Errorf("ecrecover: %w", err)
	}
	return crypto.PubkeyToAddress(*pub), nil
}

func voucherFields() []apitypes.Type {
	return []apitypes.Type{
		{Name: "grantee", Type: "address"},
		{Name: "scope", Type: "uint256"},
		{Name: "issuedAt", Type: "uint256"},
		{Name: "deadline", Type: "uint256"},
	}
}

func voucherMsg(v RetrievalVoucher) apitypes.TypedDataMessage {
	return apitypes.TypedDataMessage{
		"grantee":  v.Grantee.Hex(),
		"scope":    new(big.Int).SetUint64(v.Scope).String(),
		"issuedAt": big.NewInt(v.IssuedAt).String(),
		"deadline": big.NewInt(v.Deadline).String(),
	}
}

func proofFields() []apitypes.Type {
	return []apitypes.Type{
		{Name: "scope", Type: "uint256"},
		{Name: "resource", Type: "string"},
		{Name: "deadline", Type: "uint256"},
	}
}

func proofMsg(p RetrievalProof) apitypes.TypedDataMessage {
	return apitypes.TypedDataMessage{
		"scope":    new(big.Int).SetUint64(p.Scope).String(),
		"resource": p.Resource,
		"deadline": big.NewInt(p.Deadline).String(),
	}
}

func VoucherDigest(v RetrievalVoucher, chainID *big.Int, vc common.Address) ([]byte, error) {
	return eip712Digest(primaryTypeVoucher, voucherFields(), voucherMsg(v), chainID, vc)
}
func ProofDigest(p RetrievalProof, chainID *big.Int, vc common.Address) ([]byte, error) {
	return eip712Digest(primaryTypeProof, proofFields(), proofMsg(p), chainID, vc)
}

// RecoverVoucherSigner recovers the address that signed the voucher (must equal the scope owner).
func RecoverVoucherSigner(v RetrievalVoucher, chainID *big.Int, vc common.Address, sig []byte) (common.Address, error) {
	d, err := VoucherDigest(v, chainID, vc)
	if err != nil {
		return common.Address{}, err
	}
	return recover65(d, sig)
}

// RecoverProofSigner recovers the address that signed the proof (the requester: owner or grantee).
func RecoverProofSigner(p RetrievalProof, chainID *big.Int, vc common.Address, sig []byte) (common.Address, error) {
	d, err := ProofDigest(p, chainID, vc)
	if err != nil {
		return common.Address{}, err
	}
	return recover65(d, sig)
}

func sign65(digest, priv []byte) ([]byte, error) {
	key, err := crypto.ToECDSA(priv)
	if err != nil {
		return nil, xerrors.Errorf("parse private key: %w", err)
	}
	sig, err := crypto.Sign(digest, key)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	sig[64] += 27
	return sig, nil
}

func SignVoucher(v RetrievalVoucher, chainID *big.Int, vc common.Address, priv []byte) ([]byte, error) {
	d, err := VoucherDigest(v, chainID, vc)
	if err != nil {
		return nil, err
	}
	return sign65(d, priv)
}
func SignProof(p RetrievalProof, chainID *big.Int, vc common.Address, priv []byte) ([]byte, error) {
	d, err := ProofDigest(p, chainID, vc)
	if err != nil {
		return nil, err
	}
	return sign65(d, priv)
}
