package keysmith

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

const (
	testNow        = int64(1_800_000_000)
	testBlockChain = uint64(314159)
)

func testKey(t *testing.T, hexKey string) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA(hexKey)
	require.NoError(t, err)
	return key
}

func testNowTime() time.Time { return time.Unix(testNow, 0) }

func signDigest(t *testing.T, key *ecdsa.PrivateKey, grant grantChain, link claimLink) []byte {
	t.Helper()
	sum, err := hashTyped("ReadGrant", readGrantTypes, grantMessage(grant, link))
	require.NoError(t, err)
	sig, err := crypto.Sign(sum, key)
	require.NoError(t, err)
	sig[64] += 27
	return sig
}

func grantMessage(grant grantChain, link claimLink) map[string]any {
	return map[string]any{
		"v":                  big.NewInt(1),
		"node":               link.node,
		"owner":              grant.owner.Hex(),
		"groupKey":           grant.groupKey,
		"bearer":             link.bearer.Hex(),
		"maxDepth":           big.NewInt(int64(link.maxDepth)),
		blockChainField:      grant.blockChain,
		"keyCommitment":      link.keyCommitment,
		"groupKeyCommitment": grant.groupKeyCommitment,
		"expires":            big.NewInt(grant.expires),
	}
}

func signRequestDigest(t *testing.T, key *ecdsa.PrivateKey, blockChain *big.Int, piece []byte, expires int64) []byte {
	t.Helper()
	sum, err := hashTyped("ReadRequest", readRequestTypes, map[string]any{
		blockChainField: blockChain,
		"pieceCid":      piece,
		"expires":       big.NewInt(expires),
	})
	require.NoError(t, err)
	sig, err := crypto.Sign(sum, key)
	require.NoError(t, err)
	sig[64] += 27
	return sig
}

func encodePresentation(t *testing.T, grant grantChain, req readRequest) []byte {
	t.Helper()
	links := make([]any, len(grant.links))
	for i, link := range grant.links {
		m := map[int64]any{
			0: uint64(1),
			1: link.node,
			4: link.bearer.Bytes(),
			5: uint64(link.maxDepth),
			7: link.keyCommitment,
			8: link.signature,
		}
		if i == 0 {
			m[2] = grant.owner.Bytes()
			m[3] = grant.groupKey
			m[6] = grant.blockChain.Uint64()
			m[10] = grant.groupKeyCommitment
			m[11] = uint64(grant.expires)
		}
		links[i] = m
	}
	raw, err := cbor.Marshal([]any{uint64(1), links, map[int64]any{
		0: req.pieceCID,
		1: uint64(req.expires),
		2: req.signature,
	}})
	require.NoError(t, err)
	return raw
}

func groupsPtr(s string) *string { return &s }

