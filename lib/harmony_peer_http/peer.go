// Package harmonypeerhttp implements the HTTP transport for harmonytask's
// peer-to-peer messaging protocol. Each Curio node runs this as an HTTP
// handler mounted at /peer/v1. When a peer sends a task event (new task,
// reservation, or start notification), it arrives as an HTTP POST with
// the sender's address in the X-Peer-ID header.
//
// Design choices:
//   - Stateless HTTP POST (not WebSocket) keeps the transport simple and
//     compatible with standard HTTP infrastructure (load balancers, proxies).
//   - Each message is a single POST; there is no long-lived connection.
//     The "connection" abstraction is maintained in-memory: the first POST
//     from a new peer triggers OnConnect, creating a peerHTTPConnection that
//     feeds subsequent POSTs into a buffered channel.
//   - Failed sends immediately drop the peer (no retries). This is intentional:
//     the DB poll fallback ensures correctness, and retrying would add latency
//     to the scheduler's fire-and-forget send path.
//   - The 2-second send timeout prevents a slow peer from blocking the sender.
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

// PeerHTTP is both an HTTP handler (for receiving peer messages) and a
// PeerConnectorInterface implementation (for sending messages to peers).
// It maintains an in-memory registry of peer connections, each backed by
// a buffered channel that decouples HTTP request handling from the peering
// layer's receive loop.
type PeerHTTP struct {
	localAddr string // this node's address, sent in X-Peer-ID header
	onConnect func(peerAddr string, conn harmonytask.PeerConnection)

	connMu      sync.RWMutex // connections and onConnect
	connections map[string]*peerHTTPConnection
}

var _ harmonytask.PeerConnectorInterface = (*PeerHTTP)(nil)

// New creates a PeerHTTP instance. localAddr is this node's host:port,
// used to identify ourselves in the X-Peer-ID header on outbound messages.
func New(localAddr string) *PeerHTTP {
	return &PeerHTTP{
		localAddr:   localAddr,
		connections: make(map[string]*peerHTTPConnection),
	}
}

// SetOnConnect registers the callback that the harmonytask peering layer
// provides. It is called when the first message arrives from a new peer,
// establishing the bidirectional "connection" for that peer.
func (p *PeerHTTP) SetOnConnect(onConnect func(peerAddr string, conn harmonytask.PeerConnection)) {
	p.connMu.Lock()
	p.onConnect = onConnect
	p.connMu.Unlock()
}

// ServeHTTP handles incoming peer HTTP POST messages. Mount this at
// "/peer/v1" on the node's HTTP router.
//
// Message flow:
//  1. Extract X-Peer-ID header to identify the sender.
//  2. Look up the existing peerHTTPConnection for this sender, or create one
//     if this is the first message from this peer.
//  3. Push the message body into the connection's buffered channel.
//  4. Only when the connection is newly created, trigger OnConnect which
//     starts the peering layer's handlePeer goroutine for it. Subsequent POSTs
//     from the same peer reuse the connection and do NOT re-run the handshake —
//     otherwise every message would spawn a new handlePeer goroutine (which
//     sends an identity message back and runs DB queries), producing an
//     unbounded identity ping-pong and goroutine/DB-query storm between peers.
//
// The 100-slot buffer prevents a burst of messages from blocking HTTP
// responses. If the buffer is full, the message is dropped with 503 —
// the DB poll fallback ensures eventual consistency.
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
	defer func() { _ = r.Body.Close() }()

	peerAddr := r.Header.Get("X-Peer-ID")
	if peerAddr == "" {
		log.Warnw("peer message missing X-Peer-ID header")
		http.Error(w, "missing X-Peer-ID header", http.StatusBadRequest)
		return
	}

	log.Debugw("received peer message", "peer", peerAddr, "size", len(body))

	// Look up or create the connection and snapshot onConnect, then release
	// the lock before delivery so we never hold the shared lock on the channel.
	p.connMu.Lock()
	conn, exists := p.connections[peerAddr]
	if !exists {
		conn = &peerHTTPConnection{
			parent:   p,
			peerAddr: peerAddr,
			incoming: make(chan []byte, 100),
			done:     make(chan struct{}),
		}
		p.connections[peerAddr] = conn
	}
	onConnect := p.onConnect
	p.connMu.Unlock()

	// Only fire OnConnect for a freshly-created connection; reusing an existing
	// connection must not re-run the peering handshake.
	if !exists && onConnect != nil {
		log.Infow("new peer connection via HTTP", "peer", peerAddr)
		go onConnect(peerAddr, conn)
	}

	// Deliver without holding connMu. incoming is never closed (drops are
	// signalled via done), so the non-blocking send can neither block on a
	// gone-away peer nor panic on a closed channel.
	select {
	case conn.incoming <- body:
		w.WriteHeader(http.StatusOK)
	default:
		log.Warnw("peer message queue full, dropping", "peer", peerAddr)
		http.Error(w, "queue full", http.StatusServiceUnavailable)
	}
}

