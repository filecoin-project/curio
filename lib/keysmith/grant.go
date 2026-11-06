package keysmith

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"golang.org/x/text/unicode/norm"
)

const (
	maxGrantChainLength = 8
	maxRequestLifetime  = int64(300)
	clockSkew           = int64(60)

	domainName    = "FOC Encryption"
	domainVersion = "1"

	// blockChainField is the EIP-712 name of the chain id.
	blockChainField = "chainId"
)

// PieceLabels are the foc fields a provider read from a piece envelope.
// An empty Owner means foc/own was absent, so the piece is not gated.
// A nil Scope means foc/scope was absent.
type PieceLabels struct {
	Owner    string
	Keyspace string
	Scope    *string
	Salt     string
}

// VerifiedClaim is the final link of a presentation that verified.
type VerifiedClaim struct {
	Owner    common.Address
	Node     string
	Bearer   common.Address
	keyspace []byte
}

type claimLink struct {
	node          string
	bearer        common.Address
	maxDepth      int
	keyCommitment []byte
	signature     []byte
}

type grantChain struct {
	owner              common.Address
	keyspace           []byte
	blockChain         *big.Int
	keyspaceCommitment []byte
	expires            int64
	links              []claimLink
}

type readRequest struct {
	pieceCID  []byte
	expires   int64
	signature []byte
}

// refusal is a presentation Keysmith refuses. Error is the reference reason.
// Unwrap is ErrInvalidGrant, which the download handler maps to 403.
type refusal struct{ reason string }

func (r *refusal) Error() string { return r.reason }

func (r *refusal) Unwrap() error { return ErrInvalidGrant }

// refuse formats a reference reason. The caller has already decided the presentation is refused.
func refuse(format string, args ...any) error {
	return &refusal{reason: fmt.Sprintf(format, args...)}
}

// jsSpace is the regexp class of characters JavaScript's trim() removes. U+0085 is excluded; U+FEFF is included.
const jsSpace = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var (
	secpN     = crypto.S256().Params().N
	secpHalfN = new(big.Int).Rsh(crypto.S256().Params().N, 1)

	saltPattern     = regexp.MustCompile(`^0x(?:[0-9a-f]{2})+$`)
	keyspacePattern = regexp.MustCompile(`^0x[0-9a-f]{32}$`)
	// jsEdgeSpace matches a leading or trailing run of jsSpace.
	jsEdgeSpace = regexp.MustCompile(`^[` + jsSpace + `]+|[` + jsSpace + `]+$`)
)

var eip712Domain = []apitypes.Type{
	{Name: "name", Type: "string"},
	{Name: "version", Type: "string"},
}

var readGrantTypes = apitypes.Types{
	"EIP712Domain": eip712Domain,
	"ReadGrant": {
		{Name: "v", Type: "uint8"},
		{Name: "node", Type: "string"},
		{Name: "owner", Type: "address"},
		{Name: "keyspace", Type: "bytes16"},
		{Name: "bearer", Type: "address"},
		{Name: "maxDepth", Type: "uint8"},
		{Name: blockChainField, Type: "uint256"},
		{Name: "keyCommitment", Type: "bytes16"},
		{Name: "keyspaceCommitment", Type: "bytes16"},
		{Name: "expires", Type: "uint64"},
	},
}

var readRequestTypes = apitypes.Types{
	"EIP712Domain": eip712Domain,
	"ReadRequest": {
		{Name: blockChainField, Type: "uint256"},
		{Name: "pieceCid", Type: "bytes"},
		{Name: "expires", Type: "uint64"},
	},
}

