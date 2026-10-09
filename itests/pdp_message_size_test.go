package itests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/pdp"

	"github.com/filecoin-project/lotus/chain/messagepool"
)

// TestPDPFilecoinMessageSizeMatchesLotus pins pdp.MaxFilecoinMessageSize to Lotus.
// pdp copies MaxMessageSize instead of importing messagepool, which pulls
// filecoin-ffi and breaks the PDP no-CGO build.
func TestPDPFilecoinMessageSizeMatchesLotus(t *testing.T) {
	require.Equal(t, messagepool.MaxMessageSize, pdp.MaxFilecoinMessageSize)
}
