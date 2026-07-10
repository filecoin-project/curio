package webrpc

import (
	"math/big"
	"strings"
	"testing"
)

func TestParseSQLNumericInt(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"7387365995246367976.00000000000000000000000000000000", "7387365995246367976"},
		{"0", "0"},
		{"42", "42"},
		{"  100.5  ", "100"},
	}
	for _, tc := range cases {
		got, err := parseSQLNumericInt(tc.in)
		if err != nil {
			t.Fatalf("parseSQLNumericInt(%q): %v", tc.in, err)
		}
		if got.String() != tc.want {
			t.Fatalf("parseSQLNumericInt(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestParseSQLNumericIntInvalid(t *testing.T) {
	_, err := parseSQLNumericInt("not-a-number")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStripEthHexPrefix(t *testing.T) {
	// ltrim('0x', '0x') would also strip leading zeros from the payload.
	in := "0x0000000000000000000000000000000000000000000000000000000000000001" +
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	got := stripEthHexPrefix(in)
	if strings.HasPrefix(got, "0x") || strings.HasPrefix(got, "0X") {
		t.Fatalf("prefix not stripped: %q", got[:8])
	}
	if !strings.HasPrefix(got, "0000") {
		t.Fatalf("leading zeros were corrupted: %q", got[:8])
	}
}

func TestEthLogDataUint256Word1(t *testing.T) {
	// word0=1, word1=1e18 (1 USDFC), remaining words zero
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000001" +
		"0000000000000000000000000000000000000000000000000de0b6b3a7640000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	got, err := ethLogDataUint256Word1(data)
	if err != nil {
		t.Fatal(err)
	}
	want := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	if got.Cmp(want) != 0 {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestFormatUsdfcRejectsCorrupt(t *testing.T) {
	corrupt, ok := new(big.Int).SetString("71081384060025517185193910342595514918874447322926757347065856", 10)
	if !ok {
		t.Fatal("bad fixture")
	}
	// This is already in display units if wrongly treated; as raw wei it is still huge.
	raw := new(big.Int).Mul(corrupt, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	if formatUsdfc(raw) != "—" {
		t.Fatalf("expected em-dash for corrupt value, got %q", formatUsdfc(raw))
	}
	if formatUsdfc(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)) != "1.000" {
		t.Fatalf("expected 1.000 USDFC")
	}
}

func TestEthLogDataUint256Word1Large(t *testing.T) {
	// word1 = 0xffffffffffffffff000000000000000100000000000000020000000000000003
	data := "0x" +
		"0000000000000000000000000000000000000000000000000000000000000001" +
		"ffffffffffffffff000000000000000100000000000000020000000000000003" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000000000000000"
	got, err := ethLogDataUint256Word1(data)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the same way the SQL does: 4×uint64 chunks.
	chunks := []string{
		"ffffffffffffffff",
		"0000000000000001",
		"0000000000000002",
		"0000000000000003",
	}
	want := big.NewInt(0)
	for i, c := range chunks {
		part, ok := new(big.Int).SetString(c, 16)
		if !ok {
			t.Fatalf("bad chunk %q", c)
		}
		shift := uint((3 - i) * 64)
		want.Add(want, new(big.Int).Lsh(part, shift))
	}
	if got.Cmp(want) != 0 {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestProveGasEstimateMath(t *testing.T) {
	avg, err := parseSQLNumericInt("23000000000.000000")
	if err != nil {
		t.Fatal(err)
	}
	total := new(big.Int).Mul(avg, big.NewInt(321021))
	if total.Sign() <= 0 {
		t.Fatal("expected positive total")
	}
}
