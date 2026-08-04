package harmonytask

import (
	"fmt"
	"sync"
)

// This file provides in-memory PeerConnectorInterface / PeerConnection
// implementations for integration testing. Production code should not
// depend on these.

// newPipePair creates two linked PeerConnections for in-memory testing.
// Send on one end delivers to ReceiveMessage on the other.
func newPipePair() (PeerConnection, PeerConnection) {
	ch1 := make(chan []byte, 100)
	ch2 := make(chan []byte, 100)
	a := &testPipeConn{sendCh: ch1, recvCh: ch2}
	b := &testPipeConn{sendCh: ch2, recvCh: ch1}
	return a, b
}

type testPipeConn struct {
	sendCh    chan []byte
	recvCh    chan []byte
	closeOnce sync.Once
	closed    bool
}

func (c *testPipeConn) SendMessage(msg []byte) error {
	if c.closed {
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

func (c *testPipeConn) ReceiveMessage() ([]byte, error) {
	msg, ok := <-c.recvCh
	if !ok {
		return nil, fmt.Errorf("pipe closed")
	}
	return msg, nil
}

func (c *testPipeConn) Close() error {
	c.closed = true
	c.closeOnce.Do(func() { close(c.sendCh) })
	return nil
}

// PipeNetwork is an in-memory peer network for testing.
type PipeNetwork struct {
	mu    sync.Mutex
	nodes map[string]*PipeNode
}

// NewPipeNetwork creates a new in-memory peer network.
func NewPipeNetwork() *PipeNetwork {
	return &PipeNetwork{nodes: make(map[string]*PipeNode)}
}

// NewNode registers a node in the pipe network.
func (n *PipeNetwork) NewNode(addr string) *PipeNode {
	node := &PipeNode{addr: addr, net: n}
	n.mu.Lock()
	n.nodes[addr] = node
	n.mu.Unlock()
	return node
}

// PipeNode implements PeerConnectorInterface over the in-memory pipe network.
type PipeNode struct {
	addr      string
	net       *PipeNetwork
	onConnect func(string, PeerConnection)
}

var _ PeerConnectorInterface = (*PipeNode)(nil)

// ConnectToPeer creates a pipe pair and triggers the remote node's onConnect.
func (n *PipeNode) ConnectToPeer(peerAddr string) (PeerConnection, error) {
	n.net.mu.Lock()
	remote, ok := n.net.nodes[peerAddr]
	n.net.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown peer: %s", peerAddr)
	}
	local, remoteEnd := newPipePair()
	if remote.onConnect != nil {
		go remote.onConnect(n.addr, remoteEnd)
	}
	return local, nil
}

// SetOnConnect sets the callback for incoming peer connections.
func (n *PipeNode) SetOnConnect(fn func(string, PeerConnection)) {
	n.onConnect = fn
}
