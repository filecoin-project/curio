package hashspacesolver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func sliceOK(t *testing.T, r Range, start, end []byte) int64 {
	t.Helper()
	n, err := SliceSize(r, start, end)
	require.NoError(t, err)
	return n
}

func TestSliceSizeLinear(t *testing.T) {
	r := Range{StartHash: h(0x00), EndHash: h(0x40), Size: 64}
	require.Equal(t, int64(64), sliceOK(t, r, h(0x00), h(0x40)))
	require.Equal(t, int64(0), sliceOK(t, r, h(0x00), h(0x00)))
	require.Equal(t, int64(32), sliceOK(t, r, h(0x00), h(0x20)))
	require.Equal(t, int64(16), sliceOK(t, r, h(0x00), h(0x10)))
}

func TestSliceSizeSuffix(t *testing.T) {
	r := Range{StartHash: h(0x00), EndHash: h(0x40), Size: 64}
	require.Equal(t, int64(32), sliceOK(t, r, h(0x20), h(0x40)))
	require.Equal(t, int64(0), sliceOK(t, r, h(0x40), h(0x40)))
}

func TestSliceSizeMiddle(t *testing.T) {
	r := Range{StartHash: h(0x00), EndHash: h(0x80), Size: 80}
	require.Equal(t, int64(20), sliceOK(t, r, h(0x20), h(0x40)))
}

func TestSliceSizeWrap(t *testing.T) {
	// (0xC0, 0x40] is 128 hash units; half is 0x00.
	r := Range{StartHash: h(0xC0), EndHash: h(0x40), Size: 128}
	require.Equal(t, int64(128), sliceOK(t, r, h(0xC0), h(0x40)))
	require.Equal(t, int64(64), sliceOK(t, r, h(0xC0), h(0x00)))
	require.Equal(t, int64(64), sliceOK(t, r, h(0x00), h(0x40)))
}

func TestSliceSizeFullCircle(t *testing.T) {
	r := Range{StartHash: h(0x80), EndHash: h(0x80), Size: 80}
	require.Equal(t, int64(80), sliceOK(t, r, h(0x80), h(0x80)))
	require.Equal(t, int64(40), sliceOK(t, r, h(0x80), splitHash(r, 40)))
	require.Equal(t, int64(40), sliceOK(t, r, h(0x00), h(0x80)))
}

func TestSliceSizeRejectsWindowOffRange(t *testing.T) {
	r := Range{StartHash: h(0x20), EndHash: h(0x40), Size: 10}
	_, err := SliceSize(r, h(0x10), h(0x30))
	require.Error(t, err)
	_, err = SliceSize(r, h(0x30), h(0x50))
	require.Error(t, err)
	_, err = SliceSize(r, h(0x38), h(0x30))
	require.Error(t, err)
	_, err = SliceSize(Range{EndHash: h(0x40), Size: 10}, h(0x20), h(0x30))
	require.Error(t, err)
}

func TestLinkStarts(t *testing.T) {
	ranges := []Range{
		{EndHash: h(0x30), Size: 1},
		{EndHash: h(0x10), Size: 1},
		{EndHash: h(0xFF), Size: 1},
	}
	LinkStarts(ranges)
	require.Equal(t, h(0x10), ranges[0].StartHash)
	require.Equal(t, h(0xFF), ranges[1].StartHash)
	require.Equal(t, h(0x30), ranges[2].StartHash)
	require.NoError(t, checkTiling(ranges))
}

func TestCheckStructureRejectsBrokenTiling(t *testing.T) {
	st := mk([]int64{10, 10}, []byte{0x40, 0x80}, []int64{1, 1}, []int{0, 1})
	st.HashSpaces[0][1].StartHash = h(0x50)
	require.Error(t, Validate(st))

	st = mk([]int64{10, 10}, []byte{0x40, 0x80}, []int64{1, 1}, []int{0, 1})
	st.HashSpaces[0][0].StartHash = h(0x40)
	require.Error(t, Validate(st))
}
