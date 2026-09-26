package hashspacesolver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSliceSizeLinear(t *testing.T) {
	r := Range{EndHash: []byte{0x40}, Size: 64}
	start := []byte{0x00}
	require.Equal(t, int64(64), SliceSize(r, start, []byte{0x40}))
	require.Equal(t, int64(0), SliceSize(r, start, []byte{0x00}))
	require.Equal(t, int64(32), SliceSize(r, start, []byte{0x20}))
	require.Equal(t, int64(16), SliceSize(r, start, []byte{0x10}))
}

func TestSliceSizeWrap(t *testing.T) {
	r := Range{EndHash: []byte{0x40}, Size: 128}
	start := []byte{0xC0}
	// (0xC0, 0x40] is 128 hash units; half is 0x00.
	require.Equal(t, int64(128), SliceSize(r, start, []byte{0x40}))
	require.Equal(t, int64(64), SliceSize(r, start, []byte{0x00}))
}

func TestSliceSizeFullCircle(t *testing.T) {
	r := Range{EndHash: []byte{0x80}, Size: 80}
	start := []byte{0x80}
	require.Equal(t, int64(80), SliceSize(r, start, []byte{0x80}))
	require.Equal(t, int64(40), SliceSize(r, start, splitHash(r, start, 40)))
}

func TestStartHash(t *testing.T) {
	ranges := []Range{
		{EndHash: []byte{0x10}, Size: 1},
		{EndHash: []byte{0x30}, Size: 1},
		{EndHash: []byte{0xFF}, Size: 1},
	}
	require.Equal(t, []byte{0xFF}, StartHash(ranges, 0))
	require.Equal(t, []byte{0x10}, StartHash(ranges, 1))
	require.Equal(t, []byte{0x30}, StartHash(ranges, 2))
}
