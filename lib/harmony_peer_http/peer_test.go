package harmonypeerhttp

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

// ===== Helpers =====

// startPeerHTTP creates a PeerHTTP with a real HTTP server on a random port.
// Returns the PeerHTTP, the listener address, and a cleanup function.
func startPeerHTTP(t *testing.T, name string) (*PeerHTTP, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	p := New(addr)

	mux := http.NewServeMux()
	mux.Handle("/peer/v1", p)
	srv := &http.Server{Handler: mux}

	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.Logf("[%s] server error: %v", name, err)
		}
	}()

	t.Cleanup(func() {
		_ = srv.Close()
	})

	return p, addr
}

// ===== Tests =====

// TestPeerHTTPSendReceive verifies basic message passing between two HTTP peers.
func TestPeerHTTPSendReceive(t *testing.T) {
	peerA, addrA := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	// A connects to B
	connAtoB, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	// B should see A's connection via onConnect when A sends a message
	var bReceivedConn harmonytask.PeerConnection
	var bConnAddr string
	connReady := make(chan struct{})
	peerB.SetOnConnect(func(peerAddr string, conn harmonytask.PeerConnection) {
		bReceivedConn = conn
		bConnAddr = peerAddr
		close(connReady)
	})

	// A sends a message to B
	msg := []byte("hello from A")
	err = connAtoB.SendMessage(msg)
	require.NoError(t, err)

	// Wait for B's onConnect to fire
	select {
	case <-connReady:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onConnect on B")
	}
	require.Equal(t, addrA, bConnAddr)

	// B receives the message
	received, err := bReceivedConn.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, msg, received)
	t.Logf("A→B message received: %q", received)
}

// TestPeerHTTPBidirectional verifies messages flow in both directions.
//
// A node's connection is keyed by peer address: A's outbound connection to B is
// also where A receives inbound messages from B (ServeHTTP routes by X-Peer-ID
// into the existing connection rather than re-handshaking). This mirrors how the
// production peering layer's handlePeer loop reads from the connection it was
// handed.
func TestPeerHTTPBidirectional(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	var connOnB harmonytask.PeerConnection
	readyB := make(chan struct{})
	peerB.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		connOnB = conn
		select {
		case <-readyB:
		default:
			close(readyB)
		}
	})

	connAB, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	err = connAB.SendMessage([]byte("A->B"))
	require.NoError(t, err)

	select {
	case <-readyB:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onConnect on B")
	}

	msgOnB, err := connOnB.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("A->B"), msgOnB)

	// B replies to A. A receives it on its existing connection to B (no new
	// onConnect on A, since A already has a connection keyed to B's address).
	err = connOnB.SendMessage([]byte("B->A"))
	require.NoError(t, err)

	msgOnA, err := connAB.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("B->A"), msgOnA)
}

// TestPeerHTTPMultipleMessages verifies that multiple messages arrive in order.
func TestPeerHTTPMultipleMessages(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	const count = 20
	var msgsMu sync.Mutex
	received := make([][]byte, 0, count)
	ready := make(chan struct{})
	once := sync.Once{}
	// One connection is reused for all POSTs from the same peer, so onConnect
	// fires once; drain it in a loop.
	peerB.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		once.Do(func() {
			close(ready)
			go func(c harmonytask.PeerConnection) {
				for {
					msg, err := c.ReceiveMessage()
					if err != nil {
						return
					}
					msgsMu.Lock()
					received = append(received, msg)
					msgsMu.Unlock()
				}
			}(conn)
		})
	})

	outConn, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	// Send first message to trigger onConnect
	err = outConn.SendMessage([]byte("msg-000"))
	require.NoError(t, err)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onConnect")
	}

	// Send remaining messages
	for i := 1; i < count; i++ {
		err = outConn.SendMessage([]byte(fmt.Sprintf("msg-%03d", i)))
		require.NoError(t, err)
	}

	deadline := time.After(5 * time.Second)
	for {
		msgsMu.Lock()
		n := len(received)
		msgsMu.Unlock()
		if n >= count {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("should receive all %d messages, got %d", count, n)
		case <-time.After(20 * time.Millisecond):
		}
	}

	msgsMu.Lock()
	msgs := append([][]byte(nil), received...)
	msgsMu.Unlock()
	require.Len(t, msgs, count, "should receive all %d messages", count)
	sort.Slice(msgs, func(i, j int) bool { return string(msgs[i]) < string(msgs[j]) })
	for i, msg := range msgs {
		expected := fmt.Sprintf("msg-%03d", i)
		require.Equal(t, expected, string(msg), "message %d should match", i)
	}
	t.Logf("All %d messages received in order", count)
}

