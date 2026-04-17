package harmonytask

import (
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonytask/internal/peerregistry"
	"github.com/filecoin-project/curio/harmony/harmonytask/internal/preemptbids"
)

// PeerMessage is the JSON envelope for all peer-to-peer messages.
// The protocol is simple by design: each message is a self-contained JSON
// object sent over the transport (HTTP POST in production, channel pipes
// in tests). The three-field structure keeps serialization overhead minimal;
// verb-specific fields live in taskOther (identity uses hostAndPort; preempt
// uses cost; newTask uses retries and posted; others use taskType alone).
type PeerMessage struct {
	Verb   string    `json:"verb"`
	TaskID TaskID    `json:"taskID,omitempty"`
	Other  taskOther `json:"other,omitempty"`
}

// taskOther is the only payload shape for PeerMessage.Other. Fields are
// used depending on Verb: identity (hostAndPort), newTask (taskType, retries,
// posted), started (taskType), preemptCost (taskType, cost).
type taskOther struct {
	TaskType    string        `json:"taskType,omitempty"`
	Retries     int           `json:"retries,omitempty"`
	Posted      time.Time     `json:"posted,omitempty"`
	HostAndPort string        `json:"hostAndPort,omitempty"`
	Cost        time.Duration `json:"cost,omitempty"`
}

// messageType identifies the kind of peer-to-peer message.
//   - identity: exchanged on connection to establish peer address mapping
//   - newTask: broadcast when a task is added, so peers can attempt to claim it
//   - started: broadcast when a node begins executing a task, so peers can
//     remove it from their available-tasks map and stop attempting to claim it
//   - preemptCost: time-sensitive preemption cost comparison (see preempt.go)
type messageType string

const (
	messageTypeIdentity    messageType = "identity"
	messageTypeNewTask     messageType = "newTask"
	messageTypeStarted     messageType = "started"
	messageTypePreemptCost messageType = "preemptCost"
)

// PeerConnectorInterface abstracts the transport layer for peer connections.
// Production uses HTTP POST (lib/harmony_peer_http); tests use in-memory
// channel pipes (pipetest). This abstraction lets the peering logic be
// transport-agnostic and fully testable without network I/O.
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

// peering manages the set of active peer connections and routes task-related
// messages to peers that can handle each task type.
//
// The peering layer exists to decouple the scheduler from network I/O. The
// scheduler calls TellOthers() which serializes a message and fans it out
// to relevant peers in fire-and-forget goroutines. Incoming messages from
// peers are converted into schedulerEvents and pushed to the scheduler channel.
//
// The routing table (which peer handles which task types) lives inside
// peerregistry.Registry. Its mutex and backing maps are unexported there,
// so nothing in this package can reach them without going through methods
// that lock correctly.
type peering struct {
	// --- immutable after startPeering ---
	h             *TaskEngine
	peerConnector PeerConnectorInterface

	// --- delegated concurrent state ---
	peers *peerregistry.Registry
}

