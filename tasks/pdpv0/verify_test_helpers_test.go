package pdpv0

import (
	"bytes"
	"encoding/base32"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"testing"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/pdp/contract"
)

// ---------------------------------------------------------------------------
// SHA-254 helper
// ---------------------------------------------------------------------------

// sha254 computes SHA-256 with the two most-significant bits of the last byte
// zeroed, yielding a 254-bit digest. This is the hash function used throughout
// the Filecoin piece-commitment (commP) Merkle tree.
//
// The truncation is mandated by the BLS12-381 field size: every node in the
// binary Merkle tree must be a valid field element, so the top two bits of the
// 32nd byte are cleared after every hash.
//
// Reference implementation (upstream):
//
//	https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/commp.go
//
// Curio's local copy (savecache.go):
//
//	https://github.com/filecoin-project/curio/blob/main/lib/savecache/savecache.go
func sha254(data []byte) [32]byte {
	return proof.ComputeBinShaParent(
		[32]byte(data[:32]),
		[32]byte(data[32:64]),
	)
}

// ---------------------------------------------------------------------------
// Deterministic random data generator
// ---------------------------------------------------------------------------

// generateRandomData produces deterministic pseudo-random bytes identical to
// the data used by the go-fil-commp-hashhash test suite.
//
// The algorithm is copied from jbenet/go-random, which is the canonical data
// source for Filecoin commP test vectors. go-fil-commp-hashhash inlines the
// same logic to avoid a singleton-seed import-order issue during parallel
// tests.
//
// Source (go-fil-commp-hashhash/commp_test.go):
//
//	https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/commp_test.go#L40-L64
//
// Source (jbenet/go-random):
//
//	https://github.com/jbenet/go-random
//
// Seed: 1337 (hardcoded in the upstream test suite).
//
// Why we replicate it: we need to feed exactly the same bytes into our
// BuildSha254Memtree / savecache.Calc so that roots match the known-good
// commP CIDs published in go-fil-commp-hashhash/testdata/random.txt.
func generateRandomData(size int64) []byte {
	const seed = 1337
	rng := rand.New(rand.NewSource(seed))

	buf := make([]byte, size)
	for i := int64(0); i < size; {
		// jbenet/go-random emits one Uint32 and spreads its 4 bytes LSB-first.
		n := rng.Uint32()
		for j := 0; j < 4 && i < size; j++ {
			buf[i] = byte(n & 0xff)
			n >>= 8
			i++
		}
	}
	return buf
}

// ---------------------------------------------------------------------------
// Memtree construction helpers
// ---------------------------------------------------------------------------

// buildMemtree builds an in-memory SHA-254 Merkle tree from raw (unpadded)
// data using proof.BuildSha254Memtree and returns both the flat memtree buffer
// and the 32-byte root hash.
//
// The caller must call pool.Put(memtree) when done with the buffer.
//
// BuildSha254Memtree performs FR32 padding internally, so rawData should be
// the unpadded payload.
func buildMemtree(t *testing.T, rawData []byte, unpaddedSize abi.UnpaddedPieceSize) (memtree []byte, root [32]byte) {
	t.Helper()

	mt, err := proof.BuildSha254Memtree(bytes.NewReader(rawData), unpaddedSize)
	require.NoError(t, err)

	r := extractMemtreeRoot(mt)
	return mt, r
}

// buildMemtreeFromPayload builds an in-memory SHA-254 Merkle tree from a
// payload that may be smaller than the padded piece size. Any gap between
// the payload length and the unpadded piece size is zero-filled, exactly as
// Filecoin does when a piece is smaller than its declared size.
//
// The caller must call pool.Put(memtree) when done with the buffer.
func buildMemtreeFromPayload(t *testing.T, payload []byte, paddedSize uint64) (memtree []byte, root [32]byte) {
	t.Helper()

	unpaddedSize := abi.PaddedPieceSize(paddedSize).Unpadded()
	reader := io.LimitReader(
		io.MultiReader(
			bytes.NewReader(payload),
			&zeroReader{},
		),
		int64(unpaddedSize),
	)

	mt, err := proof.BuildSha254Memtree(reader, unpaddedSize)
	require.NoError(t, err)

	r := extractMemtreeRoot(mt)
	return mt, r
}