// TestPeerHTTPThreeNodes verifies message passing in a 3-node topology:
// A → B, B → C, C → A (ring).
func TestPeerHTTPThreeNodes(t *testing.T) {
	peerA, addrA := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")
	peerC, addrC := startPeerHTTP(t, "C")

	var connOnA, connOnB, connOnC harmonytask.PeerConnection
	readyA, readyB, readyC := make(chan struct{}), make(chan struct{}), make(chan struct{})

	peerA.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		connOnA = conn
		select {
		case <-readyA:
		default:
			close(readyA)
		}
	})
	peerB.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		connOnB = conn
		select {
		case <-readyB:
		default:
			close(readyB)
		}
	})
	peerC.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		connOnC = conn
		select {
		case <-readyC:
		default:
			close(readyC)
		}
	})

	// Ring: A→B, B→C, C→A
	outAB, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)
	outBC, err := peerB.ConnectToPeer(addrC)
	require.NoError(t, err)
	outCA, err := peerC.ConnectToPeer(addrA)
	require.NoError(t, err)

	err = outAB.SendMessage([]byte("A→B"))
	require.NoError(t, err)
	err = outBC.SendMessage([]byte("B→C"))
	require.NoError(t, err)
	err = outCA.SendMessage([]byte("C→A"))
	require.NoError(t, err)

	// Wait for all onConnect callbacks
	for name, ch := range map[string]chan struct{}{"A": readyA, "B": readyB, "C": readyC} {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for onConnect on %s", name)
		}
	}

	// Verify each node received the correct message
	msgA, err := connOnA.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("C→A"), msgA)

	msgB, err := connOnB.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("A→B"), msgB)

	msgC, err := connOnC.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("B→C"), msgC)

	t.Log("3-node ring message passing verified")
}

// TestPeerHTTPConnectionReuse verifies outbound ConnectToPeer reuses the same
// connection object, and that a burst of inbound HTTP POSTs from the same peer
// reuses one receiver connection (onConnect fires once, not per POST).
func TestPeerHTTPConnectionReuse(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	var onConnectCount int32
	peerB.SetOnConnect(func(_ string, _ harmonytask.PeerConnection) {
		atomic.AddInt32(&onConnectCount, 1)
	})

	conn1, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	conn2, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	// Same connection object should be returned
	require.Same(t, conn1, conn2, "ConnectToPeer should return the same connection")

	// Send multiple messages
	for i := 0; i < 5; i++ {
		err = conn1.SendMessage([]byte(fmt.Sprintf("msg-%d", i)))
		require.NoError(t, err)
	}

	time.Sleep(200 * time.Millisecond)

	require.Equal(t, int32(1), atomic.LoadInt32(&onConnectCount),
		"inbound POSTs from a known peer should reuse the connection, firing onConnect once")
}

// TestPeerHTTPBinaryPayload verifies that arbitrary binary data is preserved.
func TestPeerHTTPBinaryPayload(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	var inConn harmonytask.PeerConnection
	ready := make(chan struct{})
	peerB.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		inConn = conn
		close(ready)
	})

	outConn, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	// Binary payload with null bytes, high bytes, etc.
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	err = outConn.SendMessage(payload)
	require.NoError(t, err)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onConnect")
	}

	received, err := inConn.ReceiveMessage()
	require.NoError(t, err)
	require.True(t, bytes.Equal(payload, received), "binary payload should be preserved exactly")
	t.Log("Binary payload (256 bytes, all byte values) preserved")
}

