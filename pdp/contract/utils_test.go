package contract

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestDecodeAddressCapability tests the canonical address decoding.
func TestDecodeAddressCapability(t *testing.T) {
	// This is the expected address for cases 2, 3, and 4
	expectedAddr := common.HexToAddress("0x102030405060708090A0B0C0D0E0F00102030405")

	t.Run("short input", func(t *testing.T) {
		// Matches author's "should pad left to 20 bytes"
		input := []byte{0x01, 0x02, 0x03}
		expected := common.HexToAddress("0x0000000000000000000000000000000000010203")
		decoded := DecodeAddressCapability(input)
		if !bytes.Equal(decoded.Bytes(), expected.Bytes()) {
			t.Errorf("Expected %s, got %s", expected.Hex(), decoded.Hex())
		}
	})

	t.Run("empty input", func(t *testing.T) {
		// Matches author's "decodeAddressCapability('0x')"
		input := []byte{}
		expected := common.HexToAddress("0x0000000000000000000000000000000000000000")
		decoded := DecodeAddressCapability(input)
		if !bytes.Equal(decoded.Bytes(), expected.Bytes()) {
			t.Errorf("Expected %s, got %s", expected.Hex(), decoded.Hex())
		}
	})

	t.Run("exact input", func(t *testing.T) {
		// Matches author's "should decode address unmodified"
		input := []byte{
			0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
			0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05,
		}
		decoded := DecodeAddressCapability(input)
		if !bytes.Equal(decoded.Bytes(), expectedAddr.Bytes()) {
			t.Errorf("Expected %s, got %s", expectedAddr.Hex(), decoded.Hex())
		}
	})

	t.Run("medium input", func(t *testing.T) {
		// Matches author's "should decode address from the low bytes" (21-byte case)
		input := []byte{
			0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, // 6 bytes of padding
			0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
			0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05, // These 20 bytes
		}
		decoded := DecodeAddressCapability(input)
		if !bytes.Equal(decoded.Bytes(), expectedAddr.Bytes()) {
			t.Errorf("Expected %s, got %s", expectedAddr.Hex(), decoded.Hex())
		}
	})

	t.Run("long input", func(t *testing.T) {
		// Matches author's "should decode address from the low bytes" (32-byte case)
		// and correctly handles >32 bytes
		input := []byte{
			// These 12 bytes are the "head" (part of the first 32)
			0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C,
			// These are the 20 bytes we expect
			0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
			0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05,
			// These 8 bytes should be IGNORED (they are after byte 32)
			0xEE, 0xEE, 0xEE, 0xEE, 0xEE, 0xEE, 0xEE, 0xEE,
		}

		// The expected result is the last 20 bytes *of the first 32 bytes*, which is expectedAddr
		decoded := DecodeAddressCapability(input)
		if !bytes.Equal(decoded.Bytes(), expectedAddr.Bytes()) {
			t.Errorf("Expected %s, got %s", expectedAddr.Hex(), decoded.Hex())
		}
	})
}
