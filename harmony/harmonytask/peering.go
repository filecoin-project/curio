package harmonytask

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/xerrors"
)

type PeerConnectorInterface interface {
	ConnectToPeer(peerID string) (PeerConnection, error)
	SetOnConnect(onConnect func(peerAddr string, conn PeerConnection))
}

type PeerConnection interface {
	SendMessage(message []byte) error
	ReceiveMessage() ([]byte, error)
	Close() error
}

type peer struct {
	addr  string
	conn  PeerConnection
	tasks map[string]bool
}

// peering is the main struct that manages the peering process.
// Capital functions are for taskEngine to call.
type peering struct {
	h             *TaskEngine
	mustPollFast  atomic.Bool
	peerConnector PeerConnectorInterface

	peersLock sync.RWMutex // protects peers and m
	peers     []peer
	m         map[string][]int // task name -> peer indexes that can run it
}

func startPeering(h *TaskEngine, peerConnector PeerConnectorInterface) *peering {
	p := &peering{
		h:             h,
		peerConnector: peerConnector,
		m:             make(map[string][]int),
	}
	p.peerConnector.SetOnConnect(p.handlePeer)
	go func() {
		var knownPeers []string
		bPollRequired := atomic.Bool{}
		p.h.db.Select(p.h.ctx, &knownPeers, `SELECT host_and_port FROM harmony_machines`)
		for _, peer := range knownPeers {
			go func(peer string) {
				conn, err := p.peerConnector.ConnectToPeer(peer)
				if err != nil {
					log.Warnw("failed to connect to peer", "peer", peer, "error", err)
					bPollRequired.Store(true)
				}
				p.handlePeer(peer, conn)
			}(peer)
		}
	}()
	return p
}

// handlePeer is called when a remote peer connects to this node.
// It reads messages from the peer and processes them (e.g., task notifications).
func (p *peering) handlePeer(peerAddr string, conn PeerConnection) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Warnw("error closing peer connection", "peer", peerAddr, "error", err)
		}
	}()

	log.Infow("handling peer connection", "peer", peerAddr)

	// Send our identity to the peer. Get theirs.
	if err := conn.SendMessage([]byte("i:" + p.h.hostAndPort)); err != nil {
		log.Warnw("failed to send identity to peer", "peer", peerAddr, "error", err)
		return
	}
	msg, err := conn.ReceiveMessage()
	if err != nil {
		log.Warnw("failed to receive message from peer", "peer", peerAddr, "error", err)
		return
	}
	if !bytes.HasPrefix(msg, []byte("i:")) {
		log.Warnw("received unexpected message from peer", "peer", peerAddr, "message", string(msg))
		return
	}
	them := peer{
		addr:  strings.TrimPrefix(string(msg), "i:"),
		conn:  conn,
		tasks: make(map[string]bool),
	}
	var tasks string
	p.h.db.Select(p.h.ctx, &tasks, `SELECT tasks FROM harmony_machine_details WHERE machine_id = $1`, them.addr)
	p.peersLock.Lock()
	p.peers = append(p.peers, them)
	themIndex := len(p.peers) - 1
	for _, task := range strings.Split(tasks, ",") {
		them.tasks[task] = true
		p.m[task] = append(p.m[task], themIndex)
	}
	p.peersLock.Unlock()

	for {
		msg, err := conn.ReceiveMessage()
		if err != nil {
			log.Debugw("peer connection closed", "peer", peerAddr, "error", err)
			return
		}

		// Process incoming message from peer
		p.handlePeerMessage(peerAddr, msg)
	}
}

// handlePeerMessage processes a message received from a peer.
// Override this method or extend it to handle specific message types.
func (p *peering) handlePeerMessage(peerAddr string, msg []byte) error {
	if !bytes.HasPrefix(msg, []byte("t:")) {
		parts := bytes.SplitN(msg, []byte(":"), 2)
		if len(parts) != 2 {
			log.Warnw("received invalid message from peer", "peer", peerAddr, "message", string(msg))
			return xerrors.Errorf("invalid message from peer")
		}
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   TaskID(binary.BigEndian.Uint64(parts[1])),
			TaskType: string(parts[0]),
			Source:   schedulerSourcePeer,
		}
		return nil
	}
	return xerrors.Errorf("invalid message from peer")
}

func (p *peering) TellOthers(task string, tID TaskID) {
	p.peersLock.RLock()
	msg := binary.BigEndian.AppendUint64([]byte(fmt.Sprintf("t:%s:", task)), uint64(tID))
	for _, peerIndex := range p.m[task] {
		conn := p.peers[peerIndex].conn
		go func() {
			err := conn.SendMessage(msg)
			if err != nil {
				log.Warnw("failed to send message to peer", "peer", p.peers[peerIndex].addr, "error", err)
			}
		}()
	}
	p.peersLock.RUnlock()
}
