package keysmith

import (
	"crypto/ecdsa"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
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
	sum, err := hashTyped("ReadGrant", readGrantMessage(grant, link))
	require.NoError(t, err)
	sig, err := crypto.Sign(sum, key)
	require.NoError(t, err)
	sig[64] += 27
	return sig
}

func signRequestDigest(t *testing.T, key *ecdsa.PrivateKey, blockChain *big.Int, piece []byte, expires int64) []byte {
	t.Helper()
	sum, err := hashTyped("ReadRequest", readRequestMessage(blockChain, piece, expires))
	require.NoError(t, err)
	sig, err := crypto.Sign(sum, key)
	require.NoError(t, err)
	sig[64] += 27
	return sig
}

func encodePresentation(t *testing.T, grant grantChain, req readRequest) []byte {
	t.Helper()
	links := make([]any, len(grant.links))
	ceiling := NO_EXPIRY
	for i, link := range grant.links {
		m := map[int64]any{
			0: uint64(FOC_VERSION),
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
		}
		if link.expires != ceiling {
			m[11] = uint64(link.expires)
		}
		ceiling = link.expires
		links[i] = m
	}
	raw, err := presEnc.Marshal([]any{uint64(FOC_VERSION), links, map[int64]any{
		0: req.pieceCID,
		1: uint64(req.expires),
		2: req.signature,
	}})
	require.NoError(t, err)
	return raw
}

func groupPtr(s string) *string { return &s }