// VerifyPresentation checks a Keysmith FOC-Grant presentation.
// pieceCID is the binary CID of the piece being served. blockChain is this
// network. now is the provider clock. The checks match verifyPresentation
// in @filoz/keysmith: a piece with no foc/own is not gated.
func VerifyPresentation(presentation, pieceCID []byte, piece PieceLabels, blockChain *big.Int, now time.Time) (VerifiedClaim, error) {
	if blockChain == nil {
		return VerifiedClaim{}, fmt.Errorf("%w: blockchain is not configured", ErrInvalidGrant)
	}
	grant, req, err := decodePresentation(presentation)
	if err != nil {
		return VerifiedClaim{}, err
	}
	claim, err := verifyGrantChain(grant, &piece, now)
	if err != nil {
		return VerifiedClaim{}, err
	}
	if grant.blockChain.Cmp(blockChain) != 0 {
		return VerifiedClaim{}, refuse("The chain names another network")
	}
	if !bytes.Equal(req.pieceCID, pieceCID) {
		return VerifiedClaim{}, refuse("The request names another piece")
	}
	nowSec := now.Unix()
	if req.expires < nowSec-clockSkew || req.expires > nowSec+maxRequestLifetime+clockSkew {
		return VerifiedClaim{}, refuse("The request has expired, or is valid for too long")
	}
	signer, err := recoverRequest(grant.blockChain, req)
	if err != nil {
		return VerifiedClaim{}, err
	}
	if signer != claim.Bearer {
		return VerifiedClaim{}, refuse("The request is not signed by the final bearer")
	}
	if piece.Owner != "" {
		if !strings.EqualFold(piece.Owner, claim.Owner.Hex()) {
			return VerifiedClaim{}, refuse("The piece belongs to another owner or keyspace")
		}
		keyspace, err := keyspaceID(piece.Keyspace)
		if err != nil {
			return VerifiedClaim{}, err
		}
		if keyspace != "0x"+hex.EncodeToString(claim.keyspace) {
			return VerifiedClaim{}, refuse("The piece belongs to another owner or keyspace")
		}
		ok, err := reaches(claim.Node, piece)
		if err != nil {
			return VerifiedClaim{}, err
		}
		if !ok {
			scope := "(none)"
			if piece.Scope != nil {
				scope = *piece.Scope
			}
			return VerifiedClaim{}, refuse("%s does not reach this piece (scope %s)", claim.Node, scope)
		}
	}
	return claim, nil
}

// verifyGrantChain checks signatures, expiry, and scope narrowing. piece is
// required when a link names one piece; a nil piece means the caller has no labels.
func verifyGrantChain(grant grantChain, piece *PieceLabels, now time.Time) (VerifiedClaim, error) {
	links := grant.links
	if len(links) < 1 || len(links) > maxGrantChainLength {
		return VerifiedClaim{}, refuse("A chain has 1 to %d links", maxGrantChainLength)
	}
	if grant.expires < now.Unix()-clockSkew {
		when := time.Unix(grant.expires, 0).UTC().Format("2006-01-02T15:04:05.000Z")
		return VerifiedClaim{}, refuse("The chain expired at %s", when)
	}
	root := links[0]
	if root.node == "keyspace" && !bytes.Equal(root.keyCommitment, grant.keyspaceCommitment) {
		return VerifiedClaim{}, refuse("A keyspace root must carry the keyspace commitment")
	}
	signer, err := recoverLink(grant, root)
	if err != nil {
		return VerifiedClaim{}, err
	}
	if signer != grant.owner {
		return VerifiedClaim{}, refuse("The root is not signed by the owner")
	}
	for i := 1; i < len(links); i++ {
		signer, err = recoverLink(grant, links[i])
		if err != nil {
			return VerifiedClaim{}, err
		}
		if signer != links[i-1].bearer {
			return VerifiedClaim{}, refuse("Link %d is not signed by link %d's bearer", i, i-1)
		}
		if err := checkStep(links[i-1], links[i], piece, i); err != nil {
			return VerifiedClaim{}, err
		}
	}
	last := links[len(links)-1]
	return VerifiedClaim{Owner: grant.owner, Node: last.node, Bearer: last.bearer, keyspace: grant.keyspace}, nil
}

// checkStep requires the child to be no wider than its parent and within the parent's depth.
// piece may be nil; a piece node then fails. index is the child's position in the chain.
func checkStep(parent, child claimLink, piece *PieceLabels, index int) error {
	if parent.maxDepth < 1 || child.maxDepth > parent.maxDepth-1 {
		return refuse("Link %d exceeds link %d's maxDepth", index, index-1)
	}
	var narrower bool
	switch {
	case parent.node == "keyspace":
		narrower = true
	case strings.HasPrefix(child.node, "piece:"):
		if piece == nil {
			return refuse("Link %d names a piece: checking it needs the piece's labels", index)
		}
		named, err := pieceNodeOf(*piece)
		if err != nil {
			return err
		}
		covered, err := reaches(parent.node, *piece)
		if err != nil {
			return err
		}
		narrower = child.node == named && covered
	default:
		narrower = !strings.HasPrefix(parent.node, "piece:") && child.node != "keyspace" && within(parent.node, child.node)
	}
	if !narrower {
		return refuse("Link %d widens %s to %s", index, parent.node, child.node)
	}
	return nil
}

