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
	h.pollDuration.Store(constPollRarely)
	go func() {
		var knownPeers []string
		p.h.db.Select(p.h.ctx, &knownPeers, `SELECT host_and_port FROM harmony_machines`)
		for _, peer := range knownPeers {
			go func(peer string) {
				conn, err := p.peerConnector.ConnectToPeer(peer)
				if err != nil {
					log.Warnw("failed to connect to peer", "peer", peer, "error", err)
					h.pollDuration.Store(constPollFrequently)
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
	err = p.h.db.QueryRow(p.h.ctx, `SELECT id, tasks FROM harmony_machine_details WHERE machine_id = $1`, them.addr).Scan(&sql.ID, &sql.Tasks)
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
	if bytes.HasPrefix(msg, []byte{byte(messageTypeNewTask), ':'}) {
		parts := bytes.SplitN(msg, []byte(":"), 2)
		if len(parts) != 2 {
			log.Warnw("received invalid message from peer", "peer", peerAddr, "message", string(msg))
			return xerrors.Errorf("invalid message from peer")
		}
		taskID := TaskID(binary.BigEndian.Uint64(parts[1]))
		reties := binary.BigEndian.Uint16(parts[1][8:])
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   taskID,
			TaskType: string(parts[0]),
			Source:   schedulerSourcePeerNewTask,
			PeerID:   them.id,
			Retries:  int(reties),
		}
		return nil
	}
	if bytes.HasPrefix(msg, []byte{byte(messageTypeReserve), ':'}) {
		parts := bytes.SplitN(msg, []byte(":"), 2)
		if len(parts) != 2 {
			log.Warnw("received invalid message from peer", "peer", peerAddr, "message", string(msg))
			return xerrors.Errorf("invalid message from peer")
		}
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   TaskID(binary.BigEndian.Uint64(parts[1])),
			TaskType: string(parts[0]),
			Source:   schedulerSourcePeerReserved,
			PeerID:   them.id,
		}
		return nil
	}
	if bytes.HasPrefix(msg, []byte{byte(messageTypeStarted), ':'}) {
		parts := bytes.SplitN(msg, []byte(":"), 2)
		if len(parts) != 2 {
			log.Warnw("received invalid message from peer", "peer", peerAddr, "message", string(msg))
			return xerrors.Errorf("invalid message from peer")
		}
		p.h.schedulerChannel <- schedulerEvent{
			TaskID:   TaskID(binary.BigEndian.Uint64(parts[1])),
			TaskType: string(parts[0]),
			Source:   schedulerSourcePeerStarted,
			PeerID:   them.id,
		}
		return nil
	}
	return xerrors.Errorf("invalid message from peer")
}

type messageType byte

const (
	messageTypeReserve messageType = 'r'
	messageTypeNewTask messageType = 't'
	messageTypeStarted messageType = 's'
)

func (p *peering) TellOthers(messagetype messageType, task string, tID TaskID) {
	p.TellOthersMessage(task, messageRenderTaskSend{TaskType: task, TaskID: tID})
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
