package types

import (
	"bytes"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestPdpIpniContextFlags(t *testing.T) {
	pcid, err := cid.Parse("bafkzcibfxx3meais3xzh6qn56y6hiasmrufhegoweu3o5ccofs74nfdfr4yn76pqz4pq")
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		flags     PdpIpniFlags
		wireByte  byte
		payload   bool
		encrypted bool
	}{
		{name: "legacy piece", flags: 0, wireByte: 0x00},
		{name: "legacy payload", flags: PDP_IPNI_FLAG_PAYLOAD, wireByte: 0x01, payload: true},
		{name: "encrypted piece", flags: PDP_IPNI_FLAG_ENCRYPTED, wireByte: 0x02, encrypted: true},
		{name: "encrypted payload", flags: PDP_IPNI_FLAG_PAYLOAD | PDP_IPNI_FLAG_ENCRYPTED, wireByte: 0x03, payload: true, encrypted: true},
		{name: "unknown flag", flags: 0x80, wireByte: 0x80},
		{name: "all flags", flags: 0xff, wireByte: 0xff, payload: true, encrypted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := append([]byte{tc.wireByte}, pcid.Bytes()...)
			original := PdpIpniContext{PieceCID: pcid, Flags: tc.flags}
			encoded, err := original.Marshal()
			require.NoError(t, err)
			require.Equal(t, want, encoded)

			var decoded PdpIpniContext
			require.NoError(t, decoded.Unmarshal(want))
			require.Equal(t, pcid, decoded.PieceCID)
			require.Equal(t, tc.flags, decoded.Flags)
			require.Equal(t, tc.payload, decoded.Payload())
			require.Equal(t, tc.encrypted, decoded.Encrypted())

			roundTrip, err := decoded.Marshal()
			require.NoError(t, err)
			require.Equal(t, want, roundTrip)
		})
	}
}

func TestPdpIpniContextUnmarshalInvalid(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire []byte
	}{
		{name: "empty"},
		{name: "flags only", wire: []byte{0x03}},
		{name: "invalid CID", wire: []byte{0x03, 0xff}},
		{name: "too long", wire: make([]byte, 65)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var decoded PdpIpniContext
			require.Error(t, decoded.Unmarshal(tc.wire))
		})
	}
}

func TestPdpIpniContextLengthLimit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		digestSize byte
		tooLong    bool
	}{
		{name: "63 byte CID", digestSize: 59},
		{name: "64 byte CID", digestSize: 60, tooLong: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// CIDv1, raw codec, identity multihash, one-byte digest length.
			cidBytes := append([]byte{0x01, 0x55, 0x00, tc.digestSize}, bytes.Repeat([]byte{0x42}, int(tc.digestSize))...)
			pcid, err := cid.Cast(cidBytes)
			require.NoError(t, err)

			original := PdpIpniContext{PieceCID: pcid, Flags: PDP_IPNI_FLAG_PAYLOAD | PDP_IPNI_FLAG_ENCRYPTED}
			encoded, err := original.Marshal()
			if tc.tooLong {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, encoded, 64)
			var decoded PdpIpniContext
			require.NoError(t, decoded.Unmarshal(encoded))
			require.Equal(t, original, decoded)
		})
	}
}
