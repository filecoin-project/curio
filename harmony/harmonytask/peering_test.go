package harmonytask

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPreemptCostMessage verifies preempt cost bytes are parsed and routed into preemptCostChs.
func TestPreemptCostMessage(t *testing.T) {
	engine := &TaskEngine{
		schedulerChannel: make(chan schedulerEvent, 10),
		preemptCostChs:   make(map[TaskID]chan preemptCostResponse),
	}
	ch := make(chan preemptCostResponse, 1)
	engine.preemptCostChs[TaskID(123)] = ch

	p := &peering{h: engine}
	them := peer{id: 99}

	msg, err := marshalPeerMessage(messageTypePreemptCost, 123, taskOther{Cost: 5 * time.Second, TaskType: "WinPost"})
	require.NoError(t, err)
	t.Logf("wire bytes (%d): %q", len(msg), msg)

	err = p.handlePeerMessage("test-peer", them, msg)
	require.NoError(t, err)

	select {
	case resp := <-ch:
		require.Equal(t, int64(99), resp.PeerID)
		require.Equal(t, 5*time.Second, resp.Cost)
	case <-time.After(time.Second):
		t.Fatal("no response on preempt cost channel")
	}
}

// TestPreemptCostMessageWireFormat verifies preempt cost messages use the PeerMessage envelope.
func TestPreemptCostMessageWireFormat(t *testing.T) {
	msg, err := marshalPeerMessage(messageTypePreemptCost, 42, taskOther{Cost: 3 * time.Second, TaskType: "WdPost"})
	require.NoError(t, err)

	var envelope PeerMessage
	require.NoError(t, json.Unmarshal(msg, &envelope))
	require.Equal(t, string(messageTypePreemptCost), envelope.Verb)
	require.Equal(t, TaskID(42), envelope.TaskID)
	require.Equal(t, "WdPost", envelope.Other.TaskType)
	require.Equal(t, 3*time.Second, envelope.Other.Cost)
}

// ===== Toy Pipe RPC (unexported, for in-package tests only) =====

func pipePair() (*pipeConn, *pipeConn) {
	ch1 := make(chan []byte, 100)
	ch2 := make(chan []byte, 100)
	a := &pipeConn{sendCh: ch1, recvCh: ch2}
	b := &pipeConn{sendCh: ch2, recvCh: ch1}
	return a, b
}

type pipeConn struct {
	sendCh    chan []byte
	recvCh    chan []byte
	closeOnce sync.Once
	closed    atomic.Bool
}

func (c *pipeConn) SendMessage(msg []byte) error {
	if c.closed.Load() {
		return fmt.Errorf("pipe closed")
	}
	cp := make([]byte, len(msg))
	copy(cp, msg)
	select {
	case c.sendCh <- cp:
		return nil
	default:
		return fmt.Errorf("pipe full")
	}
}

func (c *pipeConn) ReceiveMessage() ([]byte, error) {
	msg, ok := <-c.recvCh
	if !ok {
		return nil, fmt.Errorf("pipe closed")
	}
	return msg, nil
}

func (c *pipeConn) Close() error {
	c.closed.Store(true)
	c.closeOnce.Do(func() { close(c.sendCh) })
	return nil
}

// ===== Message Format Tests =====

// TestMessageRoundTrip verifies that every handled message type marshals to JSON
// that handlePeerMessage can parse back into the correct schedulerEvent.
func TestMessageRoundTrip(t *testing.T) {
	ch := make(chan schedulerEvent, 10)
	p := &peering{h: &TaskEngine{schedulerChannel: ch}}
	them := peer{id: 42}

	mustMarshal := func(verb messageType, taskID TaskID, other taskOther) []byte {
		msg, err := marshalPeerMessage(verb, taskID, other)
		require.NoError(t, err)
		return msg
	}

	tests := []struct {
		name     string
		msg      []byte
		wantType string
		wantID   TaskID
		wantSrc  schedulerSource
		wantRet  int
	}{
		{
			name:     "NewTask with retries",
			msg:      mustMarshal(messageTypeNewTask, 100, taskOther{TaskType: "Seal", Retries: 3}),
			wantType: "Seal",
			wantID:   100,
			wantSrc:  schedulerSourcePeerNewTask,
			wantRet:  3,
		},
		{
			name:     "NewTask zero retries",
			msg:      mustMarshal(messageTypeNewTask, 999, taskOther{TaskType: "SDR"}),
			wantType: "SDR",
			wantID:   999,
			wantSrc:  schedulerSourcePeerNewTask,
			wantRet:  0,
		},
		{
			name:     "NewTask WinPost",
			msg:      mustMarshal(messageTypeNewTask, 7, taskOther{TaskType: "WinPost"}),
			wantType: "WinPost",
			wantID:   7,
			wantSrc:  schedulerSourcePeerNewTask,
			wantRet:  0,
		},
		{
			name:     "Started",
			msg:      mustMarshal(messageTypeStarted, 12, taskOther{TaskType: "WdPost"}),
			wantType: "WdPost",
			wantID:   12,
			wantSrc:  schedulerSourcePeerStarted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("wire JSON (%d): %s", len(tt.msg), tt.msg)

			err := p.handlePeerMessage("test-peer", them, tt.msg)
			require.NoError(t, err)

			select {
			case ev := <-ch:
				require.Equal(t, tt.wantType, ev.TaskType)
				require.Equal(t, tt.wantID, ev.TaskID)
				require.Equal(t, tt.wantSrc, ev.Source)
				require.Equal(t, int64(42), ev.PeerID)
				if tt.wantRet > 0 {
					require.Equal(t, tt.wantRet, ev.Retries)
				}
			case <-time.After(time.Second):
				t.Fatal("no event on scheduler channel")
			}
		})
	}
}