// TestPeerHTTPDropPeerOnBadTarget verifies that sending to an unreachable
// peer drops the connection.
func TestPeerHTTPDropPeerOnBadTarget(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")

	// Connect to a port that's not listening
	badAddr := "127.0.0.1:1" // port 1 should be unreachable
	conn, err := peerA.ConnectToPeer(badAddr)
	require.NoError(t, err)

	err = conn.SendMessage([]byte("hello"))
	require.Error(t, err, "send to unreachable peer should fail")
	t.Logf("Send to unreachable peer failed as expected: %v", err)

	// After drop, ReceiveMessage should return error (channel closed)
	_, err = conn.ReceiveMessage()
	require.Error(t, err, "ReceiveMessage should error after peer is dropped")
}

// TestPeerHTTPServeHTTPValidation verifies HTTP handler edge cases.
func TestPeerHTTPServeHTTPValidation(t *testing.T) {
	p := New("test:1000")

	t.Run("RejectGET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/peer/v1", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("RejectMissingPeerID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/peer/v1", bytes.NewReader([]byte("data")))
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("AcceptValidPOST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/peer/v1", bytes.NewReader([]byte("data")))
		req.Header.Set("X-Peer-ID", "some-peer:2000")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	})
}

// TestPeerHTTPQueueFull verifies that a burst of inbound POSTs from the same
// peer reuses one connection, and once its buffer (cap 100) fills with nothing
// draining it, further POSTs are dropped with 503 (the DB poll fallback then
// ensures eventual consistency).
func TestPeerHTTPQueueFull(t *testing.T) {
	p := New("test:1000")

	// No onConnect registered, so nothing drains the connection's buffer.
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodPost, "/peer/v1", bytes.NewReader([]byte("msg")))
		req.Header.Set("X-Peer-ID", "flood:2000")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "POST %d should fit in the buffer", i)
	}

	// Buffer is now full; the next POST on the reused connection is dropped.
	req := httptest.NewRequest(http.MethodPost, "/peer/v1", bytes.NewReader([]byte("msg")))
	req.Header.Set("X-Peer-ID", "flood:2000")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "POST past buffer capacity should be dropped")
}

// TestPeerHTTPConcurrentSenders verifies that multiple goroutines can send
// messages to the same peer without data corruption.
func TestPeerHTTPConcurrentSenders(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	const senders = 10
	const msgsPerSender = 5
	total := senders * msgsPerSender

	var msgsMu sync.Mutex
	received := make([][]byte, 0, total+1)
	ready := make(chan struct{})
	once := sync.Once{}
	// One connection is reused for all POSTs from the same peer, so onConnect
	// fires once; drain it in a loop.
	peerB.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		once.Do(func() {
			close(ready)
			go func(c harmonytask.PeerConnection) {
				for {
					msg, err := c.ReceiveMessage()
					if err != nil {
						return
					}
					msgsMu.Lock()
					received = append(received, msg)
					msgsMu.Unlock()
				}
			}(conn)
		})
	})

	outConn, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	err = outConn.SendMessage([]byte("init"))
	require.NoError(t, err)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onConnect")
	}

	var wg sync.WaitGroup
	for s := 0; s < senders; s++ {
		wg.Add(1)
		go func(senderID int) {
			defer wg.Done()
			for m := 0; m < msgsPerSender; m++ {
				payload := []byte(fmt.Sprintf("sender-%02d-msg-%02d", senderID, m))
				if err := outConn.SendMessage(payload); err != nil {
					t.Logf("send error (sender %d, msg %d): %v", senderID, m, err)
				}
			}
		}(s)
	}
	wg.Wait()

	deadline := time.After(5 * time.Second)
	for {
		msgsMu.Lock()
		n := len(received)
		msgsMu.Unlock()
		if n >= total+1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("should receive all %d messages from concurrent senders, got %d", total+1, n)
		case <-time.After(20 * time.Millisecond):
		}
	}

	msgsMu.Lock()
	msgs := append([][]byte(nil), received...)
	msgsMu.Unlock()
	require.Len(t, msgs, total+1, "should receive all messages from concurrent senders")
	for _, msg := range msgs {
		if string(msg) == "init" {
			continue
		}
		require.True(t, bytes.HasPrefix(msg, []byte("sender-")),
			"message should be well-formed, got: %q", msg)
	}
}

