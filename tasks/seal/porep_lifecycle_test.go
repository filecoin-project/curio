package seal

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"

	"github.com/filecoin-project/lotus/chain/types"
)

// Characterize production dispatch guards; the task-adder callback is counted,
// not executed. SQL task deletion/reference retention is tested separately.
func TestPoRepTerminalReferenceDoesNotRestartOrDrain(t *testing.T) {
	s := &SealPoller{}
	calls := make(map[int]int)
	for _, kind := range []int{pollerPoRep, pollerFinalize, pollerMoveStorage} {
		s.pollers[kind].Set(func(func(harmonytask.TaskID, *harmonydb.Tx) (bool, error)) { calls[kind]++ })
	}
	row := pollTask{
		AfterSDR: true, AfterTreeD: true, AfterTreeC: true, AfterTreeR: true,
		AfterSynth: true, AfterPrecommitMsg: true, AfterPrecommitMsgSuccess: true,
		SeedEpoch: sql.NullInt64{Int64: 1, Valid: true},
		TaskPoRep: sql.NullInt64{Int64: 1, Valid: true},
	}
	ctx := context.Background()
	ts := porepLifecycleTipSet(t)
	s.pollStartPoRep(ctx, row, ts)
	s.pollStartFinalize(ctx, row, ts)
	s.pollStartMoveStorage(ctx, row)
	require.Empty(t, calls, "retained terminal reference must not be treated as NULL or successful PoRep")

	// Positive controls prove the fixture meets all unrelated prerequisites.
	row.TaskPoRep.Valid = false
	s.pollStartPoRep(ctx, row, ts)
	require.Equal(t, 1, calls[pollerPoRep])
	row.AfterPoRep = true
	s.pollStartFinalize(ctx, row, ts)
	require.Equal(t, 1, calls[pollerFinalize])
	row.AfterFinalize = true
	s.pollStartMoveStorage(ctx, row)
	require.Equal(t, 1, calls[pollerMoveStorage])
}

// Metadata-only fixture, independent of the PreCommit topic's test helpers.
func porepLifecycleTipSet(t *testing.T) *types.TipSet {
	t.Helper()
	miner, err := address.NewIDAddress(1000)
	require.NoError(t, err)
	root, err := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	require.NoError(t, err)
	ts, err := types.NewTipSet([]*types.BlockHeader{{
		Miner: miner, Height: 10, Ticket: &types.Ticket{VRFProof: []byte{1}},
		ParentStateRoot: root, Messages: root, ParentMessageReceipts: root,
		BlockSig:     &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate: &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:    1, ParentBaseFee: types.NewInt(100),
	}})
	require.NoError(t, err)
	return ts
}