func TestVerifyPresentationMatchesKeysmith(t *testing.T) {
	ownerKey := testKey(t, "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	aKey := testKey(t, "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d")
	bKey := testKey(t, "5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a")
	strangerKey := testKey(t, "7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6")
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	a := crypto.PubkeyToAddress(aKey.PublicKey)
	b := crypto.PubkeyToAddress(bKey.PublicKey)
	stranger := crypto.PubkeyToAddress(strangerKey.PublicKey)
	groupKey := common.Hex2Bytes("00112233445566778899aabbccddeeff")
	commitment := bytes16(0x11)
	piece := []byte{0x01, 0x55, 0x91, 0x20, 0x24}
	blockChain := new(big.Int).SetUint64(testBlockChain)
	expires := testNow + 7*86400

	root := func(node string, bearer common.Address, depth int, exp int64) grantChain {
		grant := grantChain{
			owner:              owner,
			groupKey:           groupKey,
			blockChain:         blockChain,
			groupKeyCommitment: commitment,
			expires:            exp,
			links: []claimLink{{
				node:          node,
				bearer:        bearer,
				maxDepth:      depth,
				keyCommitment: commitment,
			}},
		}
		grant.links[0].signature = signDigest(t, ownerKey, grant, grant.links[0])
		return grant
	}
	delegate := func(grant grantChain, signer *ecdsa.PrivateKey, node string, bearer common.Address, depth int) grantChain {
		parent := grant.links[len(grant.links)-1]
		link := claimLink{node: node, bearer: bearer, maxDepth: depth, keyCommitment: bytes16(0x22)}
		link.signature = signDigest(t, signer, grant, link)
		grant.links = append(append([]claimLink{}, grant.links...), link)
		_ = parent
		return grant
	}
	present := func(grant grantChain, signer *ecdsa.PrivateKey, cid []byte, at int64) []byte {
		req := readRequest{pieceCID: cid, expires: at, signature: signRequestDigest(t, signer, grant.blockChain, cid, at)}
		return encodePresentation(t, grant, req)
	}
	check := func(raw []byte, labels PieceLabels, at int64, network uint64) error {
		return VerifyPresentation(raw, piece, labels, new(big.Int).SetUint64(network), time.Unix(at, 0))
	}

	team := root("groups:team", a, 1, expires)
	two := delegate(team, aKey, "groups:team/alice", b, 0)
	alice := PieceLabels{
		Owner:    owner.Hex(),
		GroupKey: "0x" + common.Bytes2Hex(groupKey),
		Groups:   groupsPtr("team/alice"),
		Salt:     "0x000102030405060708090a0b0c0d0e0f",
	}

	require.NoError(t, check(present(two, bKey, piece, testNow+120), alice, testNow, testBlockChain))
	notes := alice
	notes.Groups = groupsPtr("team/alice/notes")
	require.NoError(t, check(present(two, bKey, piece, testNow+120), notes, testNow, testBlockChain))

	bob := alice
	bob.Groups = groupsPtr("team/bob")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), bob, testNow, testBlockChain), "does not reach")
	noGroups := alice
	noGroups.Groups = nil
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), noGroups, testNow, testBlockChain), "does not reach")
	require.ErrorContains(t, check(present(two, aKey, piece, testNow+120), alice, testNow, testBlockChain), "not signed by the final bearer")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow-3600), alice, testNow, testBlockChain), "expired")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+3600), alice, testNow, testBlockChain), "too long")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), alice, testNow, 314), "another blockchain")

	other := append([]byte{}, piece...)
	other[len(other)-1] ^= 1
	require.ErrorContains(t, check(present(two, bKey, other, testNow+120), alice, testNow, testBlockChain), "another piece")

	stolen := alice
	stolen.Owner = stranger.Hex()
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), stolen, testNow, testBlockChain), "another owner")

	late := expires + 3600
	require.ErrorContains(t, check(present(two, bKey, piece, late+120), alice, late, testBlockChain), "grant chain expired")

	ungated := PieceLabels{GroupKey: alice.GroupKey, Salt: alice.Salt}
	require.NoError(t, check(present(two, bKey, piece, testNow+120), ungated, testNow, testBlockChain))

	forged := root("groupKey", a, 1, expires)
	forged.links[0].signature = signDigest(t, strangerKey, grantChain{
		owner: stranger, groupKey: groupKey, blockChain: blockChain, groupKeyCommitment: commitment, expires: expires,
		links: forged.links,
	}, forged.links[0])
	// The signature was made by the stranger over a message naming the real owner, so recovery is not the owner.
	forged.links[0].signature = signDigest(t, strangerKey, forged, forged.links[0])
	require.ErrorContains(t, check(present(forged, aKey, piece, testNow+120), ungated, testNow, testBlockChain), "not signed by the owner")

	wide := delegate(team, aKey, "groups:other", b, 0)
	require.ErrorContains(t, check(present(wide, bKey, piece, testNow+120), alice, testNow, testBlockChain), "widens")

	shallow := root("groups:team", a, 0, expires)
	tooDeep := delegate(shallow, aKey, "groups:team/alice", b, 0)
	require.ErrorContains(t, check(present(tooDeep, bKey, piece, testNow+120), alice, testNow, testBlockChain), "maxDepth")
}

func TestDecodePresentationRejectsMalformedLinks(t *testing.T) {
	ownerKey := testKey(t, "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	grant := grantChain{
		owner:              owner,
		groupKey:           bytes16(1),
		blockChain:         new(big.Int).SetUint64(testBlockChain),
		groupKeyCommitment: bytes16(2),
		expires:            testNow + 100,
		links: []claimLink{{
			node: "groupKey", bearer: owner, maxDepth: 1, keyCommitment: bytes16(2),
		}},
	}
	grant.links[0].signature = signDigest(t, ownerKey, grant, grant.links[0])
	req := readRequest{pieceCID: []byte{1}, expires: testNow + 10, signature: signRequestDigest(t, ownerKey, grant.blockChain, []byte{1}, testNow+10)}
	raw := encodePresentation(t, grant, req)

	var decoded any
	require.NoError(t, cbor.Unmarshal(raw, &decoded))
	items := decoded.([]any)
	links := items[1].([]any)
	root := links[0].(map[any]any)
	root[int64(12)] = uint64(1)
	bad, err := cbor.Marshal([]any{uint64(1), []any{root}, items[2]})
	require.NoError(t, err)
	require.ErrorContains(t, VerifyPresentation(bad, []byte{1}, PieceLabels{}, grant.blockChain, testNowTime()), "unknown field")

	delete(root, int64(12))
	child := map[int64]any{0: uint64(1), 1: "groups:x", 2: owner.Bytes(), 4: owner.Bytes(), 5: uint64(0), 7: bytes16(3), 8: bytes16(4)}
	// signature length is wrong on purpose after we first check root-only.
	bad, err = cbor.Marshal([]any{uint64(1), []any{root, child}, items[2]})
	require.NoError(t, err)
	require.ErrorContains(t, VerifyPresentation(bad, []byte{1}, PieceLabels{}, grant.blockChain, testNowTime()), "root-only field")
}

func bytes16(fill byte) []byte {
	b := make([]byte, 16)
	for i := range b {
		b[i] = fill
	}
	return b
}
