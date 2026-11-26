package pdpv0

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/alertmanager/curioalerting"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/pdp/contract"

	chainTypes "github.com/filecoin-project/lotus/chain/types"
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

type failureHandlingDataSetState struct {
	UnrecoverableEpoch sql.NullInt64
	ProvingRetryEpoch  sql.NullInt64
	InitReady          bool
	ProveAtEpoch       sql.NullInt64
	ChallengeHash      sql.NullString
	PrevChallengeEpoch sql.NullInt64
}

func TestIntegration_HandleProveSendError_UnrecoverableActionMarksAndSchedulesTermination(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	dataSetID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2000, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000aa", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1000, Valid: true},
	})
	alerts := &recordingAlerting{}
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSDataSetPaymentBeyondEndEpoch))
	currentHeight := int64(4242)

	action := handleProveSendError(alerts, dataSetID, currentHeight, sendErr)
	require.Equal(t, proveSendErrorTerminateDataset, action)

	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		return true, markDatasetProvingUnrecoverableAndTerminate(tx, dataSetID, currentHeight)
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)

	state := loadFailureHandlingDataSet(t, ctx, db, dataSetID)
	require.Equal(t, failureHandlingDataSetState{
		UnrecoverableEpoch: sql.NullInt64{Int64: currentHeight, Valid: true},
		InitReady:          false,
		PrevChallengeEpoch: sql.NullInt64{Int64: 1000, Valid: true},
	}, state)
	require.Equal(t, 0, len(alerts.events))
	require.Equal(t, 1, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleProveSendError_SkipCurrentPeriodLeavesDatasetStateUnchanged(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2100, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000bb", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1100, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	alerts := &recordingAlerting{}
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSProofAlreadySubmitted))

	action := handleProveSendError(alerts, dataSetID, 4300, sendErr)
	require.Equal(t, proveSendErrorCompleteTask, action)

	require.Equal(t, initialState, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Empty(t, alerts.events)
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleProveSendError_FWSSProvingNotStartedResetsToInitPP(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          false,
		ProveAtEpoch:       sql.NullInt64{Int64: 2150, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000ee", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1150, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	alerts := &recordingAlerting{}
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSProvingNotStarted))

	action := handleProveSendError(alerts, dataSetID, 4350, sendErr)
	require.Equal(t, proveSendErrorResetToInitPP, action)
	require.Len(t, alerts.events, 1)
	require.Equal(t, map[string]string{curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetID, 10)}, alerts.events[0].Context)

	require.NoError(t, resetDatasetToInitPP(ctx, db, dataSetID))
	require.Equal(t, failureHandlingDataSetState{
		InitReady: true,
	}, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleNextProvingPeriodSendError_SkipCurrentPeriodReconcilesFromChain(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2200, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000cc", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1200, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	var calls int
	oldGetNextChallengeEpoch := getProvingScheduleNextChallengeWindowStart
	getProvingScheduleNextChallengeWindowStart = func(_ context.Context, _ *contract.IPDPProvingSchedule, gotDataSetID int64) (*big.Int, error) {
		calls++
		require.Equal(t, dataSetID, gotDataSetID)
		return big.NewInt(5200), nil
	}
	t.Cleanup(func() {
		getProvingScheduleNextChallengeWindowStart = oldGetNextChallengeEpoch
	})
	sendErr := selectorRevert(contractErrorSelector(ErrFWSSNextProvingPeriodAlreadyCalled))
	currentHeight := int64(4400)

	var alert bool
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var err error
		alert, err = handleNextProvingPeriodSendError(ctx, tx, nil, alertNameNextPP, dataSetID, currentHeight, sendErr)
		require.NoError(t, err)
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)

	require.Equal(t, 1, calls)
	require.Equal(t, failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 5200, Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: currentHeight, Valid: true},
	}, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.False(t, alert)
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleSplitProvingSendErrors_RetryAndAlertPathsDoNotMutateDataSet(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	nextPPSendError := func(tx *harmonydb.Tx, alerts *recordingAlerting, dataSetID int64, sendErr error) error {
		alert, err := handleNextProvingPeriodSendError(ctx, tx, nil, alertNameNextPP, dataSetID, 4500, sendErr)
		if alert {
			emitProvingSendErrorAlert(alerts, alertNameNextPP, dataSetID, sendErr)
		}
		return err
	}

	tests := []struct {
		name       string
		err        error
		handle     func(tx *harmonydb.Tx, alerts *recordingAlerting, dataSetID int64, sendErr error) error
		wantAlerts int
	}{
		{
			name: "same proving period retry",
			err:  selectorRevert(contractErrorSelector(ErrFWSSChallengeWindowTooEarly)),
			handle: func(tx *harmonydb.Tx, alerts *recordingAlerting, dataSetID int64, sendErr error) error {
				require.Equal(t, proveSendErrorRetryTask, handleProveSendError(alerts, dataSetID, 4500, sendErr))
				return sendErr
			},
		},
		{
			name:       "insufficient challenge delay alerts",
			err:        selectorRevert(contractErrorSelector(ErrPDPVerifierInsufficientChallengeDelay)),
			wantAlerts: 1,
			handle:     nextPPSendError,
		},
		{
			name:   "refresh proving state retry",
			err:    selectorRevert(contractErrorSelector(ErrFWSSInvalidChallengeEpoch)),
			handle: nextPPSendError,
		},
		{
			name:       "operator attention alerts",
			err:        reasonRevert(provingRevertOnlyStorageProviderCanProve),
			wantAlerts: 1,
			handle: func(tx *harmonydb.Tx, alerts *recordingAlerting, dataSetID int64, sendErr error) error {
				require.Equal(t, proveSendErrorRetryTask, handleProveSendError(alerts, dataSetID, 4500, sendErr))
				return sendErr
			},
		},
		{
			name:       "proof generation failure alerts",
			err:        reasonRevert(provingRevertProofDidNotVerify),
			wantAlerts: 1,
			handle: func(tx *harmonydb.Tx, alerts *recordingAlerting, dataSetID int64, sendErr error) error {
				require.Equal(t, proveSendErrorRetryTask, handleProveSendError(alerts, dataSetID, 4500, sendErr))
				return sendErr
			},
		},
		{
			name:       "generic contract revert alerts",
			err:        selectorRevert("deadbeef"),
			wantAlerts: 1,
			handle: func(tx *harmonydb.Tx, alerts *recordingAlerting, dataSetID int64, sendErr error) error {
				require.Equal(t, proveSendErrorRetryTask, handleProveSendError(alerts, dataSetID, 4500, sendErr))
				return sendErr
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initialState := failureHandlingDataSetState{
				InitReady:          true,
				ProveAtEpoch:       sql.NullInt64{Int64: 2300, Valid: true},
				ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000dd", Valid: true},
				PrevChallengeEpoch: sql.NullInt64{Int64: 1300, Valid: true},
			}
			dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
			alerts := &recordingAlerting{}

			committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
				handleErr := tc.handle(tx, alerts, dataSetID, tc.err)
				require.Equal(t, tc.err, handleErr)
				return true, nil
			}, harmonydb.OptionRetry())
			require.NoError(t, err)
			_ = committed

			require.Equal(t, initialState, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
			require.Len(t, alerts.events, tc.wantAlerts)
			for _, event := range alerts.events {
				require.Equal(t, map[string]string{curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetID, 10)}, event.Context)
			}
			require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
		})
	}
}