// within reports whether child is parent or a path under it. Both are already canonical nodes.
func within(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// reaches reports whether node covers the piece these labels describe.
// The caller passes a canonical node. Scope and salt labels are canonicalised here.
func reaches(node string, piece PieceLabels) (bool, error) {
	if node == "keyspace" {
		return true, nil
	}
	if strings.HasPrefix(node, "piece:") {
		named, err := pieceNodeOf(piece)
		if err != nil {
			return false, err
		}
		return node == named, nil
	}
	if piece.Scope == nil {
		return false, nil
	}
	path, err := scopePath(*piece.Scope)
	if err != nil {
		return false, err
	}
	return within(node, "scope:"+path), nil
}

// pieceNodeOf is the canonical piece node for these labels. The caller has a salt string.
func pieceNodeOf(piece PieceLabels) (string, error) {
	return canonicalNode("piece:" + piece.Salt)
}

// canonicalNode accepts only the form Keysmith signs: keyspace, scope:<path>, or piece:<salt>.
// A node that is not already canonical returns a different string, or an error when it cannot be one.
func canonicalNode(node string) (string, error) {
	if node == "keyspace" {
		return "keyspace", nil
	}
	if rest, ok := strings.CutPrefix(node, "scope:"); ok {
		path, err := scopePath(rest)
		if err != nil {
			return "", err
		}
		return "scope:" + path, nil
	}
	if rest, ok := strings.CutPrefix(node, "piece:"); ok {
		salt, err := saltOf(rest)
		if err != nil {
			return "", err
		}
		if len(salt) != 2+32 {
			return "", refuse("A piece node names a 16-byte salt, got %s", jsonQuote(node))
		}
		return "piece:" + salt, nil
	}
	return "", refuse("Unrecognised grant node: %s", node)
}

// scopePath canonicalises a scope path: NFC names joined by "/". The caller passes the text after "scope:".
func scopePath(path string) (string, error) {
	parts := strings.Split(path, "/")
	out := make([]string, len(parts))
	for i, part := range parts {
		name, err := scopeName(part)
		if err != nil {
			return "", err
		}
		out[i] = name
	}
	return strings.Join(out, "/"), nil
}

// scopeName canonicalises one scope segment. A segment that is empty, padded, or contains "/" is refused.
func scopeName(name string) (string, error) {
	normalised := norm.NFC.String(name)
	if normalised == "" || jsEdgeSpace.MatchString(normalised) || strings.Contains(normalised, "/") {
		return "", refuse("A scope name must be non-empty, with no leading or trailing whitespace and no \"/\", got %s", jsonQuote(name))
	}
	return normalised, nil
}

// saltOf returns salt in the lowercase hex form Keysmith compares. The caller passes the text after "piece:".
func saltOf(salt string) (string, error) {
	lower := strings.ToLower(salt)
	if !saltPattern.MatchString(lower) {
		return "", refuse("A piece salt is hex bytes, got %s", jsonQuote(salt))
	}
	return lower, nil
}

// keyspaceID returns a keyspace id as 16 lowercase hex bytes with a 0x prefix.
// The caller passes a foc/ks label.
func keyspaceID(keyspace string) (string, error) {
	lower := strings.ToLower(keyspace)
	if !keyspacePattern.MatchString(lower) {
		return "", refuse("A keyspace id is 16 bytes of hex, got %s", jsonQuote(keyspace))
	}
	return lower, nil
}

// jsonQuote is JSON.stringify of s, used in reference error text.
func jsonQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(b)
}

func recoverLink(grant grantChain, link claimLink) (common.Address, error) {
	sum, err := hashTyped("ReadGrant", readGrantTypes, apitypes.TypedDataMessage{
		"v":                  big.NewInt(1),
		"node":               link.node,
		"owner":              grant.owner.Hex(),
		"keyspace":           grant.keyspace,
		"bearer":             link.bearer.Hex(),
		"maxDepth":           big.NewInt(int64(link.maxDepth)),
		blockChainField:      grant.blockChain,
		"keyCommitment":      link.keyCommitment,
		"keyspaceCommitment": grant.keyspaceCommitment,
		"expires":            big.NewInt(grant.expires),
	})
	if err != nil {
		return common.Address{}, err
	}
	return recoverAddress(sum, link.signature)
}

func recoverRequest(blockChain *big.Int, req readRequest) (common.Address, error) {
	sum, err := hashTyped("ReadRequest", readRequestTypes, apitypes.TypedDataMessage{
		blockChainField: blockChain,
		"pieceCid":      req.pieceCID,
		"expires":       big.NewInt(req.expires),
	})
	if err != nil {
		return common.Address{}, err
	}
	return recoverAddress(sum, req.signature)
}

