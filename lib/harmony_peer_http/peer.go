package harmonypeerhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

var log = logging.Logger("harmonypeerhttp")

const sendTimeout = 2 * time.Second

// PeerHTTP handles both outbound connections to peers and inbound connections
// from peers using HTTP POST. It implements harmonytask.PeerConnectorInterface.
type PeerHTTP struct {
	localAddr string // our own address for X-Peer-ID header
	onConnect func(peerAddr string, conn harmonytask.PeerConnection)

	connMu      sync.RWMutex
	connections map[string]*peerHTTPConnection
}

var _ harmonytask.PeerConnectorInterface = (*PeerHTTP)(nil)

// New creates a new PeerHTTP instance for peer-to-peer communication over HTTP.
// localAddr is this node's host:port used in the X-Peer-ID header.
func New(localAddr string) *PeerHTTP {
	return &PeerHTTP{
		localAddr:   localAddr,
		connections: make(map[string]*peerHTTPConnection),
	}
}

// SetOnConnect sets the callback for when a peer connects to this node.
func (p *PeerHTTP) SetOnConnect(onConnect func(peerAddr string, conn harmonytask.PeerConnection)) {
	p.onConnect = onConnect
}

// ServeHTTP handles incoming peer HTTP POST messages.
// Mount this at "/peer/v1" on your HTTP router.
func (p *PeerHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		log.Errorw("failed to read peer message body", "error", err)
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	peerAddr := r.Header.Get("X-Peer-ID")
	if peerAddr == "" {
		log.Warnw("peer message missing X-Peer-ID header")
		http.Error(w, "missing X-Peer-ID header", http.StatusBadRequest)
		return
	}

	log.Debugw("received peer message", "peer", peerAddr, "size", len(body))

	p.connMu.Lock()
	conn, exists := p.connections[peerAddr]
	if !exists {
		conn = &peerHTTPConnection{
			parent:   p,
			peerAddr: peerAddr,
			incoming: make(chan []byte, 100),
		}
		p.connections[peerAddr] = conn
		p.connMu.Unlock()

		log.Infow("new peer connection via HTTP", "peer", peerAddr)

		if p.onConnect != nil {
			go p.onConnect(peerAddr, conn)
		}
	} else {
		p.connMu.Unlock()
	}

	select {
	case conn.incoming <- body:
		w.WriteHeader(http.StatusOK)
	default:
		log.Warnw("peer message queue full, dropping", "peer", peerAddr)
		http.Error(w, "queue full", http.StatusServiceUnavailable)
	}
}

// ConnectToPeer establishes an HTTP-based connection to a Curio peer.
func (p *PeerHTTP) ConnectToPeer(peerID string) (harmonytask.PeerConnection, error) {
	p.connMu.Lock()
	defer p.connMu.Unlock()

	if conn, exists := p.connections[peerID]; exists {
		return conn, nil
	}

	conn := &peerHTTPConnection{
		parent:   p,
		peerAddr: peerID,
		incoming: make(chan []byte, 100),
	}
	p.connections[peerID] = conn

	log.Infow("connecting to peer via HTTP", "peer", peerID)

	return conn, nil
}

// dropPeer removes a peer connection and closes its incoming channel.
func (p *PeerHTTP) dropPeer(peerAddr string, conn *peerHTTPConnection) {
	p.connMu.Lock()
	if p.connections[peerAddr] == conn {
		delete(p.connections, peerAddr)
	}
	p.connMu.Unlock()

	conn.closeOnce.Do(func() {
		close(conn.incoming)
		log.Warnw("dropped peer", "peer", peerAddr)
	})
}

// peerHTTPConnection implements harmonytask.PeerConnection for HTTP POST.
type peerHTTPConnection struct {
	parent    *PeerHTTP
	peerAddr  string
	incoming  chan []byte
	closeOnce sync.Once
}

var _ harmonytask.PeerConnection = (*peerHTTPConnection)(nil)

// SendMessage sends a binary message to the connected peer via HTTP POST.
// If the send fails (2s timeout), the peer is dropped.
func (pc *peerHTTPConnection) SendMessage(message []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	url := fmt.Sprintf("http://%s/peer/v1", pc.peerAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(message))
	if err != nil {
		pc.parent.dropPeer(pc.peerAddr, pc)
		return fmt.Errorf("failed to create request to peer %s: %w", pc.peerAddr, err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Peer-ID", pc.parent.localAddr)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		pc.parent.dropPeer(pc.peerAddr, pc)
		return fmt.Errorf("failed to send to peer %s (dropping): %w", pc.peerAddr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		pc.parent.dropPeer(pc.peerAddr, pc)
		return fmt.Errorf("peer %s returned %d (dropping): %s", pc.peerAddr, resp.StatusCode, string(body))
	}

	return nil
}

// ReceiveMessage waits for the next message from the peer.
// Returns error when the peer is dropped (channel closed).
func (pc *peerHTTPConnection) ReceiveMessage() ([]byte, error) {
	msg, ok := <-pc.incoming
	if !ok {
		return nil, fmt.Errorf("peer %s was dropped", pc.peerAddr)
	}
	return msg, nil
}

// Close is a no-op. Peers are dropped automatically on send failure.
func (pc *peerHTTPConnection) Close() error {
	return nil
}
