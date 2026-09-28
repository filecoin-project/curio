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

	// Prefix of a sized range merges and keeps the byte total.
	sized := []rangeRow{{EndHash: h(0x40), StorageID: "a", Size: 40}, {EndHash: h(0xc0), StorageID: "b", Size: 80}}
	out, err = transferRanges(sized, h(0x40), h(0x80), "b", "a")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x80), StorageID: "a", Size: 80},
		{EndHash: h(0xc0), StorageID: "b", Size: 40},
	}, out)

	// Single full-circle range split.
	one := []rangeRow{{EndHash: h(0x80), StorageID: "a"}}
	out, err = transferRanges(one, h(0x80), h(0x10), "a", "b")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x10), StorageID: "b"},
		{EndHash: h(0x80), StorageID: "a"},
	}, out)
}

func TestIntervalOwnedBy(t *testing.T) {
	rs := []rangeRow{{EndHash: h(0x40), StorageID: "a"}, {EndHash: h(0xc0), StorageID: "b"}}

	require.True(t, intervalOwnedBy(rs, h(0x40), h(0xc0), "b"))
	require.True(t, intervalOwnedBy(rs, h(0x60), h(0x80), "b"))
	require.True(t, intervalOwnedBy(rs, h(0xd0), h(0x10), "a"))
	require.False(t, intervalOwnedBy(rs, h(0x20), h(0x60), "b"))
	require.False(t, intervalOwnedBy(rs, h(0x70), h(0x70), "b"))
	require.True(t, intervalOwnedBy([]rangeRow{{EndHash: h(0x80), StorageID: "a"}}, h(0x80), h(0x80), "a"))
	require.Equal(t, []rangeRow{{EndHash: h(0x40), StorageID: "a"}, {EndHash: h(0xc0), StorageID: "b"}}, rs)
}

func TestAssignRange(t *testing.T) {
	rs := []rangeRow{{EndHash: h(0x40), StorageID: "a"}, {EndHash: h(0xc0), StorageID: "b"}}

	// Spanning a boundary takes parts of both owners.
	out, err := assignRange(rs, h(0x20), h(0x60), "c")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x20), StorageID: "a"},
		{EndHash: h(0x60), StorageID: "c"},
		{EndHash: h(0xc0), StorageID: "b"},
	}, out)
	require.Equal(t, []rangeRow{{EndHash: h(0x40), StorageID: "a"}, {EndHash: h(0xc0), StorageID: "b"}}, rs)

	// Wrapping interval across the circle's zero point.
	out, err = assignRange(rs, h(0xa0), h(0x10), "c")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{
		{EndHash: h(0x10), StorageID: "c"},
		{EndHash: h(0x40), StorageID: "a"},
		{EndHash: h(0xa0), StorageID: "b"},
	}, out)

	// Exact existing range to its current owner changes nothing.
	out, err = assignRange(rs, h(0x40), h(0xc0), "b")
	require.NoError(t, err)
	require.Equal(t, rs, out)

	// Covering several ranges merges them under one owner.
	out, err = assignRange(rs, h(0x30), h(0xd0), "a")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{{EndHash: h(0xd0), StorageID: "a"}}, out)

	// Full circle.
	out, err = assignRange(rs, h(0x70), h(0x70), "c")
	require.NoError(t, err)
	require.Equal(t, []rangeRow{{EndHash: h(0x70), StorageID: "c"}}, out)
}
