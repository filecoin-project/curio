package contract

import (
	"bytes"
	"encoding/hex"
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
