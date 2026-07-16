package pdpv0

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ethchain"
)

var failureHandlingTestSeq int64

type recordingAlerting struct {
	events []curioalerting.AlertEvent
}

func (r *recordingAlerting) EmitEvent(_ context.Context, event curioalerting.AlertEvent) error {
	r.events = append(r.events, event)
	return nil
}

func (r *recordingAlerting) ActivateCondition(context.Context, curioalerting.AlertCondition, string) error {
	return nil
}

func (r *recordingAlerting) ResolveCondition(context.Context, curioalerting.AlertCondition) error {
	return nil
}

type getChallengeEpochEthClient struct {
	ethchain.EthClient

	challengeEpoch int64
	calls          int
}

func (m *getChallengeEpochEthClient) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	m.calls++

	uint256Ty, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	return abi.Arguments{{Type: uint256Ty}}.Pack(big.NewInt(m.challengeEpoch))
}

type failureHandlingDataSetState struct {
	UnrecoverableEpoch sql.NullInt64
	ConsecutiveFailure int
	NextProveAttempt   sql.NullInt64
	InitReady          bool
	ProveAtEpoch       sql.NullInt64
	ChallengeHash      sql.NullString
	PrevChallengeEpoch sql.NullInt64
}

func TestIntegration_HandleProvingSendError_UnrecoverableMarksAndSchedulesTermination(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	dataSetID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{
		ConsecutiveFailure: 2,
		NextProveAttempt:   sql.NullInt64{Int64: 3000, Valid: true},
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2000, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000aa", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1000, Valid: true},
	})
	alerts := &recordingAlerting{}
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSDataSetPaymentBeyondEndEpoch))
	currentHeight := int64(4242)

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		err := HandleProvingSendError(ctx, tx, nil, alerts, dataSetID, currentHeight, sendErr, contractErrorSourceProve)
		require.NoError(t, err)
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)

	state := loadFailureHandlingDataSet(t, ctx, db, dataSetID)
	require.Equal(t, failureHandlingDataSetState{
		UnrecoverableEpoch: sql.NullInt64{Int64: currentHeight, Valid: true},
		ConsecutiveFailure: 3,
		InitReady:          false,
		PrevChallengeEpoch: sql.NullInt64{Int64: 1000, Valid: true},
	}, state)
	require.Equal(t, 0, len(alerts.events))
	require.Equal(t, 1, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleProvingSendError_ProveSkipCurrentPeriodResetsFailureState(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		ConsecutiveFailure: 4,
		NextProveAttempt:   sql.NullInt64{Int64: 3100, Valid: true},
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2100, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000bb", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1100, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	alerts := &recordingAlerting{}
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSProofAlreadySubmitted))

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		err := HandleProvingSendError(ctx, tx, nil, alerts, dataSetID, 4300, sendErr, contractErrorSourceProve)
		require.NoError(t, err)
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)

	expectedState := initialState
	expectedState.ConsecutiveFailure = 0
	expectedState.NextProveAttempt = sql.NullInt64{}
	require.Equal(t, expectedState, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Empty(t, alerts.events)
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleProvingSendError_SchedulerSkipCurrentPeriodReconcilesFromChain(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		ConsecutiveFailure: 5,
		NextProveAttempt:   sql.NullInt64{Int64: 3200, Valid: true},
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2200, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000cc", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1200, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	alerts := &recordingAlerting{}
	ethClient := &getChallengeEpochEthClient{challengeEpoch: 5200}
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSNextProvingPeriodAlreadyCalled))
	currentHeight := int64(4400)

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		err := HandleProvingSendError(ctx, tx, ethClient, alerts, dataSetID, currentHeight, sendErr, contractErrorSourceNextPP)
		require.NoError(t, err)
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)

	require.Equal(t, 1, ethClient.calls)
	require.Equal(t, failureHandlingDataSetState{
		ConsecutiveFailure: 0,
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 5200, Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: currentHeight, Valid: true},
	}, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Empty(t, alerts.events)
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleProvingSendError_RetryAndAlertPathsDoNotMutateDataSet(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	tests := []struct {
		name       string
		err        error
		source     string
		wantAlerts int
	}{
		{
			name:   "same proving period retry",
			err:    selectorRevert(contractErrorSelector(ErrFWSSChallengeWindowTooEarly)),
			source: contractErrorSourceProve,
		},
		{
			name:       "insufficient challenge delay alerts",
			err:        selectorRevert(contractErrorSelector(ErrPDPVerifierInsufficientChallengeDelay)),
			source:     contractErrorSourceNextPP,
			wantAlerts: 1,
		},
		{
			name:   "refresh proving state retry",
			err:    selectorRevert(contractErrorSelector(ErrFWSSInvalidChallengeEpoch)),
			source: contractErrorSourceNextPP,
		},
		{
			name:       "unexpected proving invariant alerts",
			err:        selectorRevert(contractErrorSelector(ErrPDPVerifierExcessiveChallengeDelay)),
			source:     contractErrorSourceNextPP,
			wantAlerts: 1,
		},
		{
			name:       "operator attention alerts",
			err:        reasonRevert(provingRevertOnlyStorageProviderCanProve),
			source:     contractErrorSourceProve,
			wantAlerts: 1,
		},
		{
			name:       "proof generation failure alerts",
			err:        reasonRevert(provingRevertProofDidNotVerify),
			source:     contractErrorSourceProve,
			wantAlerts: 1,
		},
		{
			name:       "FWSS proving not started alerts",
			err:        selectorRevert(contractErrorSelector(ErrFWSSProvingNotStarted)),
			source:     contractErrorSourceProve,
			wantAlerts: 1,
		},
		{
			name:       "generic contract revert alerts",
			err:        selectorRevert("deadbeef"),
			source:     contractErrorSourceProve,
			wantAlerts: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initialState := failureHandlingDataSetState{
				ConsecutiveFailure: 6,
				NextProveAttempt:   sql.NullInt64{Int64: 3300, Valid: true},
				InitReady:          true,
				ProveAtEpoch:       sql.NullInt64{Int64: 2300, Valid: true},
				ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000dd", Valid: true},
				PrevChallengeEpoch: sql.NullInt64{Int64: 1300, Valid: true},
			}
			dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
			alerts := &recordingAlerting{}

			committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				handleErr := HandleProvingSendError(ctx, tx, nil, alerts, dataSetID, 4500, tc.err, tc.source)
				require.Equal(t, tc.err, handleErr)
				return true, nil
			}, harmonydb.OptionRetry())
			require.NoError(t, err)
			_ = committed

			require.Equal(t, initialState, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
			require.Len(t, alerts.events, tc.wantAlerts)
			require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
		})
	}
}