// extractMemtreeRoot returns the root hash from a flat memtree buffer.
//
// The memtree layout places leaves first, then internal nodes level by level,
// with the single root node last. So the root occupies the final 32 bytes.
func extractMemtreeRoot(memtree []byte) [32]byte {
	totalNodes := int64(len(memtree)) / proof.NODE_SIZE
	rootOffset := (totalNodes - 1) * proof.NODE_SIZE
	var root [32]byte
	copy(root[:], memtree[rootOffset:rootOffset+proof.NODE_SIZE])
	return root
}

// ---------------------------------------------------------------------------
// Proof construction / verification helpers
// ---------------------------------------------------------------------------

// buildProofFromMemtree builds an in-memory tree, extracts a Merkle proof for
// the given leaf index, and returns both the proof (in contract-compatible
// form) and the tree root.
//
// This is the main helper used by negative tests that need a known-good proof
// to tamper with.
func buildProofFromMemtree(t *testing.T, rawData []byte, unpaddedSize abi.UnpaddedPieceSize, leafIndex int64) (contract.IPDPTypesProof, [32]byte) {
	t.Helper()

	memtree, root := buildMemtree(t, rawData, unpaddedSize)
	defer pool.Put(memtree)

	return extractProof(t, memtree, leafIndex), root
}

// extractProof extracts a Merkle proof for a single leaf from a flat memtree.
func extractProof(t *testing.T, memtree []byte, leafIndex int64) contract.IPDPTypesProof {
	t.Helper()

	rawProof, err := proof.MemtreeProof(memtree, leafIndex)
	require.NoError(t, err)

	return contract.IPDPTypesProof{
		Leaf:  rawProof.Leaf,
		Proof: rawProof.Proof,
	}
}

// verifyProofsAtPositions generates and verifies Merkle proofs at a set of
// "interesting" leaf positions within the given memtree. This is the workhorse
// used by most roundtrip tests. It asserts that every proof passes Verify().
func verifyProofsAtPositions(t *testing.T, memtree []byte, root [32]byte, nLeaves int64, label string) {
	t.Helper()

	for _, pos := range interestingPositions(nLeaves) {
		p := extractProof(t, memtree, pos)
		require.True(t, Verify(p, root, uint64(pos)),
			"Verify failed %s pos=%d", label, pos)
	}
}

// ---------------------------------------------------------------------------
// Challenge position selection
// ---------------------------------------------------------------------------

