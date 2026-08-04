// Package pipetest provides in-memory PeerConnectorInterface / PeerConnection
// implementations for integration testing. These allow the full scheduler +
// peering stack to be tested without real HTTP connections or network latency,
// while exercising the same code paths as production.
//
// Architecture:
//
//	PipeNetwork — a registry of named PipeNodes (one per simulated machine)
//	PipeNode    — implements PeerConnectorInterface using channel-based pipes
//	pipeConn    — implements PeerConnection over paired Go channels
//
// Production code should not depend on this package.
package pipetest

import (
	"fmt"
	"sync"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

// newPipePair creates two linked PeerConnections backed by Go channels.
// Send on one end delivers to ReceiveMessage on the other, enabling
// bidirectional communication without network I/O.
func newPipePair() (harmonytask.PeerConnection, harmonytask.PeerConnection) {
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
	closed    bool
}

func (c *pipeConn) SendMessage(msg []byte) error {
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

func (c *pipeConn) ReceiveMessage() ([]byte, error) {
	msg, ok := <-c.recvCh
	if !ok {
		return nil, fmt.Errorf("pipe closed")
	}
	return msg, nil
}

func (c *pipeConn) Close() error {
	c.closed = true
	c.closeOnce.Do(func() { close(c.sendCh) })
	return nil
}

// PipeNetwork simulates a cluster of nodes for integration testing.
// Each node is registered by address and can connect to any other node
// in the network, producing channel-backed PeerConnections that behave
// identically to HTTP connections from the peering layer's perspective.
type PipeNetwork struct {
	mu    sync.Mutex
	nodes map[string]*PipeNode
}

func NewPipeNetwork() *PipeNetwork {
	return &PipeNetwork{nodes: make(map[string]*PipeNode)}
}

// NewNode registers a simulated machine in the pipe network. The returned
// PipeNode can be passed to harmonytask.New as the PeerConnectorInterface.
func (n *PipeNetwork) NewNode(addr string) *PipeNode {
	node := &PipeNode{addr: addr, net: n}
	n.mu.Lock()
	n.nodes[addr] = node
	n.mu.Unlock()
	return node
}

// PipeNode implements PeerConnectorInterface over the in-memory pipe network.
// ConnectToPeer creates a pipe pair and triggers the remote node's OnConnect
// callback, mimicking the bidirectional connection setup of real HTTP peering.
type PipeNode struct {
	addr      string
	net       *PipeNetwork
	onConnect func(string, harmonytask.PeerConnection)
}

var _ harmonytask.PeerConnectorInterface = (*PipeNode)(nil)

// ConnectToPeer creates a pipe pair and triggers the remote node's onConnect.
func (n *PipeNode) ConnectToPeer(peerAddr string) (harmonytask.PeerConnection, error) {
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
func (n *PipeNode) SetOnConnect(fn func(string, harmonytask.PeerConnection)) {
	n.onConnect = fn
}
