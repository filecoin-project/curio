package harmonytask

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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

// TestMessageRenderRoundTrip verifies that every message type renders to bytes
// that handlePeerMessage can parse back into the correct schedulerEvent.
func TestMessageRenderRoundTrip(t *testing.T) {
	ch := make(chan schedulerEvent, 10)
	p := &peering{h: &TaskEngine{schedulerChannel: ch}}
	them := peer{id: 42}

	tests := []struct {
		name     string
		render   func() []byte
		wantType string
		wantID   TaskID
		wantSrc  schedulerSource
		wantRet  int
	}{
		{
			name:     "NewTask with retries",
			render:   func() []byte { return messageRenderNewTask{TaskType: "Seal", TaskID: 100, Retries: 3}.Render() },
			wantType: "Seal",
			wantID:   100,
			wantSrc:  schedulerSourcePeerNewTask,
			wantRet:  3,
		},
		{
			name:     "NewTask zero retries",
			render:   func() []byte { return messageRenderNewTask{TaskType: "SDR", TaskID: 999, Retries: 0}.Render() },
			wantType: "SDR",
			wantID:   999,
			wantSrc:  schedulerSourcePeerNewTask,
			wantRet:  0,
		},
		{
			name: "TaskSend as NewTask",
			render: func() []byte {
				return messageRenderTaskSend{MessageType: messageTypeNewTask, TaskType: "WinPost", TaskID: 7}.Render()
			},
			wantType: "WinPost",
			wantID:   7,
			wantSrc:  schedulerSourcePeerNewTask,
			wantRet:  0,
		},
		{
			name: "Reserve",
			render: func() []byte {
				return messageRenderTaskSend{MessageType: messageTypeReserve, TaskType: "Snap", TaskID: 55}.Render()
			},
			wantType: "Snap",
			wantID:   55,
			wantSrc:  schedulerSourcePeerReserved,
		},
		{
			name: "Started",
			render: func() []byte {
				return messageRenderTaskSend{MessageType: messageTypeStarted, TaskType: "WdPost", TaskID: 12}.Render()
			},
			wantType: "WdPost",
			wantID:   12,
			wantSrc:  schedulerSourcePeerStarted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.render()
			t.Logf("wire bytes (%d): %q", len(msg), msg)

			err := p.handlePeerMessage("test-peer", them, msg)
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

// TestMessageRenderWireFormat verifies the exact byte layout of rendered messages.
func TestMessageRenderWireFormat(t *testing.T) {
	t.Run("TaskSend", func(t *testing.T) {
		msg := messageRenderTaskSend{MessageType: messageTypeReserve, TaskType: "ABC", TaskID: 1}.Render()
		require.Equal(t, byte('r'), msg[0], "first byte should be message type")
		require.Equal(t, byte(':'), msg[1])
		require.Equal(t, "ABC", string(msg[2:5]))
		require.Equal(t, byte(':'), msg[5])
		require.Len(t, msg, 6+8, "header + 8 byte taskID")
	})

	t.Run("NewTask", func(t *testing.T) {
		msg := messageRenderNewTask{TaskType: "XY", TaskID: 2, Retries: 5}.Render()
		require.Equal(t, byte('t'), msg[0])
		require.Equal(t, "XY", string(msg[2:4]))
		require.Len(t, msg, 5+8+2, "header + 8 byte taskID + 2 byte retries")
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
		{"no colons", []byte("hello")},
		{"one colon only", []byte("t:short")},
		{"unknown type", []byte("x:task:12345678")},
		{"payload too short", []byte("t:task:abc")},
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
		p.TellOthers(messageTypeReserve, "TaskB", TaskID(88))
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
		require.Equal(t, schedulerSourcePeerReserved, ev.Source)

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
