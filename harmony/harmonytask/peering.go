package harmonytask

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/xerrors"
)

// PeerMessage is the JSON envelope for all peer-to-peer messages.
// The protocol is simple by design: each message is a self-contained JSON
// object sent over the transport (HTTP POST in production, channel pipes
// in tests). The three-field structure keeps serialization overhead minimal
// while being extensible via the Other field for verb-specific payloads.
type PeerMessage struct {
	Verb   string          `json:"verb"`
	TaskID TaskID          `json:"taskID,omitempty"`
	Other  json.RawMessage `json:"other,omitempty"`
}

type identityOther struct {
	HostAndPort string `json:"hostAndPort"`
}

type taskOther struct {
	TaskType string `json:"taskType"`
	Retries  int    `json:"retries,omitempty"`
}

// messageType identifies the kind of peer-to-peer message.
// The protocol supports three task-related verbs plus identity handshake:
//   - identity: exchanged on connection to establish peer address mapping
//   - newTask: broadcast when a task is added, so peers can attempt to claim it
//   - reserve: broadcast when a node holds resources for a task (incomplete protocol)
//   - started: broadcast when a node begins executing a task, so peers can
//     remove it from their available-tasks map and stop attempting to claim it
type messageType string

const (
	messageTypeIdentity    messageType = "identity"
	messageTypeNewTask     messageType = "newTask"
	messageTypeReserve     messageType = "reserve"
	messageTypeStarted     messageType = "started"
	messageTypePreemptCost messageType = "preemptCost"
)

// PeerConnectorInterface abstracts the transport layer for peer connections.
// Production uses HTTP POST (lib/harmony_peer_http); tests use in-memory
// channel pipes (test_support.go). This abstraction lets the peering logic
// be transport-agnostic and fully testable without network I/O.
type PeerConnectorInterface interface {
	ConnectToPeer(peerID string) (PeerConnection, error)
	SetOnConnect(onConnect func(peerAddr string, conn PeerConnection))
}

// PeerConnection represents a bidirectional communication channel with
// a single peer. Messages are opaque byte slices (JSON-encoded PeerMessage).
type PeerConnection interface {
	SendMessage(message []byte) error
	ReceiveMessage() ([]byte, error)
	Close() error
}

type peer struct {
	id   int64
	addr string
	conn PeerConnection
	// peering is the main struct that manages the peering process.
	// Capital functions are for taskEngine to call.
	tasks map[string]bool // task types this peer can handle
}

// peering manages the set of active peer connections and routes task-related
// messages to peers that can handle each task type.
//
// The peering layer exists to decouple the scheduler from network I/O. The
// scheduler calls TellOthers() which serializes a message and fans it out
// to relevant peers in fire-and-forget goroutines. Incoming messages from
// peers are converted into schedulerEvents and pushed to the scheduler channel.
//
// Message types: newTask (work available), reserve (resource hold — protocol
// incomplete, see doc.go), started (task claimed and running).
//
// Peer lifecycle:
//  1. On startup, startPeering queries harmony_machines for known peers
//     and connects to each one (skipping self).
//  2. On each connection, handlePeer sends an identity message, queries the
//     DB for the peer's task capabilities, registers it in the peers/m maps,
//     and enters a receive loop.
//  3. When a peer disconnects (ReceiveMessage returns error), the deferred
//     cleanup removes it from all task-type routing maps.
//  4. If a peer connection fails at startup, the poll interval is shortened
//     (POLL_FREQUENTLY) to compensate for missing event propagation. It
//     self-heals back to POLL_RARELY once peering is restored.
type peering struct {
	h             *TaskEngine
	peerConnector PeerConnectorInterface

	peersLock sync.RWMutex // protects the peers and m (map of task type -> indexes into peers slice).
	peers     []peer
	m         map[string][]int // task type name -> indexes into peers slice
}

