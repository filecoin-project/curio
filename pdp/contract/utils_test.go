package contract

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// helper: decode hex string "0x...." to []byte
func mustHex(s string) []byte {
	if len(s) < 2 {
		return []byte{}
	}
	b, err := hex.DecodeString(s[2:])
	if err != nil {
		panic(err)
	}
	return b
}

func TestDecodeAddressCapability_Unmodified(t *testing.T) {
	tests := []string{
		"0x000000000004444c5dc75cb358380d2e3de08a90",
		"0x4a6f6B9fF1fc974096f9063a45Fd12bD5B928AD1",
	}

	for _, input := range tests {
		got := DecodeAddressCapability(mustHex(input))
		want := common.HexToAddress(input)

		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Errorf("input %s → expected %s, got %s", input, want.Hex(), got.Hex())
		}
	}
}

func TestDecodeAddressCapability_TooLongGivesZero(t *testing.T) {
	input := "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	got := DecodeAddressCapability(mustHex(input))
	want := common.HexToAddress("0x0000000000000000000000000000000000000000")

	if !bytes.Equal(got.Bytes(), want.Bytes()) {
		t.Errorf("expected %s, got %s", want.Hex(), got.Hex())
	}
}

func TestDecodeAddressCapability_FromLowBytes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"0x1234ffffffffffffffff6789ffffffffffffffffffffffffffffffffffffff0f",
			"0xffffffffffffffffffffffffffffffffffffff0f",
		},
		{
			"0x1234eeeeeeeeeeeeee6789eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee0e",
			"0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee0e",
		},
		{
			"0x12dddddddddddddddddddddddddddddddddddddd0d",
			"0xdddddddddddddddddddddddddddddddddddddd0d",
		},
	}

	for _, tc := range tests {
		got := DecodeAddressCapability(mustHex(tc.input))
		want := common.HexToAddress(tc.want)

		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Errorf("input %s → expected %s, got %s", tc.input, want.Hex(), got.Hex())
		}
	}
}

func TestDecodeAddressCapability_PadLeft(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"0x04444c5dc75cb358380d2e3de08a90",
			"0x000000000004444c5dc75cb358380d2e3de08a90",
		},
		{
			"0x",
			"0x0000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tests {
		got := DecodeAddressCapability(mustHex(tc.input))
		want := common.HexToAddress(tc.want)

		if !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Errorf("input %s → expected %s, got %s", tc.input, want.Hex(), got.Hex())
		}
	}
}

func TestOfferingToCapabilities_AdditionalHex(t *testing.T) {
	offering := PDPOfferingData{
		"https://pdp.example.com",
		big.NewInt(32),
		big.NewInt(0x1000000000000000),
		false,
		false,
		[]byte{1, 1, 1, 1, 1, 1, 1, 1, 1},
		big.NewInt(6000),
		big.NewInt(30),
		"narnia",
		common.HexToAddress("0x0000000000004946c0e9F43F4Dee607b0eF1fA1c"),
	}
	additionalCaps := make(map[string]string)
	additionalCaps["coolEndorsement"] = "0xccccaaaaddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddaaaacccc"
	additionalCaps["owner"] = "0x4a6f6B9fF1fc974096f9063a45Fd12bD5B928AD1"
	keys, values, err := OfferingToCapabilities(offering, additionalCaps)
	if err != nil {
		t.Errorf("OfferingToCapabilities returned error %s", err)
	}
	if len(keys) != len(values) {
		t.Errorf("length mismatch: %d keys for %d values", len(keys), len(values))
	}
	capabilities := make(map[string][]byte)
	for i := range len(keys) {
		if len(keys[i]) == 0 {
			t.Errorf("Got empty key at index %d", i)
		}
		if values[i] == nil {
			t.Errorf("Got nil value for key %s", keys[i])
		}
		if _, contains := capabilities[keys[i]]; contains {
			t.Errorf("Got duplicate key %s", keys[i])
		}
		capabilities[keys[i]] = values[i]
	}
	for key, valueStr := range additionalCaps {
		if value, contains := capabilities[key]; !contains {
			t.Errorf("keys: Missing '%s' key from additionalCaps", key)
		} else if expectedLength := (len(valueStr) - 2) / 2; expectedLength != len(value) {
			t.Errorf("Wrong length for hex capability '%s': expected %d actual %d", key, expectedLength, len(value))
		} else if !bytes.Equal(value, mustHex(additionalCaps[key])) {
			t.Errorf("Mismatching value for key '%s': expected %s actual 0x%x", key, valueStr, value)
		}
	}
}

func TestShouldHexEncodeCapability(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantHex bool
	}{
		// Valid UTF-8 text - should NOT be hex encoded
		{
			name:    "ASCII text",
			input:   []byte("hello world"),
			wantHex: false,
		},
		{
			name:    "URL",
			input:   []byte("https://example.com/path?query=value"),
			wantHex: false,
		},
		{
			name:    "Chinese characters",
			input:   []byte("北京"),
			wantHex: false,
		},
		{
			name:    "Japanese characters",
			input:   []byte("東京"),
			wantHex: false,
		},
		{
			name:    "Emoji",
			input:   []byte("🚀🎉"),
			wantHex: false,
		},
		{
			name:    "Mixed Unicode",
			input:   []byte("Hello 世界 🌍"),
			wantHex: false,
		},
		{
			name:    "Empty string",
			input:   []byte(""),
			wantHex: false,
		},

		// Binary data - should be hex encoded
		{
			name:    "Null byte",
			input:   []byte("hello\x00world"),
			wantHex: true,
		},
		{
			name:    "Control character (bell)",
			input:   []byte("hello\x07world"),
			wantHex: true,
		},
		{
			name:    "Control character (tab)",
			input:   []byte("hello\tworld"),
			wantHex: true,
		},
		{
			name:    "Control character (newline)",
			input:   []byte("hello\nworld"),
			wantHex: true,
		},
		{
			name:    "DEL character",
			input:   []byte("hello\x7Fworld"),
			wantHex: true,
		},
		{
			name:    "C1 control character",
			input:   []byte("hello\x80world"),
			wantHex: true,
		},
		{
			name:    "Invalid UTF-8 sequence",
			input:   []byte{0xff, 0xfe},
			wantHex: true,
		},
		{
			name:    "Ethereum address bytes",
			input:   mustHex("0x4a6f6B9fF1fc974096f9063a45Fd12bD5B928AD1"),
			wantHex: true,
		},
		{
			name:    "Certificate-like binary with null",
			input:   []byte{0x30, 0x82, 0x00, 0x00, 0x02, 0x01},
			wantHex: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ShouldHexEncodeCapability(tc.input)
			if got != tc.wantHex {
				t.Errorf("ShouldHexEncodeCapability(%q) = %v, want %v", tc.input, got, tc.wantHex)
			}
		})
	}
}