func hashTyped(primary string, types apitypes.Types, msg apitypes.TypedDataMessage) ([]byte, error) {
	sum, _, err := apitypes.TypedDataAndHash(apitypes.TypedData{
		Types:       types,
		PrimaryType: primary,
		Domain:      apitypes.TypedDataDomain{Name: domainName, Version: domainVersion},
		Message:     msg,
	})
	if err != nil {
		return nil, fmt.Errorf("hashing %s: %w", primary, err)
	}
	return sum, nil
}

// recoverAddress recovers the signer of an EIP-712 digest. sig is 65 bytes
// whose trailing byte is 27 or 28; 0 or 1 is accepted only after that check.
func recoverAddress(digest, sig []byte) (common.Address, error) {
	if len(sig) != 65 {
		return common.Address{}, fmt.Errorf("signature must be 65 bytes")
	}
	normed := make([]byte, 65)
	copy(normed, sig)
	if normed[64] >= 27 {
		normed[64] -= 27
	}
	if normed[64] > 1 {
		return common.Address{}, fmt.Errorf("signature recovery id is invalid")
	}
	pub, err := crypto.SigToPub(digest, normed)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pub), nil
}

// normalSignature returns sig in the one form Keysmith accepts: low s and v of 27 or 28.
// A signature that cannot be spelled that way yields bytes that do not equal sig.
func normalSignature(sig []byte) ([]byte, error) {
	if len(sig) != 65 {
		return nil, refuse("A signature is 65 bytes, got %d", len(sig))
	}
	v := sig[64]
	if v < 27 {
		v += 27
	}
	if v != 27 && v != 28 {
		return nil, refuse("A signature's v must be 27 or 28 (or 0 or 1), got %d", sig[64])
	}
	s := new(big.Int).SetBytes(sig[32:64])
	if s.Cmp(secpHalfN) > 0 {
		s = new(big.Int).Sub(secpN, s)
		if v == 27 {
			v = 28
		} else {
			v = 27
		}
	}
	if s.Sign() <= 0 || s.Cmp(secpN) >= 0 {
		return []byte{0}, nil
	}
	out := make([]byte, 65)
	copy(out[:32], sig[:32])
	sb := s.Bytes()
	copy(out[64-len(sb):64], sb)
	out[64] = v
	return out, nil
}

var rootOnly = map[int64]bool{2: true, 3: true, 6: true, 10: true, 11: true}

// decodePresentation reads a version-1 presentation. The bytes must be the
// core deterministic encoding of that value.
func decodePresentation(b []byte) (grantChain, readRequest, error) {
	value, err := canonicalCBOR(b)
	if err != nil {
		return grantChain{}, readRequest{}, refuse("Not a presentation: the bytes are not canonical CBOR")
	}
	items, ok := value.([]any)
	if !ok || len(items) != 3 || !isVersion1(items[0]) {
		return grantChain{}, readRequest{}, refuse("Not a version 1 presentation")
	}
	reqMap, ok := asMap(items[2])
	if !ok {
		return grantChain{}, readRequest{}, refuse("A request is a map")
	}
	for key := range reqMap {
		n, isNum := asInt(key)
		if !isNum || (n != 0 && n != 1 && n != 2) {
			return grantChain{}, readRequest{}, refuse("The request has an unknown field %s", cborKeyName(key))
		}
	}
	piece, ok := lookup(reqMap, 0)
	pieceCID, isBytes := piece.([]byte)
	if !ok || !isBytes {
		return grantChain{}, readRequest{}, refuse("A request needs a piece CID")
	}
	grant, err := decodeGrantChain(items[1])
	if err != nil {
		return grantChain{}, readRequest{}, err
	}
	expires, err := uintField(reqMap, 1, "The request's expiry")
	if err != nil {
		return grantChain{}, readRequest{}, err
	}
	sig, err := signatureField(reqMap, 2, "The request's signature")
	if err != nil {
		return grantChain{}, readRequest{}, err
	}
	return grant, readRequest{pieceCID: pieceCID, expires: int64(expires), signature: sig}, nil
}

func isVersion1(v any) bool {
	n, ok := asUint(v)
	return ok && n == 1
}