// ConnectToPeer creates or returns an existing connection to a peer.
// Unlike the test pipe implementation, HTTP connections are lazy: no
// actual network call happens here, so this method does not surface dial
// failures (harmonytask falls back to pollFrequently on handshake/send
// errors in handlePeer instead). The first SendMessage will make the HTTP
// POST, and the remote's ServeHTTP will trigger OnConnect.
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
		done:     make(chan struct{}),
	}
	p.connections[peerID] = conn

	log.Infow("connecting to peer via HTTP", "peer", peerID)

	return conn, nil
}

// dropPeer removes a peer from the connection registry and signals its
// done channel, which causes the peering layer's receive loop to exit and
// trigger cleanup of routing tables.
func (p *PeerHTTP) dropPeer(peerAddr string, conn *peerHTTPConnection) {
	p.connMu.Lock()
	if p.connections[peerAddr] == conn {
		delete(p.connections, peerAddr)
	}
	p.connMu.Unlock()

	// Signal closure via done rather than closing incoming, so a concurrent
	// ServeHTTP send (done without holding connMu) can never panic on a closed
	// channel. Buffered-but-unread messages are simply abandoned; the DB poll
	// fallback covers them.
	conn.closeOnce.Do(func() {
		close(conn.done)
	})

	log.Warnw("dropped peer", "peer", peerAddr)
}

// peerHTTPConnection implements harmonytask.PeerConnection for HTTP POST.
// Outbound messages are sent as individual HTTP POSTs. Inbound messages
// arrive via ServeHTTP and are buffered in the incoming channel, which
// the peering layer's receive loop reads from via ReceiveMessage. incoming
// is never closed; a drop is signalled by closing done.
type peerHTTPConnection struct {
	parent    *PeerHTTP
	peerAddr  string
	incoming  chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

var _ harmonytask.PeerConnection = (*peerHTTPConnection)(nil)

// SendMessage sends a binary message to the peer via HTTP POST. On any
// failure (timeout, non-200 response), the peer is immediately dropped.
// This fail-fast behavior keeps the scheduler's send path non-blocking;
// the DB poll fallback handles correctness for unreachable peers.
func (pc *peerHTTPConnection) SendMessage(message []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	url := fmt.Sprintf("http://%s/peer/v1", pc.peerAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(message))
	if err != nil {
		pc.parent.dropPeer(pc.peerAddr, pc)
		return fmt.Errorf("failed to create request to peer %s: %w", pc.peerAddr, err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Peer-ID", pc.parent.localAddr)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		pc.parent.dropPeer(pc.peerAddr, pc)
		return fmt.Errorf("failed to send to peer %s (dropping): %w", pc.peerAddr, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		pc.parent.dropPeer(pc.peerAddr, pc)
		return fmt.Errorf("peer %s returned %d (dropping): %s", pc.peerAddr, resp.StatusCode, string(body))
	}

	return nil
}

// ReceiveMessage blocks until the next message arrives from the peer or the
// connection is dropped. Buffered messages are drained before a drop is
// reported. Returns an error when the peer is dropped (done closed), which
// signals the peering layer to remove this peer from routing tables.
func (pc *peerHTTPConnection) ReceiveMessage() ([]byte, error) {
	select {
	case msg := <-pc.incoming:
		return msg, nil
	case <-pc.done:
		// Drain any message that beat the drop signal before reporting closure.
		select {
		case msg := <-pc.incoming:
			return msg, nil
		default:
			return nil, fmt.Errorf("peer %s was dropped", pc.peerAddr)
		}
	}
}

// Close is a no-op for HTTP connections. Peers are dropped automatically
// on send failure via dropPeer, which signals the done channel and triggers
// cleanup in the peering layer.
func (pc *peerHTTPConnection) Close() error {
	return nil
}
