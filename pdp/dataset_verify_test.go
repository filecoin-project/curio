package pdp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayerFromCreateExtraDataSimple(t *testing.T) {
	t.Parallel()

	// Minimal valid ABI encoding is complex; verify combined path delegates.
	_, err := PayerFromCreateExtraData(nil)
	require.Error(t, err)
}

func TestParseUint64Decimal(t *testing.T) {
	t.Parallel()

	v, err := parseUint64Decimal("12345")
	require.NoError(t, err)
	require.Equal(t, uint64(12345), v)

	_, err = parseUint64Decimal("-1")
	require.Error(t, err)
}
