package harmonypeerrpc

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log/v2"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

var log = logging.Logger("harmonypeerrpc")

// PeerRPC handles both outbound connections to peers and inbound connections
// from peers. It implements harmonytask.PeerConnectorInterface.
type PeerRPC struct {
	// onConnect is called when a peer connects to us
	onConnect func(peerAddr string, conn harmonytask.PeerConnection)

	upgrader websocket.Upgrader
}

var _ harmonytask.PeerConnectorInterface = (*PeerRPC)(nil)

// New creates a new PeerRPC instance for peer-to-peer communication.
func New() *PeerRPC {
	return &PeerRPC{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin (peers)
			},
		},
	}
}

// SetOnConnect sets the callback for when a peer connects to this node.
// This should be called before starting the HTTP server.
func (p *PeerRPC) SetOnConnect(onConnect func(peerAddr string, conn harmonytask.PeerConnection)) {
	p.onConnect = onConnect
}

// ServeHTTP handles incoming peer WebSocket connections.
// Mount this at "/peer/v0" on your HTTP router.
func (p *PeerRPC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorw("failed to upgrade peer connection", "error", err)
		return
	}

	peerAddr := r.RemoteAddr
	log.Infow("peer connected", "peer", peerAddr)

	pc := &peerConnection{
		conn:   conn,
		peerID: peerAddr,
	}

	// Handle the connection in a goroutine so ServeHTTP can return
	if p.onConnect != nil {
		go p.onConnect(peerAddr, pc)
	}
}

// ConnectToPeer establishes a bidirectional WebSocket connection to a Curio peer
// at the given host:port address.
func (p *PeerRPC) ConnectToPeer(peerID string) (harmonytask.PeerConnection, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}

	conn, _, err := dialer.Dial(fmt.Sprintf("ws://%s/peer/v0", peerID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to peer %s: %w", peerID, err)
	}

	log.Infow("connected to peer", "peer", peerID)

	return &peerConnection{
		conn:   conn,
		peerID: peerID,
	}, nil
}

// peerConnection implements harmonytask.PeerConnection for bidirectional
// communication over a WebSocket connection.
type peerConnection struct {
	conn   *websocket.Conn
	peerID string

	writeMu sync.Mutex // protects concurrent writes to the WebSocket
}

var _ harmonytask.PeerConnection = (*peerConnection)(nil)

// SendMessage sends a binary message to the connected peer.
// This method is safe for concurrent use.
func (pc *peerConnection) SendMessage(message []byte) error {
	pc.writeMu.Lock()
	defer pc.writeMu.Unlock()

	if err := pc.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
		return fmt.Errorf("failed to send message to peer %s: %w", pc.peerID, err)
	}
	return nil
}

// ReceiveMessage waits for and returns the next message from the connected peer.
// This method blocks until a message is received or an error occurs.
func (pc *peerConnection) ReceiveMessage() ([]byte, error) {
	_, message, err := pc.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("failed to receive message from peer %s: %w", pc.peerID, err)
	}
	return message, nil
}

// Close closes the WebSocket connection to the peer.
func (pc *peerConnection) Close() error {
	log.Debugw("closing peer connection", "peer", pc.peerID)

	// Send a close message to the peer for graceful shutdown
	err := pc.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		log.Warnw("failed to send close message", "peer", pc.peerID, "error", err)
	}

	return pc.conn.Close()
}