// startPeering initializes the peering layer and begins connecting to all
// known nodes in the cluster. Connections happen asynchronously so New()
// doesn't block on slow/unreachable peers.
func startPeering(h *TaskEngine, peerConnector PeerConnectorInterface) *peering {
	p := &peering{
		h:             h,
		peerConnector: peerConnector,
		m:             make(map[string][]int),
	}
	p.peerConnector.SetOnConnect(p.handlePeer)
	h.pollDuration.Store(POLL_RARELY)
	go func() {
		var knownPeers []string
		if err := p.h.db.Select(p.h.ctx, &knownPeers, `SELECT host_and_port FROM harmony_machines`); err != nil {
			log.Warnw("failed to list peers", "error", err)
			return
		}
		for _, peer := range knownPeers {
			if peer == p.h.hostAndPort {
				continue // skip self — no need to peer with ourselves
			}
			go func(peer string) {
				conn, err := p.peerConnector.ConnectToPeer(peer)
				if err != nil {
					log.Warnw("failed to connect to peer", "peer", peer, "error", err)
					// Fall back to more frequent DB polling since we can't
					// receive real-time events from this peer.
					h.pollDuration.Store(POLL_FREQUENTLY)
					return
				}
				p.handlePeer(peer, conn)
			}(peer)
		}
	}()
	return p
}

// handlePeer manages the lifecycle of a single peer connection. It is called
// both for outbound connections (we connected to them) and inbound connections
// (they connected to us via the PeerConnector's OnConnect callback).
//
// The handshake sends our identity so the remote peer can map our address.
// We then query the DB for the peer's registered task types to build the
// routing table (which peers should receive messages about which task types).
//
// The receive loop converts incoming JSON messages into scheduler events,
// enabling real-time cross-node coordination. When the connection drops,
// deferred cleanup removes the peer from all routing maps so messages are
// no longer sent to a dead connection.
func (p *peering) handlePeer(peerAddr string, conn PeerConnection) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Warnw("error closing peer connection", "peer", peerAddr, "error", err)
		}
	}()

	log.Infow("handling peer connection", "peer", peerAddr)

	// Send identity so the remote peer knows our address.
	idMsg, err := marshalPeerMessage(messageTypeIdentity, 0, identityOther{HostAndPort: p.h.hostAndPort})
	if err != nil {
		log.Warnw("failed to marshal identity", "peer", peerAddr, "error", err)
		return
	}
	if err := conn.SendMessage(idMsg); err != nil {
		log.Warnw("failed to send identity to peer", "peer", peerAddr, "error", err)
		return
	}

	them := peer{
		addr:  peerAddr,
		conn:  conn,
		tasks: make(map[string]bool),
	}

	// Look up the peer's machine ID and registered task types from the DB.
	// This determines which messages we route to this peer.
	var machineDetails struct {
		ID    int64  `db:"id"`
		Tasks string `db:"tasks"`
	}
	err = p.h.db.QueryRow(p.h.ctx,
		`SELECT hm.id, hmd.tasks FROM harmony_machine_details hmd
		 JOIN harmony_machines hm ON hm.id = hmd.machine_id
		 WHERE hm.host_and_port = $1`, them.addr).Scan(&machineDetails.ID, &machineDetails.Tasks)
	if err != nil {
		log.Warnw("failed to get machine details from peer", "peer", peerAddr, "error", err)
		return
	}
	them.id = machineDetails.ID

	// Register peer in routing maps. After this, TellOthers will include
	// this peer for any task type it handles.
	p.peersLock.Lock()
	p.peers = append(p.peers, them)
	themIndex := len(p.peers) - 1
	for _, task := range strings.Split(machineDetails.Tasks, ",") {
		them.tasks[task] = true
		p.m[task] = append(p.m[task], themIndex)
	}
	p.peersLock.Unlock()

	// Deferred cleanup: remove this peer from all routing maps on disconnect.
	// This prevents sending messages to a dead connection and ensures the
	// routing table stays accurate.
	defer func() {
		p.peersLock.Lock()
		for taskName, indexes := range p.m {
			filtered := indexes[:0]
			for _, idx := range indexes {
				if idx != themIndex {
					filtered = append(filtered, idx)
				}
			}
			if len(filtered) == 0 {
				delete(p.m, taskName)
			} else {
				p.m[taskName] = filtered
			}
		}
		p.peersLock.Unlock()
		log.Infow("removed dead peer", "peer", peerAddr)
	}()

	// Receive loop: convert incoming messages to scheduler events.
	for {
		msg, err := conn.ReceiveMessage()
		if err != nil {
			log.Debugw("peer connection closed", "peer", peerAddr, "error", err)
			return
		}

		if err := p.handlePeerMessage(peerAddr, them, msg); err != nil {
			log.Warnw("error handling peer message", "peer", peerAddr, "error", err)
		}
	}
}