// TestMessageWireFormat verifies that marshalled messages are valid JSON with expected fields.
func TestMessageWireFormat(t *testing.T) {
	t.Run("NewTaskWithRetries", func(t *testing.T) {
		msg, err := marshalPeerMessage(messageTypeNewTask, 2, taskOther{TaskType: "XY", Retries: 5})
		require.NoError(t, err)

		var envelope PeerMessage
		require.NoError(t, json.Unmarshal(msg, &envelope))
		require.Equal(t, "newTask", envelope.Verb)
		require.Equal(t, TaskID(2), envelope.TaskID)
		require.Equal(t, "XY", envelope.Other.TaskType)
		require.Equal(t, 5, envelope.Other.Retries)
	})

	t.Run("Started", func(t *testing.T) {
		msg, err := marshalPeerMessage(messageTypeStarted, 42, taskOther{TaskType: "WdPost"})
		require.NoError(t, err)

		var envelope PeerMessage
		require.NoError(t, json.Unmarshal(msg, &envelope))
		require.Equal(t, "started", envelope.Verb)
		require.Equal(t, TaskID(42), envelope.TaskID)
		require.Equal(t, "WdPost", envelope.Other.TaskType)
	})

	t.Run("Identity", func(t *testing.T) {
		msg, err := marshalPeerMessage(messageTypeIdentity, 0, taskOther{HostAndPort: "host:1234"})
		require.NoError(t, err)
		var envelope PeerMessage
		require.NoError(t, json.Unmarshal(msg, &envelope))
		require.Equal(t, "identity", envelope.Verb)
		require.Equal(t, "host:1234", envelope.Other.HostAndPort)
	})
}

// TestHandlePeerMessageRejectsGarbage ensures malformed messages are rejected.
func TestHandlePeerMessageRejectsGarbage(t *testing.T) {
	ch := make(chan schedulerEvent, 10)
	p := &peering{h: &TaskEngine{schedulerChannel: ch}}
	them := peer{id: 1}

	tests := []struct {
		name string
		msg  []byte
	}{
		{"empty", []byte{}},
		{"not json", []byte("hello")},
		{"invalid json", []byte("{bad")},
		{"unknown verb", []byte(`{"verb":"explode","taskID":1,"other":{"taskType":"X"}}`)},
		{"missing verb", []byte(`{"taskID":1,"other":{"taskType":"X"}}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.handlePeerMessage("test", them, tt.msg)
			require.Error(t, err)
			select {
			case ev := <-ch:
				t.Fatalf("unexpected event: %+v", ev)
			default:
			}
		})
	}
}

// ===== TellOthers Delivery Tests =====

// TestTellOthersDelivery verifies that TellOthers sends correctly-formatted
// messages to all peers that can run the named task.
func TestTellOthersDelivery(t *testing.T) {
	local1, remote1 := pipePair()
	local2, remote2 := pipePair()

	p := &peering{
		h: &TaskEngine{},
		peers: []peer{
			{id: 10, addr: "p1:1000", conn: local1, tasks: map[string]bool{"TaskA": true}},
			{id: 20, addr: "p2:1000", conn: local2, tasks: map[string]bool{"TaskA": true, "TaskB": true}},
		},
		m: map[string][]int{
			"TaskA": {0, 1},
			"TaskB": {1},
		},
	}

	t.Run("BroadcastToAll", func(t *testing.T) {
		p.TellOthers(messageTypeNewTask, "TaskA", TaskID(77))
		time.Sleep(50 * time.Millisecond)

		msg1, err := remote1.ReceiveMessage()
		require.NoError(t, err)
		msg2, err := remote2.ReceiveMessage()
		require.NoError(t, err)
		require.Equal(t, msg1, msg2)

		ch := make(chan schedulerEvent, 5)
		pp := &peering{h: &TaskEngine{schedulerChannel: ch}}
		err = pp.handlePeerMessage("test", peer{id: 99}, msg1)
		require.NoError(t, err)
		ev := <-ch
		require.Equal(t, "TaskA", ev.TaskType)
		require.Equal(t, TaskID(77), ev.TaskID)
		require.Equal(t, schedulerSourcePeerNewTask, ev.Source)
	})

	t.Run("UnicastToSubset", func(t *testing.T) {
		p.TellOthers(messageTypeStarted, "TaskB", TaskID(88))
		time.Sleep(50 * time.Millisecond)

		msg2, err := remote2.ReceiveMessage()
		require.NoError(t, err)

		ch := make(chan schedulerEvent, 5)
		pp := &peering{h: &TaskEngine{schedulerChannel: ch}}
		err = pp.handlePeerMessage("test", peer{id: 99}, msg2)
		require.NoError(t, err)
		ev := <-ch
		require.Equal(t, "TaskB", ev.TaskType)
		require.Equal(t, TaskID(88), ev.TaskID)
		require.Equal(t, schedulerSourcePeerStarted, ev.Source)

		select {
		case msg := <-remote1.recvCh:
			t.Fatalf("peer 1 should not receive TaskB message, got: %q", msg)
		default:
		}
	})

	t.Run("NoPeersForTask", func(t *testing.T) {
		p.TellOthers(messageTypeStarted, "Unknown", TaskID(99))
		time.Sleep(50 * time.Millisecond)

		select {
		case msg := <-remote1.recvCh:
			t.Fatalf("unexpected message on peer1: %q", msg)
		case msg := <-remote2.recvCh:
			t.Fatalf("unexpected message on peer2: %q", msg)
		default:
		}
	})
}
