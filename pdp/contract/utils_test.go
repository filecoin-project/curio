package contract

import (
    "bytes"
    "testing"
)

// TestDecodeAddressCanonical tests canonical decoding of addresses from byte slices.
func TestDecodeAddressCanonical(t *testing.T) {
    // Case 1: Input shorter than 20 bytes → left pad with zeros
    input1 := []byte{0x01, 0x02, 0x03}
    expected1 := make([]byte, 20)
    copy(expected1[20-len(input1):], input1)
    decoded1 := DecodeAddressCanonical(input1)
    if !bytes.Equal(decoded1.Bytes(), expected1) {
        t.Errorf("Case 1 failed: Expected %x, got %x", expected1, decoded1.Bytes())
    }

    // Case 2: Exactly 20 bytes input → use as is
    input2 := []byte{
        0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0,
        0xB0, 0xC0, 0xD0, 0xE0, 0xF0, 0x01, 0x02, 0x03, 0x04, 0x05,
    }
    decoded2 := DecodeAddressCanonical(input2)
    if !bytes.Equal(decoded2.Bytes(), input2) {
        t.Errorf("Case 2 failed: Expected %x, got %x", input2, decoded2.Bytes())
    }

    // Case 3: Input longer than 20 bytes → use last 20 bytes only
    input3 := []byte{
        0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
        0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11, 0x12, 0x13,
        0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D,
    }
    expected3 := input3[len(input3)-20:]
    decoded3 := DecodeAddressCanonical(input3)
    if !bytes.Equal(decoded3.Bytes(), expected3) {
        t.Errorf("Case 3 failed: Expected %x, got %x", expected3, decoded3.Bytes())
    }
}