func insertFailureHandlingDataSet(t *testing.T, ctx context.Context, db *harmonydb.DB, state failureHandlingDataSetState) int64 {
	t.Helper()

	dataSetID := 900_000_000_000 + atomic.AddInt64(&failureHandlingTestSeq, 1)
	service := fmt.Sprintf("failure-handling-%d", dataSetID)
	pubkey := []byte(service)
	createMessageHash := fmt.Sprintf("0x%064x", dataSetID)

	_, err := db.Exec(ctx, `INSERT INTO pdp_services (pubkey, service_label) VALUES ($1, $2)`, pubkey, service)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO pdp_data_sets (
			id, create_message_hash, service, proving_period, challenge_window,
			init_ready, prove_at_epoch, prev_challenge_request_epoch,
			challenge_request_msg_hash, consecutive_prove_failures,
			next_prove_attempt_at, unrecoverable_proving_failure_epoch
		)
		VALUES ($1, $2, $3, 100, 10, $4, $5, $6, $7, $8, $9, $10)
	`, dataSetID, createMessageHash, service, state.InitReady, nullableInt64Arg(state.ProveAtEpoch),
		nullableInt64Arg(state.PrevChallengeEpoch), nullableStringArg(state.ChallengeHash),
		state.ConsecutiveFailure, nullableInt64Arg(state.NextProveAttempt), nullableInt64Arg(state.UnrecoverableEpoch))
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM pdp_delete_data_set WHERE id = $1`, dataSetID)
		_, _ = db.Exec(ctx, `DELETE FROM pdp_data_sets WHERE id = $1`, dataSetID)
		_, _ = db.Exec(ctx, `DELETE FROM pdp_services WHERE service_label = $1`, service)
	})

	return dataSetID
}

func loadFailureHandlingDataSet(t *testing.T, ctx context.Context, db *harmonydb.DB, dataSetID int64) failureHandlingDataSetState {
	t.Helper()

	var state failureHandlingDataSetState
	err := db.QueryRow(ctx, `
		SELECT unrecoverable_proving_failure_epoch,
			consecutive_prove_failures,
			next_prove_attempt_at,
			init_ready,
			prove_at_epoch,
			challenge_request_msg_hash,
			prev_challenge_request_epoch
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetID).Scan(&state.UnrecoverableEpoch, &state.ConsecutiveFailure, &state.NextProveAttempt,
		&state.InitReady, &state.ProveAtEpoch, &state.ChallengeHash, &state.PrevChallengeEpoch)
	require.NoError(t, err)
	return state
}

func countFailureHandlingDeleteRows(t *testing.T, ctx context.Context, db *harmonydb.DB, dataSetID int64) int {
	t.Helper()

	var count int
	err := db.QueryRow(ctx, `SELECT COUNT(*) FROM pdp_delete_data_set WHERE id = $1`, dataSetID).Scan(&count)
	require.NoError(t, err)
	return count
}

func nullableInt64Arg(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func nullableStringArg(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
