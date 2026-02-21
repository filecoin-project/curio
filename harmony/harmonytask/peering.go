package harmonytask

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/xerrors"
)

// PeerMessage is the JSON envelope for all peer-to-peer messages.
// Verb identifies the message kind; TaskID is set for task-related verbs;
// Other carries verb-specific payload decoded lazily by the receiver.
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

type messageType string

const (
	messageTypeIdentity messageType = "identity" // sent to alert other nodes we exist.
	messageTypeNewTask  messageType = "newTask"  // sent when a task is available to run.
	messageTypeReserve  messageType = "reserve"  // FUTURE: unclear
	messageTypeStarted  messageType = "started"  // sent when a task is started.
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
	id    int64
	addr  string
	conn  PeerConnection
	tasks map[string]bool
}

// peering is the main struct that manages the peering process.
// Capital functions are for taskEngine to call.
type peering struct {
	h             *TaskEngine
	peerConnector PeerConnectorInterface

	peersLock sync.RWMutex // protects peers and m
	peers     []peer
	m         map[string][]int // task name -> peer indexes that can run it
}

// startPeering builds a relationship between all nodes in the network as they
// do not know we exist yet.
func startPeering(h *TaskEngine, peerConnector PeerConnectorInterface) *peering {
	p := &peering{
		h:             h,
		peerConnector: peerConnector,
		m:             make(map[string][]int),
	}
	p.peerConnector.SetOnConnect(p.handlePeer)
	go func() {
		var knownPeers []string
		if err := p.h.db.Select(p.h.ctx, &knownPeers, `SELECT host_and_port FROM harmony_machines`); err != nil {
			log.Warnw("failed to list peers", "error", err)
			return
		}
		for _, peer := range knownPeers {
			if peer == p.h.hostAndPort {
				continue // skip self
			}
			go func(peer string) {
				conn, err := p.peerConnector.ConnectToPeer(peer)
				if err != nil {
					log.Warnw("failed to connect to peer", "peer", peer, "error", err)
					h.pollDuration.Store(POLL_FREQUENTLY)
					return
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

	// Announce ourselves so the remote peer knows we exist.
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
	p.peersLock.Lock()
	p.peers = append(p.peers, them)
	themIndex := len(p.peers) - 1
	for _, task := range strings.Split(machineDetails.Tasks, ",") {
		them.tasks[task] = true
		p.m[task] = append(p.m[task], themIndex)
	}
	p.peersLock.Unlock()

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

// handlePeerMessage processes a JSON message received from a peer.
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
	default:
		return xerrors.Errorf("unknown message verb from peer %s: %q", peerAddr, envelope.Verb)
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

func (p *peering) TellOthers(verb messageType, taskType string, tID TaskID) {
	msg, err := marshalPeerMessage(verb, tID, taskOther{TaskType: taskType})
	if err != nil {
		log.Errorw("failed to marshal peer message", "verb", verb, "error", err)
		return
	}
	p.tellOtherBytes(taskType, msg)
}

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