func TestIntegration_HandleNextProvingPeriodSendFailure_UnknownRevertDefersRecheck(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "unclassified contract revert",
			err:  selectorRevert("deadbeef"),
		},
		{
			name: "unexpected proving invariant",
			err:  selectorRevert(contractErrorSelector(ErrPDPVerifierExcessiveChallengeDelay)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initialState := failureHandlingDataSetState{
				InitReady:          true,
				ProveAtEpoch:       sql.NullInt64{Int64: 2300, Valid: true},
				ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000dd", Valid: true},
				PrevChallengeEpoch: sql.NullInt64{Int64: 1300, Valid: true},
			}
			dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
			currentHeight := int64(4700)

			err := handleNextProvingPeriodSendFailure(ctx, db, nil, curioalerting.NewAlertingSystem(db), alertNameNextPP, dataSetID, currentHeight, tc.err)
			require.NoError(t, err)

			var eventContext []byte
			err = db.QueryRow(ctx, `
				SELECT context
				FROM alert_history
				WHERE kind = 'event' AND system = $1 AND subsystem = $2
				  AND context->>'dataset' = $3
			`, alertType, alertNameNextPP, strconv.FormatInt(dataSetID, 10)).Scan(&eventContext)
			require.NoError(t, err)
			require.JSONEq(t, fmt.Sprintf(`{"dataset": "%d"}`, dataSetID), string(eventContext))

			held := initialState
			held.ProvingRetryEpoch = sql.NullInt64{Int64: currentHeight + provingRecheckDelayEpochs, Valid: true}
			require.Equal(t, held, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
			require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
		})
	}
}

