package hashspace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func h(b byte) []byte {
	out := make([]byte, HASH_BYTES)
	out[0] = b
	return out
}

func TestTransferRanges(t *testing.T) {
	rs := []rangeRow{{EndHash: h(0x40), StorageID: "a"}, {EndHash: h(0xc0), StorageID: "b"}}

	// Middle of b's range (0x40, 0xc0] moves to c.
	out, err := transferRanges(rs, h(0x60), h(0x80), "b", "c")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x40), StorageID: "a"},
		{EndHash: h(0x60), StorageID: "b"},
		{EndHash: h(0x80), StorageID: "c"},
		{EndHash: h(0xc0), StorageID: "b"},
	}, out)

	// Prefix of b moves to a and merges with a's range.
	out, err = transferRanges(rs, h(0x40), h(0x80), "b", "a")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x80), StorageID: "a"},
		{EndHash: h(0xc0), StorageID: "b"},
	}, out)

	// Wrapping range of a (0xc0, 0x40] moves wholly to c.
	out, err = transferRanges(rs, h(0xc0), h(0x40), "a", "c")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x40), StorageID: "c"},
		{EndHash: h(0xc0), StorageID: "b"},
	}, out)

	// Wrong owner is rejected.
	_, err = transferRanges(rs, h(0x60), h(0x80), "a", "c")
	require.Error(t, err)

	// Single full-circle range split.
	one := []rangeRow{{EndHash: h(0x80), StorageID: "a"}}
	out, err = transferRanges(one, h(0x80), h(0x10), "a", "b")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x10), StorageID: "b"},
		{EndHash: h(0x80), StorageID: "a"},
	}, out)
}