func TestVerifyPresentation(t *testing.T) {
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
			links: []claimLink{{
				node:          node,
				bearer:        bearer,
				maxDepth:      depth,
				keyCommitment: commitment,
				expires:       exp,
			}},
		}
		grant.links[0].signature = signDigest(t, ownerKey, grant, grant.links[0])
		return grant
	}
	delegate := func(grant grantChain, signer *ecdsa.PrivateKey, node string, bearer common.Address, depth int) grantChain {
		link := claimLink{
			node: node, bearer: bearer, maxDepth: depth, keyCommitment: bytes16(0x22),
			expires: grant.links[len(grant.links)-1].expires,
		}
		link.signature = signDigest(t, signer, grant, link)
		grant.links = append(append([]claimLink{}, grant.links...), link)
		return grant
	}
	present := func(grant grantChain, signer *ecdsa.PrivateKey, cid []byte, at int64) []byte {
		req := readRequest{pieceCID: cid, expires: at, signature: signRequestDigest(t, signer, grant.blockChain, cid, at)}
		return encodePresentation(t, grant, req)
	}
	check := func(raw []byte, labels PieceLabels, at int64, network uint64) error {
		_, err := VerifyPresentation(raw, piece, labels, new(big.Int).SetUint64(network), time.Unix(at, 0))
		return err
	}

	team := root("group:team", a, 1, expires)
	two := delegate(team, aKey, "group:team/alice", b, 0)
	alice := PieceLabels{
		Owner:    owner.Hex(),
		GroupKey: "0x" + common.Bytes2Hex(groupKey),
		Group:    groupPtr("team/alice"),
		Salt:     "0x000102030405060708090a0b0c0d0e0f",
	}

	require.NoError(t, check(present(two, bKey, piece, testNow+120), alice, testNow, testBlockChain))
	notes := alice
	notes.Group = groupPtr("team/alice/notes")
	require.NoError(t, check(present(two, bKey, piece, testNow+120), notes, testNow, testBlockChain))

	upper := alice
	upper.GroupKey = "0x" + "00112233445566778899AABBCCDDEEFF"
	require.NoError(t, check(present(two, bKey, piece, testNow+120), upper, testNow, testBlockChain))
	checksummed := alice
	checksummed.Owner = owner.Hex()
	require.NoError(t, check(present(two, bKey, piece, testNow+120), checksummed, testNow, testBlockChain))

	bob := alice
	bob.Group = groupPtr("team/bob")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), bob, testNow, testBlockChain), "does not reach")
	noGroup := alice
	noGroup.Group = nil
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), noGroup, testNow, testBlockChain), "(group (none))")
	require.ErrorContains(t, check(present(two, aKey, piece, testNow+120), alice, testNow, testBlockChain), "not signed by the final bearer")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow-3600), alice, testNow, testBlockChain), "expired")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+3600), alice, testNow, testBlockChain), "too long")
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), alice, testNow, 314), "another network")

	other := append([]byte{}, piece...)
	other[len(other)-1] ^= 1
	require.ErrorContains(t, check(present(two, bKey, other, testNow+120), alice, testNow, testBlockChain), "another piece")

	stolen := alice
	stolen.Owner = stranger.Hex()
	require.ErrorContains(t, check(present(two, bKey, piece, testNow+120), stolen, testNow, testBlockChain), "another owner or group key")

	late := expires + 3600
	require.ErrorContains(t, check(present(two, bKey, piece, late+120), alice, late, testBlockChain), "The chain expired")

	ungated := PieceLabels{GroupKey: alice.GroupKey, Salt: alice.Salt}
	require.NoError(t, check(present(two, bKey, piece, testNow+120), ungated, testNow, testBlockChain))

	forged := root("groupKey", a, 1, expires)
	forged.links[0].signature = signDigest(t, strangerKey, forged, forged.links[0])
	require.ErrorContains(t, check(present(forged, aKey, piece, testNow+120), ungated, testNow, testBlockChain), "not signed by the owner")

	wide := delegate(team, aKey, "group:other", b, 0)
	require.ErrorContains(t, check(present(wide, bKey, piece, testNow+120), alice, testNow, testBlockChain), "widens")

	shallow := root("group:team", a, 0, expires)
	tooDeep := delegate(shallow, aKey, "group:team/alice", b, 0)
	require.ErrorContains(t, check(present(tooDeep, bKey, piece, testNow+120), alice, testNow, testBlockChain), "maxDepth")

	salt := "0x9f3c41d2e07b6a58c1f20e4d7b93a6c5"
	pieceGrant := delegate(root("groupKey", a, 1, expires), aKey, "piece:"+salt, b, 0)
	pieceLabels := alice
	pieceLabels.Salt = "0x" + "9F3C41D2E07B6A58C1F20E4D7B93A6C5"
	require.NoError(t, check(present(pieceGrant, bKey, piece, testNow+120), pieceLabels, testNow, testBlockChain))

	decomposed := alice
	decomposed.Group = groupPtr("team/alice/cafe\u0301")
	nfc := delegate(team, aKey, "group:team/alice/caf\u00e9", b, 0)
	require.NoError(t, check(present(nfc, bKey, piece, testNow+120), decomposed, testNow, testBlockChain))

	same := delegate(team, aKey, "group:team/alice", b, 0)
	var decoded any
	require.NoError(t, presDec.Unmarshal(present(same, bKey, piece, testNow+120), &decoded))
	items := decoded.([]any)
	items[1].([]any)[1].(map[any]any)[uint64(11)] = uint64(expires)
	stated, err := presEnc.Marshal(items)
	require.NoError(t, err)
	require.NoError(t, check(stated, alice, testNow, testBlockChain))

	soon := delegate(team, aKey, "group:team/alice", b, 0)
	soon.links[1].expires = testNow + 3600
	soon.links[1].signature = signDigest(t, aKey, soon, soon.links[1])
	require.NoError(t, check(present(soon, bKey, piece, testNow+120), alice, testNow, testBlockChain))
	require.ErrorContains(t, check(present(soon, bKey, piece, testNow+3600+240), alice, testNow+3600+120, testBlockChain), "The chain expired")

	longer := delegate(team, aKey, "group:team/alice", b, 0)
	longer.links[1].expires = expires + 1
	longer.links[1].signature = signDigest(t, aKey, longer, longer.links[1])
	require.ErrorContains(t, check(present(longer, bKey, piece, testNow+120), alice, testNow, testBlockChain), "extends")

	open := delegate(root("group:team", a, 1, NO_EXPIRY), aKey, "group:team/alice", b, 0)
	far := testNow + 10*365*86400
	require.NoError(t, check(present(open, bKey, piece, far+120), alice, far, testBlockChain))

	capped := delegate(root("group:team", a, 1, NO_EXPIRY), aKey, "group:team/alice", b, 0)
	capped.links[1].expires = testNow + 3600
	capped.links[1].signature = signDigest(t, aKey, capped, capped.links[1])
	require.NoError(t, check(present(capped, bKey, piece, testNow+120), alice, testNow, testBlockChain))
	require.ErrorContains(t, check(present(capped, bKey, piece, testNow+3600+240), alice, testNow+3600+120, testBlockChain), "The chain expired")

	held := root("group:team", a, 2, expires)
	held = delegate(held, aKey, "group:team/alice", b, 1)
	held = delegate(held, bKey, "group:team/alice/notes", a, 0)
	require.NoError(t, check(present(held, aKey, piece, testNow+120), notes, testNow, testBlockChain))
}

