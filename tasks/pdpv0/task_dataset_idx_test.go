package pdpv0

import (
	"context"
	"database/sql"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ethchain"
	"github.com/filecoin-project/curio/pdp/contract"
)

type revertingEthClient struct {
	ethchain.EthClient
	err error
}

func (r *revertingEthClient) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return nil, r.err
}

func runDatasetIdxTask(ctx context.Context, db *harmonydb.DB, client ethchain.EthClient) (bool, error) {
	task := NewDatasetIdxTask(db, client)
	return task.Do(ctx, harmonytask.TaskID(1), func() bool { return true })
}

func TestIntegration_DatasetIdxTask_TerminalChainErrorsResolveToFalse(t *testing.T) {
	contract.SetAddresses(contract.Addresses{
		PDPVerifier: common.HexToAddress("0x3333333333333333333333333333333333333333"),
	})

	tests := []struct {
		name string
		err  error
	}{
		{"DataSetNotLive", selectorRevert(contractErrorSelector(ErrPDPVerifierDataSetNotLive))},
		{"DataSetNotFound", selectorRevert(contractErrorSelector(ErrPDPVerifierDataSetNotFound))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := harmonydb.NewFromConfigWithITestID(t)
			require.NoError(t, err)

			dataSetID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{})

			done, doErr := runDatasetIdxTask(ctx, db, &revertingEthClient{err: tc.err})
			require.NoError(t, doErr)
			require.True(t, done)

			var ipfsIndexing sql.NullBool
			require.NoError(t, db.QueryRow(ctx, `SELECT ipfs_indexing FROM pdp_data_sets WHERE id = $1`, dataSetID).Scan(&ipfsIndexing))
			require.True(t, ipfsIndexing.Valid)
			require.False(t, ipfsIndexing.Bool)
		})
	}
}