func TestIntegration_HandleNextProvingPeriodPreflight_UnknownRevertDefersRecheck(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2300, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000dd", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1300, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	currentHeight := int64(4701)
	alerts := &recordingAlerting{}
	task := &NextProvingPeriodTask{db: db, al: alerts}

	done, err := task.handleNextProvingPeriodPreflightError(ctx, dataSetID, currentHeight, selectorRevert("deadbeef"))
	require.NoError(t, err)
	require.True(t, done)
	require.Len(t, alerts.events, 1)
	require.Equal(t, map[string]string{curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetID, 10)}, alerts.events[0].Context)

	held := initialState
	held.ProvingRetryEpoch = sql.NullInt64{Int64: currentHeight + provingRecheckDelayEpochs, Valid: true}
	require.Equal(t, held, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleInitProvingPeriodPreflight_UnknownRevertDefersRecheck(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2300, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000dd", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1300, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	currentHeight := int64(4702)
	alerts := &recordingAlerting{}

	done, err := handleProvingPeriodPreflightError(ctx, db, alerts, alertNameInitPP, dataSetID, currentHeight, selectorRevert("deadbeef"))
	require.NoError(t, err)
	require.True(t, done)
	require.Len(t, alerts.events, 1)
	require.Equal(t, alertNameInitPP, alerts.events[0].Subsystem)
	require.Equal(t, map[string]string{curioalerting.CONTEXT_KEY_DATASET: strconv.FormatInt(dataSetID, 10)}, alerts.events[0].Context)

	held := initialState
	held.ProvingRetryEpoch = sql.NullInt64{Int64: currentHeight + provingRecheckDelayEpochs, Valid: true}
	require.Equal(t, held, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleNextProvingPeriodSendError_UnsettledLockupDefersProving(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2000, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000aa", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1000, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	sendErr := selectorRevert(contractErrorSelector(ErrPaymentsLockupNotSettledRateChangeNotAllowed))
	currentHeight := int64(4600)

	var alert bool
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var handleErr error
		alert, handleErr = handleNextProvingPeriodSendError(ctx, tx, nil, alertNameNextPP, dataSetID, currentHeight, sendErr)
		require.NoError(t, handleErr)
		return true, nil
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)

	require.True(t, alert)
	held := initialState
	held.ProvingRetryEpoch = sql.NullInt64{Int64: currentHeight + provingRecheckDelayEpochs, Valid: true}
	require.Equal(t, held, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, dataSetID))
}

func TestIntegration_HandleNextProvingPeriodSendFailure_RecordsAlert(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	initialState := failureHandlingDataSetState{
		InitReady:          true,
		ProveAtEpoch:       sql.NullInt64{Int64: 2300, Valid: true},
		ChallengeHash:      sql.NullString{String: "0x00000000000000000000000000000000000000000000000000000000000000dd", Valid: true},
		PrevChallengeEpoch: sql.NullInt64{Int64: 1300, Valid: true},
	}
	dataSetID := insertFailureHandlingDataSet(t, ctx, db, initialState)
	sendErr := selectorRevert(contractErrorSelector(ErrPDPVerifierInsufficientChallengeDelay))

	// The alerting cases roll the transaction back; the alert must still be recorded.
	err = handleNextProvingPeriodSendFailure(ctx, db, nil, curioalerting.NewAlertingSystem(db), alertNameNextPP, dataSetID, 4500, sendErr)
	require.Error(t, err)

	var eventContext []byte
	err = db.QueryRow(ctx, `
		SELECT context
		FROM alert_history
		WHERE kind = 'event' AND system = $1 AND subsystem = $2
	`, alertType, alertNameNextPP).Scan(&eventContext)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"dataset": "%d"}`, dataSetID), string(eventContext))
	require.Equal(t, initialState, loadFailureHandlingDataSet(t, ctx, db, dataSetID))
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
			challenge_request_msg_hash, unrecoverable_proving_failure_epoch
		)
		VALUES ($1, $2, $3, 100, 10, $4, $5, $6, $7, $8)
	`, dataSetID, createMessageHash, service, state.InitReady, nullableInt64Arg(state.ProveAtEpoch),
		nullableInt64Arg(state.PrevChallengeEpoch), nullableStringArg(state.ChallengeHash),
		nullableInt64Arg(state.UnrecoverableEpoch))
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
			proving_retry_epoch,
			init_ready,
			prove_at_epoch,
			challenge_request_msg_hash,
			prev_challenge_request_epoch
		FROM pdp_data_sets
		WHERE id = $1
	`, dataSetID).Scan(&state.UnrecoverableEpoch, &state.ProvingRetryEpoch, &state.InitReady, &state.ProveAtEpoch,
		&state.ChallengeHash, &state.PrevChallengeEpoch)
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

func TestProvingPeriodRecheckCooling(t *testing.T) {
	require.False(t, provingPeriodRecheckCooling(sql.NullInt64{}, 100))
	require.False(t, provingPeriodRecheckCooling(sql.NullInt64{Int64: 100, Valid: true}, 100))
	require.False(t, provingPeriodRecheckCooling(sql.NullInt64{Int64: 99, Valid: true}, 100))
	require.True(t, provingPeriodRecheckCooling(sql.NullInt64{Int64: 101, Valid: true}, 100))
}

func TestIntegration_ClaimDatasetProvingPeriodTask_RespectsRecheckCooldown(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)

	dataSetID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	_, err = db.Exec(ctx, `UPDATE pdp_data_sets SET proving_retry_epoch = $2 WHERE id = $1`, dataSetID, 9000)
	require.NoError(t, err)

	claimed := claimFailureHandlingProvingTask(t, ctx, db, 1, dataSetID, 8000)
	require.False(t, claimed)
	require.False(t, failureHandlingTaskID(t, ctx, db, dataSetID).Valid)

	taskID := insertFailureHandlingHarmonyTask(t, ctx, db)
	claimed = claimFailureHandlingProvingTask(t, ctx, db, taskID, dataSetID, 9000)
	require.True(t, claimed)
	require.Equal(t, sql.NullInt64{Int64: int64(taskID), Valid: true}, failureHandlingTaskID(t, ctx, db, dataSetID))

	stolen := claimFailureHandlingProvingTask(t, ctx, db, insertFailureHandlingHarmonyTask(t, ctx, db), dataSetID, 9000)
	require.False(t, stolen)
	require.Equal(t, sql.NullInt64{Int64: int64(taskID), Valid: true}, failureHandlingTaskID(t, ctx, db, dataSetID))

	openID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	openTaskID := insertFailureHandlingHarmonyTask(t, ctx, db)
	require.True(t, claimFailureHandlingProvingTask(t, ctx, db, openTaskID, openID, 1))
	require.Equal(t, sql.NullInt64{Int64: int64(openTaskID), Valid: true}, failureHandlingTaskID(t, ctx, db, openID))
}

func TestIntegration_ProvingPeriodDo_FinishesDuringRecheckCooldown(t *testing.T) {
	ctx := context.Background()
	db, err := harmonydb.NewFromConfigWithITestID(t)
	require.NoError(t, err)
	head := staticProvingHead{ts: failureHandlingTipSet(t, 8000)}

	nextID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{
		InitReady:    true,
		ProveAtEpoch: sql.NullInt64{Int64: 2000, Valid: true},
	})
	nextTaskID := insertFailureHandlingHarmonyTask(t, ctx, db)
	_, err = db.Exec(ctx, `
		UPDATE pdp_data_sets
		SET challenge_request_task_id = $2, proving_retry_epoch = $3
		WHERE id = $1
	`, nextID, nextTaskID, 9000)
	require.NoError(t, err)

	nextDone, err := (&NextProvingPeriodTask{db: db, fil: head}).Do(ctx, nextTaskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, nextDone)
	require.Equal(t, sql.NullInt64{Int64: 9000, Valid: true}, loadFailureHandlingDataSet(t, ctx, db, nextID).ProvingRetryEpoch)
	require.Equal(t, sql.NullInt64{Int64: int64(nextTaskID), Valid: true}, failureHandlingTaskID(t, ctx, db, nextID))

	initID := insertFailureHandlingDataSet(t, ctx, db, failureHandlingDataSetState{InitReady: true})
	initTaskID := insertFailureHandlingHarmonyTask(t, ctx, db)
	_, err = db.Exec(ctx, `
		UPDATE pdp_data_sets
		SET challenge_request_task_id = $2, proving_retry_epoch = $3
		WHERE id = $1
	`, initID, initTaskID, 9000)
	require.NoError(t, err)

	initDone, err := (&InitProvingPeriodTask{db: db, fil: head}).Do(ctx, initTaskID, func() bool { return true })
	require.NoError(t, err)
	require.True(t, initDone)
	require.Equal(t, sql.NullInt64{Int64: 9000, Valid: true}, loadFailureHandlingDataSet(t, ctx, db, initID).ProvingRetryEpoch)
	require.Equal(t, sql.NullInt64{Int64: int64(initTaskID), Valid: true}, failureHandlingTaskID(t, ctx, db, initID))
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, nextID))
	require.Equal(t, 0, countFailureHandlingDeleteRows(t, ctx, db, initID))
}

type staticProvingHead struct {
	ts *chainTypes.TipSet
}

func (s staticProvingHead) ChainHead(context.Context) (*chainTypes.TipSet, error) {
	return s.ts, nil
}

func claimFailureHandlingProvingTask(t *testing.T, ctx context.Context, db *harmonydb.DB, taskID harmonytask.TaskID, dataSetID int64, height int64) bool {
	t.Helper()

	var claimed bool
	committed, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		var claimErr error
		claimed, claimErr = claimDatasetProvingPeriodTask(tx, taskID, dataSetID, height)
		return true, claimErr
	}, harmonydb.OptionRetry())
	require.NoError(t, err)
	require.True(t, committed)
	return claimed
}

func insertFailureHandlingHarmonyTask(t *testing.T, ctx context.Context, db *harmonydb.DB) harmonytask.TaskID {
	t.Helper()

	var id int64
	err := db.QueryRow(ctx, `
		INSERT INTO harmony_task (posted_time, added_by, name)
		VALUES (NOW(), 1, 'prov-cooldown')
		RETURNING id
	`).Scan(&id)
	require.NoError(t, err)
	return harmonytask.TaskID(id)
}

func failureHandlingTaskID(t *testing.T, ctx context.Context, db *harmonydb.DB, dataSetID int64) sql.NullInt64 {
	t.Helper()

	var taskID sql.NullInt64
	err := db.QueryRow(ctx, `SELECT challenge_request_task_id FROM pdp_data_sets WHERE id = $1`, dataSetID).Scan(&taskID)
	require.NoError(t, err)
	return taskID
}

func failureHandlingTipSet(t *testing.T, height int64) *chainTypes.TipSet {
	t.Helper()

	miner, err := address.NewIDAddress(1)
	require.NoError(t, err)
	root, err := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	require.NoError(t, err)
	ts, err := chainTypes.NewTipSet([]*chainTypes.BlockHeader{{
		Miner:                 miner,
		Ticket:                &chainTypes.Ticket{VRFProof: []byte{byte(height)}},
		Height:                abi.ChainEpoch(height),
		ParentStateRoot:       root,
		Messages:              root,
		ParentMessageReceipts: root,
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:             uint64(time.Now().Unix()),
		ParentBaseFee:         chainTypes.NewInt(100),
	}})
	require.NoError(t, err)
	return ts
}

func nullableStringArg(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