// interestingPositions returns a deduplicated set of leaf indices that exercise
// various parts of a binary Merkle tree:
//
//   - Boundary leaves (first, last, second-to-last).
//   - Power-of-two boundaries (quarter, half, three-quarter) which sit at
//     subtree roots and stress left-vs-right sibling selection.
//   - Off-by-one positions around those boundaries to catch fence-post errors.
//
// These positions model the kinds of challenges the PDP verifier contract can
// generate.
func interestingPositions(nLeaves int64) []int64 {
	candidates := []int64{
		0,                // first leaf
		1,                // second leaf
		nLeaves/4 - 1,   // just before quarter boundary
		nLeaves / 4,     // quarter boundary
		nLeaves/2 - 1,   // just before midpoint
		nLeaves / 2,     // midpoint
		nLeaves/2 + 1,   // just after midpoint
		nLeaves*3/4 - 1, // just before three-quarter
		nLeaves * 3 / 4, // three-quarter boundary
		nLeaves - 2,     // second-to-last leaf
		nLeaves - 1,     // last leaf
	}

	seen := make(map[int64]bool, len(candidates))
	result := make([]int64, 0, len(candidates))
	for _, p := range candidates {
		if p >= 0 && p < nLeaves && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Manual tree construction helpers
// ---------------------------------------------------------------------------

// makeLeaf creates a deterministic 32-byte leaf value.
//
// Each byte = leafIndex*32 + byteOffset, guaranteeing that every leaf in a
// hand-built tree is unique. This mirrors the "sequential byte" pattern used
// in the manual 2/4/8-leaf tree tests.
func makeLeaf(leafIndex int) [32]byte {
	var leaf [32]byte
	for j := range leaf {
		leaf[j] = byte(leafIndex*32 + j)
	}
	return leaf
}

// makeLeaves creates n deterministic leaves using makeLeaf.
func makeLeaves(n int) [][32]byte {
	leaves := make([][32]byte, n)
	for i := 0; i < n; i++ {
		leaves[i] = makeLeaf(i)
	}
	return leaves
}

// ---------------------------------------------------------------------------
// Test vector fixtures
// ---------------------------------------------------------------------------

// commPTestVector holds one row from the go-fil-commp-hashhash test fixture
// files. Each row is: unpadded_payload_size, padded_piece_size, commP_CID.
//
// Source file:
//
//	https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/testdata/random.txt
//
// The CIDs are base32-lower multibase-encoded CIDv1 values with a
// fil-commitment-unsealed codec. The last 32 bytes of the decoded CID are the
// raw commP digest.
type commPTestVector struct {
	payloadSize int64
	paddedSize  uint64
	rawCommP    [32]byte
}

// b32dec is the base32 decoder used for Filecoin CIDs (RFC 4648 lower-case,
// no padding).
//
// Reference: https://github.com/multiformats/multibase
var b32dec = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// parseTestVectors parses newline-separated test vectors in the format:
//
//	payload_size,padded_size,commP_CID
//
// This matches the format of go-fil-commp-hashhash/testdata/random.txt.
func parseTestVectors(data string) ([]commPTestVector, error) {
	var vectors []commPTestVector
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			continue
		}
		var v commPTestVector
		if _, err := fmt.Sscanf(parts[0], "%d", &v.payloadSize); err != nil {
			return nil, fmt.Errorf("parsing payload size %q: %w", parts[0], err)
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &v.paddedSize); err != nil {
			return nil, fmt.Errorf("parsing padded size %q: %w", parts[1], err)
		}
		// CID format: 'b' multibase prefix + base32-lower-encoded CIDv1.
		// The raw commP is the last 32 bytes of the decoded CID.
		rawCid, err := b32dec.DecodeString(parts[2][1:]) // [1:] drops the multibase 'b'
		if err != nil {
			return nil, fmt.Errorf("decoding CID %q: %w", parts[2], err)
		}
		copy(v.rawCommP[:], rawCid[len(rawCid)-32:])
		vectors = append(vectors, v)
	}
	return vectors, nil
}

// ---------------------------------------------------------------------------
// Test vector data
//
// These are subsets of go-fil-commp-hashhash/testdata/random.txt, the
// canonical Filecoin commP test fixture. The data corresponding to each
// vector is produced by generateRandomData (seed=1337, jbenet/go-random
// algorithm).
//
// We select vectors whose padded sizes fit within proof.MaxMemtreeSize so we
// can build the full binary tree in memory and generate + verify Merkle proofs
// end-to-end.
//
// Source:
//   https://github.com/filecoin-project/go-fil-commp-hashhash/blob/master/testdata/random.txt
// ---------------------------------------------------------------------------

