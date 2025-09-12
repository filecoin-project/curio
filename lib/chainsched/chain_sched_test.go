package chainsched

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
)

type mockNodeAPI struct {
	notifCh chan []*api.HeadChange
	head    *types.TipSet
}

func (m *mockNodeAPI) ChainHead(context.Context) (*types.TipSet, error) {
	return m.head, nil
}

func (m *mockNodeAPI) ChainNotify(context.Context) (<-chan []*api.HeadChange, error) {
	return m.notifCh, nil
}

type mockNodeAPIWithCounter struct {
	*mockNodeAPI
	notifyCallCount *int
	mu              *sync.Mutex
}

func (m *mockNodeAPIWithCounter) ChainNotify(ctx context.Context) (<-chan []*api.HeadChange, error) {
	m.mu.Lock()
	*m.notifyCallCount++
	m.mu.Unlock()
	return m.mockNodeAPI.ChainNotify(ctx)
}

func makeMockTipSet(height uint64) *types.TipSet {
	addr, _ := address.NewIDAddress(1)
	c, _ := cid.Decode("bafy2bzacea3wsdh6y3a36tb3skempjoxqpuyompjbmfeyf34fi3uy6uue42v4")
	ts, _ := types.NewTipSet([]*types.BlockHeader{{
		Miner:                 addr,
		Height:                abi.ChainEpoch(height),
		ParentStateRoot:       c,
		Messages:              c,
		ParentMessageReceipts: c,
		BlockSig:              &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		BLSAggregate:          &crypto.Signature{Type: crypto.SigTypeSecp256k1},
		Timestamp:             uint64(time.Now().Unix()),
		ParentBaseFee:         types.NewInt(100),
	}})
	return ts
}