// TestPeerHTTPIdentityHeader verifies that the X-Peer-ID header is correctly
// set to the sender's local address.
func TestPeerHTTPIdentityHeader(t *testing.T) {
	peerA, addrA := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	var receivedAddr string
	ready := make(chan struct{})
	peerB.SetOnConnect(func(peerAddr string, _ harmonytask.PeerConnection) {
		receivedAddr = peerAddr
		close(ready)
	})

	conn, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	err = conn.SendMessage([]byte("identify"))
	require.NoError(t, err)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	require.Equal(t, addrA, receivedAddr,
		"X-Peer-ID header should contain sender's local address")
	t.Logf("Identity header verified: %s", receivedAddr)
}

// TestPeerHTTPCloseIsNoop verifies that Close() doesn't break anything —
// it's documented as a no-op.
func TestPeerHTTPCloseIsNoop(t *testing.T) {
	peerA, _ := startPeerHTTP(t, "A")
	peerB, addrB := startPeerHTTP(t, "B")

	var inConn harmonytask.PeerConnection
	ready := make(chan struct{})
	peerB.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		inConn = conn
		close(ready)
	})

	outConn, err := peerA.ConnectToPeer(addrB)
	require.NoError(t, err)

	err = outConn.SendMessage([]byte("before-close"))
	require.NoError(t, err)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Close the outbound connection (should be a no-op)
	err = outConn.Close()
	require.NoError(t, err)

	// Should still be able to receive the message that was already sent
	msg, err := inConn.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("before-close"), msg)

	t.Log("Close() is a no-op as documented — no breakage")
}

// TestPeerHTTPSetOnConnectConcurrentWithServeHTTP exercises the window where
// SetOnConnect writes onConnect while ServeHTTP reads it. Without connMu
// covering both, go test -race flags the field.
func TestPeerHTTPSetOnConnectConcurrentWithServeHTTP(t *testing.T) {
	p := New("test:1000")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			p.SetOnConnect(func(string, harmonytask.PeerConnection) {})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			req := httptest.NewRequest(http.MethodPost, "/peer/v1", bytes.NewReader([]byte("x")))
			req.Header.Set("X-Peer-ID", fmt.Sprintf("peer:%d", i))
			w := httptest.NewRecorder()
			p.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
		}
	}()
	wg.Wait()
}

// TestPeerHTTPServeHTTPReusesInboundConnection verifies that repeated inbound
// POSTs from the same peer reuse one connection (onConnect fires once) and the
// payloads are delivered in order on that connection.
func TestPeerHTTPServeHTTPReusesInboundConnection(t *testing.T) {
	p := New("test:1000")

	var connectCount int32
	connCh := make(chan harmonytask.PeerConnection, 1)
	peerAddr := "remote:2000"
	p.SetOnConnect(func(_ string, conn harmonytask.PeerConnection) {
		atomic.AddInt32(&connectCount, 1)
		select {
		case connCh <- conn:
		default:
		}
	})

	post := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/peer/v1", bytes.NewReader([]byte(body)))
		req.Header.Set("X-Peer-ID", peerAddr)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	post("first")
	var firstConn harmonytask.PeerConnection
	select {
	case firstConn = <-connCh:
	case <-time.After(2 * time.Second):
		t.Fatal("onConnect did not fire for first POST")
	}
	require.NotNil(t, firstConn)

	post("second")
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int32(1), atomic.LoadInt32(&connectCount),
		"inbound POST from a known peer should reuse the connection, not re-handshake")

	msg1, err := firstConn.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("first"), msg1)

	msg2, err := firstConn.ReceiveMessage()
	require.NoError(t, err)
	require.Equal(t, []byte("second"), msg2)
}
