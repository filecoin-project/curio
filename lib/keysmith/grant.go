package keysmith

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

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

	// blockChainField is the EIP-712 name of the blockchain id. Keysmith signs it as chainId.
	blockChainField = "chainId"
)

// PieceLabels are the foc fields a provider read from a piece envelope.
// A nil Scope means foc/scope was absent. An empty Owner means the piece is not gated by owner.
type PieceLabels struct {
	Owner    string
	Keyspace string
	Scope    *string
	Salt     string
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

type verifiedClaim struct {
	owner    common.Address
	node     string
	bearer   common.Address
	keyspace []byte
}

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
// in @filoz/keysmith.
func VerifyPresentation(presentation, pieceCID []byte, piece PieceLabels, blockChain *big.Int, now time.Time) error {
	if blockChain == nil {
		return fmt.Errorf("%w: blockchain is not configured", ErrInvalidGrant)
	}
	grant, req, err := decodePresentation(presentation)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGrant, err)
	}
	claim, err := verifyGrantChain(grant, &piece, now)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGrant, err)
	}
	if grant.blockChain.Cmp(blockChain) != 0 {
		return fmt.Errorf("%w: the grant names another blockchain", ErrInvalidGrant)
	}
	if !bytesEqual(req.pieceCID, pieceCID) {
		return fmt.Errorf("%w: the request names another piece", ErrInvalidGrant)
	}
	nowSec := now.Unix()
	if req.expires < nowSec-clockSkew || req.expires > nowSec+maxRequestLifetime+clockSkew {
		return fmt.Errorf("%w: the request has expired, or is valid for too long", ErrInvalidGrant)
	}
	signer, err := recoverRequest(grant.blockChain, req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidGrant, err)
	}
	if signer != claim.bearer {
		return fmt.Errorf("%w: the request is not signed by the final bearer", ErrInvalidGrant)
	}
	if piece.Owner != "" {
		if !strings.EqualFold(piece.Owner, claim.owner.Hex()) {
			return fmt.Errorf("%w: the piece belongs to another owner or keyspace", ErrInvalidGrant)
		}
		if piece.Keyspace != "0x"+hex.EncodeToString(claim.keyspace) {
			return fmt.Errorf("%w: the piece belongs to another owner or keyspace", ErrInvalidGrant)
		}
		if !reaches(claim.node, piece) {
			scope := "(none)"
			if piece.Scope != nil {
				scope = *piece.Scope
			}
			return fmt.Errorf("%w: %s does not reach this piece (scope %s)", ErrInvalidGrant, claim.node, scope)
		}
	}
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// verifyGrantChain checks signatures, expiry, and scope narrowing. piece is required
// when a link names one piece; a nil piece means the caller has no labels.
func verifyGrantChain(grant grantChain, piece *PieceLabels, now time.Time) (verifiedClaim, error) {
	links := grant.links
	if len(links) < 1 || len(links) > maxGrantChainLength {
		return verifiedClaim{}, fmt.Errorf("a grant chain has 1 to %d links", maxGrantChainLength)
	}
	if grant.expires < now.Unix()-clockSkew {
		return verifiedClaim{}, fmt.Errorf("the grant chain expired at %s", time.Unix(grant.expires, 0).UTC().Format(time.RFC3339))
	}
	root := links[0]
	if root.node == "keyspace" && !bytesEqual(root.keyCommitment, grant.keyspaceCommitment) {
		return verifiedClaim{}, fmt.Errorf("a keyspace root must carry the keyspace commitment")
	}
	signer, err := recoverLink(grant, root)
	if err != nil {
		return verifiedClaim{}, err
	}
	if signer != grant.owner {
		return verifiedClaim{}, fmt.Errorf("the root is not signed by the owner")
	}
	for i := 1; i < len(links); i++ {
		signer, err = recoverLink(grant, links[i])
		if err != nil {
			return verifiedClaim{}, err
		}
		if signer != links[i-1].bearer {
			return verifiedClaim{}, fmt.Errorf("link %d is not signed by link %d's bearer", i, i-1)
		}
		if err := checkStep(links[i-1], links[i], piece, i); err != nil {
			return verifiedClaim{}, err
		}
	}
	last := links[len(links)-1]
	return verifiedClaim{owner: grant.owner, node: last.node, bearer: last.bearer, keyspace: grant.keyspace}, nil
}

