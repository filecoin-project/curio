package chainapi

import (
	"context"
	"math/big"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/filecoin-project/go-state-types/proof"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-libipfs/blocks"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/filecoin-project/lotus/journal/alerting"

	"github.com/filecoin-project/curio/deps/types"
	minertypes "github.com/filecoin-project/go-state-types/builtin/v9/miner"

	minertypes13 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	verifregtypes9 "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	p2pnetwork "github.com/libp2p/go-libp2p/core/network"
)

// Daemon is a subset of the Filecoin API that is supported by Forest.
type Daemon interface {
	Common
	Net
	ChainHead(context.Context) (*types.TipSet, error)
	ChainNotify(context.Context) (<-chan []*HeadChange, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (MinerInfo, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error)
	StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error)
	GasEstimateMessageGas(ctx context.Context, msg *types.Message, spec *MessageSendSpec, tsk types.TipSetKey) (*types.Message, error)
	WalletBalance(ctx context.Context, addr address.Address) (big.Int, error)
	MpoolGetNonce(context.Context, address.Address) (uint64, error)
	MpoolPush(context.Context, *types.SignedMessage) (cid.Cid, error)
	WalletSignMessage(context.Context, address.Address, *types.Message) (*types.SignedMessage, error)
	ChainGetTipSet(context.Context, types.TipSetKey) (*types.TipSet, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
	ChainGetTipSetAfterHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	StateMinerPartitions(context.Context, address.Address, uint64, types.TipSetKey) ([]Partition, error)
	StateGetRandomnessFromBeacon(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	StateMinerSectors(context.Context, address.Address, *bitfield.BitField, types.TipSetKey) ([]*SectorOnChainInfo, error)
	WalletHas(context.Context, address.Address) (bool, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateGetRandomnessFromTickets(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	GasEstimateFeeCap(context.Context, *types.Message, int64, types.TipSetKey) (types.BigInt, error)
	GasEstimateGasPremium(_ context.Context, nblocksincl uint64, sender address.Address, gaslimit int64, tsk types.TipSetKey) (types.BigInt, error)
	ChainTipSetWeight(context.Context, types.TipSetKey) (types.BigInt, error)
	StateGetBeaconEntry(context.Context, abi.ChainEpoch) (*types.BeaconEntry, error)
	SyncSubmitBlock(context.Context, *types.BlockMsg) error
	MinerGetBaseInfo(context.Context, address.Address, abi.ChainEpoch, types.TipSetKey) (*MiningBaseInfo, error)
	MinerCreateBlock(context.Context, *BlockTemplate) (*types.BlockMsg, error)
	MpoolSelect(context.Context, types.TipSetKey, float64) ([]*types.SignedMessage, error)
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)
	StateSectorPreCommitInfo(context.Context, address.Address, abi.SectorNumber, types.TipSetKey) (*SectorPreCommitOnChainInfo, error)
	StateSectorGetInfo(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*SectorOnChainInfo, error)
	StateMinerPreCommitDepositForPower(context.Context, address.Address, SectorPreCommitInfo, types.TipSetKey) (big.Int, error)
	StateMinerInitialPledgeCollateral(context.Context, address.Address, SectorPreCommitInfo, types.TipSetKey) (big.Int, error)
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes9.AllocationId, tsk types.TipSetKey) (*verifregtypes9.Allocation, error)
	StateGetAllocationIdForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (verifregtypes9.AllocationId, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	ChainGetTipSetByHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	StateSearchMsg(ctx context.Context, from types.TipSetKey, msg cid.Cid, limit abi.ChainEpoch, allowReplaced bool) (*MsgLookup, error)
	ChainGetMessage(ctx context.Context, mc cid.Cid) (*types.Message, error)
	StateMinerAllocated(ctx context.Context, a address.Address, key types.TipSetKey) (*bitfield.BitField, error)
	StateGetAllocationForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (*verifregtypes9.Allocation, error)
	ChainReadObj(context.Context, cid.Cid) ([]byte, error)
	ChainHasObj(context.Context, cid.Cid) (bool, error)
	ChainPutObj(context.Context, blocks.Block) error

	// Added afterward
	MpoolPushMessage(ctx context.Context, msg *types.Message, spec *MessageSendSpec) (*types.SignedMessage, error)
	StateMinerActiveSectors(ctx context.Context, maddr address.Address, tsk types.TipSetKey) ([]*SectorOnChainInfo, error)
	StateSectorPartition(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*SectorLocation, error)
}

type Common interface {
	// MethodGroup: Auth

	AuthVerify(ctx context.Context, token string) ([]auth.Permission, error) //perm:read
	AuthNew(ctx context.Context, perms []auth.Permission) ([]byte, error)    //perm:admin

	// MethodGroup: Log

	LogList(context.Context) ([]string, error)         //perm:write
	LogSetLevel(context.Context, string, string) error //perm:write

	// LogAlerts returns list of all, active and inactive alerts tracked by the
	// node
	LogAlerts(ctx context.Context) ([]alerting.Alert, error) //perm:admin

	// MethodGroup: Common

	// Version provides information about API provider
	Version(context.Context) (APIVersion, error) //perm:read

	// Discover returns an OpenRPC document describing an RPC API.
	Discover(ctx context.Context) (OpenRPCDocument, error) //perm:read

	// trigger graceful shutdown
	Shutdown(context.Context) error //perm:admin

	// StartTime returns node start time
	StartTime(context.Context) (time.Time, error) //perm:read

	// Session returns a random UUID of api provider session
	Session(context.Context) (uuid.UUID, error) //perm:read

	Closing(context.Context) (<-chan struct{}, error) //perm:read
}
type OpenRPCDocument map[string]interface{}

type Version uint32

type APIVersion struct {
	Version string

	// APIVersion is a binary encoded semver version of the remote implementing
	// this api
	//
	// See APIVersion in build/version.go
	APIVersion Version

	// TODO: git commit / os / genesis cid?

	// Seconds
	BlockDelay uint64
}

type Net interface {
	// MethodGroup: Net

	NetConnectedness(context.Context, peer.ID) (p2pnetwork.Connectedness, error) //perm:read
	NetPeers(context.Context) ([]peer.AddrInfo, error)                           //perm:read
	NetPing(context.Context, peer.ID) (time.Duration, error)                     //perm:read
	NetConnect(context.Context, peer.AddrInfo) error                             //perm:write
	NetAddrsListen(context.Context) (peer.AddrInfo, error)                       //perm:read
	NetDisconnect(context.Context, peer.ID) error                                //perm:write
	NetFindPeer(context.Context, peer.ID) (peer.AddrInfo, error)                 //perm:read
	NetPubsubScores(context.Context) ([]PubsubScore, error)                      //perm:read
	NetAutoNatStatus(context.Context) (NatInfo, error)                           //perm:read
	NetAgentVersion(ctx context.Context, p peer.ID) (string, error)              //perm:read
	NetPeerInfo(context.Context, peer.ID) (*ExtendedPeerInfo, error)             //perm:read

	// NetBandwidthStats returns statistics about the nodes total bandwidth
	// usage and current rate across all peers and protocols.
	NetBandwidthStats(ctx context.Context) (metrics.Stats, error) //perm:read

	// NetBandwidthStatsByPeer returns statistics about the nodes bandwidth
	// usage and current rate per peer
	NetBandwidthStatsByPeer(ctx context.Context) (map[string]metrics.Stats, error) //perm:read

	// NetBandwidthStatsByProtocol returns statistics about the nodes bandwidth
	// usage and current rate per protocol
	NetBandwidthStatsByProtocol(ctx context.Context) (map[protocol.ID]metrics.Stats, error) //perm:read

	// ConnectionGater API
	NetBlockAdd(ctx context.Context, acl NetBlockList) error    //perm:admin
	NetBlockRemove(ctx context.Context, acl NetBlockList) error //perm:admin
	NetBlockList(ctx context.Context) (NetBlockList, error)     //perm:read

	NetProtectAdd(ctx context.Context, acl []peer.ID) error    //perm:admin
	NetProtectRemove(ctx context.Context, acl []peer.ID) error //perm:admin
	NetProtectList(ctx context.Context) ([]peer.ID, error)     //perm:read

	// ResourceManager API
	NetStat(ctx context.Context, scope string) (NetStat, error)          //perm:read
	NetLimit(ctx context.Context, scope string) (NetLimit, error)        //perm:read
	NetSetLimit(ctx context.Context, scope string, limit NetLimit) error //perm:admin

	// ID returns peerID of libp2p node backing this API
	ID(context.Context) (peer.ID, error) //perm:read
}

type NetLimit struct {
	Memory int64 `json:",omitempty"`

	Streams, StreamsInbound, StreamsOutbound int
	Conns, ConnsInbound, ConnsOutbound       int
	FD                                       int
}

type NetStat struct {
	System    *p2pnetwork.ScopeStat           `json:",omitempty"`
	Transient *p2pnetwork.ScopeStat           `json:",omitempty"`
	Services  map[string]p2pnetwork.ScopeStat `json:",omitempty"`
	Protocols map[string]p2pnetwork.ScopeStat `json:",omitempty"`
	Peers     map[string]p2pnetwork.ScopeStat `json:",omitempty"`
}

type NetBlockList struct {
	Peers     []peer.ID
	IPAddrs   []string
	IPSubnets []string
}

type NatInfo struct {
	Reachability p2pnetwork.Reachability
	PublicAddrs  []string
}

type PubsubScore struct {
	ID    peer.ID
	Score *pubsub.PeerScoreSnapshot
}

type HeadChange struct {
	Type string
	Val  *types.TipSet
}

type ExtendedPeerInfo struct {
	ID          peer.ID
	Agent       string
	Addrs       []string
	Protocols   []string
	ConnMgrMeta *ConnMgrInfo
}

type ConnMgrInfo struct {
	FirstSeen time.Time
	Value     int
	Tags      map[string]int
	Conns     map[string]time.Time
}

type MinerInfo struct {
	Owner                      address.Address   // Must be an ID-address.
	Worker                     address.Address   // Must be an ID-address.
	NewWorker                  address.Address   // Must be an ID-address.
	ControlAddresses           []address.Address // Must be an ID-addresses.
	WorkerChangeEpoch          abi.ChainEpoch
	PeerId                     *peer.ID
	Multiaddrs                 []abi.Multiaddrs
	WindowPoStProofType        abi.RegisteredPoStProof
	SectorSize                 abi.SectorSize
	WindowPoStPartitionSectors uint64
	ConsensusFaultElapsed      abi.ChainEpoch
	PendingOwnerAddress        *address.Address
	Beneficiary                address.Address
	BeneficiaryTerm            *BeneficiaryTerm
	PendingBeneficiaryTerm     *PendingBeneficiaryChange
}

// MessageSendSpec contains optional fields which modify message sending behavior
type MessageSendSpec struct {
	// MaxFee specifies a cap on network fees related to this message
	MaxFee abi.TokenAmount

	// MsgUuid specifies a unique message identifier which can be used on node (or node cluster)
	// level to prevent double-sends of messages even when nonce generation is not handled by sender
	MsgUuid uuid.UUID

	// MaximizeFeeCap makes message FeeCap be based entirely on MaxFee
	MaximizeFeeCap bool
}

type Partition struct {
	AllSectors        bitfield.BitField
	FaultySectors     bitfield.BitField
	RecoveringSectors bitfield.BitField
	LiveSectors       bitfield.BitField
	ActiveSectors     bitfield.BitField
}

type MiningBaseInfo struct {
	MinerPower        types.BigInt
	NetworkPower      types.BigInt
	Sectors           []proof.ExtendedSectorInfo
	WorkerKey         address.Address
	SectorSize        abi.SectorSize
	PrevBeaconEntry   types.BeaconEntry
	BeaconEntries     []types.BeaconEntry
	EligibleForMining bool
}

type BlockTemplate struct {
	Miner            address.Address
	Parents          types.TipSetKey
	Ticket           *types.Ticket
	Eproof           *types.ElectionProof
	BeaconValues     []types.BeaconEntry
	Messages         []*types.SignedMessage
	Epoch            abi.ChainEpoch
	Timestamp        uint64
	WinningPoStProof []proof.PoStProof
}

type MsgLookup struct {
	Message   cid.Cid // Can be different than requested, in case it was replaced, but only gas values changed
	Receipt   types.MessageReceipt
	ReturnDec interface{}
	TipSet    types.TipSetKey
	Height    abi.ChainEpoch
}

type SectorOnChainInfo = minertypes13.SectorOnChainInfo
type SectorPreCommitOnChainInfo = minertypes.SectorPreCommitOnChainInfo

type SectorPreCommitInfo = minertypes.SectorPreCommitInfo

type BeneficiaryTerm = minertypes.BeneficiaryTerm

type PendingBeneficiaryChange = minertypes.PendingBeneficiaryChange

type SectorLocation struct {
	Deadline  uint64
	Partition uint64
}
