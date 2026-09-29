package webrpcporep

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/web/api/webrpc"

	"github.com/filecoin-project/lotus/chain/types"
)

type summaryChain struct {
	api.Chain
	calls int
	err   error
}

func (c *summaryChain) ChainHead(context.Context) (*types.TipSet, error) {
	c.calls++
	return &types.TipSet{}, c.err
}

func TestPoRepSummaryFailureIsNotZero(t *testing.T) {
	want := errors.New("chain snapshot unavailable")
	chain := &summaryChain{err: want}
	a := New(&webrpc.Handler{Deps: &deps.Deps{Chain: chain}})
	rows, err := a.PorepPipelineSummary(context.Background())
	require.ErrorIs(t, err, want)
	require.Nil(t, rows)
	require.Equal(t, 1, chain.calls)
}

func TestPoRepSummaryAdditiveWireContract(t *testing.T) {
	wire, err := json.Marshal(PorepPipelineSummary{Actor: "f01000", CountSDR: 12, CountDone: 3,
		SectorCounts: &PoRepSectorCounts{SDRRunning: 2, SDRTotal: 12}})
	require.NoError(t, err)
	var legacy struct {
		Actor               string
		CountSDR, CountDone int
	}
	require.NoError(t, json.Unmarshal(wire, &legacy))
	require.Equal(t, 12, legacy.CountSDR)
	require.Equal(t, 3, legacy.CountDone)
	var modern PorepPipelineSummary
	require.NoError(t, json.Unmarshal(wire, &modern))
	require.Equal(t, int64(2), modern.SectorCounts.SDRRunning)
}
