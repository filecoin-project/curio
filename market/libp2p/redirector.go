package libp2p

import (
	"fmt"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"
	"net/http"
)

// Redirector struct with a database connection
type Redirector struct {
	db *harmonydb.DB // Replace with your actual DB wrapper if different
}

// NewRedirector creates a new Redirector with a database connection
func NewRedirector(db *harmonydb.DB) *Redirector {
	return &Redirector{db: db}
}

// Router sets up the route for the WebSocket connection
func Router(mux *chi.Mux, rp *Redirector) {
	mux.Get("/libp2p", rp.handleLibp2pWebsocket)
}

// handleLibp2pWebsocket proxies the WebSocket connection to the local_listen address
func (rp *Redirector) handleLibp2pWebsocket(w http.ResponseWriter, r *http.Request) {
	// Fetch the local_listen address from the database
	var localListen string
	err := rp.db.QueryRow(r.Context(), "SELECT local_listen FROM libp2p").Scan(&localListen)
	if err != nil {
		if err == pgx.ErrNoRows {
			http.Error(w, "Remote LibP2P host undefined", http.StatusBadGateway)
			return
		}

		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Infof("Database error: %v", err)
		return
	}

	// Parse the multiaddress to get the WebSocket URL
	wsURL, err := multiAddrToWsURL(localListen)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		log.Infof("Error parsing multiaddress: %v", err)
		return
	}

	// Upgrade the client connection to a WebSocket connection
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Infof("WebSocket upgrade error: %v", err)
		return
	}
	defer clientConn.Close()

	// Connect to the target WebSocket server
	targetConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		log.Infof("Error connecting to target WebSocket server: %v", err)
		return
	}
	defer targetConn.Close()

	// Proxy data between clientConn and targetConn
	errc := make(chan error, 2)

	// Start goroutines for bidirectional copying
	go proxyWebSocket(clientConn, targetConn, errc)
	go proxyWebSocket(targetConn, clientConn, errc)

	// Wait for one of the connections to error out
	if err := <-errc; err != nil {
		log.Infof("WebSocket proxy error: %v", err)
	}
}

// proxyWebSocket copies messages from src to dst WebSocket connections
func proxyWebSocket(src, dst *websocket.Conn, errc chan error) {
	for {
		messageType, message, err := src.ReadMessage()
		if err != nil {
			errc <- err
			break
		}
		err = dst.WriteMessage(messageType, message)
		if err != nil {
			errc <- err
			break
		}
	}
}

// multiAddrToWsURL converts a multiaddress to a WebSocket URL
func multiAddrToWsURL(maddrStr string) (string, error) {
	maddr, err := ma.NewMultiaddr(maddrStr)
	if err != nil {
		return "", xerrors.Errorf("error parsing multiaddress: %w", err)
	}

	var (
		host        string
		port        string
		isWebSocket bool
		isSecure    bool
	)

	for _, p := range maddr.Protocols() {
		switch p.Code {
		case ma.P_IP4, ma.P_IP6, ma.P_DNS4, ma.P_DNS6, ma.P_DNS:
			hostValue, err := maddr.ValueForProtocol(p.Code)
			if err != nil {
				return "", xerrors.Errorf("error getting host value: %w", err)
			}
			host = hostValue
		case ma.P_TCP:
			portValue, err := maddr.ValueForProtocol(ma.P_TCP)
			if err != nil {
				return "", xerrors.Errorf("error getting port value: %w", err)
			}
			port = portValue
		case ma.P_WS:
			isWebSocket = true
		case ma.P_WSS:
			isWebSocket = true
			isSecure = true
		default:
			// Unsupported protocol
		}
	}

	if !isWebSocket {
		return "", xerrors.Errorf("multiaddress does not contain websocket protocol")
	}

	scheme := "ws"
	if isSecure {
		scheme = "wss"
	}

	wsURL := fmt.Sprintf("%s://%s:%s/", scheme, host, port)
	return wsURL, nil
}