// startPeering initializes the peering layer and begins connecting to all
// known nodes in the cluster. Connections happen asynchronously so New()
// doesn't block on slow/unreachable peers.
func startPeering(h *TaskEngine, peerConnector PeerConnectorInterface) *peering {
	p := &peering{
		h:             h,
		peerConnector: peerConnector,
		peers:         peerregistry.New(),
	}
	p.peerConnector.SetOnConnect(p.handlePeer)
	h.atomics.pollDuration.Store(pollRarely)
	go func() {
		var knownPeers []string
		if err := p.h.cfg.db.Select(p.h.cfg.ctx, &knownPeers, `SELECT host_and_port FROM harmony_machines`); err != nil {
			log.Warnw("failed to list peers", "error", err)
			return
		}
		for _, peer := range knownPeers {
			if peer == p.h.cfg.hostAndPort {
				continue
			}
			go func(peer string) {
				conn, err := p.peerConnector.ConnectToPeer(peer)
				if err != nil {
					log.Warnw("failed to connect to peer", "peer", peer, "error", err)
					// Fall back to more frequent DB polling since we can't
					// receive real-time events from this peer.
					h.atomics.pollDuration.Store(pollFrequently)
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
// the deferred remove() takes the peer out of all routing maps so messages
// are no longer sent to a dead connection.
func (p *peering) handlePeer(peerAddr string, conn PeerConnection) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.Warnw("error closing peer connection", "peer", peerAddr, "error", err)
		}
	}()

	log.Infow("handling peer connection", "peer", peerAddr)

	idMsg, err := marshalPeerMessage(messageTypeIdentity, 0, taskOther{HostAndPort: p.h.cfg.hostAndPort})
	if err != nil {
		log.Warnw("failed to marshal identity", "peer", peerAddr, "error", err)
		return
	}
	if err := conn.SendMessage(idMsg); err != nil {
		log.Warnw("failed to send identity to peer", "peer", peerAddr, "error", err)
		return
	}

	var machineDetails struct {
		ID    int64  `db:"id"`
		Tasks string `db:"tasks"`
	}
	err = p.h.cfg.db.QueryRow(p.h.cfg.ctx,
		`SELECT hm.id, hmd.tasks FROM harmony_machine_details hmd
		 JOIN harmony_machines hm ON hm.id = hmd.machine_id
		 WHERE hm.host_and_port = $1`, peerAddr).Scan(&machineDetails.ID, &machineDetails.Tasks)
	if err != nil {
		log.Warnw("failed to get machine details from peer", "peer", peerAddr, "error", err)
		return
	}

	tasks := strings.Split(machineDetails.Tasks, ",")
	remove := p.peers.Add(machineDetails.ID, peerAddr, conn, tasks)
	defer func() {
		remove()
		log.Infow("removed dead peer", "peer", peerAddr)
	}()

	for {
		msg, err := conn.ReceiveMessage()
		if err != nil {
			log.Debugw("peer connection closed", "peer", peerAddr, "error", err)
			return
		}

		if err := p.handlePeerMessage(peerAddr, machineDetails.ID, msg); err != nil {
			log.Warnw("error handling peer message", "peer", peerAddr, "error", err)
		}
	}
}

// handlePeerMessage decodes a JSON message from a peer and pushes the
// corresponding scheduler event. Each verb maps to a specific scheduler
// source so the event loop knows how to update its state.
func (p *peering) handlePeerMessage(peerAddr string, peerID int64, msg []byte) error {
	var envelope PeerMessage
	if err := json.Unmarshal(msg, &envelope); err != nil {
		log.Warnw("received invalid JSON from peer", "peer", peerAddr, "error", err)
		return xerrors.Errorf("invalid JSON message from peer: %w", err)
	}

	other := envelope.Other

	switch messageType(envelope.Verb) {
	case messageTypeIdentity:
		return nil
	case messageTypeNewTask:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:     envelope.TaskID,
			TaskType:   other.TaskType,
			Source:     schedulerSourcePeerNewTask,
			PeerID:     peerID,
			Retries:    other.Retries,
			PostedTime: other.Posted,
		}
	case messageTypeStarted:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   envelope.TaskID,
			TaskType: other.TaskType,
			Source:   schedulerSourcePeerStarted,
			PeerID:   peerID,
		}
	case messageTypePreemptCost:
		p.h.state.preemptBids.Deliver(int64(envelope.TaskID), preemptbids.Response{
			PeerID: peerID,
			Cost:   other.Cost,
		})
	default:
		return xerrors.Errorf("unknown message type from peer %s: %v", peerAddr, envelope)
	}
	return nil
}

// TellNewTask notifies peers of a new task including wall time for FIFO scheduling.
func (p *peering) TellNewTask(task string, tID TaskID, retries int, posted time.Time) {
	msg, err := marshalPeerMessage(messageTypeNewTask, tID, taskOther{TaskType: task, Retries: retries, Posted: posted})
	if err != nil {
		log.Errorw("failed to marshal new task message", "error", err)
		return
	}
	p.broadcast(task, msg)
}

func marshalPeerMessage(verb messageType, taskID TaskID, other taskOther) ([]byte, error) {
	return json.Marshal(PeerMessage{
		Verb:   string(verb),
		TaskID: taskID,
		Other:  other,
	})
}

// HasPeers reports whether at least one peer is connected and routing
// messages for any task type. Used by the background poller to decide
// whether to restore the poll interval to pollRarely (event-driven mode)
// or keep it at pollFrequently (polling fallback mode).
func (p *peering) HasPeers() bool {
	return p.peers.HasAny()
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
	p.broadcast(taskType, msg)
}

// broadcast hands the pre-serialized message to the peer registry, which
// fans out to each peer in its own goroutine.
func (p *peering) broadcast(taskType string, msg []byte) {
	p.peers.Broadcast(taskType, msg, func(addr string, err error) {
		log.Warnw("failed to send message to peer", "peer", addr, "error", err)
	})
}
