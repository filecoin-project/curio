package keysmith

import (
	"bytes"
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

func envelopeBytes(t *testing.T, meta map[string]any) []byte {
	t.Helper()
	protected, err := cbor.Marshal(map[int64]any{
		1:      int64(-65793),
		16:     "application/vnd.filecoin-encryption+cose",
		5:      []byte{1, 2, 3, 4, 5, 6, 7},
		-1:     uint64(262144),
		-65792: meta,
	})
	require.NoError(t, err)
	body, err := cbor.Marshal([]any{protected, map[int64]any{}, nil})
	require.NoError(t, err)
	tagged, err := cbor.Marshal(cbor.RawTag{Number: 16, Content: body})
	require.NoError(t, err)
	return append(tagged, []byte("ciphertext")...)
}

func validMeta() map[string]any {
	return map[string]any{
		"foc/v":    uint64(1),
		"foc/own":  "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266",
		"foc/ks":   "0x00112233445566778899aabbccddeeff",
		"foc/salt": "0x000102030405060708090a0b0c0d0e0f",
	}
}

func TestReadEnvelopeLabels(t *testing.T) {
	meta := validMeta()
	meta["foc/scope"] = "agent-memory"
	labels, err := ReadEnvelopeLabels(bytes.NewReader(makeFileRootCAR(t, envelopeBytes(t, meta))))
	require.NoError(t, err)
	require.Equal(t, int64(1), labels.Version)
	require.Equal(t, meta["foc/own"], labels.Owner)
	require.Equal(t, meta["foc/ks"], labels.Keyspace)
	require.Equal(t, meta["foc/salt"], labels.Salt)
	require.True(t, labels.HasScope)
	require.Equal(t, "agent-memory", labels.Scope)

	_, err = ReadEnvelopeLabels(bytes.NewReader(envelopeBytes(t, validMeta())))
	require.ErrorIs(t, err, ErrInvalidEnvelope)
	require.ErrorContains(t, err, "not a CAR")

	withoutScope := validMeta()
	labels, err = LabelsFromPrefix(envelopeBytes(t, withoutScope))
	require.NoError(t, err)
	require.False(t, labels.HasScope)

	badVersion := validMeta()
	badVersion["foc/v"] = uint64(2)
	_, err = LabelsFromPrefix(envelopeBytes(t, badVersion))
	require.ErrorIs(t, err, ErrInvalidEnvelope)

	missingSalt := validMeta()
	delete(missingSalt, "foc/salt")
	_, err = LabelsFromPrefix(envelopeBytes(t, missingSalt))
	require.ErrorIs(t, err, ErrInvalidEnvelope)

	_, err = LabelsFromPrefix([]byte{0x01})
	require.ErrorIs(t, err, ErrInvalidEnvelope)
}

type failReader struct{ err error }

func (r failReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadEnvelopeLabelsReadsPrefixBeforeTrailingLeaf(t *testing.T) {
	meta := validMeta()
	env := envelopeBytes(t, meta)
	trailer := bytes.Repeat([]byte{0xA5}, 64<<10)
	carBytes := makeTwoLeafFileCAR(t, env, trailer)

	labels, err := ReadEnvelopeLabels(bytes.NewReader(carBytes))
	require.NoError(t, err)
	require.Equal(t, meta["foc/own"], labels.Owner)
}

func TestReadEnvelopeLabelsKeepsIOErrors(t *testing.T) {
	_, err := ReadEnvelopeLabels(failReader{err: errors.New("disk")})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidEnvelope)
	require.ErrorContains(t, err, "disk")
}
