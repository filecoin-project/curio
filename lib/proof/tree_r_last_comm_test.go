package proof

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	poseidondst "github.com/filecoin-project/curio/lib/proof/poseidon"
)

const commRLastGoldenCommRs = `0000000 4a92 c600 5816 2932 d995 49fc 8a3f 72ec
0000010 5b36 a530 9701 95e5 df37 6c24 9fa3 1bfd
0000020 95d7 6f8d 68f2 0977 9215 9bea de51 a35c
0000030 adb0 af57 fe34 10fd 9122 dba4 ec1a 3a54
0000040 fbfa 6411 076c 1cd0 63bf ccfa 6b26 dd46
0000050 b01a d30c 05e3 ef60 cf18 fd50 f340 732d
0000060 b8dc b4d1 3ebd 2a67 8cd9 e529 05cd 2edc
0000070 a709 f06e 8308 87a4 63e1 4efe 9f71 63e6
0000080 46d8 37e9 fe87 146c 6ff2 ee49 0f61 4d43
0000090 1c02 d1c0 a5ad 6962 085e 4daf f65a 5733
00000a0 ae5a e9f4 7d77 313b 4dd7 20ad dd75 84d5
00000b0 8c57 7993 efe7 1538 c92c fa19 d127 29c6
00000c0 6740 6828 7f4d 5ec8 0f05 db31 7599 552b
00000d0 2f2a 609d 0987 eaf0 d6a2 6ac6 96fc 6be4
00000e0 b09c 331c 137f bbb4 a4f4 02ae bd96 27d9
00000f0 b5db 7924 87e8 d35b 7413 e5c9 7367 2c5f`

const commRLastGoldenPaux = `0000000 4199 9de5 a224 58a0 5577 c58a 1afa 25ab
0000010 d16b 3e2a ef88 ee51 f579 8d8b ad80 4eb9
0000020 6bf5 7f7f 8d31 ac88 724d d9e5 fd4e e77d
0000030 dff1 1b50 b098 851a fa64 b44c 3b98 4b9e`

func parseHexdumpU16Words(t *testing.T, s string) []byte {
	t.Helper()

	// The strings in this test are copied from `hexdump` (not `hexdump -C`).
	// Default hexdump prints 16-bit words (`%04x`) using native endianness.
	// On little-endian systems (amd64), a printed word like "4199" corresponds to bytes {0x99, 0x41}.
	var out []byte

	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// fields[0] is the offset, ignore it.
		for _, tok := range fields[1:] {
			if len(tok) != 4 {
				t.Fatalf("unexpected token %q in line %q", tok, line)
			}
			v, err := strconv.ParseUint(tok, 16, 16)
			if err != nil {
				t.Fatalf("parsing token %q: %v", tok, err)
			}
			out = append(out, byte(v), byte(v>>8))
		}
	}

	return out
}

func TestCommRLastFromTreeRLastRoots(t *testing.T) {
	t.Run("1", func(t *testing.T) {
		in := []PoseidonDomain{{0x01}}
		got, err := CommRLastFromTreeRLastRoots(in)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != in[0] {
			t.Fatalf("mismatch")
		}
	})

	t.Run("2", func(t *testing.T) {
		in := []PoseidonDomain{{0x01}, {0x02}}
		want := poseidonHashMulti[poseidondst.Arity2](in)
		got, err := CommRLastFromTreeRLastRoots(in)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != want {
			t.Fatalf("mismatch")
		}
	})

	t.Run("8", func(t *testing.T) {
		in := make([]PoseidonDomain, 8)
		for i := range in {
			in[i][0] = byte(i + 1)
		}
		want := poseidonHashMulti[poseidondst.Arity8](in)
		got, err := CommRLastFromTreeRLastRoots(in)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != want {
			t.Fatalf("mismatch")
		}
	})

	t.Run("16_reduction_8_8_then_2", func(t *testing.T) {
		in := make([]PoseidonDomain, 16)
		for i := range in {
			in[i][0] = byte(i + 1)
		}

		h0 := poseidonHashMulti[poseidondst.Arity8](in[:8])
		h1 := poseidonHashMulti[poseidondst.Arity8](in[8:])
		want := poseidonHashMulti[poseidondst.Arity2]([]PoseidonDomain{h0, h1})

		got, err := CommRLastFromTreeRLastRoots(in)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != want {
			t.Fatalf("mismatch")
		}
	})

	t.Run("golden_8_matches_paux_commRLast", func(t *testing.T) {
		rootsBytes := parseHexdumpU16Words(t, commRLastGoldenCommRs)
		if len(rootsBytes)%32 != 0 {
			t.Fatalf("roots bytes not multiple of 32: %d", len(rootsBytes))
		}
		nRoots := len(rootsBytes) / 32
		roots := make([]PoseidonDomain, nRoots)
		for i := 0; i < nRoots; i++ {
			copy(roots[i][:], rootsBytes[i*32:(i+1)*32])
		}

		pauxBytes := parseHexdumpU16Words(t, commRLastGoldenPaux)
		if len(pauxBytes) != 64 {
			t.Fatalf("expected p_aux to be 64 bytes, got %d", len(pauxBytes))
		}
		var pauxCommRLast PoseidonDomain
		copy(pauxCommRLast[:], pauxBytes[32:64])

		got, err := CommRLastFromTreeRLastRoots(roots)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}

		if got != pauxCommRLast {
			t.Fatalf("commRLast mismatch\ncomputed: %x\np_aux:     %x", got[:], pauxCommRLast[:])
		}

		// Sanity-check expected arity mapping for this fixture (helps catch accidental file-count changes).
		switch nRoots {
		case 1, 2, 4, 8, 16:
		default:
			t.Fatalf("fixture has unsupported root count %d", nRoots)
		}
		if nRoots != 8 {
			t.Fatalf("fixture expected 8 roots (sc-02-data-tree-r-last-0..7), got %d", nRoots)
		}

		// Also confirm golden computation is stable relative to the helper used elsewhere in this package.
		want := poseidonHashMulti[poseidondst.Arity8](roots)
		if got != want {
			t.Fatalf("unexpected: computed value differs from direct arity-8 poseidon (%s)", fmt.Sprintf("%x", want[:]))
		}
	})
}
