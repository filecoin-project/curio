package harmonytask

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"

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
		p.h.db.Select(p.h.ctx, &knownPeers, `SELECT host_and_port FROM harmony_machines`)
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
	var sql struct {
		ID    int64  `db:"id"`
		Tasks string `db:"tasks"`
	}
	err = p.h.db.QueryRow(p.h.ctx,
		`SELECT hm.id, hmd.tasks FROM harmony_machine_details hmd
		 JOIN harmony_machines hm ON hm.id = hmd.machine_id
		 WHERE hm.host_and_port = $1`, them.addr).Scan(&sql.ID, &sql.Tasks)
	if err != nil {
		log.Warnw("failed to get machine details from peer", "peer", peerAddr, "error", err)
		return
	}
	them.id = sql.ID
	p.peersLock.Lock()
	p.peers = append(p.peers, them)
	themIndex := len(p.peers) - 1
	for _, task := range strings.Split(sql.Tasks, ",") {
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

		// Process incoming message from peer
		p.handlePeerMessage(peerAddr, them, msg)
	}
}

// handlePeerMessage processes a message received from a peer.
// Override this method or extend it to handle specific message types.
func (p *peering) handlePeerMessage(peerAddr string, them peer, msg []byte) error {
	// Wire format: <type_byte>:<task_name>:<binary_payload>
	// Split into 3 parts so the binary payload (which may contain ':') stays intact.
	parts := bytes.SplitN(msg, []byte(":"), 3)
	if len(parts) < 3 || len(parts[2]) < 8 {
		log.Warnw("received invalid message from peer", "peer", peerAddr, "message_len", len(msg))
		return xerrors.Errorf("invalid message from peer: need 3 parts with >=8 byte payload")
	}

	taskType := string(parts[1])
	taskID := TaskID(binary.BigEndian.Uint64(parts[2][:8]))

	switch messageType(parts[0][0]) {
	case messageTypeNewTask:
		var retries int
		if len(parts[2]) >= 10 {
			retries = int(binary.BigEndian.Uint16(parts[2][8:10]))
		}
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   taskID,
			TaskType: taskType,
			Source:   schedulerSourcePeerNewTask,
			PeerID:   them.id,
			Retries:  retries,
		}
	case messageTypeReserve:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   taskID,
			TaskType: taskType,
			Source:   schedulerSourcePeerReserved,
			PeerID:   them.id,
		}
	case messageTypeStarted:
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   taskID,
			TaskType: taskType,
			Source:   schedulerSourcePeerStarted,
			PeerID:   them.id,
		}
	default:
		return xerrors.Errorf("unknown message type from peer %s: %c", peerAddr, parts[0][0])
	}
	return nil
}

type messageType byte

const (
	messageTypeReserve messageType = 'r'
	messageTypeNewTask messageType = 't'
	messageTypeStarted messageType = 's'
)

func (p *peering) TellOthers(messagetype messageType, task string, tID TaskID) {
	p.TellOthersMessage(task, messageRenderTaskSend{MessageType: messagetype, TaskType: task, TaskID: tID})
}

func (p *peering) TellOthersMessage(taskType string, r messageRenderer) {
	p.peersLock.RLock()
	for _, peerIndex := range p.m[taskType] {
		conn := p.peers[peerIndex].conn
		go func() {
			err := conn.SendMessage(r.Render())
			if err != nil {
				log.Warnw("failed to send message to peer", "peer", p.peers[peerIndex].addr, "error", err)
			}
		}()
	}
	p.peersLock.RUnlock()
}

type messageRenderer interface {
	Render() []byte
}

type messageRenderNewTask struct {
	TaskType string
	TaskID   TaskID
	Retries  int
}

func (m messageRenderNewTask) Render() []byte {
	return binary.BigEndian.AppendUint16(
		binary.BigEndian.AppendUint64(
			[]byte(fmt.Sprintf("%c:%s:", messageTypeNewTask, m.TaskType)), uint64(m.TaskID)), uint16(m.Retries))
}

type messageRenderTaskSend struct {
	MessageType messageType
	TaskType    string
	TaskID      TaskID
}

func (m messageRenderTaskSend) Render() []byte {
	return binary.BigEndian.AppendUint64([]byte(fmt.Sprintf("%c:%s:", m.MessageType, m.TaskType)), uint64(m.TaskID))
}