func TestAddHandlerConcurrency(t *testing.T) {
	api := &mockNodeAPI{
		notifCh: make(chan []*api.HeadChange),
	}

	sched := New(api)

	// Test concurrent AddHandler calls
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := sched.AddHandler(func(ctx context.Context, revert, apply *types.TipSet) error {
				return nil
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Should have no errors
	for err := range errors {
		require.NoError(t, err)
	}

	// Should have 10 handlers
	require.Len(t, sched.callbacks, 10)
}

func TestAddHandlerAfterStart(t *testing.T) {
	mockAPI := &mockNodeAPI{
		notifCh: make(chan []*api.HeadChange),
	}

	sched := New(mockAPI)

	// Start the scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Run(ctx)

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Try to add handler after start
	err := sched.AddHandler(func(ctx context.Context, revert, apply *types.TipSet) error {
		return nil
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot add handler after start")
}

func TestNotificationChannelResubscription(t *testing.T) {
	// This test verifies that the chain scheduler will resubscribe when
	// the notification channel is closed (simulating a disconnection).

	notifCh := make(chan []*api.HeadChange)
	mockAPI := &mockNodeAPI{
		notifCh: notifCh,
	}

	// Test that closing the notification channel causes resubscription
	notifyCallCount := 0
	mu := &sync.Mutex{}
	wrappedAPI := &mockNodeAPIWithCounter{
		mockNodeAPI:     mockAPI,
		notifyCallCount: &notifyCallCount,
		mu:              mu,
	}

	sched := New(wrappedAPI)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run scheduler in background
	go sched.Run(ctx)

	// Wait for initial setup
	time.Sleep(50 * time.Millisecond)

	// Send initial notification
	notifCh <- []*api.HeadChange{{
		Type: store.HCCurrent,
		Val:  makeMockTipSet(100),
	}}

	// Wait for processing
	time.Sleep(50 * time.Millisecond)

	// Verify initial call
	mu.Lock()
	require.Equal(t, 1, notifyCallCount)
	mu.Unlock()

	// Close the channel to simulate disconnection
	close(notifCh)

	// Give scheduler time to detect closed channel and resubscribe
	time.Sleep(100 * time.Millisecond)

	// Should have called ChainNotify again
	mu.Lock()
	finalCount := notifyCallCount
	mu.Unlock()
	require.GreaterOrEqual(t, finalCount, 2, "ChainNotify should have been called again after channel closure")
}

func TestCallbackExecution(t *testing.T) {
	notifCh := make(chan []*api.HeadChange, 10)
	mockAPI := &mockNodeAPI{
		notifCh: notifCh,
	}

	sched := New(mockAPI)

	var callbackMu sync.Mutex
	callbackCalled := false
	var receivedRevert, receivedApply *types.TipSet

	err := sched.AddHandler(func(ctx context.Context, revert, apply *types.TipSet) error {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCalled = true
		receivedRevert = revert
		receivedApply = apply
		return nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sched.Run(ctx)

	// Send initial current notification
	currentTs := makeMockTipSet(100)
	change := &api.HeadChange{
		Type: store.HCCurrent,
		Val:  currentTs,
	}
	notifCh <- []*api.HeadChange{change}

	// Wait for callback
	time.Sleep(100 * time.Millisecond)

	callbackMu.Lock()
	require.True(t, callbackCalled)
	require.Nil(t, receivedRevert)
	require.Equal(t, currentTs, receivedApply)
	callbackMu.Unlock()

	// Reset
	callbackMu.Lock()
	callbackCalled = false
	callbackMu.Unlock()

	// Send apply notification
	newTs := makeMockTipSet(101)
	applyChange := &api.HeadChange{
		Type: store.HCApply,
		Val:  newTs,
	}
	notifCh <- []*api.HeadChange{applyChange}

	// Wait for callback
	time.Sleep(100 * time.Millisecond)

	callbackMu.Lock()
	require.True(t, callbackCalled)
	require.Nil(t, receivedRevert)
	require.Equal(t, newTs, receivedApply)
	callbackMu.Unlock()
}

func TestContextCancellation(t *testing.T) {
	notifCh := make(chan []*api.HeadChange)
	mockAPI := &mockNodeAPI{
		notifCh: notifCh,
	}

	sched := New(mockAPI)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan bool)
	go func() {
		sched.Run(ctx)
		done <- true
	}()

	// Send initial notification
	change := &api.HeadChange{
		Type: store.HCCurrent,
		Val:  makeMockTipSet(100),
	}
	notifCh <- []*api.HeadChange{change}

	// Cancel context
	cancel()

	// Should exit quickly
	select {
	case <-done:
		// Good, exited
	case <-time.After(1 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestSubscriptionContextCancellation(t *testing.T) {
	// This test verifies that when resubscribing, the old subscription
	// context is properly cancelled

	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer testCancel()

	var notifyCalls []context.Context
	var mu sync.Mutex

	mockAPI := &mockNodeAPI{}

	firstCh := make(chan []*api.HeadChange, 1)
	firstCallReady := make(chan struct{})
	secondCallReady := make(chan struct{})

	// Custom ChainNotify that captures contexts
	wrappedAPI := &mockNodeAPIWithContext{
		mockNodeAPI: mockAPI,
		chainNotifyFunc: func(ctx context.Context) (<-chan []*api.HeadChange, error) {
			mu.Lock()
			notifyCalls = append(notifyCalls, ctx)
			callNum := len(notifyCalls)
			mu.Unlock()

			if callNum == 1 {
				defer close(firstCallReady)
				// First call - return channel we control below
				return firstCh, nil
			}

			if callNum == 2 {
				defer close(secondCallReady)
			}

			// Subsequent calls - return a properly initialized channel
			ch := make(chan []*api.HeadChange, 1)
			ch <- []*api.HeadChange{{
				Type: store.HCCurrent,
				Val:  makeMockTipSet(100),
			}}
			return ch, nil
		},
	}

	sched := New(wrappedAPI)

	ctx, cancel := context.WithCancel(testCtx)
	defer cancel()

	go sched.Run(ctx)

	// Wait for first ChainNotify call
	select {
	case <-firstCallReady:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for first ChainNotify call")
	}

	// Send initial notification
	firstCh <- []*api.HeadChange{{
		Type: store.HCCurrent,
		Val:  makeMockTipSet(100),
	}}

	// Verify we have the first subscription
	mu.Lock()
	require.Len(t, notifyCalls, 1)
	firstCtx := notifyCalls[0]
	mu.Unlock()

	// Close the channel to trigger resubscription
	close(firstCh)

	// Wait for second ChainNotify call
	select {
	case <-secondCallReady:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for second ChainNotify call")
	}

	// Should have multiple calls now
	mu.Lock()
	require.GreaterOrEqual(t, len(notifyCalls), 2)
	mu.Unlock()

	// Verify first context was cancelled
	select {
	case <-firstCtx.Done():
		// Good, context was cancelled
	default:
		t.Fatal("First subscription context was not cancelled on resubscription")
	}
}

type mockNodeAPIWithContext struct {
	*mockNodeAPI
	chainNotifyFunc func(context.Context) (<-chan []*api.HeadChange, error)
}

func (m *mockNodeAPIWithContext) ChainNotify(ctx context.Context) (<-chan []*api.HeadChange, error) {
	return m.chainNotifyFunc(ctx)
}

func TestTimeoutResubscription(t *testing.T) {
	// This test verifies that the scheduler will resubscribe after
	// not receiving notifications for the configured timeout period

	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer testCancel()

	firstCallCh := make(chan struct{})
	secondCallCh := make(chan struct{})
	mu := &sync.Mutex{}

	// Create a channel that won't receive new notifications after initial
	notifyCh := make(chan []*api.HeadChange, 1)

	// Use mockNodeAPIWithContext to have full control over ChainNotify
	var callCount int
	wrappedAPI := &mockNodeAPIWithContext{
		mockNodeAPI: &mockNodeAPI{},
		chainNotifyFunc: func(ctx context.Context) (<-chan []*api.HeadChange, error) {
			mu.Lock()
			callCount++
			count := callCount
			mu.Unlock()

			switch count {
			case 1:
				defer close(firstCallCh)
			case 2:
				defer close(secondCallCh)
			}

			// Always return the same channel for this test
			return notifyCh, nil
		},
	}

	// Create scheduler with very short timeout for testing
	sched := NewWithNotificationTimeout(wrappedAPI, 200*time.Millisecond)

	ctx, cancel := context.WithCancel(testCtx)
	defer cancel()

	go sched.Run(ctx)

	// Wait for first ChainNotify call
	select {
	case <-firstCallCh:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for first ChainNotify call")
	}

	// Send initial notification
	notifyCh <- []*api.HeadChange{{
		Type: store.HCCurrent,
		Val:  makeMockTipSet(100),
	}}

	// Verify initial call count
	mu.Lock()
	require.Equal(t, 1, callCount)
	mu.Unlock()

	// Wait for timeout to trigger resubscription
	// The scheduler has a 200ms timeout, so wait for the second call
	select {
	case <-secondCallCh:
		// Good, resubscription happened
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for resubscription after notification timeout")
	}

	// Verify we got the second call
	mu.Lock()
	finalCount := callCount
	mu.Unlock()
	require.GreaterOrEqual(t, finalCount, 2, "ChainNotify should have been called again after timeout")
}

func TestMultipleChanges(t *testing.T) {
	testCtx, testCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer testCancel()

	notifCh := make(chan []*api.HeadChange, 10)
	mockAPI := &mockNodeAPI{
		notifCh: notifCh,
	}

	sched := New(mockAPI)

	var callbackMu sync.Mutex
	var callCount int
	var lastApply *types.TipSet
	firstCallDone := make(chan struct{})
	secondCallDone := make(chan struct{})

	err := sched.AddHandler(func(ctx context.Context, revert, apply *types.TipSet) error {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callCount++
		lastApply = apply

		switch callCount {
		case 1:
			close(firstCallDone)
		case 2:
			close(secondCallDone)
		}
		return nil
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(testCtx)
	defer cancel()

	go sched.Run(ctx)

	// Send initial current
	ts0 := makeMockTipSet(100)
	initialChange := &api.HeadChange{
		Type: store.HCCurrent,
		Val:  ts0,
	}
	notifCh <- []*api.HeadChange{initialChange}

	// Wait for first callback
	select {
	case <-firstCallDone:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for first callback")
	}

	// Send multiple changes in one notification
	ts1 := makeMockTipSet(101)
	ts2 := makeMockTipSet(102)
	ts3 := makeMockTipSet(103)

	changes := []*api.HeadChange{
		{Type: store.HCApply, Val: ts1},
		{Type: store.HCApply, Val: ts2},
		{Type: store.HCApply, Val: ts3},
	}
	notifCh <- changes

	// Wait for second callback
	select {
	case <-secondCallDone:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for second callback")
	}

	callbackMu.Lock()
	defer callbackMu.Unlock()
	// Should be called with the highest tipset
	require.Equal(t, ts3, lastApply)
	// Initial current + one call for the batch
	require.Equal(t, 2, callCount)
}