// handlePeerMessage decodes a JSON message from a peer and pushes the
// corresponding scheduler event. Each verb maps to a specific scheduler
// source so the event loop knows how to update its state.
func (p *peering) handlePeerMessage(peerAddr string, them peer, msg []byte) error {
	var envelope PeerMessage
	if err := json.Unmarshal(msg, &envelope); err != nil {
		log.Warnw("received invalid JSON from peer", "peer", peerAddr, "error", err)
		return xerrors.Errorf("invalid JSON message from peer: %w", err)
	}

	var other taskOther
	if len(envelope.Other) > 0 {
		if err := json.Unmarshal(envelope.Other, &other); err != nil {
			return xerrors.Errorf("failed to unmarshal task other from peer %s: %w", peerAddr, err)
		}
	}

	switch messageType(envelope.Verb) {
	case messageTypeNewTask:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   envelope.TaskID,
			TaskType: other.TaskType,
			Source:   schedulerSourcePeerNewTask,
			PeerID:   them.id,
			Retries:  other.Retries,
		}
	case messageTypeReserve:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   envelope.TaskID,
			TaskType: other.TaskType,
			Source:   schedulerSourcePeerReserved,
			PeerID:   them.id,
		}
	case messageTypeStarted:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   envelope.TaskID,
			TaskType: other.TaskType,
			Source:   schedulerSourcePeerStarted,
			PeerID:   them.id,
		}
	case messageTypePreemptCost:
		var other messagePreemptCostOther
		if err := json.Unmarshal(envelope.Other, &other); err != nil {
			return xerrors.Errorf("failed to unmarshal preempt cost other from peer %s: %w", peerAddr, err)
		}

		resp := preemptCostResponse{PeerID: them.id, Cost: other.Cost}
		taskID := envelope.TaskID
		p.h.preemptCostMu.Lock()
		ch := p.h.preemptCostChs[taskID]
		if ch != nil {
			select {
			case ch <- resp:
			default:
			}
		} else {
			if p.h.preemptCostPending == nil {
				p.h.preemptCostPending = make(map[TaskID][]preemptCostResponse)
			}
			p.h.preemptCostPending[taskID] = append(p.h.preemptCostPending[taskID], resp)
		}
		p.h.preemptCostMu.Unlock()
	default:
		return xerrors.Errorf("unknown message type from peer %s: %v", peerAddr, envelope)
	}
	return nil
}

func marshalPeerMessage(verb messageType, taskID TaskID, other any) ([]byte, error) {
	otherBytes, err := json.Marshal(other)
	if err != nil {
		return nil, fmt.Errorf("marshal other: %w", err)
	}
	return json.Marshal(PeerMessage{
		Verb:   string(verb),
		TaskID: taskID,
		Other:  otherBytes,
	})
}

// HasPeers reports whether at least one peer is connected and routing
// messages for any task type. Used by the background poller to decide
// whether to restore the poll interval to POLL_RARELY (event-driven mode)
// or keep it at POLL_FREQUENTLY (polling fallback mode).
func (p *peering) HasPeers() bool {
	p.peersLock.RLock()
	defer p.peersLock.RUnlock()
	return len(p.m) > 0
}

// TellOthers broadcasts a task event to all peers that handle the given task
// type. Messages are sent in fire-and-forget goroutines so the scheduler
// thread never blocks on network I/O. If a send fails, the peer is dropped
// by the transport layer and removed from routing on the next receive error.
func (p *peering) TellOthers(verb messageType, taskType string, tID TaskID) {
	msg, err := marshalPeerMessage(verb, tID, taskOther{TaskType: taskType})
	if err != nil {
		log.Errorw("failed to marshal peer message", "verb", verb, "error", err)
		return
	}
	p.tellOtherBytes(taskType, msg)
}

// tellOtherBytes fans out a pre-serialized message to all peers registered
// for the given task type. Each send runs in its own goroutine to avoid
// blocking on slow peers or network delays.
func (p *peering) tellOtherBytes(taskType string, msg []byte) {
	p.peersLock.RLock()
	for _, peerIndex := range p.m[taskType] {
		conn := p.peers[peerIndex].conn
		go func() {
			if err := conn.SendMessage(msg); err != nil {
				log.Warnw("failed to send message to peer", "peer", p.peers[peerIndex].addr, "error", err)
			}
		}()
	}
	p.peersLock.RUnlock()
}
