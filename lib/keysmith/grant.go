package keysmith

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
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

	// NO_EXPIRY is the ceiling before a link sets field 11.
	// Omitting field 11 keeps the current ceiling. The value is not written
	// on the wire: a CBOR integer above 2^53−1 is non-canonical.
	NO_EXPIRY = int64(math.MaxInt64)

	domainName    = "Filecoin Encryption"
	domainVersion = "2"

	// DEALBOT_ROOT is the dealbot wallet on mainnet and calibnet.
	// A grant it roots may read a piece whose foc/own names another wallet.
	// FOC checks reliabiity and repairs SP outages by making another 2nd copy.
	// The data remains entirely encryptied as only the root wallet can decrypt it.
	DEALBOT_ROOT = "0x305025D07c1DEe47F25a4990179eFf2becddCA0B"
)

// extraGrantRoot is a grant-root wallet accepted besides the piece owner.
// Tests replace it. Production is the dealbot wallet, on every network.
var extraGrantRoot = common.HexToAddress(DEALBOT_ROOT)

// PieceLabels are the foc fields a provider read from a piece envelope.
// An empty Owner means foc/own was absent, so the piece is not gated.
// A nil Group means foc/group was absent.
type PieceLabels struct {
	Owner    string
	GroupKey string
	Group    *string
	Salt     string
}

// VerifiedClaim is the final link of a presentation that verified.
type VerifiedClaim struct {
	Owner    common.Address
	Node     string
	Bearer   common.Address
	groupKey []byte
}

type claimLink struct {
	node          string
	bearer        common.Address
	maxDepth      int
	keyCommitment []byte
	signature     []byte
	expires       int64
}

type grantChain struct {
	owner              common.Address
	groupKey           []byte
	blockChain         *big.Int
	groupKeyCommitment []byte
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
	groupKeyPattern = regexp.MustCompile(`^0x[0-9a-f]{32}$`)
	// jsEdgeSpace matches a leading or trailing run of jsSpace.
	jsEdgeSpace = regexp.MustCompile(`^[` + jsSpace + `]+|[` + jsSpace + `]+$`)
)

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
		sameOwner := strings.EqualFold(piece.Owner, claim.Owner.Hex())
		// The dealbot root is not the piece's groupKey owner, so that comparison does not apply.
		if sameOwner {
			groupKey, err := groupKeyID(piece.GroupKey)
			if err != nil {
				return VerifiedClaim{}, err
			}
			if groupKey != "0x"+hex.EncodeToString(claim.groupKey) {
				return VerifiedClaim{}, refuse("The piece belongs to another owner or group key")
			}
		} else if claim.Owner != extraGrantRoot {
			return VerifiedClaim{}, refuse("The piece belongs to another owner or group key")
		}
		ok, err := reaches(claim.Node, piece)
		if err != nil {
			return VerifiedClaim{}, err
		}
		if !ok {
			group := "(none)"
			if piece.Group != nil {
				group = *piece.Group
			}
			return VerifiedClaim{}, refuse("%s does not reach this piece (group %s)", claim.Node, group)
		}
	}
	return claim, nil
}

// verifyGrantChain checks signatures, expiry, and group narrowing. piece is
// required when a link names one piece; a nil piece means the caller has no labels.
func verifyGrantChain(grant grantChain, piece *PieceLabels, now time.Time) (VerifiedClaim, error) {
	links := grant.links
	if len(links) < 1 || len(links) > maxGrantChainLength {
		return VerifiedClaim{}, refuse("A chain has 1 to %d links", maxGrantChainLength)
	}
	root := links[0]
	if err := chainExpired(root.expires, now); err != nil {
		return VerifiedClaim{}, err
	}
	if root.node == NODE_GROUP_KEY && !bytes.Equal(root.keyCommitment, grant.groupKeyCommitment) {
		return VerifiedClaim{}, refuse("A group key root must carry the group key commitment")
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
		if err := chainExpired(links[i].expires, now); err != nil {
			return VerifiedClaim{}, err
		}
	}
	last := links[len(links)-1]
	return VerifiedClaim{Owner: grant.owner, Node: last.node, Bearer: last.bearer, groupKey: grant.groupKey}, nil
}

