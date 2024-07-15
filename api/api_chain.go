package api

import (
	"context"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	verifregtypes "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

// CurioChainRPC is a subset of the Filecoin API that is supported by Forest.
type CurioChainRPC interface {
	Version(p0 context.Context) (api.APIVersion, error)
	ChainHead(context.Context) (*types.TipSet, error)
	ChainNotify(context.Context) (<-chan []*api.HeadChange, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateNetworkVersion(context.Context, types.TipSetKey) (network.Version, error)
	StateAccountKey(ctx context.Context, addr address.Address, tsk types.TipSetKey) (address.Address, error)
	GasEstimateMessageGas(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec, tsk types.TipSetKey) (*types.Message, error)
	WalletBalance(ctx context.Context, addr address.Address) (big.Int, error)
	MpoolGetNonce(context.Context, address.Address) (uint64, error)
	MpoolPush(context.Context, *types.SignedMessage) (cid.Cid, error)
	WalletSignMessage(context.Context, address.Address, *types.Message) (*types.SignedMessage, error)
	ChainGetTipSet(context.Context, types.TipSetKey) (*types.TipSet, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
	ChainGetTipSetAfterHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	StateMinerPartitions(context.Context, address.Address, uint64, types.TipSetKey) ([]api.Partition, error)
	StateGetRandomnessFromBeacon(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	StateMinerSectors(context.Context, address.Address, *bitfield.BitField, types.TipSetKey) ([]*miner.SectorOnChainInfo, error)
	WalletHas(context.Context, address.Address) (bool, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateGetRandomnessFromTickets(ctx context.Context, personalization crypto.DomainSeparationTag, randEpoch abi.ChainEpoch, entropy []byte, tsk types.TipSetKey) (abi.Randomness, error)
	GasEstimateFeeCap(context.Context, *types.Message, int64, types.TipSetKey) (types.BigInt, error)
	GasEstimateGasPremium(_ context.Context, nblocksincl uint64, sender address.Address, gaslimit int64, tsk types.TipSetKey) (types.BigInt, error)
	ChainTipSetWeight(context.Context, types.TipSetKey) (types.BigInt, error)
	StateGetBeaconEntry(context.Context, abi.ChainEpoch) (*types.BeaconEntry, error)
	SyncSubmitBlock(context.Context, *types.BlockMsg) error
	MinerGetBaseInfo(context.Context, address.Address, abi.ChainEpoch, types.TipSetKey) (*api.MiningBaseInfo, error)
	MinerCreateBlock(context.Context, *api.BlockTemplate) (*types.BlockMsg, error)
	MpoolSelect(context.Context, types.TipSetKey, float64) ([]*types.SignedMessage, error)
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)
	StateSectorPreCommitInfo(context.Context, address.Address, abi.SectorNumber, types.TipSetKey) (*miner.SectorPreCommitOnChainInfo, error)
	StateSectorGetInfo(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*miner.SectorOnChainInfo, error)
	StateMinerPreCommitDepositForPower(context.Context, address.Address, miner.SectorPreCommitInfo, types.TipSetKey) (big.Int, error)
	StateMinerInitialPledgeCollateral(context.Context, address.Address, miner.SectorPreCommitInfo, types.TipSetKey) (big.Int, error)
	StateMinerPower(context.Context, address.Address, types.TipSetKey) (*api.MinerPower, error)    //perm:read
	StateMinerDeadlines(context.Context, address.Address, types.TipSetKey) ([]api.Deadline, error) //perm:read
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes.AllocationId, tsk types.TipSetKey) (*verifregtypes.Allocation, error)
	StateGetAllocationIdForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (verifregtypes.AllocationId, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	ChainGetTipSetByHeight(context.Context, abi.ChainEpoch, types.TipSetKey) (*types.TipSet, error)
	StateSearchMsg(ctx context.Context, from types.TipSetKey, msg cid.Cid, limit abi.ChainEpoch, allowReplaced bool) (*api.MsgLookup, error)
	ChainGetMessage(ctx context.Context, mc cid.Cid) (*types.Message, error)
	StateMinerAllocated(ctx context.Context, a address.Address, key types.TipSetKey) (*bitfield.BitField, error)
	StateGetAllocationForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (*verifregtypes.Allocation, error)
	ChainReadObj(context.Context, cid.Cid) ([]byte, error)
	ChainHasObj(context.Context, cid.Cid) (bool, error)
	ChainPutObj(context.Context, blocks.Block) error

	// Added afterward
	MpoolPushMessage(ctx context.Context, msg *types.Message, spec *api.MessageSendSpec) (*types.SignedMessage, error)
	StateMinerActiveSectors(ctx context.Context, maddr address.Address, tsk types.TipSetKey) ([]*miner.SectorOnChainInfo, error)
	StateSectorPartition(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*miner.SectorLocation, error)
	StateMinerAvailableBalance(context.Context, address.Address, types.TipSetKey) (big.Int, error)
}

var _ CurioChainRPC = api.FullNode(nil)