func TestDealbotRootReadsAnotherOwnersPiece(t *testing.T) {
	require.Equal(t, common.HexToAddress("0x305025D07c1DEe47F25a4990179eFf2becddCA0B"), common.HexToAddress(DEALBOT_ROOT))

	ownerKey := testKey(t, "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	bearerKey := testKey(t, "59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d")
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	bearer := crypto.PubkeyToAddress(bearerKey.PublicKey)
	piece := []byte{0x01, 0x55}
	blockChain := new(big.Int).SetUint64(testBlockChain)
	grant := grantChain{
		owner:              owner,
		groupKey:           bytes16(1),
		blockChain:         blockChain,
		groupKeyCommitment: bytes16(2),
		links: []claimLink{{
			node: "groupKey", bearer: bearer, maxDepth: 0, keyCommitment: bytes16(2),
			expires: testNow + 86400,
		}},
	}
	grant.links[0].signature = signDigest(t, ownerKey, grant, grant.links[0])
	req := readRequest{
		pieceCID:  piece,
		expires:   testNow + 120,
		signature: signRequestDigest(t, bearerKey, blockChain, piece, testNow+120),
	}
	raw := encodePresentation(t, grant, req)
	other := PieceLabels{
		Owner:    "0x70997970C51812dc3A010C7d01b50e0d17dc79C8",
		GroupKey: "0x" + "ffffffffffffffffffffffffffffffff",
		Salt:     "0x000102030405060708090a0b0c0d0e0f",
	}
	check := func() error {
		_, err := VerifyPresentation(raw, piece, other, blockChain, testNowTime())
		return err
	}
	require.ErrorContains(t, check(), "another owner or group key")

	prev := extraGrantRoot
	t.Cleanup(func() { extraGrantRoot = prev })
	extraGrantRoot = owner
	require.NoError(t, check())

	grouped := grant
	grouped.links = []claimLink{{
		node: "group:team", bearer: bearer, maxDepth: 0, keyCommitment: bytes16(2),
		expires: testNow + 86400,
	}}
	grouped.links[0].signature = signDigest(t, ownerKey, grouped, grouped.links[0])
	groupedRaw := encodePresentation(t, grouped, req)
	other.Group = groupPtr("other")
	_, err := VerifyPresentation(groupedRaw, piece, other, blockChain, testNowTime())
	require.ErrorContains(t, err, "does not reach")

	same := other
	same.Owner = owner.Hex()
	same.Group = nil
	_, err = VerifyPresentation(raw, piece, same, blockChain, testNowTime())
	require.ErrorContains(t, err, "another owner or group key")
}

func TestDecodePresentationRejectsMalformedLinks(t *testing.T) {
	ownerKey := testKey(t, "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	grant := grantChain{
		owner:              owner,
		groupKey:           bytes16(1),
		blockChain:         new(big.Int).SetUint64(testBlockChain),
		groupKeyCommitment: bytes16(2),
		links: []claimLink{{
			node: "groupKey", bearer: owner, maxDepth: 1, keyCommitment: bytes16(2),
			expires: testNow + 100,
		}},
	}
	grant.links[0].signature = signDigest(t, ownerKey, grant, grant.links[0])
	req := readRequest{pieceCID: []byte{1}, expires: testNow + 10, signature: signRequestDigest(t, ownerKey, grant.blockChain, []byte{1}, testNow+10)}
	raw := encodePresentation(t, grant, req)

	var decoded any
	require.NoError(t, presDec.Unmarshal(raw, &decoded))
	items := decoded.([]any)
	links := items[1].([]any)
	root := links[0].(map[any]any)
	root[uint64(12)] = uint64(1)
	bad, err := presEnc.Marshal([]any{uint64(FOC_VERSION), []any{root}, items[2]})
	require.NoError(t, err)
	_, err = VerifyPresentation(bad, []byte{1}, PieceLabels{}, grant.blockChain, testNowTime())
	require.ErrorContains(t, err, "unknown field")

	delete(root, uint64(12))
	child := map[int64]any{0: uint64(FOC_VERSION), 1: "group:x", 2: owner.Bytes(), 4: owner.Bytes(), 5: uint64(0), 7: bytes16(3), 8: bytes16(4)}
	bad, err = presEnc.Marshal([]any{uint64(FOC_VERSION), []any{root, child}, items[2]})
	require.NoError(t, err)
	_, err = VerifyPresentation(bad, []byte{1}, PieceLabels{}, grant.blockChain, testNowTime())
	require.ErrorContains(t, err, "root-only field")

	badNode := map[int64]any{0: uint64(FOC_VERSION), 1: "group:agent-memory/", 4: owner.Bytes(), 5: uint64(0), 7: bytes16(3), 8: grant.links[0].signature}
	bad, err = presEnc.Marshal([]any{uint64(FOC_VERSION), []any{root, badNode}, items[2]})
	require.NoError(t, err)
	_, err = VerifyPresentation(bad, []byte{1}, PieceLabels{}, grant.blockChain, testNowTime())
	require.ErrorContains(t, err, "A group name must be non-empty")
}

func TestGroupName(t *testing.T) {
	for _, r := range []rune{'\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2000', '\u2005', '\u200a', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff'} {
		pad := string(r)
		_, err := groupName(pad + "a")
		require.ErrorContains(t, err, "A group name must be non-empty")
		_, err = groupName("a" + pad)
		require.ErrorContains(t, err, "A group name must be non-empty")
	}
	for _, name := range []string{"a", "a b", "a\u0085", "a\u200bb"} {
		got, err := groupName(name)
		require.NoError(t, err)
		require.Equal(t, name, got)
	}
	for _, name := range []string{"", " ", "a/b", "/a"} {
		_, err := groupName(name)
		require.ErrorContains(t, err, "A group name must be non-empty")
	}
}

func bytes16(fill byte) []byte {
	b := make([]byte, 16)
	for i := range b {
		b[i] = fill
	}
	return b
}

func TestWireNames(t *testing.T) {
	require.Equal(t, "Filecoin Encryption", domainName)
	require.Equal(t, "2", domainVersion)
	grant := eip712Types(readGrantWire{})
	names := make([]string, len(grant))
	for i, field := range grant {
		names[i] = field.Name
	}
	require.Equal(t, []string{
		"v", "node", "owner", "groupKeyId", "bearer", "maxDepth",
		"chainId", "keyCommitment", "groupKeyCommitment", "expires",
	}, names)

	var labels Labels
	got := map[string]string{}
	typ := reflect.TypeOf(labels)
	for i := 0; i < typ.NumField(); i++ {
		name, _, skip := jsonTag(typ.Field(i))
		if skip {
			continue
		}
		got[typ.Field(i).Name] = name
	}
	require.Equal(t, map[string]string{
		"Version":  "foc/v",
		"Owner":    "foc/own",
		"GroupKey": "foc/gkid",
		"Group":    "foc/group",
		"Salt":     "foc/salt",
	}, got)
}