// checkStep requires the child to be no wider than its parent, no later than its
// parent's expiry, and within the parent's depth.
// piece may be nil; a piece node then fails. index is the child's position in the chain.
func checkStep(parent, child claimLink, piece *PieceLabels, index int) error {
	if parent.maxDepth < 1 || child.maxDepth > parent.maxDepth-1 {
		return refuse("Link %d exceeds link %d's maxDepth", index, index-1)
	}
	if child.expires > parent.expires {
		return refuse("Link %d extends link %d's expiry", index, index-1)
	}
	var narrower bool
	switch {
	case parent.node == NODE_GROUP_KEY:
		narrower = true
	case strings.HasPrefix(child.node, NODE_PIECE_PREFIX):
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
		narrower = !strings.HasPrefix(parent.node, NODE_PIECE_PREFIX) && child.node != NODE_GROUP_KEY && within(parent.node, child.node)
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
// The caller passes a canonical node. Group and salt labels are canonicalised here.
func reaches(node string, piece PieceLabels) (bool, error) {
	if node == NODE_GROUP_KEY {
		return true, nil
	}
	if strings.HasPrefix(node, NODE_PIECE_PREFIX) {
		named, err := pieceNodeOf(piece)
		if err != nil {
			return false, err
		}
		return node == named, nil
	}
	if piece.Group == nil {
		return false, nil
	}
	path, err := groupPath(*piece.Group)
	if err != nil {
		return false, err
	}
	return within(node, NODE_GROUP_PREFIX+path), nil
}

// pieceNodeOf is the canonical piece node for these labels. The caller has a salt string.
func pieceNodeOf(piece PieceLabels) (string, error) {
	return canonicalNode(NODE_PIECE_PREFIX + piece.Salt)
}

// canonicalNode accepts only the form Keysmith signs: groupKey, group:<path>, or piece:<salt>.
// A node that is not already canonical returns a different string, or an error when it cannot be one.
func canonicalNode(node string) (string, error) {
	if node == NODE_GROUP_KEY {
		return NODE_GROUP_KEY, nil
	}
	if rest, ok := strings.CutPrefix(node, NODE_GROUP_PREFIX); ok {
		path, err := groupPath(rest)
		if err != nil {
			return "", err
		}
		return NODE_GROUP_PREFIX + path, nil
	}
	if rest, ok := strings.CutPrefix(node, NODE_PIECE_PREFIX); ok {
		salt, err := saltOf(rest)
		if err != nil {
			return "", err
		}
		if len(salt) != 2+32 {
			return "", refuse("A piece node names a 16-byte salt, got %s", jsonQuote(node))
		}
		return NODE_PIECE_PREFIX + salt, nil
	}
	return "", refuse("Unrecognised grant node: %s", node)
}

// groupPath canonicalises a group path: NFC names joined by "/". The caller passes the text after "group:".
func groupPath(path string) (string, error) {
	parts := strings.Split(path, "/")
	out := make([]string, len(parts))
	for i, part := range parts {
		name, err := groupName(part)
		if err != nil {
			return "", err
		}
		out[i] = name
	}
	return strings.Join(out, "/"), nil
}

// groupName canonicalises one group segment. A segment that is empty, padded, or contains "/" is refused.
func groupName(name string) (string, error) {
	normalised := norm.NFC.String(name)
	if normalised == "" || jsEdgeSpace.MatchString(normalised) || strings.Contains(normalised, "/") {
		return "", refuse("A group name must be non-empty, with no leading or trailing whitespace and no \"/\", got %s", jsonQuote(name))
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

// groupKeyID returns a group key id as 16 lowercase hex bytes with a 0x prefix.
// The caller passes a foc/gkid label.
func groupKeyID(groupKey string) (string, error) {
	lower := strings.ToLower(groupKey)
	if !groupKeyPattern.MatchString(lower) {
		return "", refuse("A group key id is 16 bytes of hex, got %s", jsonQuote(groupKey))
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

// readGrantMessage is the EIP-712 ReadGrant for link.
// owner, groupKeyId, chainId, and groupKeyCommitment come from grant, which the caller filled from the first link.
// link.expires is the ceiling after this link, NO_EXPIRY when the link sets none.
func readGrantMessage(grant grantChain, link claimLink) readGrantWire {
	return readGrantWire{
		V:                  big.NewInt(FOC_VERSION),
		Node:               link.node,
		Owner:              grant.owner.Hex(),
		GroupKeyID:         grant.groupKey,
		Bearer:             link.bearer.Hex(),
		MaxDepth:           big.NewInt(int64(link.maxDepth)),
		ChainID:            grant.blockChain,
		KeyCommitment:      link.keyCommitment,
		GroupKeyCommitment: grant.groupKeyCommitment,
		Expires:            big.NewInt(link.expires),
	}
}

// readRequestMessage is the EIP-712 ReadRequest for these fields.
// The caller has the request's chain, piece CID, and expiry.
func readRequestMessage(blockChain *big.Int, piece []byte, expires int64) readRequestWire {
	return readRequestWire{
		ChainID:  blockChain,
		PieceCID: piece,
		Expires:  big.NewInt(expires),
	}
}

// recoverLink returns the signer of link. The caller has decoded the link.
func recoverLink(grant grantChain, link claimLink) (common.Address, error) {
	sum, err := hashTyped("ReadGrant", readGrantMessage(grant, link))
	if err != nil {
		return common.Address{}, err
	}
	return recoverAddress(sum, link.signature)
}

// recoverRequest returns the signer of req. The caller has decoded the request.
func recoverRequest(blockChain *big.Int, req readRequest) (common.Address, error) {
	sum, err := hashTyped("ReadRequest", readRequestMessage(blockChain, req.pieceCID, req.expires))
	if err != nil {
		return common.Address{}, err
	}
	return recoverAddress(sum, req.signature)
}

// hashTyped is the EIP-712 hash of msg. primary names the type, and msg is that type's wire struct.
func hashTyped(primary string, msg any) ([]byte, error) {
	sum, _, err := apitypes.TypedDataAndHash(apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": eip712Types(eip712DomainWire{}),
			primary:        eip712Types(msg),
		},
		PrimaryType: primary,
		Domain:      apitypes.TypedDataDomain{Name: domainName, Version: domainVersion},
		Message:     eip712Message(msg),
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

var rootOnly = map[int64]bool{2: true, 3: true, 6: true, 10: true}

// chainExpired refuses an expiry before now. The caller passes one link's expiry.
// clockSkew seconds of lateness are still accepted.
func chainExpired(expires int64, now time.Time) error {
	if expires >= now.Unix()-clockSkew {
		return nil
	}
	when := time.Unix(expires, 0).UTC().Format("2006-01-02T15:04:05.000Z")
	return refuse("The chain expired at %s", when)
}

// decodePresentation reads a version-2 presentation. The bytes must be the
// core deterministic encoding of that value. Any other version is refused.
func decodePresentation(b []byte) (grantChain, readRequest, error) {
	value, err := decodeWithRoundTripCheck(b)
	if err != nil {
		return grantChain{}, readRequest{}, refuse("Not a presentation: the bytes are not canonical CBOR")
	}
	items, ok := value.([]any)
	if !ok || len(items) != 3 || !isVersion(items[0], FOC_VERSION) {
		return grantChain{}, readRequest{}, refuse("Not a version 2 presentation")
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

// isVersion reports whether v is the unsigned integer want. The caller passes a decoded CBOR value.
func isVersion(v any, want uint64) bool {
	n, ok := asUint(v)
	return ok && n == want
}

// decodeGrantChain reads the root-first link array. Root-only fields on a later link are rejected.
//
// A chain of two links: the owner grants group:team, and that bearer narrows it to
// group:team/alice. Integer keys are the CBOR fields. Field 9 is an optional
// unsigned pieceCid and may appear on any link.
//
//	first link {
//	  0:  2                    v
//	  1:  "group:team"         node
//	  2:  <20 bytes>           owner
//	  3:  <16 bytes>           groupKeyId
//	  4:  <20 bytes>           bearer
//	  5:  1                    maxDepth
//	  6:  314159               chainId
//	  7:  <16 bytes>           keyCommitment
//	  8:  <65 bytes>           signature, low s, v = 27 or 28
//	  10: <16 bytes>           groupKeyCommitment
//	  11: 1800604800           expires, unix seconds
//	}
//
// Every link is signed as the same EIP-712 ReadGrant. The domain is name
// "Filecoin Encryption", version "2", with no chain id. The first link is signed by
// owner. Link i is signed by link i-1's bearer. node, bearer, maxDepth, and
// keyCommitment come from the link being signed. owner, groupKeyId, chainId, and
// groupKeyCommitment come from the first link. Field 11 is optional on every link.
// The ceiling starts at NO_EXPIRY. A link that omits field 11 keeps the current
// ceiling. A link that sets it must be that ceiling or earlier, and the signed
// expires is the ceiling after that link.
//
//	signed for the first link {
//	  v:                  2
//	  node:               "group:team"
//	  owner:              <20 bytes>
//	  groupKeyId:         <16 bytes>
//	  bearer:             <20 bytes>
//	  maxDepth:           1
//	  chainId:            314159
//	  keyCommitment:      <16 bytes>
//	  groupKeyCommitment: <16 bytes>
//	  expires:            1800604800
//	}
//
//	next link, expiry brought forward {
//	  0:  2
//	  1:  "group:team/alice"   node
//	  4:  <20 bytes>           bearer
//	  5:  0                    maxDepth
//	  7:  <16 bytes>           keyCommitment
//	  8:  <65 bytes>           signature
//	  11: 1800086400           expires, optional
//	}
//
//	signed for the next link {
//	  v:                  2
//	  node:               "group:team/alice"  // this link
//	  owner:              <20 bytes>          // first link
//	  groupKeyId:         <16 bytes>          // first link
//	  bearer:             <20 bytes>          // this link
//	  maxDepth:           0                   // this link
//	  chainId:            314159              // first link
//	  keyCommitment:      <16 bytes>          // this link
//	  groupKeyCommitment: <16 bytes>          // first link
//	  expires:            1800086400          // this link, or the current ceiling when 11 is omitted
//	}
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
		rawV, _ := lookup(m, 0)
		if !isVersion(rawV, FOC_VERSION) {
			return grantChain{}, refuse("Link %d must be version %d", i, FOC_VERSION)
		}
		maps[i] = m
	}
	root := maps[0]
	owner, err := bytesField(root, 2, 20, "owner")
	if err != nil {
		return grantChain{}, err
	}
	groupKey, err := bytesField(root, 3, 16, "groupKeyId")
	if err != nil {
		return grantChain{}, err
	}
	blockChainNum, err := uintField(root, 6, "The root chain id")
	if err != nil {
		return grantChain{}, err
	}
	commitment, err := bytesField(root, 10, 16, "groupKeyCommitment")
	if err != nil {
		return grantChain{}, err
	}
	links := make([]claimLink, len(maps))
	ceiling := NO_EXPIRY
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
		exp := ceiling
		if _, ok := lookup(m, 11); ok {
			n, err := uintField(m, 11, fmt.Sprintf("Link %d's expiry", i))
			if err != nil {
				return grantChain{}, err
			}
			exp = int64(n)
		}
		if exp <= ceiling {
			ceiling = exp
		}
		links[i] = claimLink{
			node:          text,
			bearer:        common.BytesToAddress(bearer),
			maxDepth:      int(depth),
			keyCommitment: keyCommitment,
			signature:     sig,
			expires:       exp,
		}
	}
	return grantChain{
		owner:              common.BytesToAddress(owner),
		groupKey:           groupKey,
		blockChain:         new(big.Int).SetUint64(blockChainNum),
		groupKeyCommitment: commitment,
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