// smallRandomVectors: payload sizes <= 2032 bytes (padded sizes <= 2048).
// These are fast enough to run on every `go test` invocation.
const smallRandomVectors = `96,128,baga6ea4seaqo4jg2quvjpclvaoicqywb26mnjlme54tkeb3e3ceeahbyg7tviai
126,128,baga6ea4seaqein3inkr73gcpqxdhjy56wfkb52pdw23hpuwr35csiiso2yuvyoq
127,128,baga6ea4seaqmqqwwyg6ql6scq5vvunlbvddhjxwzqsxcdnig4vqbuyh6bbzxodi
192,256,baga6ea4seaqotbhavqkao7eoxjtixti2glidgwvxp3chvidrlf52l7vaknwweky
253,256,baga6ea4seaqebawcodewpyhscsdthmmtvgqkzogca45eg6v7lafkt4tv434l6iq
254,256,baga6ea4seaqpt3iccipvltny4uvpd37qg6w5u7wbng7lqbqplbp7t4amwgvmefi
255,512,baga6ea4seaqlm52frc6d6xnpfquc75qxq4r5pinbahr2klui4fqs5wxy3doyaaq
256,512,baga6ea4seaqffn52ixzqoaahrdl7pahrljpvy45xaosmt35ty53gyvai6t456ni
384,512,baga6ea4seaqnory3ske3s5l25ykklefj5ph4ymer6bt42hmw55rme37jkkys6li
507,512,baga6ea4seaqjplkgedsxnqoygmbx2iqvqog4w2scn2dzpxw26iggia357jveuka
508,512,baga6ea4seaqidun3iro3tn65tqn3zjfwa2j2bltromfptcdqa5stlkjh5q4qeaa
509,1024,baga6ea4seaqky7sahpzp37csmkswze43zbpyzrpdli5mxzy7k5pflwridmnkgmi
512,1024,baga6ea4seaqiqj7rbfe4pz6b7rcbf3dv6xfnnr2dwbwotr6xitwec5mrgslgcea
768,1024,baga6ea4seaqp47zaubuxxi6sdjd7qdr5wzcutem2stj5pec3g554ync227336cy
1015,1024,baga6ea4seaqedz5ubjpdwgfm72tn6ejrnejs2wcvhycf3enyy6ygzwu4pulwijy
1016,1024,baga6ea4seaqnvlx4oaqkcjrog6v27jpouto6rbsymqvljftyxitm7jfalzpmwcq
1017,2048,baga6ea4seaqhx2zkltpeqwlnkvxfu2auzwv6xeqk35gd2keih65iocpjnrsqeoi
1024,2048,baga6ea4seaqjvo24o24lunggsgxp2ykw2nl6ub5oshkgyccaiscbiu7dixiwiga
1536,2048,baga6ea4seaqakth6qnuusqztecom6tlwcbp2m5a3gecgomgnc2z6qxq3fe4jggy
2031,2048,baga6ea4seaqnk4aqvhnzrlkxas5buothhuplfecjo2lvvj3bwn6j27mdj2hw6lq
2032,2048,baga6ea4seaqiqu4swshjvltped5eygatfnwt4rjnxfizqlsnlt2x4kxqpflmsay`