// checkStep requires the child to be no wider than its parent and within the parent's depth.
func checkStep(parent, child claimLink, piece *PieceLabels, index int) error {
	if parent.maxDepth < 1 || child.maxDepth > parent.maxDepth-1 {
		return fmt.Errorf("link %d exceeds link %d's maxDepth", index, index-1)
	}
	var narrower bool
	switch {
	case parent.node == "keyspace":
		narrower = true
	case strings.HasPrefix(child.node, "piece:"):
		if piece == nil {
			return fmt.Errorf("link %d names a piece: checking it needs the piece's labels", index)
		}
		narrower = child.node == "piece:"+piece.Salt && reaches(parent.node, *piece)
	default:
		narrower = !strings.HasPrefix(parent.node, "piece:") && child.node != "keyspace" && within(parent.node, child.node)
	}
	if !narrower {
		return fmt.Errorf("link %d widens %s to %s", index, parent.node, child.node)
	}
	return nil
}

func within(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// reaches reports whether node covers the piece these labels describe.
func reaches(node string, piece PieceLabels) bool {
	if node == "keyspace" {
		return true
	}
	if strings.HasPrefix(node, "piece:") {
		return node == "piece:"+piece.Salt
	}
	if piece.Scope == nil {
		return false
	}
	return within(node, "scope:"+*piece.Scope)
}

// canonicalNode accepts only the form Keysmith signs: keyspace, scope:<path>, or piece:<salt>.
func canonicalNode(node string) error {
	if node == "keyspace" {
		return nil
	}
	if rest, ok := strings.CutPrefix(node, "scope:"); ok {
		if rest == "" {
			return fmt.Errorf("link has a non-canonical node")
		}
		for _, part := range strings.Split(rest, "/") {
			if err := scopeName(part); err != nil {
				return fmt.Errorf("link has a non-canonical node")
			}
		}
		return nil
	}
	if rest, ok := strings.CutPrefix(node, "piece:"); ok {
		if err := saltText(rest); err != nil {
			return fmt.Errorf("link has a non-canonical node")
		}
		return nil
	}
	return fmt.Errorf("link has a non-canonical node")
}

// scopeName rejects a segment that is empty, padded, or not NFC.
func scopeName(name string) error {
	if name == "" || strings.TrimSpace(name) != name || strings.Contains(name, "/") || !utf8.ValidString(name) {
		return fmt.Errorf("bad scope")
	}
	if norm.NFC.String(name) != name {
		return fmt.Errorf("bad scope")
	}
	return nil
}

// saltText requires lowercase even-length hex with a 0x prefix, as Keysmith compares it.
func saltText(salt string) error {
	hexPart, ok := strings.CutPrefix(salt, "0x")
	if !ok || len(hexPart) == 0 || len(hexPart)%2 != 0 || strings.ToLower(hexPart) != hexPart {
		return fmt.Errorf("bad salt")
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return err
	}
	return nil
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

// recoverAddress recovers the signer of an EIP-712 digest. The trailing byte
// may be 0/1 or the 27/28 form viem writes into the claim.
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

var rootOnly = map[int64]bool{2: true, 3: true, 6: true, 10: true, 11: true}

// decodePresentation reads a version-1 presentation and requires every link field.
func decodePresentation(b []byte) (grantChain, readRequest, error) {
	var value any
	rest, err := decMode.UnmarshalFirst(b, &value)
	if err != nil {
		return grantChain{}, readRequest{}, fmt.Errorf("not a presentation: the bytes are not canonical CBOR: %w", err)
	}
	if len(rest) != 0 {
		return grantChain{}, readRequest{}, fmt.Errorf("not a presentation: trailing bytes")
	}
	items, ok := value.([]any)
	if !ok || len(items) != 3 || !isVersion1(items[0]) {
		return grantChain{}, readRequest{}, fmt.Errorf("not a version 1 presentation")
	}
	grant, err := decodeGrantChain(items[1])
	if err != nil {
		return grantChain{}, readRequest{}, err
	}
	reqMap, ok := asMap(items[2])
	if !ok {
		return grantChain{}, readRequest{}, fmt.Errorf("a request is a map")
	}
	piece, ok := lookup(reqMap, 0)
	pieceCID, isBytes := piece.([]byte)
	if !ok || !isBytes {
		return grantChain{}, readRequest{}, fmt.Errorf("a request needs a piece CID")
	}
	expires, err := uintField(reqMap, 1, "the request's expiry")
	if err != nil {
		return grantChain{}, readRequest{}, err
	}
	sig, err := bytesField(reqMap, 2, 65, "the request's signature")
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
		return grantChain{}, fmt.Errorf("a grant chain is an array of 1 to %d links", maxGrantChainLength)
	}
	maps := make([]map[any]any, len(items))
	for i, item := range items {
		m, ok := asMap(item)
		if !ok {
			return grantChain{}, fmt.Errorf("link %d is not a map", i)
		}
		for key := range m {
			n, ok := asInt(key)
			if !ok || n < 0 || n > 11 {
				return grantChain{}, fmt.Errorf("link %d has an unknown field %v", i, key)
			}
			if i > 0 && rootOnly[n] {
				return grantChain{}, fmt.Errorf("link %d repeats a root-only field", i)
			}
		}
		if !isVersion1(must(lookup(m, 0))) {
			return grantChain{}, fmt.Errorf("link %d has an unsupported version", i)
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
	blockChainNum, err := uintField(root, 6, "the root blockchain")
	if err != nil {
		return grantChain{}, err
	}
	commitment, err := bytesField(root, 10, 16, "keyspaceCommitment")
	if err != nil {
		return grantChain{}, err
	}
	expires, err := uintField(root, 11, "the root expiry")
	if err != nil {
		return grantChain{}, err
	}
	links := make([]claimLink, len(maps))
	for i, m := range maps {
		node, ok := lookup(m, 1)
		text, isText := node.(string)
		if !ok || !isText {
			return grantChain{}, fmt.Errorf("link %d has a non-canonical node", i)
		}
		if err := canonicalNode(text); err != nil {
			return grantChain{}, fmt.Errorf("link %d has a non-canonical node", i)
		}
		bearer, err := bytesField(m, 4, 20, fmt.Sprintf("link %d's bearer", i))
		if err != nil {
			return grantChain{}, err
		}
		depth, err := uintField(m, 5, fmt.Sprintf("link %d's maxDepth", i))
		if err != nil {
			return grantChain{}, err
		}
		if depth > 255 {
			return grantChain{}, fmt.Errorf("maxDepth must be 0 to 255, got %d", depth)
		}
		keyCommitment, err := bytesField(m, 7, 16, fmt.Sprintf("link %d's keyCommitment", i))
		if err != nil {
			return grantChain{}, err
		}
		sig, err := bytesField(m, 8, 65, fmt.Sprintf("link %d's signature", i))
		if err != nil {
			return grantChain{}, err
		}
		if raw, ok := lookup(m, 9); ok {
			if _, isBytes := raw.([]byte); !isBytes {
				return grantChain{}, fmt.Errorf("link %d's pieceCid must be bytes", i)
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

func must(v any, _ bool) any { return v }

func bytesField(m map[any]any, key int64, n int, what string) ([]byte, error) {
	value, ok := lookup(m, key)
	if !ok {
		return nil, fmt.Errorf("%s must be %d bytes", what, n)
	}
	b, ok := value.([]byte)
	if !ok || len(b) != n {
		return nil, fmt.Errorf("%s must be %d bytes", what, n)
	}
	return b, nil
}

func uintField(m map[any]any, key int64, what string) (uint64, error) {
	value, ok := lookup(m, key)
	if !ok {
		return 0, fmt.Errorf("%s must be an unsigned integer", what)
	}
	n, ok := asUint(value)
	if !ok || n > maxSafeInteger {
		return 0, fmt.Errorf("%s must be an unsigned integer", what)
	}
	return n, nil
}
