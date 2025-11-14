package contract

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestDecodeAddressCapability tests the canonical address decoding.
func TestDecodeAddressCapability(t *testing.T) {
	// Case 1: Input shorter than 20 bytes → left pad with zeros
	input1 := []byte{0x01, 0x02, 0x03}
	// Use HexToAddress for string constants
	expected1 := common.HexToAddress("0x0000000000000000000000000000000000010203")
	decoded1 := DecodeAddressCapability(input1)
	if !bytes.Equal(decoded1.Bytes(), expected1.Bytes()) {
		t.Errorf("Case 1 (short) failed: Expected %s, got %s", expected1.Hex(), decoded1.Hex())
	}

	// Case 2: Exactly 20 bytes input → use as is
	input2 := []byte{
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
		0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05,
	}
	// Use HexToAddress for string constants
	expected2 := common.HexToAddress("0x102030405060708090A0B0C0D0E0F00102030405")
	decoded2 := DecodeAddressCapability(input2)
	if !bytes.Equal(decoded2.Bytes(), expected2.Bytes()) {
		t.Errorf("Case 2 (exact) failed: Expected %s, got %s", expected2.Hex(), decoded2.Hex())
	}

	// Case 3: Input > 20 bytes but <= 32 bytes → use last 20 bytes
	input3 := []byte{
		0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, // 6 bytes of padding
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
		0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05, // These 20 bytes
	}
	// expected is the same as case 2
	decoded3 := DecodeAddressCapability(input3)
	if !bytes.Equal(decoded3.Bytes(), expected2.Bytes()) {
		t.Errorf("Case 3 (medium) failed: Expected %s, got %s", expected2.Hex(), decoded3.Hex())
	}

	// Case 4: Input > 32 bytes → take first 32, then last 20 from that
	input4 := []byte{
		// These 12 bytes are the "head" (part of the first 32)
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C,
		// These are the 20 bytes we expect
		0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
		0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05,
		// These 8 bytes should be IGNORED (they are after byte 32)
		0xEE, 0xEE, 0xEE, 0xEE, 0xEE, 0xEE, 0xEE, 0xEE,
	}
	// The expected result is the last 20 bytes *of the first 32 bytes*
	// Use HexToAddress for string constants
	expected4 := common.HexToAddress("0x030405060708090A0B0C102030405060708090A0")
	decoded4 := DecodeAddressCapability(input4)
	if !bytes.Equal(decoded4.Bytes(), expected4.Bytes()) {
		t.Errorf("Case 4 (long) failed: Expected %s, got %s", expected4.Hex(), decoded4.Hex())
	}
}