// largerRandomVectors: payload sizes > 2032 up to 131072 bytes (padded up to
// 131072). These are still within proof.MaxMemtreeSize but take longer to
// process, so they are skipped by `go test -short`.
const largerRandomVectors = `2033,4096,baga6ea4seaqfvubqe7mwtotokoo3tkephbpi3xoppi7425icaqvxybj27wm2oka
2048,4096,baga6ea4seaqj5uibbzvsimuiiy6jl2hfhhyl4jzr6ihsiwhqjpfemj2qo7q42bq
3072,4096,baga6ea4seaqkpxrivvwqvuyi4ofwcgtq3hzkyfmjjlb56p77mb2tbn6pmumlany
4063,4096,baga6ea4seaqbderkod5u2oipurx4pnnzwpmbzgnsju3jgczuqivyujfgtixj6iy
4064,4096,baga6ea4seaqn6j5vf3udiid4rbb6rv2ek2s7o3dgnlfvamb3dt5qe6o6x2pnoka
4065,8192,baga6ea4seaqaej5ao5dat4biovfbx2h2exoivsoe4bwbhhajnop4tauoue7tyoa
4096,8192,baga6ea4seaqfc5qcik7yruh4gzadmgrdxtyjoqyxabv5545okfshdrkh4ikr2ba
6144,8192,baga6ea4seaqlf6nkbnjvxinjggutsjkow7dbxussjj3puax2rhzasdhxv4dn6mq
8127,8192,baga6ea4seaqpaqnhn4rce6gsdimm5vl5v6izkh5f6lagbeheoiudqodky45oiaa
8128,8192,baga6ea4seaqotwrimtqoatryhi3zmcjvykr3uyybsx6n5fmdq2wgo6zw7nz3kiq
8129,16384,baga6ea4seaqjkxyydkhqeqzgtf3vsmwnsryyqnchnsytfnanfnnqevirf4cpopy
8192,16384,baga6ea4seaqbmiwvd7oeq5nse5xqy65woqosxoceskcxwsbbyn3pnxydmqztmpi
12288,16384,baga6ea4seaqmtdtep5xkmkavglidtslgcsjy32ej54ib2riarjy62x52kze2ihy
16255,16384,baga6ea4seaqmguzq2wp33mxdia3wq3vaahijqcw4svwbga2abgeccofvf6u5cmy
16256,16384,baga6ea4seaqdborfbr4mlgghf4ud6vo7wjkgmvc3elanqh4miez4wd7xfprl6cy
16257,32768,baga6ea4seaqhtxok7ntvuzk42rh6lmnvwioqvtenrp7tymumicoaelabizfnmha
16384,32768,baga6ea4seaqbq56qsxemtyzd3zcsjoetht7swqir7rw4fpmn6g6cfngz2djxoji
32512,32768,baga6ea4seaqhfhque5zrqd3tg62uo2ruka2lzekpn46ahlsgl3nat4uoojuyemq
32513,65536,baga6ea4seaqizdw52alqml4tyt37bqtuqisewhpz64fvlmryg7ehvdibkptqmga
32768,65536,baga6ea4seaqaxucpakjt27aafpdbrecklvae7kxj3ha6dpdo45rpm7wjzls7sdy
65024,65536,baga6ea4seaqgjaavyognzadlkkcwaovy4ij7grb3eso65dqvwkiqfg5g3cb3qbi
65025,131072,baga6ea4seaqapw7q3joywtwplratzjnyzxfsi5nuemcgtyqapmd35sdyjtnfkpa
65536,131072,baga6ea4seaqarj3zpcpxjs256fzt2zmral6cy2g6wqwtvcawfayhql7twnrgioa`

// ---------------------------------------------------------------------------
// Common test-size tables
// ---------------------------------------------------------------------------

// smallUnpaddedSizes are unpadded piece sizes whose padded forms are small
// enough to build full memtrees and verify every leaf position exhaustively.
// They also happen to be the exact sizes for which FR32 padding produces the
// most compact power-of-two padded pieces (unpadded = padded * 127/128).
var smallUnpaddedSizes = []abi.UnpaddedPieceSize{127, 254, 508, 1016}

// wideUnpaddedSizes span the range from the smallest to the largest piece that
// fits in proof.MaxMemtreeSize. Used for roundtrip proof/verify tests where
// exhaustive leaf enumeration would be too slow.
var wideUnpaddedSizes = []abi.UnpaddedPieceSize{
	127,      // 4 leaves
	254,      // 8 leaves
	508,      // 16 leaves
	1016,     // 32 leaves
	2032,     // 64 leaves
	4064,     // 128 leaves
	8128,     // 256 leaves
	16256,    // 512 leaves
	32512,    // 1024 leaves
	65024,    // 2048 leaves
	130048,   // 4096 leaves
	1040384,  // 32768 leaves
	2080768,  // 65536 leaves
	4161536,  // 131072 leaves
	8323072,  // 262144 leaves
	16646144, // 524288 leaves
}

// ---------------------------------------------------------------------------
// zeroReader - infinite zero-byte source
// ---------------------------------------------------------------------------

// zeroReader is an io.Reader that emits zero bytes forever. It is used to
// pad payloads shorter than their declared unpadded piece size, matching how
// Filecoin pads under-sized pieces with trailing zeros.
type zeroReader struct{}

func (z *zeroReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
