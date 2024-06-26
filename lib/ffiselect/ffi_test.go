package ffiselect

import (
	"context"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestSelf(t *testing.T) {
	testCID := cid.NewCidV1(1, []byte("test"))
	cid, err := FFISelect.SelfTest(context.Background(), 12345678, testCID)
	require.NoError(t, err)
	require.Equal(t, testCID, cid)
}