// decodeGrantChain reads the root-first link array. Root-only fields on a later link are rejected.
func decodeGrantChain(value any) (grantChain, error) {
	items, ok := value.([]any)
	if !ok || len(items) < 1 || len(items) > maxGrantChainLength {
		return grantChain{}, refuse("A chain is an array of 1 to %d links", maxGrantChainLength)
	}
	maps := make([]map[any]any, len(items))
	for i, item := range items {
		m, ok := asMap(item)
		if !ok {
			return grantChain{}, refuse("Link %d is not a map", i)
		}
		for key := range m {
			n, isNum := asInt(key)
			if !isNum || n < 0 || n > 11 {
				return grantChain{}, refuse("Link %d has an unknown field %s", i, cborKeyName(key))
			}
			if i > 0 && rootOnly[n] {
				return grantChain{}, refuse("Link %d repeats a root-only field", i)
			}
		}
		if !isVersion1(must(lookup(m, 0))) {
			return grantChain{}, refuse("Link %d has an unsupported version", i)
		}
		maps[i] = m
	}
	root := maps[0]
	owner, err := bytesField(root, 2, 20, "owner")
	if err != nil {
		return grantChain{}, err
	}
	keyspace, err := bytesField(root, 3, 16, "keyspace")
	if err != nil {
		return grantChain{}, err
	}
	blockChainNum, err := uintField(root, 6, "The root chain id")
	if err != nil {
		return grantChain{}, err
	}
	commitment, err := bytesField(root, 10, 16, "keyspaceCommitment")
	if err != nil {
		return grantChain{}, err
	}
	expires, err := uintField(root, 11, "The root expiry")
	if err != nil {
		return grantChain{}, err
	}
	links := make([]claimLink, len(maps))
	for i, m := range maps {
		node, ok := lookup(m, 1)
		text, isText := node.(string)
		if !ok || !isText {
			return grantChain{}, refuse("Link %d has a non-canonical node", i)
		}
		canon, err := canonicalNode(text)
		if err != nil {
			return grantChain{}, err
		}
		if canon != text {
			return grantChain{}, refuse("Link %d has a non-canonical node", i)
		}
		bearer, err := bytesField(m, 4, 20, fmt.Sprintf("Link %d's bearer", i))
		if err != nil {
			return grantChain{}, err
		}
		depth, err := uintField(m, 5, fmt.Sprintf("Link %d's maxDepth", i))
		if err != nil {
			return grantChain{}, err
		}
		if depth > 255 {
			return grantChain{}, refuse("maxDepth must be 0 to 255, got %d", depth)
		}
		keyCommitment, err := bytesField(m, 7, 16, fmt.Sprintf("Link %d's keyCommitment", i))
		if err != nil {
			return grantChain{}, err
		}
		sig, err := signatureField(m, 8, fmt.Sprintf("Link %d's signature", i))
		if err != nil {
			return grantChain{}, err
		}
		if raw, ok := lookup(m, 9); ok {
			if _, isBytes := raw.([]byte); !isBytes {
				return grantChain{}, refuse("Link %d's pieceCid must be bytes", i)
			}
		}
		links[i] = claimLink{
			node:          text,
			bearer:        common.BytesToAddress(bearer),
			maxDepth:      int(depth),
			keyCommitment: keyCommitment,
			signature:     sig,
		}
	}
	return grantChain{
		owner:              common.BytesToAddress(owner),
		keyspace:           keyspace,
		blockChain:         new(big.Int).SetUint64(blockChainNum),
		keyspaceCommitment: commitment,
		expires:            int64(expires),
		links:              links,
	}, nil
}

// cborKeyName is the text Keysmith puts after "unknown field". The caller passes a map key.
func cborKeyName(key any) string {
	switch k := key.(type) {
	case string:
		return k
	case uint64:
		return strconv.FormatUint(k, 10)
	case int64:
		return strconv.FormatInt(k, 10)
	case int:
		return strconv.Itoa(k)
	default:
		return fmt.Sprint(key)
	}
}

func must(v any, _ bool) any { return v }

func bytesField(m map[any]any, key int64, n int, what string) ([]byte, error) {
	value, ok := lookup(m, key)
	if !ok {
		return nil, refuse("%s must be %d bytes", what, n)
	}
	b, ok := value.([]byte)
	if !ok || len(b) != n {
		return nil, refuse("%s must be %d bytes", what, n)
	}
	return b, nil
}

// signatureField requires a 65-byte signature already in low-s form with v 27 or 28.
func signatureField(m map[any]any, key int64, what string) ([]byte, error) {
	sig, err := bytesField(m, key, 65, what)
	if err != nil {
		return nil, err
	}
	normed, err := normalSignature(sig)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(normed, sig) {
		return nil, refuse("%s must have a low s and v = 27 or 28", what)
	}
	return sig, nil
}

func uintField(m map[any]any, key int64, what string) (uint64, error) {
	value, ok := lookup(m, key)
	if !ok {
		return 0, refuse("%s must be an unsigned integer", what)
	}
	n, ok := asUint(value)
	if !ok || n > maxSafeInteger {
		return 0, refuse("%s must be an unsigned integer", what)
	}
	return n, nil
}
